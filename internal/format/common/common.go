// Package common holds helpers shared by "file tree" formats (Helm, APT,
// YUM, Alpine, CRAN, p2, CocoaPods, Conda...) whose protocol is mostly
// "mirror this path, treat some paths as mutable metadata".
package common

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path"
	"sort"
	"strings"

	"github.com/holiaokho/holiaokho/internal/auth"
	"github.com/holiaokho/holiaokho/internal/format"
	"github.com/holiaokho/holiaokho/internal/model"
	"github.com/holiaokho/holiaokho/internal/repo"
)

// Mirror serves GET/HEAD/PUT/DELETE for a plain path; isMetadata decides
// whether a path is a mutable index (short TTL) or immutable content.
type Mirror struct {
	Repo       *model.Repository
	Deps       format.Deps
	Challenge  string
	IsMetadata func(p string) bool
	// ContentType guesses a MIME type for stored/served files.
	ContentType func(p string) string
	// Parse extracts a package from an uploaded path (may be nil).
	Parse func(p string) *model.Package
	// OnPut runs after a successful hosted upload (e.g. rebuild an index).
	OnPut func(ctx context.Context, p string)
	// OnDelete runs after a hosted delete.
	OnDelete func(ctx context.Context, p string)
	// Rewrite, when set, post-processes a served metadata document so URLs
	// inside point at this server (Helm index.yaml, Composer JSON...).
	Rewrite func(r *http.Request, p string, body []byte) []byte
	// Generate, when set and the path is missing in a hosted repo, produces
	// the document on the fly (index files derived from the database).
	Generate func(ctx context.Context, rp *model.Repository, p string) ([]byte, bool)
}

func (m *Mirror) Serve(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, "/")
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		if !format.Authorize(w, r, m.Repo, auth.Read, m.Challenge) {
			return
		}
		if p == "" || strings.HasSuffix(p, "/") {
			m.Listing(w, r, p)
			return
		}
		m.Get(w, r, p)
	case http.MethodPut, http.MethodPost:
		if !format.Authorize(w, r, m.Repo, auth.Write, m.Challenge) {
			return
		}
		m.Put(w, r, p, r.Body, r.Header.Get("Content-Type"))
	case http.MethodDelete:
		if !format.Authorize(w, r, m.Repo, auth.Delete, m.Challenge) {
			return
		}
		cp, err := repo.CleanPath(p)
		if err != nil {
			format.MapError(w, err, m.Deps.Log)
			return
		}
		if err := m.Deps.Engine.Delete(r.Context(), m.Repo, cp); err != nil {
			format.MapError(w, err, m.Deps.Log)
			return
		}
		if m.OnDelete != nil {
			m.OnDelete(r.Context(), cp)
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		format.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (m *Mirror) policy(p string) repo.Policy {
	pol := repo.Policy{Kind: repo.Content, Immutable: true}
	if m.IsMetadata != nil && m.IsMetadata(p) {
		pol.Kind, pol.Immutable = repo.Metadata, false
	}
	if m.ContentType != nil {
		pol.ContentType = m.ContentType(p)
	}
	if m.Parse != nil && pol.Immutable {
		pol.Package = m.Parse(p)
	}
	return pol
}

// Get serves a file, merging/generating metadata where the format asks for it.
func (m *Mirror) Get(w http.ResponseWriter, r *http.Request, p string) {
	cp, err := repo.CleanPath(p)
	if err != nil {
		format.MapError(w, err, m.Deps.Log)
		return
	}
	pol := m.policy(cp)
	meta := pol.Kind == repo.Metadata
	if meta && (m.Rewrite != nil || m.Generate != nil) {
		b, res, err := m.ReadMetadata(r.Context(), m.Repo, cp, pol)
		if err != nil {
			format.MapError(w, err, m.Deps.Log)
			return
		}
		if m.Rewrite != nil {
			b = m.Rewrite(r, cp, b)
		}
		ct := pol.ContentType
		if res != nil && res.ContentType != "" && ct == "" {
			ct = res.ContentType
		}
		if ct == "" {
			ct = "application/octet-stream"
		}
		var mod = zeroTime
		if res != nil && res.Asset != nil {
			mod = res.Asset.UpdatedAt
		}
		format.ServeBytes(w, r, ct, b, mod)
		return
	}
	format.FetchAndServe(w, r, m.Deps, m.Repo, cp, pol)
}

// ReadMetadata fetches a metadata document, falling back to Generate for
// hosted repositories that have no stored copy.
func (m *Mirror) ReadMetadata(ctx context.Context, rp *model.Repository, p string, pol repo.Policy) ([]byte, *repo.Result, error) {
	if rp.Type == model.Group && m.Generate != nil {
		// Groups: let the format merge members via Generate.
		if b, ok := m.Generate(ctx, rp, p); ok {
			return b, nil, nil
		}
	}
	b, res, err := m.Deps.Engine.ReadAll(ctx, rp, p, pol, 256<<20)
	if err == nil {
		return b, res, nil
	}
	if rp.Type == model.Hosted && m.Generate != nil {
		if gb, ok := m.Generate(ctx, rp, p); ok {
			return gb, nil, nil
		}
	}
	return nil, nil, err
}

func (m *Mirror) Put(w http.ResponseWriter, r *http.Request, p string, body io.Reader, ct string) {
	cp, err := repo.CleanPath(p)
	if err != nil {
		format.MapError(w, err, m.Deps.Log)
		return
	}
	if ct == "" && m.ContentType != nil {
		ct = m.ContentType(cp)
	}
	opt := repo.PutOptions{ContentType: ct}
	if m.Parse != nil {
		opt.Package = m.Parse(cp)
	}
	if m.IsMetadata != nil && m.IsMetadata(cp) {
		opt.AllowRedeploy = true
	}
	a, err := m.Deps.Engine.Put(r.Context(), m.Repo, cp, body, opt)
	if err != nil {
		format.MapError(w, err, m.Deps.Log)
		return
	}
	if m.OnPut != nil {
		m.OnPut(r.Context(), cp)
	}
	w.Header().Set("X-Checksum-Sha256", strings.TrimPrefix(*a.BlobDigest, "sha256:"))
	w.WriteHeader(http.StatusCreated)
}

// Listing returns a JSON directory listing merged across group members.
func (m *Mirror) Listing(w http.ResponseWriter, r *http.Request, dir string) {
	repos := []*model.Repository{m.Repo}
	if m.Repo.Type == model.Group {
		repos, _ = m.Deps.Engine.Members(r.Context(), m.Repo)
	}
	type entry struct {
		Name string `json:"name"`
		Dir  bool   `json:"directory"`
		Size int64  `json:"size,omitempty"`
	}
	seen := map[string]bool{}
	out := []entry{}
	for _, rp := range repos {
		ds, fs, err := m.Deps.Content.ListChildren(r.Context(), rp.ID, dir)
		if err != nil {
			format.MapError(w, err, m.Deps.Log)
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
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	if len(out) == 0 && dir != "" {
		format.WriteError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// Members returns the repositories to consult: the repo itself, or the
// members of a group (recursively flattened).
func Members(ctx context.Context, d format.Deps, rp *model.Repository) []*model.Repository {
	if rp.Type != model.Group {
		return []*model.Repository{rp}
	}
	ms, _ := d.Engine.Members(ctx, rp)
	var out []*model.Repository
	for _, m := range ms {
		out = append(out, Members(ctx, d, m)...)
	}
	return out
}

// ContentTypeByExt is a small extension map shared by formats.
func ContentTypeByExt(p string) string {
	switch strings.ToLower(path.Ext(p)) {
	case ".json":
		return "application/json"
	case ".yaml", ".yml":
		return "application/yaml"
	case ".xml":
		return "application/xml"
	case ".gz", ".tgz":
		return "application/gzip"
	case ".zip", ".jar", ".whl", ".nupkg":
		return "application/zip"
	case ".txt", ".md":
		return "text/plain; charset=utf-8"
	case ".html":
		return "text/html; charset=utf-8"
	}
	return "application/octet-stream"
}
