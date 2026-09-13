// Package terraform implements Terraform/OpenTofu registries: provider
// registry protocol and provider network mirror protocol (proxying
// registry.terraform.io), module registry (proxy + hosted uploads).
package terraform

import (
	"bytes"
	"context"
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
	"github.com/holiaokho/holiaokho/internal/format"
	"github.com/holiaokho/holiaokho/internal/format/common"
	"github.com/holiaokho/holiaokho/internal/model"
	"github.com/holiaokho/holiaokho/internal/repo"
)

const Name = "terraform"

type Format struct{}

func (Format) Name() string                                                           { return Name }
func (Format) ValidateAttributes(*model.Repository, map[string]json.RawMessage) error { return nil }
func (Format) VersionLess(a, b string) bool                                           { return a < b }

// Parse maps modules/<ns>/<name>/<provider>/<version>.tgz and
// providers/<ns>/<type>/<version>/<file>.zip to packages.
func (Format) Parse(p string) *model.Package {
	segs := strings.Split(strings.Trim(p, "/"), "/")
	switch {
	case len(segs) == 5 && segs[0] == "modules" && strings.HasSuffix(segs[4], ".tgz"):
		return &model.Package{Namespace: segs[1] + "/" + segs[3], Name: segs[2], Version: strings.TrimSuffix(segs[4], ".tgz")}
	case len(segs) == 5 && segs[0] == "providers" && strings.HasSuffix(segs[4], ".zip"):
		return &model.Package{Namespace: segs[1], Name: segs[2], Version: segs[3]}
	}
	return nil
}

type handler struct {
	repo *model.Repository
	d    format.Deps
}

func (Format) Handler(r *model.Repository, d format.Deps) http.Handler { return &handler{r, d} }

const challenge = `Basic realm="Holiaokho Terraform"`

func (h *handler) base(r *http.Request) string { return h.d.BaseURL(r) + "/repository/" + h.repo.Name }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := strings.Trim(r.URL.Path, "/")
	segs := strings.Split(p, "/")
	if r.Method == http.MethodPut || r.Method == http.MethodPost {
		if !format.Authorize(w, r, h.repo, auth.Write, challenge) {
			return
		}
		h.upload(w, r, p)
		return
	}
	if r.Method == http.MethodDelete {
		if !format.Authorize(w, r, h.repo, auth.Delete, challenge) {
			return
		}
		if err := h.d.Engine.Delete(r.Context(), h.repo, p); err != nil {
			format.MapError(w, err, h.d.Log)
			return
		}
		w.WriteHeader(204)
		return
	}
	if !format.Authorize(w, r, h.repo, auth.Read, challenge) {
		return
	}
	switch {
	case p == ".well-known/terraform.json":
		writeJSON(w, 200, map[string]any{"providers.v1": h.base(r) + "/v1/providers/", "modules.v1": h.base(r) + "/v1/modules/"})
	case len(segs) >= 4 && segs[0] == "v1" && segs[1] == "providers":
		h.providers(w, r, segs[2:])
	case len(segs) >= 4 && segs[0] == "providers":
		h.mirror(w, r, segs[1:])
	case len(segs) >= 5 && segs[0] == "v1" && segs[1] == "modules":
		h.modules(w, r, segs[2:])
	case len(segs) == 5 && segs[0] == "modules" && strings.HasSuffix(segs[4], ".tgz"):
		pol := repo.Policy{Kind: repo.Content, Immutable: true, ContentType: "application/gzip", Package: Format{}.Parse(p)}
		if up := r.URL.Query().Get("upstream"); up != "" && strings.HasPrefix(up, "https://") {
			pol.UpstreamPath = up
		}
		for _, rp := range common.Members(r.Context(), h.d, h.repo) {
			if rp.Type == model.Hosted || pol.UpstreamPath != "" {
				if res, err := h.d.Engine.Fetch(r.Context(), rp, p, pol); err == nil {
					format.ServeResult(w, r, h.d, res)
					return
				}
			}
		}
		writeJSON(w, 404, map[string]any{"errors": []string{"module archive not found"}})
	default:
		writeJSON(w, 404, map[string]any{"errors": []string{"not found"}})
	}
}

// ------------------------------------------------------------- providers

// upstreamProviders returns the upstream providers.v1 base for a proxy.
func (h *handler) upstreamBase(ctx context.Context, rp *model.Repository, kind string) string {
	remote := strings.TrimSuffix(rp.Proxy.RemoteURL, "/")
	pol := repo.Policy{Kind: repo.Metadata, ContentType: "application/json", UpstreamPath: remote + "/.well-known/terraform.json"}
	b, _, err := h.d.Engine.ReadAll(ctx, rp, ".well-known/terraform.json", pol, 1<<20)
	if err == nil {
		var doc map[string]string
		if json.Unmarshal(b, &doc) == nil {
			if u := doc[kind+".v1"]; u != "" {
				if strings.HasPrefix(u, "/") {
					return remote + strings.TrimSuffix(u, "/")
				}
				return strings.TrimSuffix(u, "/")
			}
		}
	}
	return remote + "/v1/" + kind
}

type providerVersions struct {
	Versions []struct {
		Version   string   `json:"version"`
		Protocols []string `json:"protocols"`
		Platforms []struct {
			OS   string `json:"os"`
			Arch string `json:"arch"`
		} `json:"platforms"`
	} `json:"versions"`
}

func (h *handler) providerVersions(ctx context.Context, rp *model.Repository, ns, typ string) (*providerVersions, error) {
	switch rp.Type {
	case model.Proxy:
		pol := repo.Policy{Kind: repo.Metadata, ContentType: "application/json", UpstreamPath: h.upstreamBase(ctx, rp, "providers") + "/" + ns + "/" + typ + "/versions"}
		b, _, err := h.d.Engine.ReadAll(ctx, rp, "v1/providers/"+ns+"/"+typ+"/versions", pol, 16<<20)
		if err != nil {
			return nil, err
		}
		var pv providerVersions
		if err := json.Unmarshal(b, &pv); err != nil {
			return nil, fmt.Errorf("%w: bad versions document", repo.ErrUpstream)
		}
		return &pv, nil
	case model.Group:
		for _, m := range common.Members(ctx, h.d, rp) {
			if pv, err := h.providerVersions(ctx, m, ns, typ); err == nil {
				return pv, nil
			}
		}
	}
	return nil, repo.ErrNotFound
}

type providerDownload struct {
	Protocols           []string       `json:"protocols"`
	OS                  string         `json:"os"`
	Arch                string         `json:"arch"`
	Filename            string         `json:"filename"`
	DownloadURL         string         `json:"download_url"`
	ShasumsURL          string         `json:"shasums_url"`
	ShasumsSignatureURL string         `json:"shasums_signature_url"`
	Shasum              string         `json:"shasum"`
	SigningKeys         map[string]any `json:"signing_keys"`
}

func (h *handler) providerDownload(ctx context.Context, rp *model.Repository, ns, typ, ver, os, arch string) (*providerDownload, *model.Repository, error) {
	switch rp.Type {
	case model.Proxy:
		pol := repo.Policy{Kind: repo.Metadata, ContentType: "application/json", UpstreamPath: fmt.Sprintf("%s/%s/%s/%s/download/%s/%s", h.upstreamBase(ctx, rp, "providers"), ns, typ, ver, os, arch)}
		b, _, err := h.d.Engine.ReadAll(ctx, rp, fmt.Sprintf("v1/providers/%s/%s/%s/download/%s/%s", ns, typ, ver, os, arch), pol, 4<<20)
		if err != nil {
			return nil, nil, err
		}
		var pd providerDownload
		if err := json.Unmarshal(b, &pd); err != nil {
			return nil, nil, fmt.Errorf("%w: bad download document", repo.ErrUpstream)
		}
		return &pd, rp, nil
	case model.Group:
		for _, m := range common.Members(ctx, h.d, rp) {
			if pd, src, err := h.providerDownload(ctx, m, ns, typ, ver, os, arch); err == nil {
				return pd, src, nil
			}
		}
	}
	return nil, nil, repo.ErrNotFound
}

// providers serves the registry protocol under v1/providers/.
func (h *handler) providers(w http.ResponseWriter, r *http.Request, segs []string) {
	ns, typ := segs[0], segs[1]
	switch {
	case len(segs) == 3 && segs[2] == "versions":
		pv, err := h.providerVersions(r.Context(), h.repo, ns, typ)
		if err != nil {
			mapErr(w, err, h.d)
			return
		}
		writeJSON(w, 200, pv)
	case len(segs) == 6 && segs[3] == "download":
		pd, _, err := h.providerDownload(r.Context(), h.repo, ns, typ, segs[2], segs[4], segs[5])
		if err != nil {
			mapErr(w, err, h.d)
			return
		}
		out := *pd
		fb := fmt.Sprintf("%s/providers/%s/%s/%s/files/", h.base(r), ns, typ, segs[2])
		out.DownloadURL = fb + path.Base(pd.DownloadURL)
		out.ShasumsURL = fb + path.Base(pd.ShasumsURL)
		out.ShasumsSignatureURL = fb + path.Base(pd.ShasumsSignatureURL)
		writeJSON(w, 200, out)
	default:
		writeJSON(w, 404, map[string]any{"errors": []string{"not found"}})
	}
}

// mirror serves both the network-mirror protocol
// (providers/<host>/<ns>/<type>/index.json and <version>.json) and the
// provider files referenced by the registry protocol
// (providers/<ns>/<type>/<version>/files/<file>).
func (h *handler) mirror(w http.ResponseWriter, r *http.Request, segs []string) {
	// Files: providers/<ns>/<type>/<version>/files/<file>
	if len(segs) == 5 && segs[3] == "files" {
		h.providerFile(w, r, segs[0], segs[1], segs[2], segs[4])
		return
	}
	if len(segs) != 4 {
		writeJSON(w, 404, map[string]any{"errors": []string{"not found"}})
		return
	}
	host, ns, typ, last := segs[0], segs[1], segs[2], segs[3]
	_ = host
	pv, err := h.providerVersions(r.Context(), h.repo, ns, typ)
	if err != nil {
		mapErr(w, err, h.d)
		return
	}
	if last == "index.json" {
		versions := map[string]any{}
		for _, v := range pv.Versions {
			versions[v.Version] = map[string]any{}
		}
		writeJSON(w, 200, map[string]any{"versions": versions})
		return
	}
	ver := strings.TrimSuffix(last, ".json")
	archives := map[string]any{}
	for _, v := range pv.Versions {
		if v.Version != ver {
			continue
		}
		for _, pl := range v.Platforms {
			pd, _, err := h.providerDownload(r.Context(), h.repo, ns, typ, ver, pl.OS, pl.Arch)
			if err != nil {
				continue
			}
			entry := map[string]any{"url": fmt.Sprintf("%s/providers/%s/%s/%s/files/%s", h.base(r), ns, typ, ver, path.Base(pd.DownloadURL))}
			if pd.Shasum != "" {
				entry["hashes"] = []string{"zh:" + pd.Shasum}
			}
			archives[pl.OS+"_"+pl.Arch] = entry
		}
	}
	if len(archives) == 0 {
		writeJSON(w, 404, map[string]any{"errors": []string{"version not found"}})
		return
	}
	writeJSON(w, 200, map[string]any{"archives": archives})
}

func (h *handler) providerFile(w http.ResponseWriter, r *http.Request, ns, typ, ver, file string) {
	p := fmt.Sprintf("providers/%s/%s/%s/%s", ns, typ, ver, file)
	pol := repo.Policy{Kind: repo.Content, Immutable: true, ContentType: "application/octet-stream"}
	if strings.HasSuffix(file, ".zip") {
		pol.ContentType = "application/zip"
		pol.Package = &model.Package{Namespace: ns, Name: typ, Version: ver}
	}
	for _, rp := range common.Members(r.Context(), h.d, h.repo) {
		if _, err := h.d.Content.Asset(r.Context(), rp.ID, p); err == nil {
			format.FetchAndServe(w, r, h.d, rp, p, pol)
			return
		}
		if rp.Type != model.Proxy {
			continue
		}
		// Find the upstream URL from any platform download document whose
		// files include this one.
		pv, err := h.providerVersions(r.Context(), rp, ns, typ)
		if err != nil {
			continue
		}
		for _, v := range pv.Versions {
			if v.Version != ver {
				continue
			}
			for _, pl := range v.Platforms {
				pd, _, err := h.providerDownload(r.Context(), rp, ns, typ, ver, pl.OS, pl.Arch)
				if err != nil {
					continue
				}
				for _, u := range []string{pd.DownloadURL, pd.ShasumsURL, pd.ShasumsSignatureURL} {
					if path.Base(u) == file {
						pol.UpstreamPath = u
						format.FetchAndServe(w, r, h.d, rp, p, pol)
						return
					}
				}
			}
		}
	}
	writeJSON(w, 404, map[string]any{"errors": []string{"file not found"}})
}

// --------------------------------------------------------------- modules

func (h *handler) moduleVersions(ctx context.Context, rp *model.Repository, ns, name, provider string) ([]string, error) {
	switch rp.Type {
	case model.Hosted:
		pkgs, err := h.d.Content.PackageVersions(ctx, rp.ID, ns+"/"+provider, name)
		if err != nil {
			return nil, err
		}
		var out []string
		for _, p := range pkgs {
			out = append(out, p.Version)
		}
		if len(out) == 0 {
			return nil, repo.ErrNotFound
		}
		return out, nil
	case model.Proxy:
		pol := repo.Policy{Kind: repo.Metadata, ContentType: "application/json", UpstreamPath: h.upstreamBase(ctx, rp, "modules") + "/" + ns + "/" + name + "/" + provider + "/versions"}
		b, _, err := h.d.Engine.ReadAll(ctx, rp, "v1/modules/"+ns+"/"+name+"/"+provider+"/versions", pol, 8<<20)
		if err != nil {
			return nil, err
		}
		var doc struct {
			Modules []struct {
				Versions []struct {
					Version string `json:"version"`
				} `json:"versions"`
			} `json:"modules"`
		}
		json.Unmarshal(b, &doc)
		var out []string
		for _, m := range doc.Modules {
			for _, v := range m.Versions {
				out = append(out, v.Version)
			}
		}
		if len(out) == 0 {
			return nil, repo.ErrNotFound
		}
		return out, nil
	case model.Group:
		seen := map[string]bool{}
		var out []string
		for _, m := range common.Members(ctx, h.d, rp) {
			vs, err := h.moduleVersions(ctx, m, ns, name, provider)
			if err != nil {
				continue
			}
			for _, v := range vs {
				if !seen[v] {
					seen[v] = true
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

func (h *handler) modules(w http.ResponseWriter, r *http.Request, segs []string) {
	if len(segs) < 4 {
		writeJSON(w, 404, map[string]any{"errors": []string{"not found"}})
		return
	}
	ns, name, provider := segs[0], segs[1], segs[2]
	switch {
	case len(segs) == 4 && segs[3] == "versions":
		vs, err := h.moduleVersions(r.Context(), h.repo, ns, name, provider)
		if err != nil {
			mapErr(w, err, h.d)
			return
		}
		sort.Strings(vs)
		var list []map[string]any
		for _, v := range vs {
			list = append(list, map[string]any{"version": v})
		}
		writeJSON(w, 200, map[string]any{"modules": []map[string]any{{"source": ns + "/" + name + "/" + provider, "versions": list}}})
	case len(segs) == 5 && segs[4] == "download":
		ver := segs[3]
		for _, rp := range common.Members(r.Context(), h.d, h.repo) {
			switch rp.Type {
			case model.Hosted:
				p := fmt.Sprintf("modules/%s/%s/%s/%s.tgz", ns, name, provider, ver)
				if _, err := h.d.Content.Asset(r.Context(), rp.ID, p); err == nil {
					w.Header().Set("X-Terraform-Get", h.base(r)+"/"+p)
					w.WriteHeader(204)
					return
				}
			case model.Proxy:
				// Ask upstream for the source location; archive URLs are
				// proxied, git:: and other sources are handed to the client.
				u := fmt.Sprintf("%s/%s/%s/%s/%s/download", h.upstreamBase(r.Context(), rp, "modules"), ns, name, provider, ver)
				resp, err := h.d.Engine.Upstream(r.Context(), rp, http.MethodGet, u, nil, nil, repo.Policy{Kind: repo.NoCache})
				if err != nil {
					continue
				}
				get := resp.Header.Get("X-Terraform-Get")
				resp.Body.Close()
				if get == "" {
					continue
				}
				if strings.HasPrefix(get, "https://") && (strings.HasSuffix(get, ".tgz") || strings.HasSuffix(get, ".tar.gz") || strings.HasSuffix(get, ".zip") || strings.Contains(get, "archive=")) {
					w.Header().Set("X-Terraform-Get", fmt.Sprintf("%s/modules/%s/%s/%s/%s.tgz?upstream=%s", h.base(r), ns, name, provider, ver, get))
				} else {
					w.Header().Set("X-Terraform-Get", get)
				}
				w.WriteHeader(204)
				return
			}
		}
		writeJSON(w, 404, map[string]any{"errors": []string{"module version not found"}})
	default:
		writeJSON(w, 404, map[string]any{"errors": []string{"not found"}})
	}
}

// upload stores a module archive: PUT modules/<ns>/<name>/<provider>/<version>.tgz
func (h *handler) upload(w http.ResponseWriter, r *http.Request, p string) {
	if h.repo.Type != model.Hosted {
		writeJSON(w, 400, map[string]any{"errors": []string{"only hosted repositories accept uploads"}})
		return
	}
	pkg := Format{}.Parse(p)
	if pkg == nil {
		writeJSON(w, 400, map[string]any{"errors": []string{"path must be modules/<namespace>/<name>/<provider>/<version>.tgz"}})
		return
	}
	data, _ := io.ReadAll(io.LimitReader(r.Body, 512<<20))
	if _, err := h.d.Engine.Put(r.Context(), h.repo, p, bytes.NewReader(data), repo.PutOptions{ContentType: "application/gzip", Package: pkg}); err != nil {
		if errors.Is(err, repo.ErrRedeploy) {
			writeJSON(w, 409, map[string]any{"errors": []string{"version already exists"}})
			return
		}
		format.MapError(w, err, h.d.Log)
		return
	}
	writeJSON(w, 201, map[string]any{"path": p, "version": pkg.Version})
}

func mapErr(w http.ResponseWriter, err error, d format.Deps) {
	if errors.Is(err, repo.ErrNotFound) {
		writeJSON(w, 404, map[string]any{"errors": []string{"not found"}})
		return
	}
	format.MapError(w, err, d.Log)
}

var _ = time.Now
