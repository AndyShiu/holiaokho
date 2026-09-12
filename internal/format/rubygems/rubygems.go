// Package rubygems implements RubyGems repositories: compact index
// (/versions, /info/<gem>, /names), gem downloads, legacy specs indexes and
// `gem push` for hosted repositories; proxies of rubygems.org; groups.
package rubygems

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
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

const Name = "rubygems"

type Format struct{}

func (Format) Name() string                                                           { return Name }
func (Format) ValidateAttributes(*model.Repository, map[string]json.RawMessage) error { return nil }
func (Format) VersionLess(a, b string) bool                                           { return gemVersionLess(a, b) }

func (Format) Parse(p string) *model.Package {
	if !strings.HasPrefix(p, "gems/") || !strings.HasSuffix(p, ".gem") {
		return nil
	}
	name, ver, plat := splitGemName(strings.TrimSuffix(path.Base(p), ".gem"))
	if name == "" {
		return nil
	}
	return &model.Package{Name: name, Version: ver, Namespace: plat}
}

// splitGemName splits "name-1.2.3[-platform]" at the first "-<digit>".
func splitGemName(base string) (name, ver, platform string) {
	for i := 1; i < len(base)-1; i++ {
		if base[i] == '-' && base[i+1] >= '0' && base[i+1] <= '9' {
			name, rest := base[:i], base[i+1:]
			// platform suffix (e.g. -x86_64-linux) starts at the first "-<letter>".
			for j := 0; j < len(rest)-1; j++ {
				if rest[j] == '-' && (rest[j+1] < '0' || rest[j+1] > '9') && rest[j+1] != '.' {
					return name, rest[:j], rest[j+1:]
				}
			}
			return name, rest, ""
		}
	}
	return "", "", ""
}

type handler struct {
	repo *model.Repository
	d    format.Deps
}

func (Format) Handler(r *model.Repository, d format.Deps) http.Handler { return &handler{r, d} }

const challenge = `Basic realm="Holiaokho RubyGems"`

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := strings.Trim(r.URL.Path, "/")
	// gem push sends "Authorization: <api key>" without a scheme.
	if a := r.Header.Get("Authorization"); a != "" && !strings.Contains(a, " ") && strings.HasPrefix(a, auth.TokenPrefix) {
		if pr, err := h.d.Auth.Login(r.Context(), auth.ClientIP(r), "", a); err == nil {
			r = r.WithContext(auth.WithPrincipal(r.Context(), pr))
		}
	}
	switch {
	case p == "api/v1/gems" && r.Method == http.MethodPost:
		if !format.Authorize(w, r, h.repo, auth.Write, challenge) {
			return
		}
		h.push(w, r)
		return
	case p == "api/v1/gems/yank" && (r.Method == http.MethodDelete || r.Method == http.MethodPost):
		if !format.Authorize(w, r, h.repo, auth.Delete, challenge) {
			return
		}
		h.yank(w, r)
		return
	case p == "api/v1/api_key" || p == "api/v1/api_key.yaml":
		// `gem signin`: return a user token as the API key.
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
		secret, _, err := h.d.Auth.CreateToken(r.Context(), pr.Username, "gem signin", nil)
		if err != nil {
			w.WriteHeader(500)
			return
		}
		if strings.HasSuffix(p, ".yaml") {
			format.ServeBytes(w, r, "text/yaml", []byte(":rubygems_api_key: "+secret+"\n"), time.Time{})
		} else {
			format.ServeBytes(w, r, "text/plain", []byte(secret), time.Time{})
		}
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		format.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if !format.Authorize(w, r, h.repo, auth.Read, challenge) {
		return
	}
	switch {
	case p == "versions":
		h.versions(w, r)
	case p == "names":
		h.names(w, r)
	case strings.HasPrefix(p, "info/"):
		h.info(w, r, strings.TrimPrefix(p, "info/"))
	case strings.HasPrefix(p, "gems/") && strings.HasSuffix(p, ".gem"):
		pol := repo.Policy{Kind: repo.Content, Immutable: true, ContentType: "application/octet-stream", Package: Format{}.Parse(p)}
		format.FetchAndServe(w, r, h.d, h.repo, p, pol)
	case strings.HasPrefix(p, "quick/") || strings.HasSuffix(p, "specs.4.8.gz") || p == "api/v1/dependencies":
		h.legacy(w, r, p)
	default:
		format.WriteError(w, http.StatusNotFound, "not_found", "not found")
	}
}

// ------------------------------------------------------------ gem records

// gemVersion is one version line of /info/<name>.
type gemVersion struct {
	Version  string
	Platform string
	Line     string // full compact-index line
}

// infoLines returns the compact-index lines for a gem from one repository.
func (h *handler) infoLines(ctx context.Context, rp *model.Repository, name string) ([]gemVersion, error) {
	switch rp.Type {
	case model.Hosted:
		pkgs, err := h.d.Content.Search(ctx, content.SearchQuery{Name: name, Repo: rp.Name, Format: Name, Limit: 500})
		if err != nil {
			return nil, err
		}
		if len(pkgs) == 0 {
			return nil, repo.ErrNotFound
		}
		var out []gemVersion
		for _, p := range pkgs {
			var gi GemInfo
			json.Unmarshal(p.Attrs, &gi)
			assets, _ := h.d.Content.PackageAssets(ctx, p.ID)
			sum := ""
			for _, a := range assets {
				if strings.HasSuffix(a.Path, ".gem") && a.BlobDigest != nil {
					sum = strings.TrimPrefix(*a.BlobDigest, "sha256:")
				}
			}
			var deps []string
			for _, d := range gi.Dependencies {
				if d.Type == "runtime" || d.Type == "" {
					req := strings.Join(d.Requirement, "&")
					if req == "" {
						req = ">= 0"
					}
					deps = append(deps, d.Name+":"+req)
				}
			}
			ver := p.Version
			if p.Namespace != "" && p.Namespace != "ruby" {
				ver += "-" + p.Namespace
			}
			line := ver + " " + strings.Join(deps, ",") + "|checksum:" + sum
			if gi.RequiredRuby != "" {
				line += ",ruby:" + gi.RequiredRuby
			}
			if gi.RequiredRG != "" {
				line += ",rubygems:" + gi.RequiredRG
			}
			out = append(out, gemVersion{Version: p.Version, Platform: p.Namespace, Line: line})
		}
		return out, nil
	case model.Proxy:
		pol := repo.Policy{Kind: repo.Metadata, ContentType: "text/plain"}
		b, _, err := h.d.Engine.ReadAll(ctx, rp, "info/"+name, pol, 64<<20)
		if err != nil {
			return nil, err
		}
		var out []gemVersion
		for _, line := range strings.Split(string(b), "\n") {
			if line == "---" || strings.TrimSpace(line) == "" {
				continue
			}
			v, _, _ := strings.Cut(line, " ")
			ver, plat := v, ""
			if i := strings.IndexByte(v, '-'); i > 0 {
				ver, plat = v[:i], v[i+1:]
			}
			out = append(out, gemVersion{Version: ver, Platform: plat, Line: line})
		}
		return out, nil
	case model.Group:
		seen := map[string]bool{}
		var out []gemVersion
		for _, m := range common.Members(ctx, h.d, rp) {
			ls, err := h.infoLines(ctx, m, name)
			if err != nil {
				continue
			}
			for _, l := range ls {
				k := l.Version + "-" + l.Platform
				if !seen[k] {
					seen[k] = true
					out = append(out, l)
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

func renderInfo(lines []gemVersion) []byte {
	var b bytes.Buffer
	b.WriteString("---\n")
	for _, l := range lines {
		b.WriteString(l.Line + "\n")
	}
	return b.Bytes()
}

func (h *handler) info(w http.ResponseWriter, r *http.Request, name string) {
	if h.repo.Type == model.Proxy {
		// Serve the upstream document verbatim (supports Range/ETag for bundler).
		format.FetchAndServe(w, r, h.d, h.repo, "info/"+name, repo.Policy{Kind: repo.Metadata, ContentType: "text/plain; charset=utf-8"})
		return
	}
	lines, err := h.infoLines(r.Context(), h.repo, name)
	if err != nil {
		format.MapError(w, err, h.d.Log)
		return
	}
	format.ServeBytes(w, r, "text/plain; charset=utf-8", renderInfo(lines), time.Time{})
}

// versions builds the /versions document. Proxies pass the upstream file
// through; hosted/group documents are generated.
func (h *handler) versions(w http.ResponseWriter, r *http.Request) {
	if h.repo.Type == model.Proxy {
		format.FetchAndServe(w, r, h.d, h.repo, "versions", repo.Policy{Kind: repo.Metadata, ContentType: "text/plain; charset=utf-8"})
		return
	}
	// Collect gem names: hosted from DB; proxy members from their /versions.
	names := map[string]bool{}
	var proxyDoc []byte
	for _, m := range common.Members(r.Context(), h.d, h.repo) {
		switch m.Type {
		case model.Hosted:
			hits, _ := h.d.Content.Search(r.Context(), content.SearchQuery{Repo: m.Name, Format: Name, Limit: 500})
			for _, hit := range hits {
				names[hit.Name] = true
			}
		case model.Proxy:
			if proxyDoc == nil {
				b, _, err := h.d.Engine.ReadAll(r.Context(), m, "versions", repo.Policy{Kind: repo.Metadata}, 256<<20)
				if err == nil {
					proxyDoc = b
				}
			}
		}
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "created_at: %s\n---\n", time.Now().UTC().Format(time.RFC3339))
	var sorted []string
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)
	for _, n := range sorted {
		lines, err := h.infoLines(r.Context(), h.repo, n)
		if err != nil {
			continue
		}
		var vs []string
		for _, l := range lines {
			v := l.Version
			if l.Platform != "" && l.Platform != "ruby" {
				v += "-" + l.Platform
			}
			vs = append(vs, v)
		}
		sum := md5.Sum(renderInfo(lines))
		fmt.Fprintf(&b, "%s %s %s\n", n, strings.Join(vs, ","), hex.EncodeToString(sum[:]))
	}
	if proxyDoc != nil {
		// Append upstream entries (after its header) that are not local gems.
		body := string(proxyDoc)
		if i := strings.Index(body, "\n---\n"); i >= 0 {
			body = body[i+5:]
		}
		for _, line := range strings.Split(body, "\n") {
			n, _, _ := strings.Cut(line, " ")
			if n != "" && !names[n] {
				b.WriteString(line + "\n")
			}
		}
	}
	format.ServeBytes(w, r, "text/plain; charset=utf-8", b.Bytes(), time.Time{})
}

func (h *handler) names(w http.ResponseWriter, r *http.Request) {
	if h.repo.Type == model.Proxy {
		format.FetchAndServe(w, r, h.d, h.repo, "names", repo.Policy{Kind: repo.Metadata, ContentType: "text/plain; charset=utf-8"})
		return
	}
	names := map[string]bool{}
	for _, m := range common.Members(r.Context(), h.d, h.repo) {
		if m.Type == model.Hosted {
			hits, _ := h.d.Content.Search(r.Context(), content.SearchQuery{Repo: m.Name, Format: Name, Limit: 500})
			for _, hit := range hits {
				names[hit.Name] = true
			}
		}
	}
	var sorted []string
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)
	format.ServeBytes(w, r, "text/plain; charset=utf-8", []byte("---\n"+strings.Join(sorted, "\n")+"\n"), time.Time{})
}

// legacy serves Marshal-based indexes: proxies pass through, hosted
// repositories generate specs.4.8.gz variants and quick gemspecs.
func (h *handler) legacy(w http.ResponseWriter, r *http.Request, p string) {
	if h.repo.Type == model.Proxy {
		pol := repo.Policy{Kind: repo.Metadata, ContentType: "application/octet-stream"}
		if strings.HasPrefix(p, "quick/") {
			pol.Kind, pol.Immutable = repo.Content, true
		}
		format.FetchAndServe(w, r, h.d, h.repo, p, pol)
		return
	}
	if strings.HasPrefix(p, "quick/") {
		h.quick(w, r, p)
		return
	}
	if !strings.HasSuffix(p, "specs.4.8.gz") {
		format.WriteError(w, http.StatusNotFound, "not_found", "not available for hosted repositories (use the compact index)")
		return
	}
	var specs [][3]string
	latest := map[string]string{}
	for _, m := range common.Members(r.Context(), h.d, h.repo) {
		if m.Type != model.Hosted {
			continue
		}
		hits, _ := h.d.Content.Search(r.Context(), content.SearchQuery{Repo: m.Name, Format: Name, Limit: 500})
		for _, hit := range hits {
			plat := hit.Namespace
			if plat == "" {
				plat = "ruby"
			}
			pre := strings.ContainsAny(hit.Version, "abcdefghijklmnopqrstuvwxyz")
			switch {
			case strings.HasPrefix(p, "prerelease") && pre, p == "specs.4.8.gz" && !pre:
				specs = append(specs, [3]string{hit.Name, hit.Version, plat})
			case strings.HasPrefix(p, "latest") && !pre:
				if cur, ok := latest[hit.Name]; !ok || gemVersionLess(cur, hit.Version) {
					latest[hit.Name] = hit.Version
				}
			}
		}
	}
	if strings.HasPrefix(p, "latest") {
		for n, v := range latest {
			specs = append(specs, [3]string{n, v, "ruby"})
		}
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i][0] < specs[j][0] })
	format.ServeBytes(w, r, "application/gzip", SpecsIndex(specs), time.Time{})
}

// quick serves quick/Marshal.4.8/<name>-<ver>[-plat].gemspec.rz for hosted
// gems (generated) or from proxy members (fetched), whichever has the gem.
func (h *handler) quick(w http.ResponseWriter, r *http.Request, p string) {
	base := strings.TrimSuffix(path.Base(p), ".gemspec.rz")
	name, ver, plat := splitGemName(base)
	for _, m := range common.Members(r.Context(), h.d, h.repo) {
		switch m.Type {
		case model.Hosted:
			pk, err := h.d.Content.Package(r.Context(), m.ID, plat, name, ver)
			if err != nil {
				continue
			}
			var gi GemInfo
			json.Unmarshal(pk.Attrs, &gi)
			if gi.Name == "" {
				continue
			}
			format.ServeBytes(w, r, "application/octet-stream", GemspecRz(&gi, pk.CreatedAt), pk.CreatedAt)
			return
		case model.Proxy:
			res, err := h.d.Engine.Fetch(r.Context(), m, p, repo.Policy{Kind: repo.Content, Immutable: true, ContentType: "application/octet-stream"})
			if err == nil {
				format.ServeResult(w, r, h.d, res)
				return
			}
		}
	}
	format.WriteError(w, http.StatusNotFound, "not_found", "gemspec not found")
}

// ------------------------------------------------------------------ push

func (h *handler) push(w http.ResponseWriter, r *http.Request) {
	if h.repo.Type != model.Hosted {
		format.WriteError(w, http.StatusBadRequest, "repo.read_only", "only hosted repositories accept pushes")
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, 512<<20))
	if err != nil {
		format.WriteError(w, http.StatusBadRequest, "body.invalid", "read body")
		return
	}
	gi, err := ReadGem(data)
	if err != nil {
		format.WriteError(w, http.StatusBadRequest, "gem.invalid", "%s", err.Error())
		return
	}
	file := gi.Name + "-" + gi.Version
	plat := ""
	if gi.Platform != "" && gi.Platform != "ruby" {
		file += "-" + gi.Platform
		plat = gi.Platform
	}
	attrs, _ := json.Marshal(gi)
	pkg := &model.Package{Name: gi.Name, Version: gi.Version, Namespace: plat, Attrs: attrs}
	if _, err := h.d.Engine.Put(r.Context(), h.repo, "gems/"+file+".gem", bytes.NewReader(data), repo.PutOptions{ContentType: "application/octet-stream", Package: pkg}); err != nil {
		if errors.Is(err, repo.ErrRedeploy) {
			format.WriteError(w, http.StatusConflict, "gem.exists", "gem version already exists")
			return
		}
		format.MapError(w, err, h.d.Log)
		return
	}
	format.ServeBytes(w, r, "text/plain", []byte(fmt.Sprintf("Successfully registered gem: %s (%s)\n", gi.Name, gi.Version)), time.Time{})
}

func (h *handler) yank(w http.ResponseWriter, r *http.Request) {
	name, ver := r.URL.Query().Get("gem_name"), r.URL.Query().Get("version")
	if name == "" || ver == "" {
		r.ParseForm()
		name, ver = r.Form.Get("gem_name"), r.Form.Get("version")
	}
	plat := r.URL.Query().Get("platform")
	p, err := h.d.Content.Package(r.Context(), h.repo.ID, plat, name, ver)
	if err != nil {
		format.WriteError(w, http.StatusNotFound, "not_found", "gem not found")
		return
	}
	h.d.Content.DeletePackage(r.Context(), p.ID)
	format.ServeBytes(w, r, "text/plain", []byte("Successfully yanked gem: "+name+" ("+ver+")\n"), time.Time{})
}

// gemVersionLess orders versions per Gem::Version (numeric segments,
// letters mark prereleases that sort before the release).
func gemVersionLess(a, b string) bool {
	sa, sb := gemSegments(a), gemSegments(b)
	for i := 0; i < len(sa) || i < len(sb); i++ {
		var x, y string
		if i < len(sa) {
			x = sa[i]
		}
		if i < len(sb) {
			y = sb[i]
		}
		if x == y {
			continue
		}
		xi, xe := atoi(x)
		yi, ye := atoi(y)
		switch {
		case x == "":
			return ye != nil // missing < prerelease? Ruby: "1.0" > "1.0.a"
		case y == "":
			return xe == nil && false || xe != nil
		case xe == nil && ye == nil:
			return xi < yi
		case xe == nil:
			return false
		case ye == nil:
			return true
		}
		return x < y
	}
	return false
}

func gemSegments(v string) []string {
	var segs []string
	cur := ""
	digit := func(c byte) bool { return c >= '0' && c <= '9' }
	for i := 0; i < len(v); i++ {
		c := v[i]
		if c == '.' || c == '-' {
			if cur != "" {
				segs = append(segs, cur)
				cur = ""
			}
			continue
		}
		if cur != "" && digit(cur[len(cur)-1]) != digit(c) {
			segs = append(segs, cur)
			cur = ""
		}
		cur += string(c)
	}
	if cur != "" {
		segs = append(segs, cur)
	}
	return segs
}

func atoi(s string) (int, error) {
	if s == "" {
		return 0, errors.New("empty")
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errors.New("nan")
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}
