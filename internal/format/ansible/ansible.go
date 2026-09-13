// Package ansible implements Ansible Galaxy collection repositories (Galaxy
// API v3): collection/version listings, artifact downloads and publishing
// (POST multipart), proxying galaxy.ansible.com.
package ansible

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
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
	"github.com/holiaokho/holiaokho/internal/format"
	"github.com/holiaokho/holiaokho/internal/format/common"
	"github.com/holiaokho/holiaokho/internal/model"
	"github.com/holiaokho/holiaokho/internal/repo"
)

const Name = "ansiblegalaxy"

type Format struct{}

func (Format) Name() string                                                           { return Name }
func (Format) ValidateAttributes(*model.Repository, map[string]json.RawMessage) error { return nil }
func (Format) VersionLess(a, b string) bool                                           { return a < b }

// Parse maps artifacts/<ns>-<name>-<version>.tar.gz to a package.
func (Format) Parse(p string) *model.Package {
	if !strings.HasPrefix(p, "artifacts/") || !strings.HasSuffix(p, ".tar.gz") {
		return nil
	}
	base := strings.TrimSuffix(strings.TrimPrefix(p, "artifacts/"), ".tar.gz")
	parts := strings.SplitN(base, "-", 3)
	if len(parts) != 3 {
		return nil
	}
	return &model.Package{Namespace: parts[0], Name: parts[1], Version: parts[2]}
}

type handler struct {
	repo *model.Repository
	d    format.Deps
}

func (Format) Handler(r *model.Repository, d format.Deps) http.Handler { return &handler{r, d} }

const challenge = `Bearer realm="Holiaokho Galaxy"`

func (h *handler) base(r *http.Request) string { return h.d.BaseURL(r) + "/repository/" + h.repo.Name }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

const colPrefix = "api/v3/plugin/ansible/content/published/collections/index/"

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := strings.Trim(r.URL.Path, "/")
	// ansible-galaxy sends "Authorization: Token <key>".
	if a := r.Header.Get("Authorization"); strings.HasPrefix(a, "Token ") {
		if pr, err := h.d.Auth.Login(r.Context(), auth.ClientIP(r), "", strings.TrimSpace(strings.TrimPrefix(a, "Token "))); err == nil {
			r = r.WithContext(auth.WithPrincipal(r.Context(), pr))
		}
	}
	switch {
	case p == "api" || p == "api/":
		writeJSON(w, 200, map[string]any{"available_versions": map[string]string{"v3": "v3/"}, "description": "Holiaokho Galaxy"})
	case (p == "api/v3/artifacts/collections" || p == "api/v3/plugin/ansible/content/published/collections/artifacts") && r.Method == http.MethodPost:
		if !format.Authorize(w, r, h.repo, auth.Write, challenge) {
			return
		}
		h.publish(w, r)
	case strings.HasPrefix(p, "api/v3/imports/collections/") || strings.HasPrefix(p, "api/v3/plugin/ansible/imports/collections/"):
		writeJSON(w, 200, map[string]any{"id": strings.TrimSuffix(p[strings.LastIndex(p, "/")+1:], "/"), "state": "completed", "finished_at": time.Now().UTC().Format(time.RFC3339), "messages": []any{}})
	case strings.HasPrefix(p, "api/v3/plugin/ansible/content/published/collections/artifacts/") || strings.HasPrefix(p, "api/v3/artifacts/collections/"):
		if !format.Authorize(w, r, h.repo, auth.Read, challenge) {
			return
		}
		h.artifact(w, r, p[strings.LastIndex(p, "/")+1:])
	case strings.HasPrefix(p, colPrefix) || strings.HasPrefix(p, "api/v3/collections/"):
		if !format.Authorize(w, r, h.repo, auth.Read, challenge) {
			return
		}
		rest := strings.TrimPrefix(strings.TrimPrefix(p, colPrefix), "api/v3/collections/")
		h.collection(w, r, strings.Split(strings.Trim(rest, "/"), "/"))
	default:
		writeJSON(w, 404, map[string]any{"detail": "not found"})
	}
}

// -------------------------------------------------------------- versions

type versionInfo struct {
	Version     string         `json:"version"`
	Namespace   string         `json:"-"`
	Name        string         `json:"-"`
	DownloadURL string         `json:"download_url"`
	Artifact    map[string]any `json:"artifact"`
	Metadata    map[string]any `json:"metadata"`
	CreatedAt   string         `json:"created_at"`
	Href        string         `json:"href"`
}

func (h *handler) upstreamBase(rp *model.Repository) string {
	return strings.TrimSuffix(rp.Proxy.RemoteURL, "/")
}

// versions lists the versions of a collection from one repository.
func (h *handler) versions(ctx context.Context, rp *model.Repository, ns, name string) ([]versionInfo, error) {
	switch rp.Type {
	case model.Hosted:
		pkgs, err := h.d.Content.PackageVersions(ctx, rp.ID, ns, name)
		if err != nil {
			return nil, err
		}
		if len(pkgs) == 0 {
			return nil, repo.ErrNotFound
		}
		var out []versionInfo
		for _, p := range pkgs {
			var v versionInfo
			json.Unmarshal(p.Attrs, &v)
			v.Version, v.Namespace, v.Name = p.Version, ns, name
			v.CreatedAt = p.CreatedAt.UTC().Format(time.RFC3339)
			out = append(out, v)
		}
		return out, nil
	case model.Proxy:
		// Page through the upstream version list.
		var out []versionInfo
		next := "/" + colPrefix + ns + "/" + name + "/versions/?limit=100"
		for i := 0; next != "" && i < 50; i++ {
			pol := repo.Policy{Kind: repo.Metadata, ContentType: "application/json", UpstreamPath: h.upstreamBase(rp) + next}
			b, _, err := h.d.Engine.ReadAll(ctx, rp, "cache"+strings.ReplaceAll(strings.ReplaceAll(next, "?", "_"), "&", "_"), pol, 16<<20)
			if err != nil {
				if i == 0 {
					return nil, err
				}
				break
			}
			var doc struct {
				Data  []versionInfo `json:"data"`
				Links struct {
					Next string `json:"next"`
				} `json:"links"`
			}
			if err := json.Unmarshal(b, &doc); err != nil {
				return nil, err
			}
			out = append(out, doc.Data...)
			next = doc.Links.Next
		}
		if len(out) == 0 {
			return nil, repo.ErrNotFound
		}
		return out, nil
	case model.Group:
		seen := map[string]bool{}
		var out []versionInfo
		for _, m := range common.Members(ctx, h.d, rp) {
			vs, err := h.versions(ctx, m, ns, name)
			if err != nil {
				continue
			}
			for _, v := range vs {
				if !seen[v.Version] {
					seen[v.Version] = true
					out = append(out, v)
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

// versionDetail returns the full version document (with artifact + metadata).
func (h *handler) versionDetail(ctx context.Context, rp *model.Repository, ns, name, ver string) (*versionInfo, *model.Repository, error) {
	switch rp.Type {
	case model.Hosted:
		p, err := h.d.Content.Package(ctx, rp.ID, ns, name, ver)
		if err != nil {
			return nil, nil, repo.ErrNotFound
		}
		var v versionInfo
		json.Unmarshal(p.Attrs, &v)
		v.Version, v.Namespace, v.Name = ver, ns, name
		v.CreatedAt = p.CreatedAt.UTC().Format(time.RFC3339)
		return &v, rp, nil
	case model.Proxy:
		pol := repo.Policy{Kind: repo.Metadata, ContentType: "application/json", UpstreamPath: h.upstreamBase(rp) + "/" + colPrefix + ns + "/" + name + "/versions/" + ver + "/"}
		b, _, err := h.d.Engine.ReadAll(ctx, rp, colPrefix+ns+"/"+name+"/versions/"+ver, pol, 8<<20)
		if err != nil {
			return nil, nil, err
		}
		var v versionInfo
		if err := json.Unmarshal(b, &v); err != nil {
			return nil, nil, err
		}
		v.Namespace, v.Name = ns, name
		return &v, rp, nil
	case model.Group:
		for _, m := range common.Members(ctx, h.d, rp) {
			if v, src, err := h.versionDetail(ctx, m, ns, name, ver); err == nil {
				return v, src, nil
			}
		}
	}
	return nil, nil, repo.ErrNotFound
}

func (h *handler) collection(w http.ResponseWriter, r *http.Request, segs []string) {
	if len(segs) < 2 {
		writeJSON(w, 404, map[string]any{"detail": "not found"})
		return
	}
	ns, name := segs[0], segs[1]
	base := h.base(r)
	colHref := fmt.Sprintf("%s/%s%s/%s/", base, colPrefix, ns, name)
	switch {
	case len(segs) == 2:
		vs, err := h.versions(r.Context(), h.repo, ns, name)
		if err != nil {
			mapErr(w, err, h.d)
			return
		}
		sort.SliceStable(vs, func(i, j int) bool { return vs[i].Version > vs[j].Version })
		writeJSON(w, 200, map[string]any{"href": colHref, "namespace": ns, "name": name, "deprecated": false,
			"versions_url": colHref + "versions/", "highest_version": map[string]any{"href": colHref + "versions/" + vs[0].Version + "/", "version": vs[0].Version},
			"created_at": vs[len(vs)-1].CreatedAt, "updated_at": vs[0].CreatedAt})
	case len(segs) == 3 && segs[2] == "versions":
		vs, err := h.versions(r.Context(), h.repo, ns, name)
		if err != nil {
			mapErr(w, err, h.d)
			return
		}
		sort.SliceStable(vs, func(i, j int) bool { return vs[i].Version > vs[j].Version })
		data := []map[string]any{}
		for _, v := range vs {
			data = append(data, map[string]any{"version": v.Version, "href": colHref + "versions/" + v.Version + "/", "created_at": v.CreatedAt, "updated_at": v.CreatedAt, "requires_ansible": ""})
		}
		writeJSON(w, 200, map[string]any{"meta": map[string]any{"count": len(data)}, "links": map[string]any{"first": colHref + "versions/", "previous": nil, "next": nil, "last": colHref + "versions/"}, "data": data})
	case len(segs) == 4 && segs[2] == "versions":
		v, _, err := h.versionDetail(r.Context(), h.repo, ns, name, segs[3])
		if err != nil {
			mapErr(w, err, h.d)
			return
		}
		file := fmt.Sprintf("%s-%s-%s.tar.gz", ns, name, v.Version)
		v.DownloadURL = base + "/api/v3/plugin/ansible/content/published/collections/artifacts/" + file
		v.Href = colHref + "versions/" + v.Version + "/"
		out := map[string]any{"version": v.Version, "href": v.Href, "download_url": v.DownloadURL, "artifact": v.Artifact, "metadata": v.Metadata, "created_at": v.CreatedAt, "updated_at": v.CreatedAt,
			"namespace": map[string]any{"name": ns}, "collection": map[string]any{"name": name, "href": colHref}, "name": name}
		writeJSON(w, 200, out)
	default:
		writeJSON(w, 404, map[string]any{"detail": "not found"})
	}
}

// artifact serves artifacts/<ns>-<name>-<version>.tar.gz.
func (h *handler) artifact(w http.ResponseWriter, r *http.Request, file string) {
	pkg := Format{}.Parse("artifacts/" + file)
	if pkg == nil {
		writeJSON(w, 404, map[string]any{"detail": "not found"})
		return
	}
	pol := repo.Policy{Kind: repo.Content, Immutable: true, ContentType: "application/gzip", Package: pkg}
	for _, rp := range common.Members(r.Context(), h.d, h.repo) {
		mp := pol
		if rp.Type == model.Proxy {
			v, _, err := h.versionDetail(r.Context(), rp, pkg.Namespace, pkg.Name, pkg.Version)
			if err != nil || v.DownloadURL == "" {
				continue
			}
			mp.UpstreamPath = v.DownloadURL
		}
		if res, err := h.d.Engine.Fetch(r.Context(), rp, "artifacts/"+file, mp); err == nil {
			format.ServeResult(w, r, h.d, res)
			return
		}
	}
	writeJSON(w, 404, map[string]any{"detail": "artifact not found"})
}

// ---------------------------------------------------------------- publish

func (h *handler) publish(w http.ResponseWriter, r *http.Request) {
	if h.repo.Type != model.Hosted {
		writeJSON(w, 400, map[string]any{"detail": "only hosted repositories accept publishes"})
		return
	}
	if err := r.ParseMultipartForm(512 << 20); err != nil {
		writeJSON(w, 400, map[string]any{"detail": "multipart expected"})
		return
	}
	f, fh, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, 400, map[string]any{"detail": "file field required"})
		return
	}
	defer f.Close()
	data, _ := io.ReadAll(f)
	// ansible-galaxy encodes the artifact part with base64 transfer encoding.
	if strings.EqualFold(fh.Header.Get("Content-Transfer-Encoding"), "base64") {
		if dec, err := base64.StdEncoding.DecodeString(strings.Map(func(r rune) rune {
			if r == '\n' || r == '\r' {
				return -1
			}
			return r
		}, string(data))); err == nil {
			data = dec
		}
	}
	manifest, err := readManifest(data)
	if err != nil {
		writeJSON(w, 400, map[string]any{"detail": "invalid collection: " + err.Error()})
		return
	}
	ci, _ := manifest["collection_info"].(map[string]any)
	ns, _ := ci["namespace"].(string)
	name, _ := ci["name"].(string)
	ver, _ := ci["version"].(string)
	if ns == "" || name == "" || ver == "" {
		writeJSON(w, 400, map[string]any{"detail": "MANIFEST.json lacks namespace/name/version"})
		return
	}
	sum := sha256.Sum256(data)
	file := fmt.Sprintf("%s-%s-%s.tar.gz", ns, name, ver)
	v := versionInfo{Artifact: map[string]any{"filename": file, "sha256": hex.EncodeToString(sum[:]), "size": len(data)}, Metadata: map[string]any{}}
	for _, k := range []string{"dependencies", "description", "authors", "license", "tags", "repository", "homepage", "documentation", "issues"} {
		if val, ok := ci[k]; ok {
			v.Metadata[k] = val
		}
	}
	if _, ok := v.Metadata["dependencies"]; !ok {
		v.Metadata["dependencies"] = map[string]any{}
	}
	attrs, _ := json.Marshal(v)
	pkg := &model.Package{Namespace: ns, Name: name, Version: ver, Attrs: attrs}
	if _, err := h.d.Engine.Put(r.Context(), h.repo, "artifacts/"+file, bytes.NewReader(data), repo.PutOptions{ContentType: "application/gzip", Package: pkg}); err != nil {
		if errors.Is(err, repo.ErrRedeploy) {
			writeJSON(w, 409, map[string]any{"detail": "collection version already exists"})
			return
		}
		format.MapError(w, err, h.d.Log)
		return
	}
	task := h.base(r) + "/api/v3/imports/collections/" + hex.EncodeToString(sum[:8]) + "/"
	writeJSON(w, 202, map[string]any{"task": task})
}

func readManifest(data []byte) (map[string]any, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err != nil {
			return nil, errors.New("MANIFEST.json not found")
		}
		if strings.TrimPrefix(hdr.Name, "./") == "MANIFEST.json" {
			var m map[string]any
			if err := json.NewDecoder(io.LimitReader(tr, 4<<20)).Decode(&m); err != nil {
				return nil, err
			}
			return m, nil
		}
	}
}

func mapErr(w http.ResponseWriter, err error, d format.Deps) {
	if errors.Is(err, repo.ErrNotFound) {
		writeJSON(w, 404, map[string]any{"detail": "not found"})
		return
	}
	format.MapError(w, err, d.Log)
}
