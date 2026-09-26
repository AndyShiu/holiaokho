// Package pub implements the Dart/Flutter pub repository protocol:
// package listings with archive URLs (proxying pub.dev), tarball
// downloads, and the multi-step `dart pub publish` upload flow for hosted
// repositories.
package pub

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/holiaokho/holiaokho/internal/auth"
	"github.com/holiaokho/holiaokho/internal/format"
	"github.com/holiaokho/holiaokho/internal/format/common"
	"github.com/holiaokho/holiaokho/internal/model"
	"github.com/holiaokho/holiaokho/internal/repo"
)

const Name = "pub"

type Format struct{}

func (Format) Name() string                                                           { return Name }
func (Format) ValidateAttributes(*model.Repository, map[string]json.RawMessage) error { return nil }
func (Format) VersionLess(a, b string) bool                                           { return a < b }

// Parse maps packages/<name>/versions/<version>.tar.gz to a package.
func (Format) Parse(p string) *model.Package {
	segs := strings.Split(strings.Trim(p, "/"), "/")
	if len(segs) == 4 && segs[0] == "packages" && segs[2] == "versions" && strings.HasSuffix(segs[3], ".tar.gz") {
		return &model.Package{Name: segs[1], Version: strings.TrimSuffix(segs[3], ".tar.gz")}
	}
	return nil
}

type handler struct {
	repo *model.Repository
	d    format.Deps
}

func (Format) Handler(r *model.Repository, d format.Deps) http.Handler { return &handler{r, d} }

const challenge = `Bearer realm="Holiaokho pub"`

func (h *handler) base(r *http.Request) string { return h.d.BaseURL(r) + "/repository/" + h.repo.Name }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/vnd.pub.v2+json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func pubErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": msg}})
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := strings.Trim(r.URL.Path, "/")
	switch {
	case p == "api/packages/versions/new":
		if !format.Authorize(w, r, h.repo, auth.Write, challenge) {
			return
		}
		h.newUpload(w, r)
	case p == "api/packages/versions/newUpload" && r.Method == http.MethodPost:
		if !format.Authorize(w, r, h.repo, auth.Write, challenge) {
			return
		}
		h.receiveUpload(w, r)
	case p == "api/packages/versions/newUploadFinish":
		if !format.Authorize(w, r, h.repo, auth.Write, challenge) {
			return
		}
		h.finishUpload(w, r)
	case strings.HasPrefix(p, "api/packages/") && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		if !format.Authorize(w, r, h.repo, auth.Read, challenge) {
			return
		}
		rest := strings.TrimPrefix(p, "api/packages/")
		if i := strings.Index(rest, "/versions/"); i > 0 {
			h.versionDoc(w, r, rest[:i], rest[i+len("/versions/"):])
			return
		}
		h.packageDoc(w, r, rest)
	case strings.HasPrefix(p, "packages/") && strings.HasSuffix(p, ".tar.gz"):
		if !format.Authorize(w, r, h.repo, auth.Read, challenge) {
			return
		}
		h.archive(w, r, p)
	default:
		pubErr(w, 404, "not_found", "not found")
	}
}

// -------------------------------------------------------------- listings

type version struct {
	Version       string         `json:"version"`
	Pubspec       map[string]any `json:"pubspec"`
	ArchiveURL    string         `json:"archive_url"`
	ArchiveSHA256 string         `json:"archive_sha256,omitempty"`
	Published     string         `json:"published,omitempty"`
	Retracted     bool           `json:"retracted,omitempty"`
}

func (h *handler) versions(ctx context.Context, rp *model.Repository, name string) ([]version, error) {
	switch rp.Type {
	case model.Hosted:
		pkgs, err := h.d.Content.PackageVersions(ctx, rp.ID, "", name)
		if err != nil {
			return nil, err
		}
		if len(pkgs) == 0 {
			return nil, repo.ErrNotFound
		}
		var out []version
		for _, p := range pkgs {
			var v version
			json.Unmarshal(p.Attrs, &v)
			v.Version = p.Version
			v.ArchiveURL = "packages/" + name + "/versions/" + p.Version + ".tar.gz"
			v.Published = p.CreatedAt.UTC().Format(time.RFC3339)
			out = append(out, v)
		}
		return out, nil
	case model.Proxy:
		pol := repo.Policy{Kind: repo.Metadata, ContentType: "application/json", UpstreamPath: "api/packages/" + name}
		b, _, err := h.d.Engine.ReadAll(ctx, rp, "api/packages/"+name, pol, 32<<20)
		if err != nil {
			return nil, err
		}
		var doc struct {
			Versions []version `json:"versions"`
		}
		if err := json.Unmarshal(b, &doc); err != nil {
			return nil, err
		}
		return doc.Versions, nil
	case model.Group:
		seen := map[string]bool{}
		var out []version
		for _, m := range common.Members(ctx, h.d, rp) {
			vs, err := h.versions(ctx, m, name)
			if err != nil {
				continue
			}
			for _, v := range vs {
				if !seen[v.Version] {
					seen[v.Version] = true
					out = append(out, v)
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

func (h *handler) rewrite(r *http.Request, name string, vs []version) []version {
	for i := range vs {
		vs[i].ArchiveURL = h.base(r) + "/packages/" + name + "/versions/" + vs[i].Version + ".tar.gz"
	}
	return vs
}

func (h *handler) packageDoc(w http.ResponseWriter, r *http.Request, name string) {
	vs, err := h.versions(r.Context(), h.repo, name)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			pubErr(w, 404, "not_found", "package not found")
			return
		}
		format.MapError(w, err, h.d.Log)
		return
	}
	vs = h.rewrite(r, name, vs)
	sort.SliceStable(vs, func(i, j int) bool { return vs[i].Version < vs[j].Version })
	latest := vs[len(vs)-1]
	for i := len(vs) - 1; i >= 0; i-- {
		if !strings.Contains(vs[i].Version, "-") {
			latest = vs[i]
			break
		}
	}
	writeJSON(w, 200, map[string]any{"name": name, "latest": latest, "versions": vs})
}

func (h *handler) versionDoc(w http.ResponseWriter, r *http.Request, name, ver string) {
	vs, err := h.versions(r.Context(), h.repo, name)
	if err != nil {
		pubErr(w, 404, "not_found", "package not found")
		return
	}
	for _, v := range h.rewrite(r, name, vs) {
		if v.Version == ver {
			writeJSON(w, 200, v)
			return
		}
	}
	pubErr(w, 404, "not_found", "version not found")
}

func (h *handler) archive(w http.ResponseWriter, r *http.Request, p string) {
	pkg := Format{}.Parse(p)
	if pkg == nil {
		pubErr(w, 404, "not_found", "not found")
		return
	}
	pol := repo.Policy{Kind: repo.Content, Immutable: true, ContentType: "application/gzip", Package: pkg}
	for _, rp := range common.Members(r.Context(), h.d, h.repo) {
		mp := pol
		if rp.Type == model.Proxy {
			vs, err := h.versions(r.Context(), rp, pkg.Name)
			if err != nil {
				continue
			}
			found := false
			for _, v := range vs {
				if v.Version == pkg.Version {
					mp.UpstreamPath = v.ArchiveURL
					found = true
				}
			}
			if !found {
				continue
			}
		}
		res, err := h.d.Engine.Fetch(r.Context(), rp, p, mp)
		if err == nil {
			format.ServeResult(w, r, h.d, res)
			return
		}
		if errors.Is(err, repo.ErrMalicious) {
			format.MapError(w, err, h.d.Log)
			return
		}
	}
	pubErr(w, 404, "not_found", "archive not found")
}

// ---------------------------------------------------------------- publish

// newUpload starts the publish flow: the client POSTs the tarball to "url".
func (h *handler) newUpload(w http.ResponseWriter, r *http.Request) {
	if h.repo.Type != model.Hosted {
		pubErr(w, 400, "read_only", "only hosted repositories accept publishes")
		return
	}
	writeJSON(w, 200, map[string]any{"url": h.base(r) + "/api/packages/versions/newUpload", "fields": map[string]string{}})
}

func (h *handler) receiveUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(512 << 20); err != nil {
		pubErr(w, 400, "bad_request", "multipart expected")
		return
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		pubErr(w, 400, "bad_request", "file field required")
		return
	}
	defer f.Close()
	data, _ := io.ReadAll(f)
	spec, err := readPubspec(data)
	if err != nil {
		pubErr(w, 400, "bad_request", "invalid package: "+err.Error())
		return
	}
	name, _ := spec["name"].(string)
	ver, _ := spec["version"].(string)
	if name == "" || ver == "" {
		pubErr(w, 400, "bad_request", "pubspec.yaml must define name and version")
		return
	}
	sum := sha256.Sum256(data)
	v := version{Version: ver, Pubspec: spec, ArchiveSHA256: hex.EncodeToString(sum[:])}
	attrs, _ := json.Marshal(v)
	pkg := &model.Package{Name: name, Version: ver, Attrs: attrs}
	p := "packages/" + name + "/versions/" + ver + ".tar.gz"
	if _, err := h.d.Engine.Put(r.Context(), h.repo, p, bytes.NewReader(data), repo.PutOptions{ContentType: "application/gzip", Package: pkg}); err != nil {
		if errors.Is(err, repo.ErrRedeploy) {
			pubErr(w, 400, "conflict", "version "+ver+" of "+name+" already exists")
			return
		}
		format.MapError(w, err, h.d.Log)
		return
	}
	w.Header().Set("Location", h.base(r)+"/api/packages/versions/newUploadFinish?name="+name+"&version="+ver)
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) finishUpload(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	writeJSON(w, 200, map[string]any{"success": map[string]string{"message": "Successfully uploaded " + q.Get("name") + " " + q.Get("version") + "."}})
}

func readPubspec(data []byte) (map[string]any, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err != nil {
			return nil, errors.New("pubspec.yaml not found")
		}
		if path.Base(hdr.Name) == "pubspec.yaml" && strings.Count(strings.TrimPrefix(hdr.Name, "./"), "/") == 0 {
			raw, _ := io.ReadAll(io.LimitReader(tr, 1<<20))
			var m map[string]any
			if err := yaml.Unmarshal(raw, &m); err != nil {
				return nil, err
			}
			return m, nil
		}
	}
}
