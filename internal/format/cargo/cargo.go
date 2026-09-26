// Package cargo implements Cargo registries using the sparse index
// protocol: config.json, index/<prefix>/<name> (JSON lines), crate
// downloads, `cargo publish` (PUT api/v1/crates/new), yank/unyank and
// search. Proxies mirror crates.io (index.crates.io + static.crates.io).
package cargo

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/holiaokho/holiaokho/internal/auth"
	"github.com/holiaokho/holiaokho/internal/content"
	"github.com/holiaokho/holiaokho/internal/format"
	"github.com/holiaokho/holiaokho/internal/format/common"
	"github.com/holiaokho/holiaokho/internal/model"
	"github.com/holiaokho/holiaokho/internal/repo"
)

const Name = "cargo"

// Attrs: proxies need the download template of the upstream (crates.io:
// https://static.crates.io/crates/{crate}/{version}/download).
type Attrs struct {
	DownloadURL string `json:"downloadUrl"`
}

func attrsOf(r *model.Repository) Attrs {
	var a Attrs
	format.AttrBlock(r, "cargo", &a)
	if a.DownloadURL == "" && r.Proxy != nil && strings.Contains(r.Proxy.RemoteURL, "crates.io") {
		a.DownloadURL = "https://static.crates.io/crates/{crate}/{version}/download"
	}
	return a
}

type Format struct{}

func (Format) Name() string { return Name }

func (Format) ValidateAttributes(r *model.Repository, attrs map[string]json.RawMessage) error {
	var a Attrs
	if raw, ok := attrs["cargo"]; ok {
		json.Unmarshal(raw, &a)
	}
	if r.Type == model.Proxy && a.DownloadURL == "" && r.Proxy != nil && strings.Contains(r.Proxy.RemoteURL, "crates.io") {
		a.DownloadURL = "https://static.crates.io/crates/{crate}/{version}/download"
	}
	raw, _ := json.Marshal(a)
	attrs["cargo"] = raw
	return nil
}

func (Format) VersionLess(a, b string) bool { return semverLess(a, b) }

func (Format) Parse(p string) *model.Package {
	// crates/<name>/<version>/download  or  crates/<name>-<version>.crate
	segs := strings.Split(strings.Trim(p, "/"), "/")
	if len(segs) == 4 && segs[0] == "crates" && segs[3] == "download" {
		return &model.Package{Name: segs[1], Version: segs[2]}
	}
	return nil
}

// indexPath returns the sparse-index path for a crate name.
func indexPath(name string) string {
	name = strings.ToLower(name)
	switch len(name) {
	case 1:
		return "1/" + name
	case 2:
		return "2/" + name
	case 3:
		return "3/" + name[:1] + "/" + name
	}
	return name[:2] + "/" + name[2:4] + "/" + name
}

// Entry is one line of an index file.
type Entry struct {
	Name        string              `json:"name"`
	Vers        string              `json:"vers"`
	Deps        []json.RawMessage   `json:"deps"`
	Cksum       string              `json:"cksum"`
	Features    map[string][]string `json:"features"`
	Yanked      bool                `json:"yanked"`
	Links       string              `json:"links,omitempty"`
	V           int                 `json:"v,omitempty"`
	Features2   map[string][]string `json:"features2,omitempty"`
	RustVersion string              `json:"rust_version,omitempty"`
}

type handler struct {
	repo *model.Repository
	d    format.Deps
	a    Attrs
}

func (Format) Handler(r *model.Repository, d format.Deps) http.Handler {
	return &handler{repo: r, d: d, a: attrsOf(r)}
}

const challenge = `Basic realm="Holiaokho Cargo"`

func (h *handler) base(r *http.Request) string { return h.d.BaseURL(r) + "/repository/" + h.repo.Name }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func cargoErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"errors": []map[string]string{{"detail": msg}}})
}

// withCargoToken authenticates cargo's "Authorization: <token>" (no scheme),
// which it sends on API calls.
func withCargoToken(r *http.Request, d format.Deps) *http.Request {
	if a := r.Header.Get("Authorization"); a != "" && !strings.Contains(a, " ") && strings.HasPrefix(a, auth.TokenPrefix) {
		if pr, err := d.Auth.Login(r.Context(), auth.ClientIP(r), "", a); err == nil {
			return r.WithContext(auth.WithPrincipal(r.Context(), pr))
		}
	}
	return r
}

// AuthorizeWrite checks a publish addressed to a group with cargo's token
// (see format.WriteAuthorizer).
func (Format) AuthorizeWrite(w http.ResponseWriter, r *http.Request, rp *model.Repository, d format.Deps) bool {
	return format.Authorize(w, withCargoToken(r, d), rp, auth.Write, challenge)
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := strings.Trim(r.URL.Path, "/")
	r = withCargoToken(r, h.d)
	switch {
	case p == "api/v1/crates/new" && r.Method == http.MethodPut:
		if !format.Authorize(w, r, h.repo, auth.Write, challenge) {
			return
		}
		h.publish(w, r)
		return
	case strings.HasPrefix(p, "api/v1/crates/") && (strings.HasSuffix(p, "/yank") || strings.HasSuffix(p, "/unyank")):
		if !format.Authorize(w, r, h.repo, auth.Write, challenge) {
			return
		}
		h.yank(w, r, p)
		return
	case p == "api/v1/crates" && r.Method == http.MethodGet:
		if !format.Authorize(w, r, h.repo, auth.Read, challenge) {
			return
		}
		h.search(w, r)
		return
	case p == "me":
		format.ServeBytes(w, r, "text/plain", []byte("Create a token with: holiao token create\n"), time.Time{})
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		cargoErr(w, 405, "method not allowed")
		return
	}
	if !format.Authorize(w, r, h.repo, auth.Read, challenge) {
		return
	}
	switch {
	case p == "config.json" || p == "index/config.json":
		writeJSON(w, 200, map[string]any{"dl": h.base(r) + "/api/v1/crates", "api": h.base(r), "auth-required": false})
	case strings.HasPrefix(p, "index/"):
		h.index(w, r, strings.TrimPrefix(p, "index/"))
	case strings.HasPrefix(p, "api/v1/crates/") && strings.HasSuffix(p, "/download"):
		segs := strings.Split(p, "/")
		if len(segs) != 6 {
			cargoErr(w, 404, "not found")
			return
		}
		h.download(w, r, segs[3], segs[4])
	default:
		cargoErr(w, 404, "not found")
	}
}

// ------------------------------------------------------------------ index

// entries returns the index lines of a crate from one repository.
func (h *handler) entries(ctx context.Context, rp *model.Repository, name string) ([]Entry, error) {
	switch rp.Type {
	case model.Hosted:
		pkgs, err := h.d.Content.PackageVersions(ctx, rp.ID, "", name)
		if err != nil {
			return nil, err
		}
		if len(pkgs) == 0 {
			return nil, repo.ErrNotFound
		}
		var out []Entry
		for _, p := range pkgs {
			var e Entry
			if json.Unmarshal(p.Attrs, &e) != nil || e.Name == "" {
				continue
			}
			out = append(out, e)
		}
		return out, nil
	case model.Proxy:
		pol := repo.Policy{Kind: repo.Metadata, ContentType: "text/plain", UpstreamPath: indexPath(name)}
		b, _, err := h.d.Engine.ReadAll(ctx, rp, "index/"+indexPath(name), pol, 64<<20)
		if err != nil {
			return nil, err
		}
		var out []Entry
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			var e Entry
			if json.Unmarshal([]byte(line), &e) == nil && e.Name != "" {
				out = append(out, e)
			}
		}
		return out, nil
	case model.Group:
		seen := map[string]bool{}
		var out []Entry
		for _, m := range common.Members(ctx, h.d, rp) {
			es, err := h.entries(ctx, m, name)
			if err != nil {
				continue
			}
			for _, e := range es {
				if !seen[e.Vers] {
					seen[e.Vers] = true
					out = append(out, e)
				}
			}
		}
		if len(out) == 0 {
			return nil, repo.ErrNotFound
		}
		return out, nil
	}
	return nil, repo.ErrNotFound
}

func (h *handler) index(w http.ResponseWriter, r *http.Request, rel string) {
	segs := strings.Split(rel, "/")
	name := segs[len(segs)-1]
	if indexPath(name) != rel {
		cargoErr(w, 404, "not found")
		return
	}
	es, err := h.entries(r.Context(), h.repo, name)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			cargoErr(w, 404, "crate not found")
			return
		}
		format.MapError(w, err, h.d.Log)
		return
	}
	sort.SliceStable(es, func(i, j int) bool { return semverLess(es[i].Vers, es[j].Vers) })
	var b bytes.Buffer
	for _, e := range es {
		line, _ := json.Marshal(e)
		b.Write(line)
		b.WriteByte('\n')
	}
	format.ServeBytes(w, r, "text/plain; charset=utf-8", b.Bytes(), time.Time{})
}

// --------------------------------------------------------------- download

func (h *handler) download(w http.ResponseWriter, r *http.Request, name, ver string) {
	p := "crates/" + name + "/" + ver + "/download"
	for _, rp := range common.Members(r.Context(), h.d, h.repo) {
		pol := repo.Policy{Kind: repo.Content, Immutable: true, ContentType: "application/x-tar", Package: &model.Package{Name: name, Version: ver}}
		if rp.Type == model.Proxy {
			a := attrsOf(rp)
			if a.DownloadURL == "" {
				continue
			}
			pol.UpstreamPath = strings.NewReplacer("{crate}", name, "{version}", ver, "{prefix}", indexPath(name)[:len(indexPath(name))-len(name)-1], "{lowerprefix}", strings.ToLower(indexPath(name)[:len(indexPath(name))-len(name)-1])).Replace(a.DownloadURL)
		}
		res, err := h.d.Engine.Fetch(r.Context(), rp, p, pol)
		if err == nil {
			format.ServeResult(w, r, h.d, res)
			return
		}
		if errors.Is(err, repo.ErrMalicious) {
			cargoErr(w, 403, err.Error())
			return
		}
	}
	cargoErr(w, 404, "crate not found")
}

// ---------------------------------------------------------------- publish

// publish parses cargo's binary body: u32 json len, json, u32 crate len, crate.
// Publishes sent to a group reach here already routed to its first hosted
// member (server.groupDeploys), so one registry entry in .cargo/config.toml
// serves both directions.
func (h *handler) publish(w http.ResponseWriter, r *http.Request) {
	if h.repo.Type != model.Hosted {
		cargoErr(w, 400, "only hosted registries accept publishes")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 256<<20))
	if err != nil || len(body) < 8 {
		cargoErr(w, 400, "bad body")
		return
	}
	jl := binary.LittleEndian.Uint32(body[0:4])
	if int(4+jl+4) > len(body) {
		cargoErr(w, 400, "bad body")
		return
	}
	var meta struct {
		Name        string              `json:"name"`
		Vers        string              `json:"vers"`
		Deps        []json.RawMessage   `json:"deps"`
		Features    map[string][]string `json:"features"`
		Links       string              `json:"links"`
		RustVersion string              `json:"rust_version"`
		Description string              `json:"description"`
	}
	if err := json.Unmarshal(body[4:4+jl], &meta); err != nil {
		cargoErr(w, 400, "bad metadata json")
		return
	}
	cl := binary.LittleEndian.Uint32(body[4+jl : 8+jl])
	if int(8+jl+cl) > len(body) {
		cargoErr(w, 400, "bad crate length")
		return
	}
	crate := body[8+jl : 8+jl+cl]
	sum := sha256.Sum256(crate)
	// Index deps use "req"/"kind"/... same names as the publish metadata
	// except "version_req" → "req" and "explicit_name_in_toml"/"name" swap.
	var deps []json.RawMessage
	for _, d := range meta.Deps {
		var m map[string]any
		json.Unmarshal(d, &m)
		if vr, ok := m["version_req"]; ok {
			m["req"] = vr
			delete(m, "version_req")
		}
		if en, ok := m["explicit_name_in_toml"]; ok && en != nil {
			m["package"] = m["name"]
			m["name"] = en
			delete(m, "explicit_name_in_toml")
		}
		b, _ := json.Marshal(m)
		deps = append(deps, b)
	}
	if deps == nil {
		deps = []json.RawMessage{}
	}
	if meta.Features == nil {
		meta.Features = map[string][]string{}
	}
	entry := Entry{Name: meta.Name, Vers: meta.Vers, Deps: deps, Cksum: hex.EncodeToString(sum[:]), Features: meta.Features, Links: meta.Links, RustVersion: meta.RustVersion}
	attrs, _ := json.Marshal(entry)
	pkg := &model.Package{Name: meta.Name, Version: meta.Vers, Attrs: attrs}
	p := "crates/" + meta.Name + "/" + meta.Vers + "/download"
	if _, err := h.d.Engine.Put(r.Context(), h.repo, p, bytes.NewReader(crate), repo.PutOptions{ContentType: "application/x-tar", Package: pkg, Attrs: map[string]any{"description": meta.Description}}); err != nil {
		if errors.Is(err, repo.ErrRedeploy) {
			cargoErr(w, 409, "crate version already exists")
			return
		}
		format.MapError(w, err, h.d.Log)
		return
	}
	writeJSON(w, 200, map[string]any{"warnings": map[string]any{"invalid_categories": []string{}, "invalid_badges": []string{}, "other": []string{}}})
}

func (h *handler) yank(w http.ResponseWriter, r *http.Request, p string) {
	segs := strings.Split(p, "/")
	if len(segs) != 6 {
		cargoErr(w, 404, "not found")
		return
	}
	name, ver, action := segs[3], segs[4], segs[5]
	target := h.repo
	if h.repo.Type == model.Group {
		for _, m := range common.Members(r.Context(), h.d, h.repo) {
			if m.Type == model.Hosted {
				if _, err := h.d.Content.Package(r.Context(), m.ID, "", name, ver); err == nil {
					target = m
					break
				}
			}
		}
	}
	pk, err := h.d.Content.Package(r.Context(), target.ID, "", name, ver)
	if err != nil {
		cargoErr(w, 404, "crate not found")
		return
	}
	var e Entry
	json.Unmarshal(pk.Attrs, &e)
	e.Yanked = action == "yank"
	pk.Attrs, _ = json.Marshal(e)
	if err := h.d.Content.UpsertPackage(r.Context(), pk); err != nil {
		format.MapError(w, err, h.d.Log)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (h *handler) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	hits, _ := h.d.Content.Search(r.Context(), content.SearchQuery{Q: q, Format: Name, Limit: 200})
	seen := map[string]bool{}
	crates := []map[string]any{}
	for _, hit := range hits {
		if seen[hit.Name] {
			continue
		}
		seen[hit.Name] = true
		var attrs struct {
			Description string `json:"description"`
		}
		json.Unmarshal(hit.Attrs, &attrs)
		crates = append(crates, map[string]any{"name": hit.Name, "max_version": hit.Version, "description": attrs.Description})
	}
	writeJSON(w, 200, map[string]any{"crates": crates, "meta": map[string]any{"total": len(crates)}})
}

func semverLess(a, b string) bool {
	pa, pb := parse(a), parse(b)
	for i := 0; i < 3; i++ {
		if pa.n[i] != pb.n[i] {
			return pa.n[i] < pb.n[i]
		}
	}
	if pa.pre == "" || pb.pre == "" {
		return pa.pre != "" && pb.pre == ""
	}
	return pa.pre < pb.pre
}

type sv struct {
	n   [3]int
	pre string
}

func parse(v string) sv {
	var s sv
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	if i := strings.IndexByte(v, '-'); i >= 0 {
		s.pre, v = v[i+1:], v[:i]
	}
	for i, p := range strings.SplitN(v, ".", 3) {
		fmt.Sscanf(p, "%d", &s.n[i])
	}
	return s
}
