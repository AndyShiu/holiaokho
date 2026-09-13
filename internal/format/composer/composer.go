// Package composer implements Composer (PHP) repositories: proxies of
// Packagist with p2 metadata and dist rewriting, hosted uploads of package
// zips (composer.json inside) and group merging.
package composer

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/holiaokho/holiaokho/internal/auth"
	"github.com/holiaokho/holiaokho/internal/content"
	"github.com/holiaokho/holiaokho/internal/format"
	"github.com/holiaokho/holiaokho/internal/format/common"
	"github.com/holiaokho/holiaokho/internal/model"
	"github.com/holiaokho/holiaokho/internal/repo"
)

const Name = "composer"

type Format struct{}

func (Format) Name() string                                                           { return Name }
func (Format) ValidateAttributes(*model.Repository, map[string]json.RawMessage) error { return nil }
func (Format) VersionLess(a, b string) bool                                           { return a < b }

func (Format) Parse(p string) *model.Package {
	// dist/<vendor>/<name>/<version>.zip
	segs := strings.Split(strings.Trim(p, "/"), "/")
	if len(segs) != 4 || segs[0] != "dist" || !strings.HasSuffix(segs[3], ".zip") {
		return nil
	}
	return &model.Package{Namespace: segs[1], Name: segs[2], Version: strings.TrimSuffix(segs[3], ".zip")}
}

type handler struct {
	repo *model.Repository
	d    format.Deps
}

func (Format) Handler(r *model.Repository, d format.Deps) http.Handler { return &handler{r, d} }

const challenge = `Basic realm="Holiaokho Composer"`

func (h *handler) base(r *http.Request) string { return h.d.BaseURL(r) + "/repository/" + h.repo.Name }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := strings.Trim(r.URL.Path, "/")
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		if !format.Authorize(w, r, h.repo, auth.Read, challenge) {
			return
		}
		switch {
		case p == "" || p == "packages.json":
			h.root(w, r)
		case strings.HasPrefix(p, "p2/") && strings.HasSuffix(p, ".json"):
			h.p2(w, r, strings.TrimSuffix(strings.TrimPrefix(p, "p2/"), ".json"))
		case strings.HasPrefix(p, "dist/") && strings.HasSuffix(p, ".zip"):
			h.dist(w, r, p)
		case strings.HasPrefix(p, "search.json"):
			h.search(w, r)
		default:
			writeJSON(w, 404, map[string]any{"error": "not found"})
		}
	case http.MethodPut, http.MethodPost:
		if !format.Authorize(w, r, h.repo, auth.Write, challenge) {
			return
		}
		h.upload(w, r, p)
	case http.MethodDelete:
		if !format.Authorize(w, r, h.repo, auth.Delete, challenge) {
			return
		}
		if err := h.d.Engine.Delete(r.Context(), h.repo, p); err != nil {
			format.MapError(w, err, h.d.Log)
			return
		}
		w.WriteHeader(204)
	default:
		writeJSON(w, 405, map[string]any{"error": "method not allowed"})
	}
}

func (h *handler) root(w http.ResponseWriter, r *http.Request) {
	b := h.base(r)
	writeJSON(w, 200, map[string]any{
		"packages":                   map[string]any{},
		"metadata-url":               b + "/p2/%package%.json",
		"search":                     b + "/search.json?q=%query%&type=%type%",
		"available-package-patterns": []string{"*"},
	})
}

// p2Doc is a minimal-versions metadata document: {"packages": {name: [versions...]}}.
func (h *handler) p2Entries(ctx context.Context, rp *model.Repository, name string) ([]map[string]any, time.Time, error) {
	dev := strings.HasSuffix(name, "~dev")
	base := strings.TrimSuffix(name, "~dev")
	switch rp.Type {
	case model.Hosted:
		vendor, short, ok := strings.Cut(base, "/")
		if !ok {
			return nil, time.Time{}, repo.ErrNotFound
		}
		pkgs, err := h.d.Content.PackageVersions(ctx, rp.ID, vendor, short)
		if err != nil {
			return nil, time.Time{}, err
		}
		var out []map[string]any
		var latest time.Time
		for _, p := range pkgs {
			isDev := strings.HasPrefix(p.Version, "dev-") || strings.HasSuffix(p.Version, "-dev")
			if isDev != dev {
				continue
			}
			var e map[string]any
			json.Unmarshal(p.Attrs, &e)
			if e == nil {
				e = map[string]any{"name": base, "version": p.Version}
			}
			e["name"], e["version"] = base, p.Version
			e["dist"] = map[string]any{"type": "zip", "url": "dist/" + vendor + "/" + short + "/" + p.Version + ".zip"}
			out = append(out, e)
			if p.UpdatedAt.After(latest) {
				latest = p.UpdatedAt
			}
		}
		if len(out) == 0 {
			return nil, time.Time{}, repo.ErrNotFound
		}
		return out, latest, nil
	case model.Proxy:
		// Resolve the upstream metadata-url template from its packages.json.
		tmpl := h.upstreamMetadataURL(ctx, rp)
		up := strings.ReplaceAll(tmpl, "%package%", name)
		pol := repo.Policy{Kind: repo.Metadata, ContentType: "application/json", UpstreamPath: up}
		b, res, err := h.d.Engine.ReadAll(ctx, rp, "p2/"+name+".json", pol, 64<<20)
		if err != nil {
			return nil, time.Time{}, err
		}
		var doc struct {
			Packages map[string][]map[string]any `json:"packages"`
			Minified string                      `json:"minified"`
		}
		if err := json.Unmarshal(b, &doc); err != nil {
			return nil, time.Time{}, fmt.Errorf("%w: bad p2 document", repo.ErrUpstream)
		}
		entries := doc.Packages[base]
		if doc.Minified == "composer/2.0" {
			entries = expandMinified(entries)
		}
		var t time.Time
		if res != nil && res.Asset != nil {
			t = res.Asset.UpdatedAt
		}
		return entries, t, nil
	case model.Group:
		seen := map[string]bool{}
		var out []map[string]any
		var latest time.Time
		for _, m := range common.Members(ctx, h.d, rp) {
			es, t, err := h.p2Entries(ctx, m, name)
			if err != nil {
				continue
			}
			if t.After(latest) {
				latest = t
			}
			for _, e := range es {
				v, _ := e["version"].(string)
				if !seen[v] {
					seen[v] = true
					out = append(out, e)
				}
			}
		}
		if len(out) == 0 {
			return nil, time.Time{}, repo.ErrNotFound
		}
		return out, latest, nil
	}
	return nil, time.Time{}, repo.ErrNotFound
}

// expandMinified undoes Composer 2's "minified" encoding where each entry
// only carries the keys that differ from the previous one.
func expandMinified(entries []map[string]any) []map[string]any {
	var out []map[string]any
	prev := map[string]any{}
	for _, e := range entries {
		cur := map[string]any{}
		for k, v := range prev {
			cur[k] = v
		}
		for k, v := range e {
			if v == "__unset" {
				delete(cur, k)
			} else {
				cur[k] = v
			}
		}
		out = append(out, cur)
		prev = cur
	}
	return out
}

func (h *handler) upstreamMetadataURL(ctx context.Context, rp *model.Repository) string {
	pol := repo.Policy{Kind: repo.Metadata, ContentType: "application/json", UpstreamPath: "packages.json"}
	b, _, err := h.d.Engine.ReadAll(ctx, rp, "packages.json", pol, 8<<20)
	if err != nil {
		return strings.TrimSuffix(rp.Proxy.RemoteURL, "/") + "/p2/%package%.json"
	}
	var doc struct {
		MetadataURL string `json:"metadata-url"`
	}
	json.Unmarshal(b, &doc)
	if doc.MetadataURL == "" {
		return strings.TrimSuffix(rp.Proxy.RemoteURL, "/") + "/p2/%package%.json"
	}
	if strings.HasPrefix(doc.MetadataURL, "/") {
		return strings.TrimSuffix(rp.Proxy.RemoteURL, "/") + doc.MetadataURL
	}
	return doc.MetadataURL
}

// p2 serves the metadata document with dist URLs pointing at us. The
// upstream dist URL is remembered per version so dist() can fetch it.
func (h *handler) p2(w http.ResponseWriter, r *http.Request, name string) {
	entries, t, err := h.p2Entries(r.Context(), h.repo, name)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			writeJSON(w, 404, map[string]any{"error": "package not found"})
			return
		}
		format.MapError(w, err, h.d.Log)
		return
	}
	base := strings.TrimSuffix(name, "~dev")
	vendor, short, _ := strings.Cut(base, "/")
	for _, e := range entries {
		dist, _ := e["dist"].(map[string]any)
		if dist == nil {
			continue
		}
		ver, _ := e["version"].(string)
		ref := safeRef(ver)
		if u, _ := dist["url"].(string); u != "" && !strings.HasPrefix(u, "dist/") {
			// Remember the upstream URL under a key composer ignores.
			e["dist-upstream"] = u
		}
		dist["url"] = h.base(r) + "/dist/" + vendor + "/" + short + "/" + ref + ".zip"
		dist["type"] = "zip"
		e["dist"] = dist
	}
	writeJSON(w, 200, map[string]any{"packages": map[string]any{base: entries}, "minified": "", "last-modified": t.UTC().Format(http.TimeFormat)})
}

func safeRef(v string) string {
	return strings.NewReplacer("/", "_", " ", "_").Replace(v)
}

// dist serves dist/<vendor>/<name>/<version>.zip, fetching proxy dists from
// the upstream URL recorded in the p2 metadata.
func (h *handler) dist(w http.ResponseWriter, r *http.Request, p string) {
	segs := strings.Split(p, "/")
	if len(segs) != 4 {
		writeJSON(w, 404, map[string]any{"error": "not found"})
		return
	}
	vendor, short, ref := segs[1], segs[2], strings.TrimSuffix(segs[3], ".zip")
	pkgName := vendor + "/" + short
	for _, rp := range common.Members(r.Context(), h.d, h.repo) {
		pol := repo.Policy{Kind: repo.Content, Immutable: true, ContentType: "application/zip"}
		if rp.Type == model.Hosted {
			if _, err := h.d.Content.Asset(r.Context(), rp.ID, p); err == nil {
				format.FetchAndServe(w, r, h.d, rp, p, pol)
				return
			}
			continue
		}
		if _, err := h.d.Content.Asset(r.Context(), rp.ID, p); err == nil {
			format.FetchAndServe(w, r, h.d, rp, p, pol)
			return
		}
		for _, suffix := range []string{"", "~dev"} {
			entries, _, err := h.p2Entries(r.Context(), rp, pkgName+suffix)
			if err != nil {
				continue
			}
			for _, e := range entries {
				ver, _ := e["version"].(string)
				if safeRef(ver) != ref {
					continue
				}
				dist, _ := e["dist"].(map[string]any)
				u, _ := dist["url"].(string)
				if u == "" {
					continue
				}
				pol.UpstreamPath = u
				pol.Package = &model.Package{Namespace: vendor, Name: short, Version: ver}
				format.FetchAndServe(w, r, h.d, rp, p, pol)
				return
			}
		}
	}
	writeJSON(w, 404, map[string]any{"error": "dist not found"})
}

// upload stores a package zip; metadata comes from composer.json inside.
func (h *handler) upload(w http.ResponseWriter, r *http.Request, p string) {
	if h.repo.Type != model.Hosted {
		writeJSON(w, 400, map[string]any{"error": "only hosted repositories accept uploads"})
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, 512<<20))
	if err != nil {
		writeJSON(w, 400, map[string]any{"error": "read body"})
		return
	}
	meta, err := readComposerJSON(data)
	if err != nil {
		writeJSON(w, 400, map[string]any{"error": "invalid package: " + err.Error()})
		return
	}
	name, _ := meta["name"].(string)
	ver, _ := meta["version"].(string)
	if v := r.URL.Query().Get("version"); v != "" {
		ver = v
	}
	if name == "" || ver == "" {
		writeJSON(w, 400, map[string]any{"error": "composer.json must define name and version (or pass ?version=)"})
		return
	}
	vendor, short, ok := strings.Cut(name, "/")
	if !ok {
		writeJSON(w, 400, map[string]any{"error": "name must be vendor/package"})
		return
	}
	meta["version"] = ver
	attrs, _ := json.Marshal(meta)
	pkg := &model.Package{Namespace: vendor, Name: short, Version: ver, Attrs: attrs}
	dst := "dist/" + vendor + "/" + short + "/" + safeRef(ver) + ".zip"
	if _, err := h.d.Engine.Put(r.Context(), h.repo, dst, bytes.NewReader(data), repo.PutOptions{ContentType: "application/zip", Package: pkg}); err != nil {
		format.MapError(w, err, h.d.Log)
		return
	}
	writeJSON(w, 201, map[string]any{"name": name, "version": ver, "path": dst})
}

func readComposerJSON(data []byte) (map[string]any, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	var best *zip.File
	for _, f := range zr.File {
		if path.Base(f.Name) == "composer.json" && (best == nil || len(f.Name) < len(best.Name)) {
			best = f
		}
	}
	if best == nil {
		return nil, errors.New("composer.json not found in zip")
	}
	rc, err := best.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	var m map[string]any
	if err := json.NewDecoder(io.LimitReader(rc, 4<<20)).Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

func (h *handler) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	hits, _ := h.d.Content.Search(r.Context(), content.SearchQuery{Q: q, Format: Name, Limit: 100})
	seen := map[string]bool{}
	results := []map[string]any{}
	for _, hit := range hits {
		n := hit.Namespace + "/" + hit.Name
		if seen[n] {
			continue
		}
		seen[n] = true
		var attrs struct {
			Description string `json:"description"`
		}
		json.Unmarshal(hit.Attrs, &attrs)
		results = append(results, map[string]any{"name": n, "description": attrs.Description, "url": h.base(r) + "/p2/" + n + ".json"})
	}
	writeJSON(w, 200, map[string]any{"results": results, "total": len(results)})
}

var _ = context.Background
