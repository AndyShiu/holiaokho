// Package conan implements the Conan 2 remote API (v1 auth endpoints and
// the v2 recipe/package revision layout). Proxies mirror conancenter
// (latest/revisions/search are mutable, revision files immutable);
// hosted remotes store uploaded files and derive the revision listings
// from the stored paths.
package conan

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
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

const Name = "conan"

type Format struct{}

func (Format) Name() string                                                           { return Name }
func (Format) ValidateAttributes(*model.Repository, map[string]json.RawMessage) error { return nil }
func (Format) VersionLess(a, b string) bool                                           { return a < b }

// Parse maps v2/conans/<name>/<version>/<user>/<channel>/revisions/<rrev>/files/conanfile.py to a package.
func (Format) Parse(p string) *model.Package {
	segs := strings.Split(strings.Trim(p, "/"), "/")
	if len(segs) >= 8 && segs[0] == "v2" && segs[1] == "conans" && segs[6] == "revisions" && path.Base(p) == "conanfile.py" {
		ns := segs[4] + "/" + segs[5]
		if segs[4] == "_" {
			ns = ""
		}
		return &model.Package{Namespace: ns, Name: segs[2], Version: segs[3]}
	}
	return nil
}

type handler struct {
	repo *model.Repository
	d    format.Deps
}

func (Format) Handler(r *model.Repository, d format.Deps) http.Handler { return &handler{r, d} }

const challenge = `Basic realm="Holiaokho Conan"`

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := strings.Trim(r.URL.Path, "/")
	w.Header().Set("X-Conan-Server-Capabilities", "complex_search,revisions,matrix_params")
	switch {
	case p == "v1/ping" || p == "v2/ping":
		w.WriteHeader(200)
		return
	case p == "v1/users/authenticate" || p == "v2/users/authenticate":
		user, pass, ok := r.BasicAuth()
		if !ok {
			w.Header().Set("WWW-Authenticate", challenge)
			w.WriteHeader(401)
			return
		}
		pr, err := h.d.Auth.Login(r.Context(), auth.ClientIP(r), user, pass)
		if err != nil {
			w.WriteHeader(401)
			return
		}
		secret, _, err := h.d.Auth.CreateToken(r.Context(), pr.Username, "conan", nil)
		if err != nil {
			w.WriteHeader(500)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(secret))
		return
	case p == "v1/users/check_credentials" || p == "v2/users/check_credentials":
		pr := auth.PrincipalFrom(r.Context())
		if pr == nil || pr.Anonymous {
			w.Header().Set("WWW-Authenticate", challenge)
			w.WriteHeader(401)
			return
		}
		w.Write([]byte(pr.Username))
		return
	}
	if !strings.HasPrefix(p, "v2/conans/") && !strings.HasPrefix(p, "v2/conans") {
		writeJSON(w, 404, map[string]any{"error": "not found"})
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		if !format.Authorize(w, r, h.repo, auth.Read, challenge) {
			return
		}
		h.get(w, r, p)
	case http.MethodPut:
		if !format.Authorize(w, r, h.repo, auth.Write, challenge) {
			return
		}
		h.put(w, r, p)
	case http.MethodDelete:
		if !format.Authorize(w, r, h.repo, auth.Delete, challenge) {
			return
		}
		h.delete(w, r, p)
	default:
		writeJSON(w, 405, map[string]any{"error": "method not allowed"})
	}
}

// isListing reports whether the v2 path is a derived listing (latest,
// revisions, files, search) rather than a stored file.
func isListing(p string) bool {
	b := path.Base(p)
	return b == "latest" || b == "revisions" || b == "files" || b == "search" || strings.HasPrefix(b, "search?")
}

func (h *handler) get(w http.ResponseWriter, r *http.Request, p string) {
	if isListing(p) {
		h.listing(w, r, p)
		return
	}
	// Stored files (conanfile.py, conanmanifest.txt, conan_export.tgz,
	// conan_package.tgz, conaninfo.txt ...) are immutable under a revision.
	pol := repo.Policy{Kind: repo.Content, Immutable: true, ContentType: "application/octet-stream", Package: Format{}.Parse(p)}
	if strings.HasSuffix(p, ".txt") || strings.HasSuffix(p, ".py") {
		pol.ContentType = "text/plain"
	}
	format.FetchAndServe(w, r, h.d, h.repo, p, pol)
}

// listing serves latest/revisions/files/search: proxies fetch upstream
// (short TTL, query string included), hosted derives from stored paths,
// groups merge.
func (h *handler) listing(w http.ResponseWriter, r *http.Request, p string) {
	q := r.URL.RawQuery
	var merged map[string]any
	for _, rp := range common.Members(r.Context(), h.d, h.repo) {
		var doc map[string]any
		switch rp.Type {
		case model.Proxy:
			up := p
			if q != "" {
				up += "?" + q
			}
			pol := repo.Policy{Kind: repo.Metadata, ContentType: "application/json", UpstreamPath: up}
			b, _, err := h.d.Engine.ReadAll(r.Context(), rp, strings.ReplaceAll(up, "?", "_"), pol, 16<<20)
			if err != nil {
				continue
			}
			if json.Unmarshal(b, &doc) != nil {
				continue
			}
		case model.Hosted:
			doc = h.hostedListing(r, rp, p)
			if doc == nil {
				continue
			}
		}
		if merged == nil {
			merged = doc
			continue
		}
		mergeListing(merged, doc)
	}
	if merged == nil {
		writeJSON(w, 404, map[string]any{"error": "not found"})
		return
	}
	writeJSON(w, 200, merged)
}

func mergeListing(dst, src map[string]any) {
	if revs, ok := src["revisions"].([]any); ok {
		seen := map[string]bool{}
		out, _ := dst["revisions"].([]any)
		for _, rv := range out {
			if m, ok := rv.(map[string]any); ok {
				seen[fmt.Sprint(m["revision"])] = true
			}
		}
		for _, rv := range revs {
			if m, ok := rv.(map[string]any); ok && !seen[fmt.Sprint(m["revision"])] {
				out = append(out, rv)
			}
		}
		dst["revisions"] = out
	}
	if files, ok := src["files"].(map[string]any); ok {
		cur, _ := dst["files"].(map[string]any)
		if cur == nil {
			cur = map[string]any{}
		}
		for k, v := range files {
			cur[k] = v
		}
		dst["files"] = cur
	}
	if res, ok := src["results"].([]any); ok {
		cur, _ := dst["results"].([]any)
		dst["results"] = append(cur, res...)
	}
	for k, v := range src {
		if _, ok := dst[k]; !ok {
			dst[k] = v
		}
	}
}

// hostedListing derives listings from stored asset paths.
func (h *handler) hostedListing(r *http.Request, rp *model.Repository, p string) map[string]any {
	ctx := r.Context()
	base := path.Dir(p)
	switch path.Base(p) {
	case "files":
		assets, _ := h.d.Content.ListAssets(ctx, rp.ID, base+"/files/", 1000)
		files := map[string]any{}
		for _, a := range assets {
			files[strings.TrimPrefix(a.Path, base+"/files/")] = map[string]any{}
		}
		if len(files) == 0 {
			return nil
		}
		return map[string]any{"files": files}
	case "revisions", "latest":
		assets, _ := h.d.Content.ListAssets(ctx, rp.ID, base+"/revisions/", 10000)
		type rev struct {
			id string
			t  time.Time
		}
		revs := map[string]rev{}
		for _, a := range assets {
			rest := strings.TrimPrefix(a.Path, base+"/revisions/")
			id := strings.SplitN(rest, "/", 2)[0]
			if cur, ok := revs[id]; !ok || a.UpdatedAt.After(cur.t) {
				revs[id] = rev{id, a.UpdatedAt}
			}
		}
		if len(revs) == 0 {
			return nil
		}
		var list []rev
		for _, rv := range revs {
			list = append(list, rv)
		}
		sort.Slice(list, func(i, j int) bool { return list[i].t.After(list[j].t) })
		if path.Base(p) == "latest" {
			return map[string]any{"revision": list[0].id, "time": list[0].t.UTC().Format(isoTime)}
		}
		var out []any
		for _, rv := range list {
			out = append(out, map[string]any{"revision": rv.id, "time": rv.t.UTC().Format(isoTime)})
		}
		return map[string]any{"revisions": out, "reference": referenceOf(base)}
	case "search":
		if strings.HasPrefix(p, "v2/conans/search") {
			// Reference search: v2/conans/search?q=<pattern>
			q := r.URL.Query().Get("q")
			// Patterns look like "name/*", "name/1.0@user/channel", "*" — match on the name part.
			namePart := strings.Trim(strings.SplitN(q, "/", 2)[0], "*")
			hits, _ := h.d.Content.Search(ctx, content.SearchQuery{Q: namePart, Repo: rp.Name, Format: Name, Limit: 500})
			seen := map[string]bool{}
			results := []any{}
			for _, hit := range hits {
				ref := hit.Name + "/" + hit.Version
				if hit.Namespace != "" {
					ref += "@" + hit.Namespace
				}
				if !seen[ref] {
					seen[ref] = true
					results = append(results, ref)
				}
			}
			return map[string]any{"results": results}
		}
		// Package search under a recipe revision: list package ids from stored conaninfo.txt.
		assets, _ := h.d.Content.ListAssets(ctx, rp.ID, base+"/packages/", 10000)
		pkgs := map[string]any{}
		for _, a := range assets {
			rest := strings.TrimPrefix(a.Path, base+"/packages/")
			id := strings.SplitN(rest, "/", 2)[0]
			if _, ok := pkgs[id]; !ok {
				pkgs[id] = map[string]any{"settings": map[string]any{}, "options": map[string]any{}, "requires": []any{}}
			}
		}
		return pkgs
	}
	return nil
}

// isoTime matches conan_server's datetime.isoformat() output.
const isoTime = "2006-01-02T15:04:05.000000+00:00"

func referenceOf(base string) string {
	segs := strings.Split(base, "/")
	if len(segs) < 6 {
		return ""
	}
	ref := segs[2] + "/" + segs[3]
	if segs[4] != "_" {
		ref += "@" + segs[4] + "/" + segs[5]
	}
	return ref
}

// put stores an upload. Uploads sent to a group reach here already routed to
// its first hosted member (server.groupDeploys).
func (h *handler) put(w http.ResponseWriter, r *http.Request, p string) {
	if h.repo.Type != model.Hosted {
		writeJSON(w, 400, map[string]any{"error": "only hosted remotes accept uploads"})
		return
	}
	cp, err := repo.CleanPath(p)
	if err != nil {
		format.MapError(w, err, h.d.Log)
		return
	}
	if r.Header.Get("X-Checksum-Deploy") == "true" {
		// Checksum-only deploys cannot be honoured (we key by sha256); tell
		// the client to send the bytes.
		writeJSON(w, 404, map[string]any{"error": "checksum deploy not supported"})
		return
	}
	pol := repo.PutOptions{ContentType: "application/octet-stream", Package: Format{}.Parse(cp), AllowRedeploy: true}
	if _, err := h.d.Engine.Put(r.Context(), h.repo, cp, r.Body, pol); err != nil {
		if errors.Is(err, repo.ErrRedeploy) {
			writeJSON(w, 409, map[string]any{"error": "already exists"})
			return
		}
		format.MapError(w, err, h.d.Log)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (h *handler) delete(w http.ResponseWriter, r *http.Request, p string) {
	if h.repo.Type == model.Group {
		for _, m := range common.Members(r.Context(), h.d, h.repo) {
			if m.Type == model.Hosted {
				(&handler{repo: m, d: h.d}).delete(w, r, p)
				return
			}
		}
	}
	// Deleting a recipe/revision/package removes every stored file under it.
	assets, _ := h.d.Content.ListAssets(r.Context(), h.repo.ID, strings.TrimSuffix(p, "/")+"/", 100000)
	n := 0
	for _, a := range assets {
		if h.d.Engine.Delete(r.Context(), h.repo, a.Path) == nil {
			n++
		}
	}
	if err := h.d.Engine.Delete(r.Context(), h.repo, p); err == nil {
		n++
	}
	if n == 0 {
		writeJSON(w, 404, map[string]any{"error": "not found"})
		return
	}
	w.WriteHeader(http.StatusOK)
	_ = io.Discard
}
