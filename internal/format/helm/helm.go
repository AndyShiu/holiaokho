// Package helm implements Helm chart repositories (index.yaml + .tgz
// charts): proxy of upstream repos with URL rewriting, hosted uploads
// (ChartMuseum-style POST /api/charts or PUT <name>-<ver>.tgz) with a
// generated index, and group merging. OCI charts are served by the Docker
// format.
package helm

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
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

const Name = "helm"

type Format struct{}

func (Format) Name() string                                                           { return Name }
func (Format) ValidateAttributes(*model.Repository, map[string]json.RawMessage) error { return nil }
func (Format) VersionLess(a, b string) bool                                           { return semverLess(a, b) }

var chartRe = regexp.MustCompile(`^(.*?)-(\d+\.\d+\.\d+[^/]*)\.tgz$`)

func (Format) Parse(p string) *model.Package {
	m := chartRe.FindStringSubmatch(path.Base(p))
	if m == nil {
		return nil
	}
	return &model.Package{Name: m[1], Version: m[2]}
}

// Entry is one chart version in index.yaml.
type Entry struct {
	Name         string              `yaml:"name" json:"name"`
	Version      string              `yaml:"version" json:"version"`
	AppVersion   string              `yaml:"appVersion,omitempty" json:"appVersion,omitempty"`
	Description  string              `yaml:"description,omitempty" json:"description,omitempty"`
	APIVersion   string              `yaml:"apiVersion,omitempty" json:"apiVersion,omitempty"`
	Type         string              `yaml:"type,omitempty" json:"type,omitempty"`
	Digest       string              `yaml:"digest,omitempty" json:"digest,omitempty"`
	URLs         []string            `yaml:"urls" json:"urls"`
	Created      time.Time           `yaml:"created,omitempty" json:"created,omitempty"`
	Keywords     []string            `yaml:"keywords,omitempty" json:"keywords,omitempty"`
	Home         string              `yaml:"home,omitempty" json:"home,omitempty"`
	Icon         string              `yaml:"icon,omitempty" json:"icon,omitempty"`
	Deprecated   bool                `yaml:"deprecated,omitempty" json:"deprecated,omitempty"`
	Sources      []string            `yaml:"sources,omitempty" json:"sources,omitempty"`
	Annotations  map[string]string   `yaml:"annotations,omitempty" json:"annotations,omitempty"`
	Maintainers  []map[string]string `yaml:"maintainers,omitempty" json:"maintainers,omitempty"`
	Dependencies []map[string]any    `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`
	KubeVersion  string              `yaml:"kubeVersion,omitempty" json:"kubeVersion,omitempty"`
}

type Index struct {
	APIVersion string             `yaml:"apiVersion"`
	Entries    map[string][]Entry `yaml:"entries"`
	Generated  time.Time          `yaml:"generated"`
}

type handler struct {
	repo *model.Repository
	d    format.Deps
	m    *common.Mirror
}

func (Format) Handler(r *model.Repository, d format.Deps) http.Handler {
	h := &handler{repo: r, d: d}
	h.m = &common.Mirror{Repo: r, Deps: d, Challenge: `Basic realm="Holiaokho Helm"`,
		IsMetadata:  func(p string) bool { return p == "index.yaml" },
		ContentType: common.ContentTypeByExt,
		Parse:       Format{}.Parse,
		Rewrite:     h.rewrite,
		Generate:    h.generate,
	}
	return h
}

func (h *handler) base(r *http.Request) string { return h.d.BaseURL(r) + "/repository/" + h.repo.Name }

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := strings.Trim(r.URL.Path, "/")
	// ChartMuseum-compatible upload: POST /api/charts with the .tgz body.
	if r.Method == http.MethodPost && (p == "api/charts" || p == "charts") {
		if !format.Authorize(w, r, h.repo, auth.Write, h.m.Challenge) {
			return
		}
		h.upload(w, r)
		return
	}
	if r.Method == http.MethodGet && strings.HasPrefix(p, "api/charts") {
		if !format.Authorize(w, r, h.repo, auth.Read, h.m.Challenge) {
			return
		}
		h.apiCharts(w, r, strings.TrimPrefix(strings.TrimPrefix(p, "api/charts"), "/"))
		return
	}
	// Proxied charts referenced by absolute upstream URL live under charts/<file>.
	if (r.Method == http.MethodGet || r.Method == http.MethodHead) && h.repo.Type != model.Hosted && strings.HasSuffix(p, ".tgz") {
		if !format.Authorize(w, r, h.repo, auth.Read, h.m.Challenge) {
			return
		}
		h.chart(w, r, p)
		return
	}
	h.m.Serve(w, r)
}

// ------------------------------------------------------------------ index

// readIndex returns the parsed index.yaml of a repository (hosted: generated).
func (h *handler) readIndex(ctx context.Context, rp *model.Repository) (*Index, error) {
	if rp.Type == model.Hosted {
		return h.hostedIndex(ctx, rp)
	}
	pol := repo.Policy{Kind: repo.Metadata, ContentType: "application/yaml"}
	b, _, err := h.d.Engine.ReadAll(ctx, rp, "index.yaml", pol, 256<<20)
	if err != nil {
		return nil, err
	}
	var idx Index
	if err := yaml.Unmarshal(b, &idx); err != nil {
		return nil, err
	}
	// Resolve relative chart URLs against the upstream base.
	if rp.Type == model.Proxy {
		base, _ := url.Parse(strings.TrimSuffix(rp.Proxy.RemoteURL, "/") + "/")
		for name, es := range idx.Entries {
			for i := range es {
				for j, u := range es[i].URLs {
					if pu, err := url.Parse(u); err == nil && base != nil && !pu.IsAbs() {
						es[i].URLs[j] = base.ResolveReference(pu).String()
					}
				}
			}
			idx.Entries[name] = es
		}
	}
	return &idx, nil
}

func (h *handler) hostedIndex(ctx context.Context, rp *model.Repository) (*Index, error) {
	assets, err := h.d.Content.ListAssets(ctx, rp.ID, "", 100000)
	if err != nil {
		return nil, err
	}
	idx := &Index{APIVersion: "v1", Entries: map[string][]Entry{}, Generated: time.Now().UTC()}
	for _, a := range assets {
		if !strings.HasSuffix(a.Path, ".tgz") || a.PackageID == nil {
			continue
		}
		var e Entry
		json.Unmarshal(a.Attrs, &e)
		if e.Name == "" {
			m := chartRe.FindStringSubmatch(path.Base(a.Path))
			if m == nil {
				continue
			}
			e.Name, e.Version = m[1], m[2]
		}
		e.URLs = []string{a.Path}
		if a.BlobDigest != nil {
			e.Digest = strings.TrimPrefix(*a.BlobDigest, "sha256:")
		}
		e.Created = a.CreatedAt
		idx.Entries[e.Name] = append(idx.Entries[e.Name], e)
	}
	return idx, nil
}

// merged returns the index for this repository with URLs rewritten to us.
// Proxy entries keep the upstream URL in attrs so chart() can resolve it.
func (h *handler) merged(r *http.Request) (*Index, error) {
	out := &Index{APIVersion: "v1", Entries: map[string][]Entry{}, Generated: time.Now().UTC()}
	base := h.base(r)
	seen := map[string]bool{}
	var firstErr error
	for _, rp := range common.Members(r.Context(), h.d, h.repo) {
		idx, err := h.readIndex(r.Context(), rp)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for name, es := range idx.Entries {
			for _, e := range es {
				k := name + "\x00" + e.Version
				if seen[k] {
					continue
				}
				seen[k] = true
				if len(e.URLs) > 0 {
					file := path.Base(e.URLs[0])
					if rp.Type == model.Hosted {
						e.URLs = []string{base + "/" + strings.TrimPrefix(e.URLs[0], "/")}
					} else {
						e.URLs = []string{base + "/charts/" + file}
					}
				}
				out.Entries[name] = append(out.Entries[name], e)
			}
		}
	}
	if len(out.Entries) == 0 && firstErr != nil {
		return nil, firstErr
	}
	for name := range out.Entries {
		es := out.Entries[name]
		sort.SliceStable(es, func(i, j int) bool { return semverLess(es[j].Version, es[i].Version) })
		out.Entries[name] = es
	}
	return out, nil
}

func (h *handler) rewrite(r *http.Request, p string, body []byte) []byte {
	idx, err := h.merged(r)
	if err != nil {
		return body
	}
	b, _ := yaml.Marshal(idx)
	return b
}

func (h *handler) generate(ctx context.Context, rp *model.Repository, p string) ([]byte, bool) {
	if p != "index.yaml" {
		return nil, false
	}
	if rp.Type == model.Hosted {
		idx, err := h.hostedIndex(ctx, rp)
		if err != nil {
			return nil, false
		}
		b, _ := yaml.Marshal(idx)
		return b, true
	}
	// Groups are rendered through rewrite (needs the request for base URL).
	return []byte("apiVersion: v1\nentries: {}\n"), true
}

// ------------------------------------------------------------------ charts

// chart serves charts/<file> for proxy/group repositories by resolving the
// upstream URL from the member index.
func (h *handler) chart(w http.ResponseWriter, r *http.Request, p string) {
	file := path.Base(p)
	for _, rp := range common.Members(r.Context(), h.d, h.repo) {
		pol := repo.Policy{Kind: repo.Content, Immutable: true, ContentType: "application/gzip", Package: Format{}.Parse(file)}
		if rp.Type == model.Hosted {
			assets, _ := h.d.Content.ListAssets(r.Context(), rp.ID, "", 100000)
			for _, a := range assets {
				if path.Base(a.Path) == file {
					format.FetchAndServe(w, r, h.d, rp, a.Path, pol)
					return
				}
			}
			continue
		}
		if _, err := h.d.Content.Asset(r.Context(), rp.ID, "charts/"+file); err == nil {
			format.FetchAndServe(w, r, h.d, rp, "charts/"+file, pol)
			return
		}
		idx, err := h.readIndex(r.Context(), rp)
		if err != nil {
			continue
		}
		for _, es := range idx.Entries {
			for _, e := range es {
				for _, u := range e.URLs {
					if path.Base(u) == file {
						pol.UpstreamPath = u
						format.FetchAndServe(w, r, h.d, rp, "charts/"+file, pol)
						return
					}
				}
			}
		}
	}
	format.WriteError(w, http.StatusNotFound, "not_found", "chart not found")
}

// ------------------------------------------------------------------ upload

// chartMeta reads Chart.yaml from a chart tarball.
func chartMeta(b []byte) (*Entry, error) {
	gz, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		parts := strings.Split(strings.TrimPrefix(hdr.Name, "./"), "/")
		if len(parts) == 2 && parts[1] == "Chart.yaml" {
			raw, err := io.ReadAll(io.LimitReader(tr, 1<<20))
			if err != nil {
				return nil, err
			}
			var e Entry
			if err := yaml.Unmarshal(raw, &e); err != nil {
				return nil, err
			}
			return &e, nil
		}
	}
	return nil, io.ErrUnexpectedEOF
}

func (h *handler) upload(w http.ResponseWriter, r *http.Request) {
	if h.repo.Type != model.Hosted {
		format.WriteError(w, http.StatusBadRequest, "repo.read_only", "only hosted repositories accept uploads")
		return
	}
	var data []byte
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		if err := r.ParseMultipartForm(512 << 20); err == nil {
			if f, _, err := r.FormFile("chart"); err == nil {
				data, _ = io.ReadAll(f)
				f.Close()
			}
		}
	}
	if data == nil {
		data, _ = io.ReadAll(io.LimitReader(r.Body, 512<<20))
	}
	h.storeChart(w, r, data, r.URL.Query().Get("force") == "true")
}

func (h *handler) storeChart(w http.ResponseWriter, r *http.Request, data []byte, force bool) {
	e, err := chartMeta(data)
	if err != nil {
		format.WriteError(w, http.StatusBadRequest, "helm.invalid", "not a chart tarball: %s", err.Error())
		return
	}
	sum := sha256.Sum256(data)
	e.Digest = hex.EncodeToString(sum[:])
	p := e.Name + "-" + e.Version + ".tgz"
	attrs := map[string]any{}
	jb, _ := json.Marshal(e)
	json.Unmarshal(jb, &attrs)
	pkg := &model.Package{Name: e.Name, Version: e.Version}
	pkg.Attrs = jb
	if _, err := h.d.Engine.Put(r.Context(), h.repo, p, bytes.NewReader(data), repo.PutOptions{ContentType: "application/gzip", Package: pkg, Attrs: attrs, AllowRedeploy: force}); err != nil {
		format.MapError(w, err, h.d.Log)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{"saved": true, "name": e.Name, "version": e.Version})
}

// apiCharts implements the ChartMuseum read API used by `helm cm-push` and UIs.
func (h *handler) apiCharts(w http.ResponseWriter, r *http.Request, rest string) {
	idx, err := h.merged(r)
	if err != nil {
		format.MapError(w, err, h.d.Log)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if rest == "" {
		json.NewEncoder(w).Encode(idx.Entries)
		return
	}
	parts := strings.Split(rest, "/")
	es, ok := idx.Entries[parts[0]]
	if !ok {
		format.WriteError(w, http.StatusNotFound, "not_found", "chart not found")
		return
	}
	if len(parts) == 1 {
		json.NewEncoder(w).Encode(es)
		return
	}
	for _, e := range es {
		if e.Version == parts[1] {
			json.NewEncoder(w).Encode(e)
			return
		}
	}
	format.WriteError(w, http.StatusNotFound, "not_found", "version not found")
}

// semverLess compares chart versions (semver 2 with prerelease).
func semverLess(a, b string) bool {
	pa, pb := parse(a), parse(b)
	for i := 0; i < 3; i++ {
		if pa.n[i] != pb.n[i] {
			return pa.n[i] < pb.n[i]
		}
	}
	if pa.pre == "" || pb.pre == "" {
		return pa.pre != "" && pb.pre == ""
	}
	return pa.pre < pb.pre
}

type sv struct {
	n   [3]int
	pre string
}

func parse(v string) sv {
	var s sv
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	if i := strings.IndexByte(v, '-'); i >= 0 {
		s.pre, v = v[i+1:], v[:i]
	}
	for i, p := range strings.SplitN(v, ".", 3) {
		n := 0
		for _, c := range p {
			if c < '0' || c > '9' {
				break
			}
			n = n*10 + int(c-'0')
		}
		s.n[i] = n
	}
	return s
}
