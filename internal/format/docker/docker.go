// Package docker implements the Docker Registry HTTP API V2 (and OCI
// distribution) for hosted, proxy and group repositories, including the
// registry-mirror mode used by the Docker daemon's `registry-mirrors`.
package docker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/holiaokho/holiaokho/internal/auth"
	"github.com/holiaokho/holiaokho/internal/content"
	"github.com/holiaokho/holiaokho/internal/format"
	"github.com/holiaokho/holiaokho/internal/model"
	"github.com/holiaokho/holiaokho/internal/repo"
	"github.com/holiaokho/holiaokho/internal/storage"
)

const Name = "docker"

// Attrs is the "docker" block of repository attributes.
type Attrs struct {
	HTTPPort int `json:"httpPort"`
	// HTTPSPort serves the registry over TLS using TLSCert/TLSKey (PEM paths).
	HTTPSPort int    `json:"httpsPort"`
	TLSCert   string `json:"tlsCert"`
	TLSKey    string `json:"tlsKey"`
	// Subdomain routes requests whose Host starts with "<subdomain>." to this
	// repository on the main port (Nexus subdomain connector).
	Subdomain      string `json:"subdomain"`
	ForceBasicAuth bool   `json:"forceBasicAuth"`
	// IndexType: HUB (add library/ for single-segment names), REGISTRY, CUSTOM.
	IndexType string `json:"indexType"`
	// PathEnabled allows /v2/<repo>/<image> addressing on the main port.
	PathEnabled bool `json:"pathEnabled"`
}

func AttrsOf(r *model.Repository) Attrs {
	a := Attrs{PathEnabled: true}
	format.AttrBlock(r, "docker", &a)
	return a
}

var manifestTypes = []string{
	"application/vnd.docker.distribution.manifest.v2+json",
	"application/vnd.docker.distribution.manifest.list.v2+json",
	"application/vnd.oci.image.manifest.v1+json",
	"application/vnd.oci.image.index.v1+json",
	"application/vnd.docker.distribution.manifest.v1+prettyjws",
}

var (
	nameRe = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*(?:/[a-z0-9]+(?:[._-][a-z0-9]+)*)*$`)
	tagRe  = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$`)
)

type Format struct {
	Tokens *TokenIssuer
	// clients caches per-repo upstream clients.
	mu      sync.Mutex
	clients map[string]*http.Client
}

func New(tokens *TokenIssuer) *Format {
	return &Format{Tokens: tokens, clients: map[string]*http.Client{}}
}

func (f *Format) Name() string { return Name }

func (f *Format) VersionLess(a, b string) bool { return a < b }

func (f *Format) Parse(p string) *model.Package {
	i := strings.Index(p, "/manifests/")
	if i <= 0 {
		return nil
	}
	ref := p[i+len("/manifests/"):]
	if strings.HasPrefix(ref, "sha256:") {
		return nil
	}
	return packageFor(p[:i], ref)
}

func packageFor(name, tag string) *model.Package {
	ns, short := "", name
	if i := strings.LastIndexByte(name, '/'); i > 0 {
		ns, short = name[:i], name[i+1:]
	}
	attrs, _ := json.Marshal(map[string]any{"image": name})
	return &model.Package{Namespace: ns, Name: short, Version: tag, Attrs: attrs}
}

func (f *Format) ValidateAttributes(r *model.Repository, attrs map[string]json.RawMessage) error {
	a := Attrs{PathEnabled: true}
	if raw, ok := attrs["docker"]; ok {
		if err := json.Unmarshal(raw, &a); err != nil {
			return fmt.Errorf("docker attributes: %w", err)
		}
	}
	if a.HTTPPort < 0 || a.HTTPPort > 65535 || a.HTTPSPort < 0 || a.HTTPSPort > 65535 {
		return errors.New("docker port out of range")
	}
	if a.HTTPSPort > 0 && (a.TLSCert == "" || a.TLSKey == "") {
		return errors.New("docker.httpsPort requires tlsCert and tlsKey")
	}
	if a.Subdomain != "" && !regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`).MatchString(a.Subdomain) {
		return errors.New("docker.subdomain must be a DNS label")
	}
	a.IndexType = strings.ToUpper(a.IndexType)
	if a.IndexType == "" {
		if r.Proxy != nil && strings.Contains(r.Proxy.RemoteURL, "docker.io") {
			a.IndexType = "HUB"
		} else {
			a.IndexType = "REGISTRY"
		}
	}
	raw, _ := json.Marshal(a)
	attrs["docker"] = raw
	return nil
}

func (f *Format) upstreamClient(rp *model.Repository, base *http.Client) *http.Client {
	key := rp.Name + "|" + rp.Proxy.Username + "|" + rp.Proxy.Password + "|" + rp.Proxy.RemoteURL
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.clients[key]; ok {
		return c
	}
	c := newAuthClient(base, rp.Proxy.Username, rp.Proxy.Password)
	f.clients[key] = c
	return c
}

type handler struct {
	f    *Format
	repo *model.Repository
	d    format.Deps
	a    Attrs
}

func (f *Format) Handler(r *model.Repository, d format.Deps) http.Handler {
	return &handler{f: f, repo: r, d: d, a: AttrsOf(r)}
}

// ---------------------------------------------------------------- errors

func regErr(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"errors": []map[string]any{{"code": code, "message": msg, "detail": nil}}})
}

func (h *handler) mapErr(w http.ResponseWriter, err error, kind string) {
	switch {
	case errors.Is(err, repo.ErrNotFound), errors.Is(err, content.ErrNotFound), errors.Is(err, repo.ErrUpstreamDenied):
		// Docker Hub answers 401 for repositories that do not exist (or are
		// private); to the client that is simply "unknown".
		if kind == "blob" {
			regErr(w, 404, "BLOB_UNKNOWN", "blob unknown to registry")
		} else {
			regErr(w, 404, "MANIFEST_UNKNOWN", "manifest unknown")
		}
	case errors.Is(err, repo.ErrRedeploy), errors.Is(err, repo.ErrWriteDenied), errors.Is(err, repo.ErrReadOnly):
		regErr(w, 403, "DENIED", err.Error())
	case errors.Is(err, repo.ErrUpstream):
		regErr(w, 502, "UNAVAILABLE", err.Error())
	default:
		h.d.Log.Error("docker", "err", err)
		regErr(w, 500, "UNKNOWN", "internal error")
	}
}

// ------------------------------------------------------------------ auth

// principal resolves docker bearer tokens in addition to the generic auth.
func (h *handler) principal(r *http.Request) *auth.Principal {
	if p := h.f.Tokens.Principal(r.Context(), r); p != nil {
		return p
	}
	return auth.PrincipalFrom(r.Context())
}

func (h *handler) challenge(r *http.Request, name, action string) string {
	if h.a.ForceBasicAuth {
		return `Basic realm="Holiaokho Docker"`
	}
	scope := ""
	if name != "" {
		acts := "pull"
		if action == auth.Write || action == auth.Delete {
			acts = "pull,push"
		}
		scope = fmt.Sprintf(`,scope="repository:%s:%s"`, name, acts)
	}
	return fmt.Sprintf(`Bearer realm="%s/v2/token",service="%s"%s`, h.d.BaseURL(r), r.Host, scope)
}

func (h *handler) authorize(w http.ResponseWriter, r *http.Request, name, action string) bool {
	p := h.principal(r)
	if p != nil && p.CanContent(h.repo.Name, h.repo.Format, r.URL.Path, action) {
		return true
	}
	if p == nil || p.Anonymous {
		w.Header().Set("WWW-Authenticate", h.challenge(r, name, action))
		regErr(w, 401, "UNAUTHORIZED", "authentication required")
		return false
	}
	regErr(w, 403, "DENIED", "requested access to the resource is denied")
	return false
}

// -------------------------------------------------------------- routing

// ServeHTTP expects paths relative to /v2 (e.g. "/library/alpine/manifests/latest").
func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")
	p := strings.Trim(r.URL.Path, "/")
	switch {
	case p == "":
		// Version check. Like Docker Hub and Nexus we always challenge
		// requests that carry no credentials — even when anonymous pulls are
		// allowed — so that `docker login` is actually verified at the token
		// endpoint instead of silently "succeeding" against a 200.
		pr := h.principal(r)
		if pr == nil || pr.Anonymous {
			w.Header().Set("WWW-Authenticate", h.challenge(r, "", auth.Read))
			regErr(w, 401, "UNAUTHORIZED", "authentication required")
			return
		}
		if !pr.CanRepo(h.repo.Name, h.repo.Format, auth.Read) {
			regErr(w, 403, "DENIED", "requested access to the resource is denied")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{}"))
		return
	case p == "_catalog":
		if !h.authorize(w, r, "", auth.Read) {
			return
		}
		h.catalog(w, r)
		return
	}
	// <name>/(manifests|blobs|tags)/...
	var name, kind, rest string
	for _, marker := range []string{"/manifests/", "/blobs/", "/tags/"} {
		if i := strings.Index(p, marker); i > 0 {
			name, kind, rest = p[:i], strings.Trim(marker, "/"), p[i+len(marker):]
			break
		}
	}
	if name == "" || !nameRe.MatchString(name) {
		regErr(w, 404, "NAME_INVALID", "invalid repository name")
		return
	}
	switch kind {
	case "manifests":
		h.manifests(w, r, name, rest)
	case "blobs":
		if strings.HasPrefix(rest, "uploads/") || rest == "uploads" {
			h.uploads(w, r, name, strings.TrimPrefix(strings.TrimPrefix(rest, "uploads"), "/"))
			return
		}
		h.blobs(w, r, name, rest)
	case "tags":
		if rest != "list" {
			regErr(w, 404, "UNSUPPORTED", "unsupported")
			return
		}
		if !h.authorize(w, r, name, auth.Read) {
			return
		}
		h.tagsList(w, r, name)
	}
}

// upstreamName applies HUB index semantics (alpine → library/alpine).
func (h *handler) upstreamName(name string) string {
	if h.a.IndexType == "HUB" && !strings.Contains(name, "/") {
		return "library/" + name
	}
	return name
}

func (h *handler) policy(kind repo.Kind, immutable bool, upstreamPath, ct string, pkg *model.Package) repo.Policy {
	pol := repo.Policy{Kind: kind, Immutable: immutable, UpstreamPath: upstreamPath, ContentType: ct, Package: pkg}
	if h.repo.Type == model.Proxy {
		pol.Client = h.f.upstreamClient(h.repo, h.d.Engine.HTTPClient())
	}
	return pol
}

// memberPolicy adapts the policy for a specific group member (each proxy
// member needs its own upstream client and index semantics).
func (h *handler) fetch(ctx context.Context, name, sub string, kind repo.Kind, immutable bool, hdr http.Header, pkg *model.Package) (*repo.Result, error) {
	path := name + "/" + sub
	if h.repo.Type == model.Group {
		members, _ := h.d.Engine.Members(ctx, h.repo)
		var lastErr error = repo.ErrNotFound
		for _, m := range members {
			mh := &handler{f: h.f, repo: m, d: h.d, a: AttrsOf(m)}
			res, err := mh.fetch(ctx, name, sub, kind, immutable, hdr, pkg)
			if err == nil {
				return res, nil
			}
			if !errors.Is(err, repo.ErrNotFound) {
				lastErr = err
			}
		}
		return nil, lastErr
	}
	pol := h.policy(kind, immutable, "v2/"+h.upstreamName(name)+"/"+sub, "", pkg)
	pol.Headers = hdr
	if i := strings.LastIndexByte(sub, '/'); i >= 0 && isDigest(sub[i+1:]) {
		pol.ExpectedDigest = storage.Digest(sub[i+1:])
	}
	return h.d.Engine.Fetch(ctx, h.repo, path, pol)
}

// registryBase returns the external URL prefix under which this handler is
// mounted (e.g. http://host:8081/v2/docker-s3 in path mode, http://host:5000/v2
// on a port connector). http.StripPrefix leaves RequestURI untouched, so the
// prefix is the original path minus the relative path we were handed.
func (h *handler) registryBase(r *http.Request) string {
	orig := r.RequestURI
	if i := strings.IndexByte(orig, '?'); i >= 0 {
		orig = orig[:i]
	}
	rel := r.URL.Path
	prefix := orig
	if strings.HasSuffix(orig, rel) {
		prefix = strings.TrimSuffix(orig, rel)
	}
	if prefix == "" || prefix == "/" {
		prefix = "/v2"
	}
	return h.d.BaseURL(r) + strings.TrimSuffix(prefix, "/")
}

// ------------------------------------------------------------- manifests

func (h *handler) manifests(w http.ResponseWriter, r *http.Request, name, ref string) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		if !h.authorize(w, r, name, auth.Read) {
			return
		}
		h.getManifest(w, r, name, ref)
	case http.MethodPut:
		if !h.authorize(w, r, name, auth.Write) {
			return
		}
		h.putManifest(w, r, name, ref)
	case http.MethodDelete:
		if !h.authorize(w, r, name, auth.Delete) {
			return
		}
		h.deleteManifest(w, r, name, ref)
	default:
		regErr(w, 405, "UNSUPPORTED", "method not allowed")
	}
}

func isDigest(ref string) bool { return strings.HasPrefix(ref, "sha256:") && len(ref) == 71 }

func (h *handler) getManifest(w http.ResponseWriter, r *http.Request, name, ref string) {
	if !isDigest(ref) && !tagRe.MatchString(ref) {
		regErr(w, 404, "MANIFEST_UNKNOWN", "invalid reference")
		return
	}
	hdr := http.Header{}
	accept := r.Header.Values("Accept")
	if len(accept) == 0 {
		accept = manifestTypes
	}
	for _, a := range accept {
		hdr.Add("Accept", a)
	}
	var pkg *model.Package
	kind, immutable := repo.Metadata, false
	if isDigest(ref) {
		kind, immutable = repo.Content, true
	} else {
		pkg = packageFor(name, ref)
	}
	res, err := h.fetch(r.Context(), name, "manifests/"+ref, kind, immutable, hdr, pkg)
	if err != nil {
		h.mapErr(w, err, "manifest")
		return
	}
	defer res.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		h.mapErr(w, err, "manifest")
		return
	}
	digest := "sha256:" + hex.EncodeToString(sha256Sum(body))
	if isDigest(ref) && digest != ref {
		regErr(w, 404, "MANIFEST_UNKNOWN", "digest mismatch")
		return
	}
	// Index the manifest under its digest too, so digest pulls hit the cache
	// (and so tags can be resolved to digests for deletion).
	if !isDigest(ref) && res.Asset != nil && res.Asset.BlobDigest != nil && res.Repo != nil {
		go h.indexDigest(context.WithoutCancel(r.Context()), res.Repo, name, digest, res.Asset, ref)
	}
	ct := res.ContentType
	if ct == "" || !strings.Contains(ct, "json") {
		ct = detectManifestType(body)
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Docker-Content-Digest", digest)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("ETag", `"`+digest+`"`)
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		w.Write(body)
	}
}

func sha256Sum(b []byte) []byte {
	s := sha256.Sum256(b)
	return s[:]
}

func detectManifestType(body []byte) string {
	var m struct {
		MediaType string `json:"mediaType"`
		Manifests []any  `json:"manifests"`
	}
	json.Unmarshal(body, &m)
	if m.MediaType != "" {
		return m.MediaType
	}
	if len(m.Manifests) > 0 {
		return "application/vnd.oci.image.index.v1+json"
	}
	return "application/vnd.oci.image.manifest.v1+json"
}

func (h *handler) indexDigest(ctx context.Context, rp *model.Repository, name, digest string, src *model.Asset, tag string) {
	a := &model.Asset{RepoID: rp.ID, Path: name + "/manifests/" + digest, BlobDigest: src.BlobDigest, Size: src.Size, ContentType: src.ContentType}
	a.Attrs, _ = json.Marshal(map[string]any{"digest": digest})
	if err := h.d.Content.UpsertAsset(ctx, a); err != nil {
		h.d.Log.Warn("index manifest digest", "err", err)
	}
	// Record the tag → digest mapping on the tag asset.
	if src.Path != a.Path {
		var attrs map[string]any
		json.Unmarshal(src.Attrs, &attrs)
		if attrs == nil {
			attrs = map[string]any{}
		}
		if attrs["digest"] != digest {
			attrs["digest"] = digest
			src.Attrs, _ = json.Marshal(attrs)
			h.d.Content.UpsertAsset(ctx, src)
		}
	}
}

type manifestDoc struct {
	SchemaVersion int    `json:"schemaVersion"`
	MediaType     string `json:"mediaType"`
	Config        *struct {
		Digest string `json:"digest"`
	} `json:"config"`
	Layers []struct {
		Digest string `json:"digest"`
	} `json:"layers"`
	Manifests []struct {
		Digest string `json:"digest"`
	} `json:"manifests"`
}

func (h *handler) putManifest(w http.ResponseWriter, r *http.Request, name, ref string) {
	if h.repo.Type != model.Hosted {
		regErr(w, 405, "UNSUPPORTED", "repository does not accept pushes")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		regErr(w, 400, "MANIFEST_INVALID", "read body")
		return
	}
	var m manifestDoc
	if err := json.Unmarshal(body, &m); err != nil {
		regErr(w, 400, "MANIFEST_INVALID", "invalid json")
		return
	}
	ctx := r.Context()
	// Verify referenced blobs/manifests exist; auto-mount blobs known globally.
	var refs []string
	if m.Config != nil {
		refs = append(refs, m.Config.Digest)
	}
	for _, l := range m.Layers {
		refs = append(refs, l.Digest)
	}
	for _, d := range refs {
		if _, err := h.d.Content.Asset(ctx, h.repo.ID, name+"/blobs/"+d); err == nil {
			continue
		}
		size, ok, err := h.d.Content.BlobExists(ctx, storage.Digest(d))
		if err != nil || !ok {
			regErr(w, 400, "MANIFEST_BLOB_UNKNOWN", "blob unknown: "+d)
			return
		}
		ds := d
		h.d.Content.UpsertAsset(ctx, &model.Asset{RepoID: h.repo.ID, Path: name + "/blobs/" + d, BlobDigest: &ds, Size: size, ContentType: "application/octet-stream"})
	}
	for _, sub := range m.Manifests {
		if _, err := h.d.Content.Asset(ctx, h.repo.ID, name+"/manifests/"+sub.Digest); err != nil {
			regErr(w, 400, "MANIFEST_BLOB_UNKNOWN", "manifest unknown: "+sub.Digest)
			return
		}
	}
	digest := "sha256:" + hex.EncodeToString(sha256Sum(body))
	if isDigest(ref) && ref != digest {
		regErr(w, 400, "DIGEST_INVALID", "digest does not match content")
		return
	}
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		ct = detectManifestType(body)
	}
	// Always store by digest; also by tag.
	if _, err := h.d.Engine.Put(ctx, h.repo, name+"/manifests/"+digest, strings.NewReader(string(body)), repo.PutOptions{ContentType: ct, AllowRedeploy: true, Attrs: map[string]any{"digest": digest}}); err != nil {
		h.mapErr(w, err, "manifest")
		return
	}
	if !isDigest(ref) {
		if !tagRe.MatchString(ref) {
			regErr(w, 400, "TAG_INVALID", "invalid tag")
			return
		}
		_, err := h.d.Engine.Put(ctx, h.repo, name+"/manifests/"+ref, strings.NewReader(string(body)), repo.PutOptions{
			ContentType: ct, AllowRedeploy: true, Package: packageFor(name, ref), Attrs: map[string]any{"digest": digest}})
		if err != nil {
			h.mapErr(w, err, "manifest")
			return
		}
	}
	w.Header().Set("Location", fmt.Sprintf("%s/%s/manifests/%s", h.registryBase(r), name, digest))
	w.Header().Set("Docker-Content-Digest", digest)
	w.WriteHeader(http.StatusCreated)
}

func (h *handler) deleteManifest(w http.ResponseWriter, r *http.Request, name, ref string) {
	if h.repo.Type == model.Group {
		regErr(w, 405, "UNSUPPORTED", "group repositories are read-only")
		return
	}
	ctx := r.Context()
	if isDigest(ref) {
		// Remove every tag pointing at the digest, then the digest itself.
		assets, _ := h.d.Content.ListAssets(ctx, h.repo.ID, name+"/manifests/", 10000)
		found := false
		for _, a := range assets {
			var attrs map[string]any
			json.Unmarshal(a.Attrs, &attrs)
			if attrs["digest"] == ref || strings.HasSuffix(a.Path, "/"+ref) {
				h.d.Engine.Delete(ctx, h.repo, a.Path)
				if a.PackageID != nil {
					h.d.Content.DeletePackage(ctx, *a.PackageID)
				}
				found = true
			}
		}
		if !found {
			regErr(w, 404, "MANIFEST_UNKNOWN", "manifest unknown")
			return
		}
		w.WriteHeader(http.StatusAccepted)
		return
	}
	a, err := h.d.Content.Asset(ctx, h.repo.ID, name+"/manifests/"+ref)
	if err != nil {
		regErr(w, 404, "MANIFEST_UNKNOWN", "manifest unknown")
		return
	}
	h.d.Engine.Delete(ctx, h.repo, a.Path)
	if a.PackageID != nil {
		h.d.Content.DeletePackage(ctx, *a.PackageID)
	}
	w.WriteHeader(http.StatusAccepted)
}

// ----------------------------------------------------------------- blobs

func (h *handler) blobs(w http.ResponseWriter, r *http.Request, name, digest string) {
	if !isDigest(digest) {
		regErr(w, 400, "DIGEST_INVALID", "invalid digest")
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		if !h.authorize(w, r, name, auth.Read) {
			return
		}
		res, err := h.fetch(r.Context(), name, "blobs/"+digest, repo.Content, true, nil, nil)
		if err != nil {
			h.mapErr(w, err, "blob")
			return
		}
		w.Header().Set("Docker-Content-Digest", digest)
		if res.ContentType == "" {
			res.ContentType = "application/octet-stream"
		}
		format.ServeResult(w, r, h.d, res)
	case http.MethodDelete:
		if !h.authorize(w, r, name, auth.Delete) {
			return
		}
		if err := h.d.Engine.Delete(r.Context(), h.repo, name+"/blobs/"+digest); err != nil {
			h.mapErr(w, err, "blob")
			return
		}
		w.WriteHeader(http.StatusAccepted)
	default:
		regErr(w, 405, "UNSUPPORTED", "method not allowed")
	}
}

// --------------------------------------------------------------- uploads

func (h *handler) uploads(w http.ResponseWriter, r *http.Request, name, id string) {
	if !h.authorize(w, r, name, auth.Write) {
		return
	}
	if h.repo.Type != model.Hosted {
		regErr(w, 405, "UNSUPPORTED", "repository does not accept pushes")
		return
	}
	ctx := r.Context()
	st, err := h.d.Content.Store(h.repo.StorageID)
	if err != nil {
		h.mapErr(w, err, "blob")
		return
	}
	loc := func(id string) string { return fmt.Sprintf("%s/%s/blobs/uploads/%s", h.registryBase(r), name, id) }
	switch {
	case r.Method == http.MethodPost && id == "":
		q := r.URL.Query()
		// Cross-repository mount (or plain existence) — content-addressed, so
		// any known blob can be mounted instantly.
		if mount := q.Get("mount"); mount != "" && isDigest(mount) {
			if size, ok, _ := h.d.Content.BlobExists(ctx, storage.Digest(mount)); ok {
				ds := mount
				h.d.Content.UpsertAsset(ctx, &model.Asset{RepoID: h.repo.ID, Path: name + "/blobs/" + mount, BlobDigest: &ds, Size: size, ContentType: "application/octet-stream"})
				w.Header().Set("Location", fmt.Sprintf("%s/%s/blobs/%s", h.registryBase(r), name, mount))
				w.Header().Set("Docker-Content-Digest", mount)
				w.WriteHeader(http.StatusCreated)
				return
			}
		}
		uid := uuid.NewString()
		up, err := st.Begin(ctx, uid)
		if err != nil {
			h.mapErr(w, err, "blob")
			return
		}
		if d := q.Get("digest"); d != "" {
			// Monolithic upload in one POST.
			if _, err := io.Copy(up, r.Body); err != nil {
				up.Abort(ctx)
				h.mapErr(w, err, "blob")
				return
			}
			h.finish(w, r, name, up, d)
			return
		}
		closeUpload(up) // keep the (empty) upload file for PATCH/PUT
		w.Header().Set("Location", loc(uid))
		w.Header().Set("Docker-Upload-UUID", uid)
		w.Header().Set("Range", "0-0")
		w.WriteHeader(http.StatusAccepted)
	case r.Method == http.MethodPatch && id != "":
		up, err := st.Resume(ctx, id)
		if err != nil {
			regErr(w, 404, "BLOB_UPLOAD_UNKNOWN", "upload unknown")
			return
		}
		if _, err := io.Copy(up, r.Body); err != nil {
			h.mapErr(w, err, "blob")
			return
		}
		size := up.Size()
		closeUpload(up)
		w.Header().Set("Location", loc(id))
		w.Header().Set("Docker-Upload-UUID", id)
		w.Header().Set("Range", fmt.Sprintf("0-%d", max(size-1, 0)))
		w.WriteHeader(http.StatusAccepted)
	case r.Method == http.MethodPut && id != "":
		d := r.URL.Query().Get("digest")
		up, err := st.Resume(ctx, id)
		if err != nil {
			regErr(w, 404, "BLOB_UPLOAD_UNKNOWN", "upload unknown")
			return
		}
		if r.ContentLength != 0 {
			if _, err := io.Copy(up, r.Body); err != nil {
				h.mapErr(w, err, "blob")
				return
			}
		}
		h.finish(w, r, name, up, d)
	case r.Method == http.MethodGet && id != "":
		up, err := st.Resume(ctx, id)
		if err != nil {
			regErr(w, 404, "BLOB_UPLOAD_UNKNOWN", "upload unknown")
			return
		}
		size := up.Size()
		closeUpload(up)
		w.Header().Set("Docker-Upload-UUID", id)
		w.Header().Set("Range", fmt.Sprintf("0-%d", max(size-1, 0)))
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodDelete && id != "":
		if up, err := st.Resume(ctx, id); err == nil {
			up.Abort(ctx)
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		regErr(w, 405, "UNSUPPORTED", "method not allowed")
	}
}

// closeUpload releases the file handle of a resumed upload without
// committing or aborting it.
func closeUpload(up storage.Upload) {
	if c, ok := up.(io.Closer); ok {
		c.Close()
	}
}

func (h *handler) finish(w http.ResponseWriter, r *http.Request, name string, up storage.Upload, d string) {
	ctx := r.Context()
	if !isDigest(d) {
		up.Abort(ctx)
		regErr(w, 400, "DIGEST_INVALID", "digest required")
		return
	}
	info, err := up.Commit(ctx, storage.Digest(d))
	if err != nil {
		if strings.Contains(err.Error(), "mismatch") {
			regErr(w, 400, "DIGEST_INVALID", err.Error())
			return
		}
		h.mapErr(w, err, "blob")
		return
	}
	if err := h.d.Content.RecordBlob(ctx, h.repo.StorageID, info); err != nil {
		h.mapErr(w, err, "blob")
		return
	}
	ds := string(info.Digest)
	if err := h.d.Content.UpsertAsset(ctx, &model.Asset{RepoID: h.repo.ID, Path: name + "/blobs/" + ds, BlobDigest: &ds, Size: info.Size, ContentType: "application/octet-stream"}); err != nil {
		h.mapErr(w, err, "blob")
		return
	}
	w.Header().Set("Location", fmt.Sprintf("%s/%s/blobs/%s", h.registryBase(r), name, ds))
	w.Header().Set("Docker-Content-Digest", ds)
	w.WriteHeader(http.StatusCreated)
}

// ------------------------------------------------------------- tags/catalog

func (h *handler) localTags(ctx context.Context, rp *model.Repository, name string) []string {
	assets, _ := h.d.Content.ListAssets(ctx, rp.ID, name+"/manifests/", 10000)
	var tags []string
	for _, a := range assets {
		ref := strings.TrimPrefix(a.Path, name+"/manifests/")
		if !isDigest(ref) && !strings.Contains(ref, "/") {
			tags = append(tags, ref)
		}
	}
	return tags
}

func (h *handler) tags(ctx context.Context, rp *model.Repository, name string) ([]string, error) {
	switch rp.Type {
	case model.Hosted:
		return h.localTags(ctx, rp, name), nil
	case model.Proxy:
		mh := &handler{f: h.f, repo: rp, d: h.d, a: AttrsOf(rp)}
		pol := mh.policy(repo.Metadata, false, "v2/"+mh.upstreamName(name)+"/tags/list", "application/json", nil)
		b, _, err := h.d.Engine.ReadAll(ctx, rp, name+"/tags/list", pol, 32<<20)
		if err != nil {
			if errors.Is(err, repo.ErrNotFound) {
				return h.localTags(ctx, rp, name), nil
			}
			return nil, err
		}
		var doc struct {
			Tags []string `json:"tags"`
		}
		json.Unmarshal(b, &doc)
		return doc.Tags, nil
	case model.Group:
		members, _ := h.d.Engine.Members(ctx, rp)
		seen := map[string]bool{}
		var out []string
		for _, m := range members {
			ts, err := h.tags(ctx, m, name)
			if err != nil {
				continue
			}
			for _, t := range ts {
				if !seen[t] {
					seen[t] = true
					out = append(out, t)
				}
			}
		}
		return out, nil
	}
	return nil, nil
}

func (h *handler) tagsList(w http.ResponseWriter, r *http.Request, name string) {
	tags, err := h.tags(r.Context(), h.repo, name)
	if err != nil {
		h.mapErr(w, err, "manifest")
		return
	}
	if tags == nil {
		tags = []string{}
	}
	sort.Strings(tags)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"name": name, "tags": tags})
}

func (h *handler) catalog(w http.ResponseWriter, r *http.Request) {
	var repos []*model.Repository
	if h.repo.Type == model.Group {
		repos, _ = h.d.Engine.Members(r.Context(), h.repo)
	} else {
		repos = []*model.Repository{h.repo}
	}
	seen := map[string]bool{}
	var names []string
	for _, rp := range repos {
		assets, _ := h.d.Content.ListAssets(r.Context(), rp.ID, "", 100000)
		for _, a := range assets {
			if i := strings.Index(a.Path, "/manifests/"); i > 0 {
				n := a.Path[:i]
				if !seen[n] {
					seen[n] = true
					names = append(names, n)
				}
			}
		}
	}
	sort.Strings(names)
	if names == nil {
		names = []string{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"repositories": names})
}

// Ping serves the bare /v2/ version check for the path-mode endpoint on the
// main port: 401 + Bearer challenge until the client presents a token.
func (f *Format) Ping(w http.ResponseWriter, r *http.Request, baseURL string) {
	w.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")
	pr := f.Tokens.Principal(r.Context(), r)
	if pr == nil {
		pr = auth.PrincipalFrom(r.Context())
	}
	if pr == nil || pr.Anonymous {
		w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s/v2/token",service="%s"`, baseURL, r.Host))
		regErr(w, 401, "UNAUTHORIZED", "authentication required")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte("{}"))
}
