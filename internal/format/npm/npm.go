// Package npm implements the npm registry protocol: packuments (package
// documents), tarballs, publish, dist-tags, login/whoami, search and audit
// pass-through, for hosted, proxy and group repositories.
package npm

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/holiaokho/holiaokho/internal/auth"
	"github.com/holiaokho/holiaokho/internal/content"
	"github.com/holiaokho/holiaokho/internal/format"
	"github.com/holiaokho/holiaokho/internal/logx"
	"github.com/holiaokho/holiaokho/internal/model"
	"github.com/holiaokho/holiaokho/internal/repo"
)

const Name = "npm"

const challenge = `Basic realm="Holiaokho npm"`

// nameRe is the npm package name grammar (optionally scoped).
var nameRe = regexp.MustCompile(`^(@[a-z0-9-~][a-z0-9-._~]*/)?[a-z0-9-~][a-z0-9-._~]*$`)

type Format struct{}

func (Format) Name() string { return Name }

func (Format) ValidateAttributes(r *model.Repository, attrs map[string]json.RawMessage) error {
	return nil
}

func (Format) VersionLess(a, b string) bool { return semverLess(a, b) }

func (Format) Parse(p string) *model.Package {
	name, rest := splitName(p)
	if name == "" || len(rest) != 2 || rest[0] != "-" {
		return nil
	}
	return packageFor(name, versionFromFile(name, rest[1]))
}

func packageFor(name, version string) *model.Package {
	if version == "" {
		return nil
	}
	ns, short := "", name
	if i := strings.IndexByte(name, '/'); i > 0 {
		ns, short = name[:i], name[i+1:]
	}
	attrs, _ := json.Marshal(map[string]any{"name": name})
	return &model.Package{Namespace: ns, Name: short, Version: version, Attrs: attrs}
}

// versionFromFile derives the version from "<short>-<version>.tgz".
func versionFromFile(name, file string) string {
	short := name
	if i := strings.IndexByte(name, '/'); i >= 0 {
		short = name[i+1:]
	}
	f := strings.TrimSuffix(file, ".tgz")
	if strings.HasPrefix(f, short+"-") {
		return f[len(short)+1:]
	}
	return ""
}

// splitName splits "/@scope/name/rest..." or "/name/rest..." into the
// package name and the remaining segments.
func splitName(p string) (string, []string) {
	p = strings.Trim(p, "/")
	if p == "" {
		return "", nil
	}
	segs := strings.Split(p, "/")
	if strings.HasPrefix(segs[0], "@") {
		if len(segs) < 2 {
			return "", nil
		}
		return segs[0] + "/" + segs[1], segs[2:]
	}
	return segs[0], segs[1:]
}

type handler struct {
	repo *model.Repository
	d    format.Deps
}

// IsDeploy keeps npm's registry-level writes with the group they were sent
// to: `npm login` (PUT -/user/...) and `npm audit` (POST -/npm/v1/security/...).
// Publishes, unpublishes and dist-tag changes go to the hosted member.
func (Format) IsDeploy(r *http.Request) bool {
	p := strings.TrimPrefix(r.URL.Path, "/")
	return !strings.HasPrefix(p, "-/user/") && !strings.HasPrefix(p, "-/npm/v1/") && !strings.HasPrefix(p, "-/v1/")
}

func (Format) Handler(r *model.Repository, d format.Deps) http.Handler {
	return &handler{repo: r, d: d}
}

func (h *handler) base(r *http.Request) string {
	return h.d.BaseURL(r) + "/repository/" + h.repo.Name
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, "/")
	// Registry-level endpoints.
	switch {
	case p == "-/ping":
		writeJSON(w, 200, map[string]any{})
		return
	case p == "-/whoami":
		pr := auth.PrincipalFrom(r.Context())
		if pr == nil || pr.Anonymous {
			w.Header().Set("WWW-Authenticate", challenge)
			writeJSON(w, 401, map[string]any{"error": "authentication required"})
			return
		}
		writeJSON(w, 200, map[string]any{"username": pr.Username})
		return
	case strings.HasPrefix(p, "-/user/org.couchdb.user:") && r.Method == http.MethodPut:
		h.login(w, r, strings.TrimPrefix(p, "-/user/org.couchdb.user:"))
		return
	case strings.HasPrefix(p, "-/user/token/") && r.Method == http.MethodDelete:
		w.WriteHeader(http.StatusOK)
		return
	case p == "-/v1/search":
		if !format.Authorize(w, r, h.repo, auth.Read, challenge) {
			return
		}
		h.search(w, r)
		return
	case strings.HasPrefix(p, "-/npm/v1/security/") || strings.HasPrefix(p, "-/npm/v1/advisories"):
		if !format.Authorize(w, r, h.repo, auth.Read, challenge) {
			return
		}
		h.passthrough(w, r, p)
		return
	case strings.HasPrefix(p, "-/package/") && strings.Contains(p, "/dist-tags"):
		h.distTags(w, r, strings.TrimPrefix(p, "-/package/"))
		return
	}

	name, rest := splitName(p)
	if name == "" || !nameRe.MatchString(name) {
		writeJSON(w, 404, map[string]any{"error": "not found"})
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		if !format.Authorize(w, r, h.repo, auth.Read, challenge) {
			return
		}
		switch {
		case len(rest) == 0:
			h.packument(w, r, name)
		case rest[0] == "-" && len(rest) == 2:
			h.tarball(w, r, name, rest[1])
		case len(rest) == 1:
			h.versionDoc(w, r, name, rest[0])
		default:
			writeJSON(w, 404, map[string]any{"error": "not found"})
		}
	case http.MethodPut:
		if !format.Authorize(w, r, h.repo, auth.Write, challenge) {
			return
		}
		if len(rest) == 0 || (len(rest) == 1 && strings.HasPrefix(rest[0], "-rev")) || (len(rest) == 2 && rest[0] == "-rev") {
			h.publish(w, r, name)
			return
		}
		writeJSON(w, 400, map[string]any{"error": "unsupported"})
	case http.MethodDelete:
		if !format.Authorize(w, r, h.repo, auth.Delete, challenge) {
			return
		}
		h.unpublish(w, r, name, rest)
	default:
		writeJSON(w, 405, map[string]any{"error": "method not allowed"})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// ------------------------------------------------------------ packuments

// readPackument fetches the raw stored/proxied packument of one repository.
func (h *handler) readPackument(r *http.Request, rp *model.Repository, name string) (map[string]any, time.Time, error) {
	pol := repo.Policy{Kind: repo.Metadata, ContentType: "application/json", UpstreamPath: upstreamName(name)}
	b, res, err := h.d.Engine.ReadAll(r.Context(), rp, name, pol, 64<<20)
	if err != nil {
		return nil, time.Time{}, err
	}
	doc, err := decodePackument(b)
	if err != nil {
		return nil, time.Time{}, err
	}
	var t time.Time
	if res.Asset != nil {
		t = res.Asset.UpdatedAt
	}
	return doc, t, nil
}

// decodePackument decodes a packument but leaves each version object as raw
// bytes. A popular package has thousands of versions, each a deep object, and
// decoding them into map[string]any turns a few MB of JSON into hundreds of MB
// of heap — which is what pushed a real deployment into the OOM killer. Nothing
// here reads inside a version except the dist.tarball rewrite, which unpacks
// just the one version it is touching.
func decodePackument(b []byte) (map[string]any, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("%w: bad packument", repo.ErrUpstream)
	}
	out := make(map[string]any, len(doc))
	for k, raw := range doc {
		if k == "versions" {
			var vs map[string]json.RawMessage
			if err := json.Unmarshal(raw, &vs); err != nil {
				return nil, fmt.Errorf("%w: bad packument versions", repo.ErrUpstream)
			}
			out[k] = vs
			continue
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, fmt.Errorf("%w: bad packument", repo.ErrUpstream)
		}
		out[k] = v
	}
	return out, nil
}

// rawVersions normalises the "versions" value, which is raw when it came from
// decodePackument and decoded when a caller (or a test) built it by hand.
func rawVersions(v any) map[string]json.RawMessage {
	switch m := v.(type) {
	case map[string]json.RawMessage:
		return m
	case map[string]any:
		out := make(map[string]json.RawMessage, len(m))
		for k, vv := range m {
			b, err := json.Marshal(vv)
			if err != nil {
				continue
			}
			out[k] = b
		}
		return out
	}
	return nil
}

// upstreamName encodes a scoped name for the upstream URL (@scope%2Fname).
func upstreamName(name string) string {
	if strings.HasPrefix(name, "@") {
		return strings.Replace(name, "/", "%2F", 1)
	}
	return name
}

// mergedPackument returns the packument for this repository, merging group
// members when needed.
func (h *handler) mergedPackument(r *http.Request, name string) (map[string]any, time.Time, error) {
	if h.repo.Type != model.Group {
		return h.readPackument(r, h.repo, name)
	}
	members, _ := h.d.Engine.Members(r.Context(), h.repo)
	var docs []map[string]any
	var latest time.Time
	for _, m := range members {
		mh := &handler{repo: m, d: h.d}
		doc, t, err := mh.mergedPackument(r, name)
		if err != nil {
			if !errors.Is(err, repo.ErrNotFound) && !errors.Is(err, repo.ErrBlocked) {
				if logx.Disconnected(err) {
					h.d.Log.Debug("npm group member cancelled", "member", m.Name, "name", name)
				} else {
					h.d.Log.Warn("npm group member", "member", m.Name, "name", name, "err", err)
				}
			}
			continue
		}
		if t.After(latest) {
			latest = t
		}
		docs = append(docs, doc)
	}
	if len(docs) == 0 {
		return nil, time.Time{}, repo.ErrNotFound
	}
	return MergePackuments(docs), latest, nil
}

// MergePackuments unions versions and dist-tags; earlier documents win.
func MergePackuments(docs []map[string]any) map[string]any {
	out := map[string]any{}
	versions := map[string]json.RawMessage{}
	tags := map[string]any{}
	times := map[string]any{}
	for _, d := range docs {
		for k, v := range d {
			switch k {
			case "versions", "dist-tags", "time", "_attachments":
				continue
			}
			if _, ok := out[k]; !ok {
				out[k] = v
			}
		}
		for ver, v := range rawVersions(d["versions"]) {
			if _, ok := versions[ver]; !ok {
				versions[ver] = v
			}
		}
		if ts, ok := d["dist-tags"].(map[string]any); ok {
			for tag, v := range ts {
				if _, ok := tags[tag]; !ok {
					tags[tag] = v
				}
			}
		}
		if tm, ok := d["time"].(map[string]any); ok {
			for k, v := range tm {
				if _, ok := times[k]; !ok {
					times[k] = v
				}
			}
		}
	}
	out["versions"] = versions
	out["dist-tags"] = tags
	if len(times) > 0 {
		out["time"] = times
	}
	if _, ok := tags["latest"]; !ok && len(versions) > 0 {
		var vers []string
		for v := range versions {
			vers = append(vers, v)
		}
		sort.Slice(vers, func(i, j int) bool { return semverLess(vers[i], vers[j]) })
		tags["latest"] = vers[len(vers)-1]
	}
	return out
}

// rewriteTarballs points every dist.tarball at this repository.
func (h *handler) rewriteTarballs(r *http.Request, name string, doc map[string]any) {
	base := h.base(r) + "/" + name + "/-/"
	vs := rawVersions(doc["versions"])
	for ver, raw := range vs {
		patched, ok := rewriteVersionTarball(raw, base)
		if ok {
			vs[ver] = patched
		}
	}
	doc["versions"] = vs
}

// rewriteVersionTarball repoints dist.tarball without decoding the rest of the
// version: only the top level and the small dist object are unpacked, so
// dependencies, scripts and the like stay as bytes.
func rewriteVersionTarball(raw json.RawMessage, base string) (json.RawMessage, bool) {
	var v map[string]json.RawMessage
	if json.Unmarshal(raw, &v) != nil {
		return nil, false
	}
	rawDist, ok := v["dist"]
	if !ok {
		return nil, false
	}
	var dist map[string]any
	if json.Unmarshal(rawDist, &dist) != nil {
		return nil, false
	}
	tb, _ := dist["tarball"].(string)
	if tb == "" {
		return nil, false
	}
	dist["tarball"] = base + path.Base(tb)
	nd, err := json.Marshal(dist)
	if err != nil {
		return nil, false
	}
	v["dist"] = nd
	nv, err := json.Marshal(v)
	if err != nil {
		return nil, false
	}
	return nv, true
}

func (h *handler) packument(w http.ResponseWriter, r *http.Request, name string) {
	doc, t, err := h.mergedPackument(r, name)
	if err != nil {
		mapErr(w, err, h.d)
		return
	}
	h.rewriteTarballs(r, name, doc)
	b, _ := json.Marshal(doc)
	format.ServeBytes(w, r, "application/json", b, t)
}

func (h *handler) versionDoc(w http.ResponseWriter, r *http.Request, name, version string) {
	doc, t, err := h.mergedPackument(r, name)
	if err != nil {
		mapErr(w, err, h.d)
		return
	}
	h.rewriteTarballs(r, name, doc)
	vs := rawVersions(doc["versions"])
	v, ok := vs[version]
	if !ok {
		if tags, _ := doc["dist-tags"].(map[string]any); tags != nil {
			if tv, ok := tags[version].(string); ok {
				v = vs[tv]
			}
		}
	}
	if len(v) == 0 {
		writeJSON(w, 404, map[string]any{"error": "version not found"})
		return
	}
	// Already JSON: serve the stored bytes instead of re-encoding.
	format.ServeBytes(w, r, "application/json", v, t)
}

// -------------------------------------------------------------- tarballs

func (h *handler) tarball(w http.ResponseWriter, r *http.Request, name, file string) {
	p := name + "/-/" + file
	pol := repo.Policy{Kind: repo.Content, Immutable: true, ContentType: "application/octet-stream", Package: packageFor(name, versionFromFile(name, file))}
	format.FetchAndServe(w, r, h.d, h.repo, p, pol)
}

// --------------------------------------------------------------- publish

func (h *handler) publish(w http.ResponseWriter, r *http.Request, name string) {
	if h.repo.Type != model.Hosted {
		writeJSON(w, 400, map[string]any{"error": "only hosted repositories accept publishes"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 512<<20))
	if err != nil {
		writeJSON(w, 400, map[string]any{"error": "read body"})
		return
	}
	var in map[string]any
	if err := json.Unmarshal(body, &in); err != nil {
		writeJSON(w, 400, map[string]any{"error": "invalid json"})
		return
	}
	if n, _ := in["name"].(string); n != "" && n != name {
		writeJSON(w, 400, map[string]any{"error": "name mismatch"})
		return
	}
	ctx := r.Context()
	// Existing document (if any).
	existing := map[string]any{}
	if a, err := h.d.Content.Asset(ctx, h.repo.ID, name); err == nil && a.BlobDigest != nil {
		if b, _, err := h.d.Engine.ReadAll(ctx, h.repo, name, repo.Policy{}, 64<<20); err == nil {
			json.Unmarshal(b, &existing)
		}
	}
	exVersions, _ := existing["versions"].(map[string]any)
	if exVersions == nil {
		exVersions = map[string]any{}
	}
	newVersions, _ := in["versions"].(map[string]any)
	attachments, _ := in["_attachments"].(map[string]any)

	// Store tarballs.
	for ver, v := range newVersions {
		if _, dup := exVersions[ver]; dup && h.repo.Hosted.WritePolicy == model.WriteAllowOnce {
			// Deprecate updates re-send existing versions without attachments.
			if len(attachments) > 0 {
				writeJSON(w, 403, map[string]any{"error": fmt.Sprintf("cannot modify pre-existing version: %s", ver)})
				return
			}
		}
		vm, _ := v.(map[string]any)
		dist, _ := vm["dist"].(map[string]any)
		file := ""
		if dist != nil {
			if tb, _ := dist["tarball"].(string); tb != "" {
				file = path.Base(tb)
			}
		}
		if file == "" {
			short := name
			if i := strings.IndexByte(name, '/'); i >= 0 {
				short = name[i+1:]
			}
			file = short + "-" + ver + ".tgz"
		}
		if att, ok := attachments[file].(map[string]any); ok {
			data, _ := att["data"].(string)
			raw, err := base64.StdEncoding.DecodeString(data)
			if err != nil {
				writeJSON(w, 400, map[string]any{"error": "bad attachment"})
				return
			}
			_, err = h.d.Engine.Put(ctx, h.repo, name+"/-/"+file, bytes.NewReader(raw), repo.PutOptions{
				ContentType: "application/octet-stream", Package: packageFor(name, ver), AllowRedeploy: h.repo.Hosted.WritePolicy != model.WriteAllowOnce})
			if err != nil {
				mapErr(w, err, h.d)
				return
			}
		}
		if dist != nil {
			dist["tarball"] = h.base(r) + "/" + name + "/-/" + file
		}
		exVersions[ver] = v
	}
	// Merge document.
	for k, v := range in {
		switch k {
		case "versions", "_attachments", "dist-tags", "time":
			continue
		}
		existing[k] = v
	}
	existing["name"] = name
	existing["versions"] = exVersions
	tags, _ := existing["dist-tags"].(map[string]any)
	if tags == nil {
		tags = map[string]any{}
	}
	if nt, ok := in["dist-tags"].(map[string]any); ok {
		for k, v := range nt {
			tags[k] = v
		}
	}
	existing["dist-tags"] = tags
	times, _ := existing["time"].(map[string]any)
	if times == nil {
		times = map[string]any{}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for ver := range newVersions {
		if _, ok := times[ver]; !ok {
			times[ver] = now
		}
	}
	times["modified"] = now
	if _, ok := times["created"]; !ok {
		times["created"] = now
	}
	existing["time"] = times
	if err := h.storeDoc(r, name, existing); err != nil {
		mapErr(w, err, h.d)
		return
	}
	writeJSON(w, 201, map[string]any{"ok": true, "id": name, "rev": fmt.Sprintf("%d-%s", len(exVersions), now)})
}

func (h *handler) storeDoc(r *http.Request, name string, doc map[string]any) error {
	b, _ := json.Marshal(doc)
	_, err := h.d.Engine.Put(r.Context(), h.repo, name, bytes.NewReader(b), repo.PutOptions{ContentType: "application/json", AllowRedeploy: true})
	return err
}

func (h *handler) unpublish(w http.ResponseWriter, r *http.Request, name string, rest []string) {
	if h.repo.Type != model.Hosted {
		writeJSON(w, 400, map[string]any{"error": "only hosted repositories support unpublish"})
		return
	}
	ctx := r.Context()
	// DELETE /name/-/file.tgz/-rev/x  → remove one tarball (version already removed from doc by npm).
	if len(rest) >= 2 && rest[0] == "-" {
		file := rest[1]
		if err := h.d.Engine.Delete(ctx, h.repo, name+"/-/"+file); err != nil && !errors.Is(err, repo.ErrNotFound) {
			mapErr(w, err, h.d)
			return
		}
		if pk := packageFor(name, versionFromFile(name, file)); pk != nil {
			if p, err := h.d.Content.Package(ctx, h.repo.ID, pk.Namespace, pk.Name, pk.Version); err == nil {
				h.d.Content.DeletePackage(ctx, p.ID)
			}
		}
		writeJSON(w, 200, map[string]any{"ok": true})
		return
	}
	// DELETE /name/-rev/x → whole package.
	assets, _ := h.d.Content.ListAssets(ctx, h.repo.ID, name+"/", 10000)
	for _, a := range assets {
		h.d.Engine.Delete(ctx, h.repo, a.Path)
		if a.PackageID != nil {
			h.d.Content.DeletePackage(ctx, *a.PackageID)
		}
	}
	h.d.Engine.Delete(ctx, h.repo, name)
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (h *handler) distTags(w http.ResponseWriter, r *http.Request, p string) {
	// p = <name>/dist-tags[/<tag>]
	i := strings.Index(p, "/dist-tags")
	name := p[:i]
	tag := strings.TrimPrefix(strings.TrimPrefix(p[i:], "/dist-tags"), "/")
	switch r.Method {
	case http.MethodGet:
		if !format.Authorize(w, r, h.repo, auth.Read, challenge) {
			return
		}
		doc, _, err := h.mergedPackument(r, name)
		if err != nil {
			mapErr(w, err, h.d)
			return
		}
		writeJSON(w, 200, doc["dist-tags"])
	case http.MethodPut, http.MethodPost, http.MethodDelete:
		if !format.Authorize(w, r, h.repo, auth.Write, challenge) {
			return
		}
		if h.repo.Type != model.Hosted || tag == "" {
			writeJSON(w, 400, map[string]any{"error": "unsupported"})
			return
		}
		doc, _, err := h.readPackument(r, h.repo, name)
		if err != nil {
			mapErr(w, err, h.d)
			return
		}
		tags, _ := doc["dist-tags"].(map[string]any)
		if tags == nil {
			tags = map[string]any{}
		}
		if r.Method == http.MethodDelete {
			delete(tags, tag)
		} else {
			var ver string
			b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			if err := json.Unmarshal(b, &ver); err != nil {
				ver = strings.Trim(strings.TrimSpace(string(b)), `"`)
			}
			tags[tag] = ver
		}
		doc["dist-tags"] = tags
		if err := h.storeDoc(r, name, doc); err != nil {
			mapErr(w, err, h.d)
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	default:
		writeJSON(w, 405, map[string]any{"error": "method not allowed"})
	}
}

// ----------------------------------------------------------------- login

// login implements `npm login` (legacy couch-style): returns a user token.
func (h *handler) login(w http.ResponseWriter, r *http.Request, username string) {
	var body struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]any{"error": "invalid body"})
		return
	}
	if body.Name == "" {
		body.Name = username
	}
	pr, err := h.d.Auth.Login(r.Context(), auth.ClientIP(r), body.Name, body.Password)
	if err != nil {
		if errors.Is(err, auth.ErrRateLimited) {
			writeJSON(w, 429, map[string]any{"error": "too many attempts"})
			return
		}
		writeJSON(w, 401, map[string]any{"error": "invalid credentials"})
		return
	}
	secret, _, err := h.d.Auth.CreateToken(r.Context(), pr.Username, "npm login "+time.Now().Format("2006-01-02"), nil)
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "token"})
		return
	}
	writeJSON(w, 201, map[string]any{"ok": true, "id": "org.couchdb.user:" + pr.Username, "token": secret})
}

// ---------------------------------------------------------------- search

func (h *handler) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	text := q.Get("text")
	if h.repo.Type == model.Proxy {
		h.passthrough(w, r, "-/v1/search?"+q.Encode())
		return
	}
	if h.repo.Type == model.Group {
		members, _ := h.d.Engine.Members(r.Context(), h.repo)
		for _, m := range members {
			if m.Type == model.Proxy {
				(&handler{repo: m, d: h.d}).passthrough(w, r, "-/v1/search?"+q.Encode())
				return
			}
		}
	}
	hits, err := h.d.Content.Search(r.Context(), content.SearchQuery{Q: text, Format: Name, Repo: h.repo.Name, Limit: 50})
	if err != nil {
		mapErr(w, err, h.d)
		return
	}
	seen := map[string]bool{}
	var objects []map[string]any
	for _, hit := range hits {
		full := hit.Name
		if hit.Namespace != "" {
			full = hit.Namespace + "/" + hit.Name
		}
		if seen[full] {
			continue
		}
		seen[full] = true
		objects = append(objects, map[string]any{"package": map[string]any{"name": full, "version": hit.Version}, "score": map[string]any{"final": 1}, "searchScore": 1})
	}
	if objects == nil {
		objects = []map[string]any{}
	}
	writeJSON(w, 200, map[string]any{"objects": objects, "total": len(objects), "time": time.Now().UTC().Format(time.RFC1123)})
}

// passthrough forwards the request to the proxy upstream without caching.
func (h *handler) passthrough(w http.ResponseWriter, r *http.Request, p string) {
	target := h.repo
	if h.repo.Type == model.Group {
		members, _ := h.d.Engine.Members(r.Context(), h.repo)
		target = nil
		for _, m := range members {
			if m.Type == model.Proxy {
				target = m
				break
			}
		}
	}
	if target == nil || target.Type != model.Proxy {
		writeJSON(w, 404, map[string]any{"error": "not available"})
		return
	}
	if !strings.Contains(p, "?") && r.URL.RawQuery != "" {
		p += "?" + r.URL.RawQuery
	}
	hdr := http.Header{}
	for _, k := range []string{"Content-Type", "Accept", "Content-Encoding", "Npm-Command"} {
		if v := r.Header.Get(k); v != "" {
			hdr.Set(k, v)
		}
	}
	resp, err := h.d.Engine.Upstream(r.Context(), target, r.Method, p, r.Body, hdr, repo.Policy{Kind: repo.NoCache})
	if err != nil {
		mapErr(w, err, h.d)
		return
	}
	defer resp.Body.Close()
	for _, k := range []string{"Content-Type", "Content-Encoding", "Content-Length"} {
		if v := resp.Header.Get(k); v != "" {
			w.Header().Set(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func mapErr(w http.ResponseWriter, err error, d format.Deps) {
	switch {
	case errors.Is(err, repo.ErrNotFound), errors.Is(err, content.ErrNotFound):
		writeJSON(w, 404, map[string]any{"error": "not found"})
	case errors.Is(err, repo.ErrRedeploy):
		writeJSON(w, 403, map[string]any{"error": "cannot modify pre-existing version"})
	default:
		format.MapError(w, err, d.Log)
	}
}

var _ = url.PathEscape
