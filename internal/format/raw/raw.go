// Package raw implements the "raw" format: plain files at arbitrary paths
// (installers, docs, tarballs). Proxy repositories mirror any HTTP server.
package raw

import (
	"encoding/json"
	"net/http"
	"path"
	"strings"

	"github.com/holiaokho/holiaokho/internal/auth"
	"github.com/holiaokho/holiaokho/internal/format"
	"github.com/holiaokho/holiaokho/internal/model"
	"github.com/holiaokho/holiaokho/internal/repo"
)

const Name = "raw"

type Format struct{}

func (Format) Name() string                                                           { return Name }
func (Format) ValidateAttributes(*model.Repository, map[string]json.RawMessage) error { return nil }
func (Format) VersionLess(a, b string) bool                                           { return a < b }

// Parse treats the first path segment as the package name and the file as the
// version, e.g. "installer/2.1/setup.exe" → installer @ 2.1.
func (Format) Parse(p string) *model.Package {
	segs := strings.Split(strings.Trim(p, "/"), "/")
	if len(segs) < 2 {
		return nil
	}
	ns := ""
	if len(segs) > 2 {
		ns = strings.Join(segs[:len(segs)-2], "/")
	}
	return &model.Package{Namespace: ns, Name: segs[len(segs)-2], Version: segs[len(segs)-1]}
}

type handler struct {
	repo *model.Repository
	d    format.Deps
}

func (Format) Handler(r *model.Repository, d format.Deps) http.Handler { return &handler{r, d} }

const challenge = `Basic realm="Holiaokho"`

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, "/")
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		if !format.Authorize(w, r, h.repo, auth.Read, challenge) {
			return
		}
		if p == "" || strings.HasSuffix(p, "/") {
			h.listing(w, r, p)
			return
		}
		cp, err := repo.CleanPath(p)
		if err != nil {
			format.MapError(w, err, h.d.Log)
			return
		}
		format.FetchAndServe(w, r, h.d, h.repo, cp, repo.Policy{Kind: repo.Metadata, ContentType: contentType(cp)})
	case http.MethodPut, http.MethodPost:
		if !format.Authorize(w, r, h.repo, auth.Write, challenge) {
			return
		}
		cp, err := repo.CleanPath(p)
		if err != nil {
			format.MapError(w, err, h.d.Log)
			return
		}
		ct := r.Header.Get("Content-Type")
		if ct == "" {
			ct = contentType(cp)
		}
		a, err := h.d.Engine.Put(r.Context(), h.repo, cp, r.Body, repo.PutOptions{ContentType: ct, Package: Format{}.Parse(cp)})
		if err != nil {
			format.MapError(w, err, h.d.Log)
			return
		}
		w.Header().Set("X-Checksum-Sha256", strings.TrimPrefix(*a.BlobDigest, "sha256:"))
		w.WriteHeader(http.StatusCreated)
	case http.MethodDelete:
		if !format.Authorize(w, r, h.repo, auth.Delete, challenge) {
			return
		}
		cp, err := repo.CleanPath(p)
		if err != nil {
			format.MapError(w, err, h.d.Log)
			return
		}
		if err := h.d.Engine.Delete(r.Context(), h.repo, cp); err != nil {
			format.MapError(w, err, h.d.Log)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		format.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (h *handler) listing(w http.ResponseWriter, r *http.Request, dir string) {
	repos := []*model.Repository{h.repo}
	if h.repo.Type == model.Group {
		repos, _ = h.d.Engine.Members(r.Context(), h.repo)
	}
	type entry struct {
		Name string `json:"name"`
		Dir  bool   `json:"directory"`
		Size int64  `json:"size,omitempty"`
	}
	seen := map[string]bool{}
	var out []entry
	for _, rp := range repos {
		ds, fs, err := h.d.Content.ListChildren(r.Context(), rp.ID, dir)
		if err != nil {
			format.MapError(w, err, h.d.Log)
			return
		}
		for _, d := range ds {
			if !seen[d+"/"] {
				seen[d+"/"] = true
				out = append(out, entry{Name: d, Dir: true})
			}
		}
		for _, f := range fs {
			n := path.Base(f.Path)
			if !seen[n] {
				seen[n] = true
				out = append(out, entry{Name: n, Size: f.Size})
			}
		}
	}
	if out == nil {
		if dir != "" {
			format.WriteError(w, http.StatusNotFound, "not_found", "not found")
			return
		}
		out = []entry{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

func contentType(p string) string {
	switch strings.ToLower(path.Ext(p)) {
	case ".txt", ".md":
		return "text/plain; charset=utf-8"
	case ".json":
		return "application/json"
	case ".xml":
		return "application/xml"
	case ".html":
		return "text/html; charset=utf-8"
	case ".zip":
		return "application/zip"
	case ".gz", ".tgz":
		return "application/gzip"
	case ".pdf":
		return "application/pdf"
	case ".sh":
		return "text/x-shellscript"
	}
	return "application/octet-stream"
}
