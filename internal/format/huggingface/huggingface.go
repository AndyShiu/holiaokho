// Package huggingface proxies the Hugging Face Hub (models, datasets,
// spaces): repo metadata under /api/... and file downloads via
// /<repo>/resolve/<revision>/<path>. Hosted repositories accept file
// uploads at the same paths. huggingface_hub relies on X-Repo-Commit,
// ETag and X-Linked-* headers, which are captured from upstream.
package huggingface

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/holiaokho/holiaokho/internal/auth"
	"github.com/holiaokho/holiaokho/internal/format"
	"github.com/holiaokho/holiaokho/internal/format/common"
	"github.com/holiaokho/holiaokho/internal/model"
	"github.com/holiaokho/holiaokho/internal/repo"
)

const Name = "huggingface"

var keep = []string{"X-Repo-Commit", "X-Linked-Etag", "X-Linked-Size", "X-Error-Code", "Content-Type"}
var shaRe = regexp.MustCompile(`^[0-9a-f]{40}$`)

type Format struct{}

func (Format) Name() string                                                           { return Name }
func (Format) ValidateAttributes(*model.Repository, map[string]json.RawMessage) error { return nil }
func (Format) VersionLess(a, b string) bool                                           { return a < b }

// Parse maps [<type>/]<owner>/<name>/resolve/<rev>/<file> to a package.
func (Format) Parse(p string) *model.Package {
	i := strings.Index(p, "/resolve/")
	if i <= 0 {
		return nil
	}
	repoPath := p[:i]
	rest := strings.SplitN(p[i+len("/resolve/"):], "/", 2)
	if len(rest) != 2 {
		return nil
	}
	ns, name := "", repoPath
	if j := strings.LastIndexByte(repoPath, '/'); j > 0 {
		ns, name = repoPath[:j], repoPath[j+1:]
	}
	return &model.Package{Namespace: ns, Name: name, Version: rest[0]}
}

type handler struct {
	repo *model.Repository
	d    format.Deps
	m    *common.Mirror
}

func (Format) Handler(r *model.Repository, d format.Deps) http.Handler {
	h := &handler{repo: r, d: d}
	h.m = &common.Mirror{Repo: r, Deps: d, Challenge: `Bearer realm="Holiaokho HuggingFace"`, ContentType: func(string) string { return "" }, Parse: Format{}.Parse,
		IsMetadata: func(p string) bool { return strings.HasPrefix(p, "api/") }}
	return h
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := strings.Trim(r.URL.Path, "/")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		h.m.Serve(w, r)
		return
	}
	if !format.Authorize(w, r, h.repo, auth.Read, h.m.Challenge) {
		return
	}
	cp, err := repo.CleanPath(p)
	if err != nil {
		format.MapError(w, err, h.d.Log)
		return
	}
	pol := repo.Policy{Kind: repo.Metadata, KeepHeaders: keep}
	if i := strings.Index(cp, "/resolve/"); i > 0 {
		rev := strings.SplitN(cp[i+len("/resolve/"):], "/", 2)[0]
		pol.Package = Format{}.Parse(cp)
		if shaRe.MatchString(rev) {
			pol.Kind, pol.Immutable = repo.Content, true
		}
		// The Hub answers 302 to a CDN; the upstream client follows it.
	} else if !strings.HasPrefix(cp, "api/") {
		format.WriteError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	if q := r.URL.RawQuery; q != "" && strings.HasPrefix(cp, "api/") {
		pol.UpstreamPath = cp + "?" + q
		cp = cp + "?" + q
	}
	res, err := h.d.Engine.Fetch(r.Context(), h.repo, cp, pol)
	if err != nil {
		format.MapError(w, err, h.d.Log)
		return
	}
	format.ReplayHeaders(w, res.Asset)
	if res.Asset != nil && res.Asset.BlobDigest != nil {
		// huggingface_hub compares ETag with X-Linked-Etag for LFS files;
		// serving the blob digest as ETag keeps them distinct and stable.
		w.Header().Set("ETag", `"`+strings.TrimPrefix(*res.Asset.BlobDigest, "sha256:")+`"`)
	}
	format.ServeResult(w, r, h.d, res)
}
