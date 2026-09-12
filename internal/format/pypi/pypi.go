// Package pypi implements the Python package index protocol (PEP 503 simple
// index) for hosted (twine upload), proxy (pypi.org) and group repositories.
package pypi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/holiaokho/holiaokho/internal/auth"
	"github.com/holiaokho/holiaokho/internal/format"
	"github.com/holiaokho/holiaokho/internal/model"
	"github.com/holiaokho/holiaokho/internal/repo"
)

const Name = "pypi"

const challenge = `Basic realm="Holiaokho PyPI"`

var normRe = regexp.MustCompile(`[-_.]+`)

// Normalize applies PEP 503 name normalisation.
func Normalize(name string) string { return strings.ToLower(normRe.ReplaceAllString(name, "-")) }

type Format struct{}

func (Format) Name() string                                                           { return Name }
func (Format) ValidateAttributes(*model.Repository, map[string]json.RawMessage) error { return nil }
func (Format) VersionLess(a, b string) bool                                           { return pep440Less(a, b) }

// Parse maps "packages/<project>/<file>" to a package.
func (Format) Parse(p string) *model.Package {
	segs := strings.Split(strings.Trim(p, "/"), "/")
	if len(segs) != 3 || segs[0] != "packages" {
		return nil
	}
	name, ver := parseFilename(segs[2])
	if ver == "" {
		return nil
	}
	return packageFor(name, ver)
}

func packageFor(name, version string) *model.Package {
	attrs, _ := json.Marshal(map[string]any{"name": name})
	return &model.Package{Name: Normalize(name), Version: version, Attrs: attrs}
}

// parseFilename extracts project name and version from a wheel or sdist name.
func parseFilename(f string) (name, version string) {
	switch {
	case strings.HasSuffix(f, ".whl"):
		parts := strings.Split(strings.TrimSuffix(f, ".whl"), "-")
		if len(parts) < 2 {
			return "", ""
		}
		return parts[0], parts[1]
	default:
		for _, ext := range []string{".tar.gz", ".zip", ".tar.bz2", ".tgz", ".egg"} {
			if strings.HasSuffix(f, ext) {
				stem := strings.TrimSuffix(f, ext)
				i := strings.LastIndexByte(stem, '-')
				if i <= 0 {
					return "", ""
				}
				return stem[:i], stem[i+1:]
			}
		}
	}
	return "", ""
}

type handler struct {
	repo *model.Repository
	d    format.Deps
}

func (Format) Handler(r *model.Repository, d format.Deps) http.Handler { return &handler{r, d} }

func (h *handler) base(r *http.Request) string {
	return h.d.BaseURL(r) + "/repository/" + h.repo.Name
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := strings.Trim(r.URL.Path, "/")
	switch {
	case r.Method == http.MethodPost && (p == "" || p == "legacy"):
		if !format.Authorize(w, r, h.repo, auth.Write, challenge) {
			return
		}
		h.upload(w, r)
	case (r.Method == http.MethodGet || r.Method == http.MethodHead) && (p == "simple" || strings.HasPrefix(p, "simple/")):
		if !format.Authorize(w, r, h.repo, auth.Read, challenge) {
			return
		}
		project := strings.Trim(strings.TrimPrefix(p, "simple"), "/")
		if project == "" {
			h.rootIndex(w, r)
			return
		}
		h.projectIndex(w, r, project)
	case (r.Method == http.MethodGet || r.Method == http.MethodHead) && strings.HasPrefix(p, "packages/"):
		if !format.Authorize(w, r, h.repo, auth.Read, challenge) {
			return
		}
		h.file(w, r, p)
	case r.Method == http.MethodGet && p == "":
		http.Redirect(w, r, r.URL.Path+"simple/", http.StatusFound)
	default:
		format.WriteError(w, http.StatusNotFound, "not_found", "not found")
	}
}

// ------------------------------------------------------------------ links

type link struct {
	File     string
	URL      string // absolute upstream URL or our path
	Hash     string // "sha256=<hex>"
	Requires string // data-requires-python
	Yanked   string
}

var anchorRe = regexp.MustCompile(`(?is)<a\s+([^>]*)>([^<]+)</a>`)
var attrRe = regexp.MustCompile(`([a-zA-Z-]+)\s*=\s*"([^"]*)"`)

func parseSimple(b []byte) []link {
	var out []link
	for _, m := range anchorRe.FindAllSubmatch(b, -1) {
		attrs := map[string]string{}
		for _, a := range attrRe.FindAllSubmatch(m[1], -1) {
			attrs[strings.ToLower(string(a[1]))] = html.UnescapeString(string(a[2]))
		}
		href := attrs["href"]
		if href == "" {
			continue
		}
		l := link{File: strings.TrimSpace(html.UnescapeString(string(m[2]))), Requires: attrs["data-requires-python"], Yanked: attrs["data-yanked"]}
		if i := strings.IndexByte(href, '#'); i >= 0 {
			l.Hash = href[i+1:]
			href = href[:i]
		}
		l.URL = href
		out = append(out, l)
	}
	return out
}

func renderSimple(project string, links []link, fileURL func(link) string) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "<!DOCTYPE html><html><head><meta name=\"pypi:repository-version\" content=\"1.0\"><title>Links for %s</title></head><body><h1>Links for %s</h1>\n", html.EscapeString(project), html.EscapeString(project))
	for _, l := range links {
		u := fileURL(l)
		if l.Hash != "" {
			u += "#" + l.Hash
		}
		extra := ""
		if l.Requires != "" {
			extra += fmt.Sprintf(` data-requires-python="%s"`, html.EscapeString(l.Requires))
		}
		if l.Yanked != "" {
			extra += fmt.Sprintf(` data-yanked="%s"`, html.EscapeString(l.Yanked))
		}
		fmt.Fprintf(&b, "<a href=\"%s\"%s>%s</a><br/>\n", html.EscapeString(u), extra, html.EscapeString(l.File))
	}
	b.WriteString("</body></html>\n")
	return b.Bytes()
}

// projectLinks returns the links for a project from one repository, with
// URLs pointing at that repository's packages/ path. Upstream absolute URLs
// are remembered in the stored index so file requests can be resolved.
func (h *handler) projectLinks(ctx context.Context, rp *model.Repository, project string) ([]link, time.Time, error) {
	project = Normalize(project)
	switch rp.Type {
	case model.Hosted:
		assets, err := h.d.Content.ListAssets(ctx, rp.ID, "packages/"+project+"/", 10000)
		if err != nil {
			return nil, time.Time{}, err
		}
		if len(assets) == 0 {
			return nil, time.Time{}, repo.ErrNotFound
		}
		var out []link
		var latest time.Time
		for _, a := range assets {
			l := link{File: path.Base(a.Path), URL: "packages/" + project + "/" + path.Base(a.Path)}
			if a.BlobDigest != nil {
				l.Hash = "sha256=" + strings.TrimPrefix(*a.BlobDigest, "sha256:")
			}
			var at map[string]any
			json.Unmarshal(a.Attrs, &at)
			if rp, _ := at["requiresPython"].(string); rp != "" {
				l.Requires = rp
			}
			if a.UpdatedAt.After(latest) {
				latest = a.UpdatedAt
			}
			out = append(out, l)
		}
		return out, latest, nil
	case model.Proxy:
		pol := repo.Policy{Kind: repo.Metadata, ContentType: "text/html", UpstreamPath: "simple/" + project + "/",
			Headers: http.Header{"Accept": []string{"application/vnd.pypi.simple.v1+html, text/html;q=0.9"}}}
		b, res, err := h.d.Engine.ReadAll(ctx, rp, "simple/"+project+"/index.html", pol, 16<<20)
		if err != nil {
			return nil, time.Time{}, err
		}
		var t time.Time
		if res != nil && res.Asset != nil {
			t = res.Asset.UpdatedAt
		}
		links := parseSimple(b)
		// Resolve relative upstream hrefs against the upstream index URL.
		baseURL, _ := url.Parse(strings.TrimSuffix(rp.Proxy.RemoteURL, "/") + "/simple/" + project + "/")
		for i := range links {
			if u, err := url.Parse(links[i].URL); err == nil && baseURL != nil {
				links[i].URL = baseURL.ResolveReference(u).String()
			}
		}
		return links, t, nil
	case model.Group:
		members, _ := h.d.Engine.Members(ctx, rp)
		seen := map[string]bool{}
		var out []link
		var latest time.Time
		for _, m := range members {
			ls, t, err := h.projectLinks(ctx, m, project)
			if err != nil {
				continue
			}
			if t.After(latest) {
				latest = t
			}
			for _, l := range ls {
				if !seen[l.File] {
					seen[l.File] = true
					out = append(out, l)
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

func (h *handler) projectIndex(w http.ResponseWriter, r *http.Request, project string) {
	norm := Normalize(project)
	if norm != project {
		http.Redirect(w, r, "../"+norm+"/", http.StatusMovedPermanently)
		return
	}
	links, t, err := h.projectLinks(r.Context(), h.repo, norm)
	if err != nil {
		format.MapError(w, err, h.d.Log)
		return
	}
	sort.Slice(links, func(i, j int) bool { return links[i].File < links[j].File })
	base := h.base(r)
	body := renderSimple(norm, links, func(l link) string { return base + "/packages/" + norm + "/" + l.File })
	format.ServeBytes(w, r, "text/html; charset=utf-8", body, t)
}

func (h *handler) rootIndex(w http.ResponseWriter, r *http.Request) {
	// Only local projects are enumerable; proxies would need the full upstream index.
	repos := []*model.Repository{h.repo}
	if h.repo.Type == model.Group {
		repos, _ = h.d.Engine.Members(r.Context(), h.repo)
	}
	names := map[string]bool{}
	for _, rp := range repos {
		if rp.Type != model.Hosted {
			continue
		}
		ds, _, _ := h.d.Content.ListChildren(r.Context(), rp.ID, "packages")
		for _, d := range ds {
			names[d] = true
		}
	}
	var list []string
	for n := range names {
		list = append(list, n)
	}
	sort.Strings(list)
	var b bytes.Buffer
	b.WriteString("<!DOCTYPE html><html><head><meta name=\"pypi:repository-version\" content=\"1.0\"><title>Simple index</title></head><body>\n")
	for _, n := range list {
		fmt.Fprintf(&b, "<a href=\"%s/\">%s</a><br/>\n", n, n)
	}
	b.WriteString("</body></html>\n")
	format.ServeBytes(w, r, "text/html; charset=utf-8", b.Bytes(), time.Time{})
}

// ------------------------------------------------------------------ files

// file serves packages/<project>/<file>. For proxies the upstream URL is
// looked up in the (cached) project index.
func (h *handler) file(w http.ResponseWriter, r *http.Request, p string) {
	segs := strings.Split(p, "/")
	if len(segs) != 3 {
		format.WriteError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	project, file := Normalize(segs[1]), segs[2]
	name, ver := parseFilename(file)
	pol := repo.Policy{Kind: repo.Content, Immutable: true, ContentType: contentType(file)}
	if ver != "" {
		pol.Package = packageFor(name, ver)
	}
	target := h.repo
	if h.repo.Type == model.Group {
		// Find the member that lists this file.
		members, _ := h.d.Engine.Members(r.Context(), h.repo)
		target = nil
		for _, m := range members {
			if m.Type == model.Hosted {
				if _, err := h.d.Content.Asset(r.Context(), m.ID, p); err == nil {
					target = m
					break
				}
				continue
			}
			ls, _, err := h.projectLinks(r.Context(), m, project)
			if err != nil {
				continue
			}
			for _, l := range ls {
				if l.File == file {
					target = m
					pol.UpstreamPath = l.URL
					break
				}
			}
			if target != nil {
				break
			}
		}
		if target == nil {
			format.WriteError(w, http.StatusNotFound, "not_found", "not found")
			return
		}
	} else if h.repo.Type == model.Proxy {
		if _, err := h.d.Content.Asset(r.Context(), h.repo.ID, p); err != nil {
			ls, _, err := h.projectLinks(r.Context(), h.repo, project)
			if err != nil {
				format.MapError(w, err, h.d.Log)
				return
			}
			for _, l := range ls {
				if l.File == file {
					pol.UpstreamPath = l.URL
				}
			}
			if pol.UpstreamPath == "" {
				format.WriteError(w, http.StatusNotFound, "not_found", "not found")
				return
			}
		}
	}
	format.FetchAndServe(w, r, h.d, target, p, pol)
}

func contentType(f string) string {
	switch {
	case strings.HasSuffix(f, ".whl"), strings.HasSuffix(f, ".zip"):
		return "application/zip"
	case strings.HasSuffix(f, ".tar.gz"), strings.HasSuffix(f, ".tgz"):
		return "application/gzip"
	}
	return "application/octet-stream"
}

// ----------------------------------------------------------------- upload

// upload implements the legacy twine/setuptools multipart upload.
func (h *handler) upload(w http.ResponseWriter, r *http.Request) {
	if h.repo.Type != model.Hosted {
		format.WriteError(w, http.StatusBadRequest, "repo.read_only", "only hosted repositories accept uploads")
		return
	}
	if err := r.ParseMultipartForm(256 << 20); err != nil {
		format.WriteError(w, http.StatusBadRequest, "body.invalid", "multipart form expected")
		return
	}
	if act := r.FormValue(":action"); act != "" && act != "file_upload" {
		format.WriteError(w, http.StatusBadRequest, "pypi.action", "unsupported action %s", act)
		return
	}
	f, hdr, err := r.FormFile("content")
	if err != nil {
		format.WriteError(w, http.StatusBadRequest, "body.invalid", "content file required")
		return
	}
	defer f.Close()
	name := r.FormValue("name")
	version := r.FormValue("version")
	if name == "" || version == "" {
		name, version = parseFilename(hdr.Filename)
	}
	if name == "" {
		format.WriteError(w, http.StatusBadRequest, "pypi.name", "cannot determine project name")
		return
	}
	project := Normalize(name)
	p := "packages/" + project + "/" + hdr.Filename
	attrs := map[string]any{"name": name, "version": version}
	if rp := r.FormValue("requires_python"); rp != "" {
		attrs["requiresPython"] = rp
	}
	if s := r.FormValue("summary"); s != "" {
		attrs["summary"] = s
	}
	pkg := packageFor(name, version)
	pkg.Attrs, _ = json.Marshal(attrs)
	var expected string
	if d := r.FormValue("sha256_digest"); len(d) == 64 {
		expected = "sha256:" + strings.ToLower(d)
	}
	opt := repo.PutOptions{ContentType: contentType(hdr.Filename), Package: pkg, Attrs: attrs}
	if expected != "" {
		opt.Digest = digest(expected)
	}
	if _, err := h.d.Engine.Put(r.Context(), h.repo, p, f, opt); err != nil {
		if errors.Is(err, repo.ErrRedeploy) {
			format.WriteError(w, http.StatusConflict, "pypi.exists", "file already exists")
			return
		}
		format.MapError(w, err, h.d.Log)
		return
	}
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, "OK")
}
