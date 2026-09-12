// Package goproxy implements the GOPROXY protocol for proxy and group
// repositories (list / .info / .mod / .zip / @latest) plus checksum
// database pass-through under /sumdb/, so GOSUMDB works behind the proxy.
package goproxy

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/holiaokho/holiaokho/internal/auth"
	"github.com/holiaokho/holiaokho/internal/format"
	"github.com/holiaokho/holiaokho/internal/model"
	"github.com/holiaokho/holiaokho/internal/repo"
)

const Name = "go"

type Format struct{}

func (Format) Name() string { return Name }

func (Format) ValidateAttributes(r *model.Repository, attrs map[string]json.RawMessage) error {
	return nil
}

func (Format) VersionLess(a, b string) bool { return a < b }

// Parse maps "<module>/@v/<version>.zip" to a package.
func (Format) Parse(p string) *model.Package {
	i := strings.Index(p, "/@v/")
	if i <= 0 || !strings.HasSuffix(p, ".zip") {
		return nil
	}
	mod := decodePath(p[:i])
	ver := strings.TrimSuffix(p[i+4:], ".zip")
	ns, name := "", mod
	if j := strings.LastIndexByte(mod, '/'); j > 0 {
		ns, name = mod[:j], mod[j+1:]
	}
	return &model.Package{Namespace: ns, Name: name, Version: ver}
}

// decodePath undoes the "!x" → "X" escaping of module paths.
func decodePath(p string) string {
	var b strings.Builder
	up := false
	for _, c := range p {
		if c == '!' {
			up = true
			continue
		}
		if up {
			b.WriteRune(c - 'a' + 'A')
			up = false
		} else {
			b.WriteRune(c)
		}
	}
	return b.String()
}

type handler struct {
	repo *model.Repository
	d    format.Deps
}

func (Format) Handler(r *model.Repository, d format.Deps) http.Handler { return &handler{r, d} }

const challenge = `Basic realm="Holiaokho Go"`

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		format.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Go repositories are read-only proxies")
		return
	}
	if !format.Authorize(w, r, h.repo, auth.Read, challenge) {
		return
	}
	p := strings.TrimPrefix(r.URL.Path, "/")
	cp, err := repo.CleanPath(p)
	if err != nil {
		format.MapError(w, err, h.d.Log)
		return
	}
	pol := repo.Policy{Kind: repo.Metadata, ContentType: "text/plain; charset=utf-8"}
	switch {
	case strings.HasPrefix(cp, "sumdb/"):
		// Checksum database: "supported" must answer 200 for any proxy that
		// forwards; everything else is a short-lived lookup.
		if strings.HasSuffix(cp, "/supported") {
			w.WriteHeader(http.StatusOK)
			return
		}
		rest := strings.TrimPrefix(cp, "sumdb/")
		host, path, _ := strings.Cut(rest, "/")
		pol.UpstreamPath = "https://" + host + "/" + path
		if strings.HasPrefix(path, "tile/") {
			pol.Kind, pol.Immutable = repo.Content, true
		}
	case strings.HasSuffix(cp, "/@latest") || strings.HasSuffix(cp, "/@v/list"):
		// mutable
	case strings.Contains(cp, "/@v/"):
		pol.Kind, pol.Immutable = repo.Content, true
		switch {
		case strings.HasSuffix(cp, ".zip"):
			pol.ContentType = "application/zip"
			pol.Package = Format{}.Parse(cp)
		case strings.HasSuffix(cp, ".info"):
			pol.ContentType = "application/json"
		}
	default:
		format.WriteError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	format.FetchAndServe(w, r, h.d, h.repo, cp, pol)
}
