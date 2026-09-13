// Package swift implements the Swift Package Registry API (SE-0292):
// release lists, release metadata, Package.swift manifests, source
// archives, identifier lookup and publishing (hosted); groups merge.
package swift

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
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

const Name = "swift"

type Format struct{}

func (Format) Name() string                                                           { return Name }
func (Format) ValidateAttributes(*model.Repository, map[string]json.RawMessage) error { return nil }
func (Format) VersionLess(a, b string) bool                                           { return a < b }

// Parse maps <scope>/<name>/<version>.zip to a package.
func (Format) Parse(p string) *model.Package {
	segs := strings.Split(strings.Trim(p, "/"), "/")
	if len(segs) == 3 && strings.HasSuffix(segs[2], ".zip") {
		return &model.Package{Namespace: segs[0], Name: segs[1], Version: strings.TrimSuffix(segs[2], ".zip")}
	}
	return nil
}

type handler struct {
	repo *model.Repository
	d    format.Deps
}

func (Format) Handler(r *model.Repository, d format.Deps) http.Handler { return &handler{r, d} }

const challenge = `Basic realm="Holiaokho Swift"`
const ctJSON = "application/json"

func (h *handler) base(r *http.Request) string { return h.d.BaseURL(r) + "/repository/" + h.repo.Name }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", ctJSON)
	w.Header().Set("Content-Version", "1")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func problem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Content-Version", "1")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"detail": detail})
}

type release struct {
	Version   string         `json:"version"`
	Manifest  string         `json:"-"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	Checksum  string         `json:"checksum"`
	Published string         `json:"publishedAt"`
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Version", "1")
	p := strings.Trim(r.URL.Path, "/")
	segs := strings.Split(p, "/")
	if p == "identifiers" {
		if !format.Authorize(w, r, h.repo, auth.Read, challenge) {
			return
		}
		h.identifiers(w, r)
		return
	}
	if len(segs) < 2 {
		problem(w, 404, "not found")
		return
	}
	scope, name := strings.ToLower(segs[0]), strings.ToLower(segs[1])
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		if !format.Authorize(w, r, h.repo, auth.Read, challenge) {
			return
		}
		switch {
		case len(segs) == 2:
			h.releases(w, r, scope, name)
		case len(segs) == 3 && strings.HasSuffix(segs[2], ".zip"):
			h.archive(w, r, scope, name, strings.TrimSuffix(segs[2], ".zip"))
		case len(segs) == 3:
			h.release(w, r, scope, name, segs[2])
		case len(segs) == 4 && segs[3] == "Package.swift":
			h.manifest(w, r, scope, name, segs[2])
		default:
			problem(w, 404, "not found")
		}
	case http.MethodPut:
		if !format.Authorize(w, r, h.repo, auth.Write, challenge) {
			return
		}
		if len(segs) != 3 {
			problem(w, 404, "not found")
			return
		}
		h.publish(w, r, scope, name, segs[2])
	default:
		problem(w, 405, "method not allowed")
	}
}

func (h *handler) load(ctx context.Context, rp *model.Repository, scope, name string) ([]release, *model.Repository, error) {
	switch rp.Type {
	case model.Hosted:
		pkgs, err := h.d.Content.PackageVersions(ctx, rp.ID, scope, name)
		if err != nil || len(pkgs) == 0 {
			return nil, nil, repo.ErrNotFound
		}
		var out []release
		for _, p := range pkgs {
			var rel release
			var extra struct {
				Manifest string `json:"manifest"`
			}
			json.Unmarshal(p.Attrs, &rel)
			json.Unmarshal(p.Attrs, &extra)
			rel.Manifest = extra.Manifest
			rel.Version = p.Version
			rel.Published = p.CreatedAt.UTC().Format(time.RFC3339)
			out = append(out, rel)
		}
		return out, rp, nil
	case model.Group:
		for _, m := range common.Members(ctx, h.d, rp) {
			if rels, src, err := h.load(ctx, m, scope, name); err == nil {
				return rels, src, nil
			}
		}
	}
	return nil, nil, repo.ErrNotFound
}

func (h *handler) releases(w http.ResponseWriter, r *http.Request, scope, name string) {
	rels, _, err := h.load(r.Context(), h.repo, scope, name)
	if err != nil {
		problem(w, 404, "package not found")
		return
	}
	sort.Slice(rels, func(i, j int) bool { return rels[i].Version < rels[j].Version })
	out := map[string]any{}
	for _, rel := range rels {
		out[rel.Version] = map[string]any{"url": fmt.Sprintf("%s/%s/%s/%s", h.base(r), scope, name, rel.Version)}
	}
	w.Header().Set("Link", fmt.Sprintf(`<%s/%s/%s/%s>; rel="latest-version"`, h.base(r), scope, name, rels[len(rels)-1].Version))
	writeJSON(w, 200, map[string]any{"releases": out})
}

func (h *handler) find(r *http.Request, scope, name, version string) (*release, error) {
	rels, _, err := h.load(r.Context(), h.repo, scope, name)
	if err != nil {
		return nil, err
	}
	for i := range rels {
		if rels[i].Version == version {
			return &rels[i], nil
		}
	}
	return nil, repo.ErrNotFound
}

func (h *handler) release(w http.ResponseWriter, r *http.Request, scope, name, version string) {
	rel, err := h.find(r, scope, name, version)
	if err != nil {
		problem(w, 404, "release not found")
		return
	}
	base := h.base(r)
	writeJSON(w, 200, map[string]any{"id": scope + "." + name, "version": version, "publishedAt": rel.Published, "metadata": rel.Metadata,
		"resources": []map[string]any{{"name": "source-archive", "type": "application/zip", "checksum": rel.Checksum}},
		"_links":    map[string]string{"latest-version": base + "/" + scope + "/" + name}})
}

func (h *handler) manifest(w http.ResponseWriter, r *http.Request, scope, name, version string) {
	rel, err := h.find(r, scope, name, version)
	if err != nil || rel.Manifest == "" {
		problem(w, 404, "manifest not found")
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="Package.swift"`)
	format.ServeBytes(w, r, "text/x-swift", []byte(rel.Manifest), time.Time{})
}

func (h *handler) archive(w http.ResponseWriter, r *http.Request, scope, name, version string) {
	p := scope + "/" + name + "/" + version + ".zip"
	rel, err := h.find(r, scope, name, version)
	if err != nil {
		problem(w, 404, "release not found")
		return
	}
	if rel.Checksum != "" {
		w.Header().Set("Digest", "sha-256="+rel.Checksum)
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-%s.zip"`, name, version))
	for _, rp := range common.Members(r.Context(), h.d, h.repo) {
		if res, err := h.d.Engine.Fetch(r.Context(), rp, p, repo.Policy{Kind: repo.Content, Immutable: true, ContentType: "application/zip"}); err == nil {
			format.ServeResult(w, r, h.d, res)
			return
		}
	}
	problem(w, 404, "archive not found")
}

func (h *handler) identifiers(w http.ResponseWriter, r *http.Request) {
	u := strings.TrimSuffix(strings.ToLower(r.URL.Query().Get("url")), ".git")
	var ids []string
	hits, _ := h.d.Content.Search(r.Context(), content.SearchQuery{Format: Name, Limit: 1000})
	seen := map[string]bool{}
	for _, hit := range hits {
		var rel release
		json.Unmarshal(hit.Attrs, &rel)
		repoURL, _ := rel.Metadata["repositoryURLs"].([]any)
		for _, ru := range repoURL {
			if s, _ := ru.(string); strings.TrimSuffix(strings.ToLower(s), ".git") == u {
				id := hit.Namespace + "." + hit.Name
				if !seen[id] {
					seen[id] = true
					ids = append(ids, id)
				}
			}
		}
	}
	if ids == nil {
		problem(w, 404, "no package matches the URL")
		return
	}
	writeJSON(w, 200, map[string]any{"identifiers": ids})
}

// publish implements PUT /{scope}/{name}/{version} (multipart: source-archive, metadata).
func (h *handler) publish(w http.ResponseWriter, r *http.Request, scope, name, version string) {
	if h.repo.Type != model.Hosted {
		problem(w, 405, "only hosted registries accept publishes")
		return
	}
	var archive []byte
	var meta map[string]any
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		if err := r.ParseMultipartForm(512 << 20); err != nil {
			problem(w, 400, "multipart expected")
			return
		}
		if f, _, err := r.FormFile("source-archive"); err == nil {
			archive, _ = io.ReadAll(f)
			f.Close()
		}
		if m := r.FormValue("metadata"); m != "" {
			json.Unmarshal([]byte(m), &meta)
		} else if f, _, err := r.FormFile("metadata"); err == nil {
			json.NewDecoder(f).Decode(&meta)
			f.Close()
		}
	} else {
		archive, _ = io.ReadAll(io.LimitReader(r.Body, 512<<20))
	}
	if len(archive) == 0 {
		problem(w, 400, "source-archive required")
		return
	}
	manifest := readManifest(archive)
	sum := sha256.Sum256(archive)
	rel := release{Version: version, Manifest: manifest, Metadata: meta, Checksum: hex.EncodeToString(sum[:])}
	attrs, _ := json.Marshal(struct {
		release
		Manifest string `json:"manifest"`
	}{rel, manifest})
	pkg := &model.Package{Namespace: scope, Name: name, Version: version, Attrs: attrs}
	p := scope + "/" + name + "/" + version + ".zip"
	if _, err := h.d.Engine.Put(r.Context(), h.repo, p, bytes.NewReader(archive), repo.PutOptions{ContentType: "application/zip", Package: pkg}); err != nil {
		if errors.Is(err, repo.ErrRedeploy) {
			problem(w, 409, "release already exists")
			return
		}
		format.MapError(w, err, h.d.Log)
		return
	}
	w.Header().Set("Location", fmt.Sprintf("%s/%s/%s/%s", h.base(r), scope, name, version))
	w.WriteHeader(http.StatusCreated)
}

func readManifest(archive []byte) string {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return ""
	}
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, "Package.swift") && strings.Count(f.Name, "/") <= 1 {
			rc, err := f.Open()
			if err != nil {
				continue
			}
			b, _ := io.ReadAll(io.LimitReader(rc, 1<<20))
			rc.Close()
			return string(b)
		}
	}
	return ""
}
