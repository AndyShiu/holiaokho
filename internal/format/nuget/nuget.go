// Package nuget implements the NuGet V3 protocol (service index, flat
// container, registration, search, push) for hosted, proxy and group
// repositories. Proxy registration pages are rewritten so clients stay on
// this server; nupkg/nuspec files are cached immutably.
package nuget

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/holiaokho/holiaokho/internal/auth"
	"github.com/holiaokho/holiaokho/internal/content"
	"github.com/holiaokho/holiaokho/internal/format"
	"github.com/holiaokho/holiaokho/internal/model"
	"github.com/holiaokho/holiaokho/internal/repo"
)

const Name = "nuget"

const challenge = `Basic realm="Holiaokho NuGet"`

type Format struct{}

func (Format) Name() string                                                           { return Name }
func (Format) ValidateAttributes(*model.Repository, map[string]json.RawMessage) error { return nil }
func (Format) VersionLess(a, b string) bool                                           { return VersionLess(a, b) }

// Parse maps "v3/flatcontainer/<id>/<ver>/<id>.<ver>.nupkg" to a package.
func (Format) Parse(p string) *model.Package {
	segs := strings.Split(strings.Trim(p, "/"), "/")
	if len(segs) != 5 || segs[0] != "v3" || segs[1] != "flatcontainer" || !strings.HasSuffix(segs[4], ".nupkg") {
		return nil
	}
	return packageFor(segs[2], segs[3], nil)
}

func packageFor(id, version string, ns *Nuspec) *model.Package {
	attrs := map[string]any{"id": id}
	if ns != nil {
		attrs["nuspec"] = ns
	}
	raw, _ := json.Marshal(attrs)
	return &model.Package{Name: strings.ToLower(id), Version: NormalizeVersion(version), Attrs: raw}
}

type handler struct {
	repo *model.Repository
	d    format.Deps
}

func (Format) Handler(r *model.Repository, d format.Deps) http.Handler { return &handler{r, d} }

func (h *handler) base(r *http.Request) string {
	return h.d.BaseURL(r) + "/repository/" + h.repo.Name
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := strings.Trim(r.URL.Path, "/")
	segs := strings.Split(p, "/")
	// Push / delete (PackagePublish resource): api/v2/package[/id/version]
	if strings.HasPrefix(p, "api/v2/package") {
		h.publish(w, r, segs)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeJSON(w, 405, map[string]any{"error": "method not allowed"})
		return
	}
	if !format.Authorize(w, r, h.repo, auth.Read, challenge) {
		return
	}
	switch {
	case p == "" || p == "index.json" || p == "v3/index.json":
		h.serviceIndex(w, r)
	case len(segs) >= 4 && segs[0] == "v3" && segs[1] == "flatcontainer":
		h.flatContainer(w, r, segs[2:])
	case len(segs) >= 4 && segs[0] == "v3" && segs[1] == "registration":
		h.registration(w, r, segs[2:])
	case len(segs) == 2 && segs[0] == "v3" && strings.HasPrefix(segs[1], "query"):
		h.search(w, r)
	case len(segs) == 2 && segs[0] == "v3" && strings.HasPrefix(segs[1], "autocomplete"):
		h.autocomplete(w, r)
	default:
		writeJSON(w, 404, map[string]any{"error": "not found"})
	}
}

// ---------------------------------------------------------- service index

func (h *handler) serviceIndex(w http.ResponseWriter, r *http.Request) {
	b := h.base(r)
	res := func(id, typ string) map[string]any { return map[string]any{"@id": id, "@type": typ} }
	idx := map[string]any{
		"version": "3.0.0",
		"resources": []map[string]any{
			res(b+"/v3/flatcontainer/", "PackageBaseAddress/3.0.0"),
			res(b+"/v3/registration/", "RegistrationsBaseUrl"),
			res(b+"/v3/registration/", "RegistrationsBaseUrl/3.0.0-rc"),
			res(b+"/v3/registration/", "RegistrationsBaseUrl/3.0.0-beta"),
			res(b+"/v3/registration/", "RegistrationsBaseUrl/3.4.0"),
			res(b+"/v3/registration/", "RegistrationsBaseUrl/3.6.0"),
			res(b+"/v3/query", "SearchQueryService"),
			res(b+"/v3/query", "SearchQueryService/3.0.0-rc"),
			res(b+"/v3/query", "SearchQueryService/3.0.0-beta"),
			res(b+"/v3/query", "SearchQueryService/3.5.0"),
			res(b+"/v3/autocomplete", "SearchAutocompleteService"),
			res(b+"/v3/autocomplete", "SearchAutocompleteService/3.0.0-rc"),
			res(b+"/v3/autocomplete", "SearchAutocompleteService/3.5.0"),
			res(b+"/api/v2/package", "PackagePublish/2.0.0"),
		},
	}
	writeJSON(w, 200, idx)
}

// -------------------------------------------------------- flat container

// versions lists the (normalised, lowercase) versions of a package id in one repository.
func (h *handler) versions(ctx context.Context, rp *model.Repository, id string) ([]string, error) {
	id = strings.ToLower(id)
	switch rp.Type {
	case model.Hosted:
		pkgs, err := h.d.Content.PackageVersions(ctx, rp.ID, "", id)
		if err != nil {
			return nil, err
		}
		if len(pkgs) == 0 {
			return nil, repo.ErrNotFound
		}
		var out []string
		for _, p := range pkgs {
			out = append(out, p.Version)
		}
		return out, nil
	case model.Proxy:
		pol := repo.Policy{Kind: repo.Metadata, ContentType: "application/json", UpstreamPath: "v3-flatcontainer/" + id + "/index.json"}
		if fc := h.upstreamFlat(ctx, rp); fc != "" {
			pol.UpstreamPath = fc + id + "/index.json"
		}
		b, _, err := h.d.Engine.ReadAll(ctx, rp, "v3/flatcontainer/"+id+"/index.json", pol, 8<<20)
		if err != nil {
			return nil, err
		}
		var doc struct {
			Versions []string `json:"versions"`
		}
		if err := json.Unmarshal(b, &doc); err != nil {
			return nil, fmt.Errorf("%w: bad flat container index", repo.ErrUpstream)
		}
		return doc.Versions, nil
	case model.Group:
		members, _ := h.d.Engine.Members(ctx, rp)
		seen := map[string]bool{}
		var out []string
		for _, m := range members {
			vs, err := h.versions(ctx, m, id)
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

// upstreamResources resolves the upstream service index of a proxy and
// returns the PackageBaseAddress / RegistrationsBaseUrl / SearchQueryService
// URLs (cached as metadata).
type upstreamRes struct {
	Flat, Registration, Search string
}

func (h *handler) upstream(ctx context.Context, rp *model.Repository) upstreamRes {
	remote := rp.Proxy.RemoteURL
	idxPath := "index.json"
	if strings.HasSuffix(remote, "index.json") {
		// remoteUrl already points at the service index.
		u, _ := url.Parse(remote)
		idxPath = ""
		if u != nil {
			idxPath = u.Path
		}
	}
	pol := repo.Policy{Kind: repo.Metadata, ContentType: "application/json", UpstreamPath: strings.TrimSuffix(remote, "/") + "/" + idxPath}
	if strings.HasSuffix(remote, "index.json") {
		pol.UpstreamPath = remote
	}
	b, _, err := h.d.Engine.ReadAll(ctx, rp, "v3/upstream-index.json", pol, 4<<20)
	if err != nil {
		return upstreamRes{}
	}
	var doc struct {
		Resources []struct {
			ID   string `json:"@id"`
			Type string `json:"@type"`
		} `json:"resources"`
	}
	json.Unmarshal(b, &doc)
	var res upstreamRes
	prefer := func(cur, cand, typ string, wants ...string) string {
		for _, w := range wants {
			if typ == w && (cur == "" || w == wants[0]) {
				return cand
			}
		}
		return cur
	}
	for _, x := range doc.Resources {
		res.Flat = prefer(res.Flat, x.ID, x.Type, "PackageBaseAddress/3.0.0")
		res.Registration = prefer(res.Registration, x.ID, x.Type, "RegistrationsBaseUrl/3.6.0", "RegistrationsBaseUrl/3.4.0", "RegistrationsBaseUrl")
		res.Search = prefer(res.Search, x.ID, x.Type, "SearchQueryService/3.5.0", "SearchQueryService")
	}
	for _, p := range []*string{&res.Flat, &res.Registration} {
		if *p != "" && !strings.HasSuffix(*p, "/") {
			*p += "/"
		}
	}
	return res
}

func (h *handler) upstreamFlat(ctx context.Context, rp *model.Repository) string {
	return h.upstream(ctx, rp).Flat
}

func (h *handler) flatContainer(w http.ResponseWriter, r *http.Request, segs []string) {
	id := strings.ToLower(segs[0])
	switch {
	case len(segs) == 2 && segs[1] == "index.json":
		vs, err := h.versions(r.Context(), h.repo, id)
		if err != nil {
			mapErr(w, err, h.d)
			return
		}
		sort.Slice(vs, func(i, j int) bool { return VersionLess(vs[i], vs[j]) })
		writeJSON(w, 200, map[string]any{"versions": vs})
	case len(segs) == 3:
		ver := strings.ToLower(segs[1])
		file := strings.ToLower(segs[2])
		p := "v3/flatcontainer/" + id + "/" + ver + "/" + file
		pol := repo.Policy{Kind: repo.Content, Immutable: true, ContentType: "application/octet-stream"}
		if strings.HasSuffix(file, ".nuspec") {
			pol.ContentType = "application/xml"
		} else if strings.HasSuffix(file, ".nupkg") {
			pol.Package = packageFor(id, ver, nil)
		}
		if h.repo.Type == model.Group {
			h.groupFetch(w, r, p, id, ver, file, pol)
			return
		}
		if h.repo.Type == model.Proxy {
			pol.UpstreamPath = h.upstreamFlat(r.Context(), h.repo) + id + "/" + ver + "/" + file
		}
		format.FetchAndServe(w, r, h.d, h.repo, p, pol)
	default:
		writeJSON(w, 404, map[string]any{"error": "not found"})
	}
}

// groupFetch resolves a flat-container file through group members, each
// with its own upstream address.
func (h *handler) groupFetch(w http.ResponseWriter, r *http.Request, p, id, ver, file string, pol repo.Policy) {
	members, _ := h.d.Engine.Members(r.Context(), h.repo)
	for _, m := range members {
		mp := pol
		if m.Type == model.Proxy {
			mp.UpstreamPath = h.upstreamFlat(r.Context(), m) + id + "/" + ver + "/" + file
		}
		res, err := h.d.Engine.Fetch(r.Context(), m, p, mp)
		if err == nil {
			format.ServeResult(w, r, h.d, res)
			return
		}
	}
	writeJSON(w, 404, map[string]any{"error": "not found"})
}

// ----------------------------------------------------------- registration

// catalogEntry builds the registration catalog entry for a version.
func (h *handler) catalogEntry(r *http.Request, id, version string, ns *Nuspec, published time.Time) map[string]any {
	b := h.base(r)
	entry := map[string]any{
		"@id": b + "/v3/registration/" + id + "/" + version + ".json", "@type": "PackageDetails",
		"id": id, "version": version, "listed": true, "published": published.UTC().Format(time.RFC3339),
		"packageContent": b + "/v3/flatcontainer/" + id + "/" + version + "/" + id + "." + version + ".nupkg",
	}
	if ns != nil {
		entry["id"] = ns.ID
		entry["description"] = ns.Description
		entry["authors"] = ns.Authors
		entry["title"] = ns.Title
		entry["summary"] = ns.Summary
		entry["tags"] = strings.Fields(ns.Tags)
		entry["projectUrl"] = ns.ProjectURL
		entry["licenseUrl"] = ns.LicenseURL
		entry["iconUrl"] = ns.IconURL
		entry["requireLicenseAcceptance"] = ns.RequireLicense
		var groups []map[string]any
		for _, g := range ns.DependencyGroups {
			deps := []map[string]any{}
			for _, d := range g.Dependencies {
				deps = append(deps, map[string]any{"@type": "PackageDependency", "id": d.ID, "range": d.Range, "registration": b + "/v3/registration/" + strings.ToLower(d.ID) + "/index.json"})
			}
			gm := map[string]any{"@type": "PackageDependencyGroup", "dependencies": deps}
			if g.TargetFramework != "" {
				gm["targetFramework"] = g.TargetFramework
			}
			groups = append(groups, gm)
		}
		if groups == nil {
			groups = []map[string]any{}
		}
		entry["dependencyGroups"] = groups
	}
	return entry
}

// hostedItems builds registration leaf items for a hosted repository.
func (h *handler) hostedItems(r *http.Request, rp *model.Repository, id string) ([]map[string]any, error) {
	pkgs, err := h.d.Content.PackageVersions(r.Context(), rp.ID, "", id)
	if err != nil {
		return nil, err
	}
	var items []map[string]any
	for _, p := range pkgs {
		var attrs struct {
			Nuspec *Nuspec `json:"nuspec"`
		}
		json.Unmarshal(p.Attrs, &attrs)
		entry := h.catalogEntry(r, id, p.Version, attrs.Nuspec, p.CreatedAt)
		items = append(items, map[string]any{
			"@id": entry["@id"], "@type": "Package", "catalogEntry": entry, "packageContent": entry["packageContent"],
			"registration": h.base(r) + "/v3/registration/" + id + "/index.json",
		})
	}
	return items, nil
}

// proxyRegistration fetches and rewrites the upstream registration index.
// gunzipIfNeeded decompresses bodies that arrived gzip-encoded (nuget.org's
// registration5-gz endpoints) when the transport did not do it for us.
func gunzipIfNeeded(b []byte) []byte {
	if len(b) > 2 && b[0] == 0x1f && b[1] == 0x8b {
		if zr, err := gzip.NewReader(bytes.NewReader(b)); err == nil {
			if out, err := io.ReadAll(io.LimitReader(zr, 64<<20)); err == nil {
				return out
			}
		}
	}
	return b
}

func (h *handler) proxyRegistration(r *http.Request, rp *model.Repository, id string) (map[string]any, error) {
	up := h.upstream(r.Context(), rp)
	if up.Registration == "" {
		return nil, fmt.Errorf("%w: no RegistrationsBaseUrl upstream", repo.ErrUpstream)
	}
	pol := repo.Policy{Kind: repo.Metadata, ContentType: "application/json", UpstreamPath: up.Registration + id + "/index.json"}
	b, _, err := h.d.Engine.ReadAll(r.Context(), rp, "v3/registration/"+id+"/index.json", pol, 32<<20)
	if err != nil {
		return nil, err
	}
	b = gunzipIfNeeded(b)
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("%w: bad registration index", repo.ErrUpstream)
	}
	// Inline pages that are only referenced by URL, so a single document
	// describes every version (clients then never need upstream URLs).
	items, _ := doc["items"].([]any)
	for i, pg := range items {
		page, _ := pg.(map[string]any)
		if page == nil {
			continue
		}
		if _, inline := page["items"]; inline {
			continue
		}
		pid, _ := page["@id"].(string)
		if pid == "" {
			continue
		}
		ppol := repo.Policy{Kind: repo.Metadata, ContentType: "application/json", UpstreamPath: pid}
		pb, _, err := h.d.Engine.ReadAll(r.Context(), rp, "v3/registration/"+id+"/page/"+strconv.Itoa(i)+".json", ppol, 32<<20)
		if err != nil {
			continue
		}
		var full map[string]any
		if json.Unmarshal(gunzipIfNeeded(pb), &full) == nil {
			page["items"] = full["items"]
			items[i] = page
		}
	}
	return doc, nil
}

// rewriteRegistration points every URL in a registration document at us.
func (h *handler) rewriteRegistration(r *http.Request, id string, doc map[string]any, upstreamReg, upstreamFlat string) {
	b := h.base(r)
	regBase := b + "/v3/registration/"
	flatBase := b + "/v3/flatcontainer/"
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, val := range x {
				if s, ok := val.(string); ok {
					switch k {
					case "@id", "registration":
						if upstreamReg != "" && strings.HasPrefix(s, upstreamReg) {
							x[k] = regBase + strings.TrimPrefix(s, upstreamReg)
						} else if m := regURLRe.FindStringSubmatch(s); m != nil {
							// Other registration flavours (semver1, non-gz) used by search results.
							x[k] = regBase + m[1]
						}
					case "packageContent":
						if upstreamFlat != "" && strings.HasPrefix(s, upstreamFlat) {
							x[k] = flatBase + strings.TrimPrefix(s, upstreamFlat)
						} else if i := strings.Index(s, "/flatcontainer/"); i >= 0 {
							x[k] = flatBase + s[i+len("/flatcontainer/"):]
						}
					}
				} else {
					walk(val)
				}
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		}
	}
	walk(doc)
	doc["@id"] = regBase + id + "/index.json"
}

func (h *handler) registrationIndex(r *http.Request, rp *model.Repository, id string) ([]map[string]any, error) {
	switch rp.Type {
	case model.Hosted:
		items, err := h.hostedItems(r, rp, id)
		if err != nil {
			return nil, err
		}
		if len(items) == 0 {
			return nil, repo.ErrNotFound
		}
		return items, nil
	case model.Proxy:
		doc, err := h.proxyRegistration(r, rp, id)
		if err != nil {
			return nil, err
		}
		up := h.upstream(r.Context(), rp)
		h.rewriteRegistration(r, id, doc, up.Registration, up.Flat)
		var items []map[string]any
		for _, pg := range asSlice(doc["items"]) {
			page, _ := pg.(map[string]any)
			for _, it := range asSlice(page["items"]) {
				if m, ok := it.(map[string]any); ok {
					items = append(items, m)
				}
			}
		}
		if len(items) == 0 {
			return nil, repo.ErrNotFound
		}
		return items, nil
	case model.Group:
		members, _ := h.d.Engine.Members(r.Context(), rp)
		seen := map[string]bool{}
		var items []map[string]any
		for _, m := range members {
			its, err := h.registrationIndex(r, m, id)
			if err != nil {
				continue
			}
			for _, it := range its {
				ce, _ := it["catalogEntry"].(map[string]any)
				v, _ := ce["version"].(string)
				nv := NormalizeVersion(v)
				if !seen[nv] {
					seen[nv] = true
					items = append(items, it)
				}
			}
		}
		if len(items) == 0 {
			return nil, repo.ErrNotFound
		}
		return items, nil
	}
	return nil, repo.ErrNotFound
}

var regURLRe = regexp.MustCompile(`^https?://[^/]+/v3/registration[^/]*/(.+)$`)

func asSlice(v any) []any {
	s, _ := v.([]any)
	return s
}

func (h *handler) registration(w http.ResponseWriter, r *http.Request, segs []string) {
	id := strings.ToLower(segs[0])
	if len(segs) != 2 {
		writeJSON(w, 404, map[string]any{"error": "not found"})
		return
	}
	items, err := h.registrationIndex(r, h.repo, id)
	if err != nil {
		mapErr(w, err, h.d)
		return
	}
	ver := func(it map[string]any) string {
		ce, _ := it["catalogEntry"].(map[string]any)
		v, _ := ce["version"].(string)
		return v
	}
	sort.SliceStable(items, func(i, j int) bool { return VersionLess(ver(items[i]), ver(items[j])) })
	if segs[1] != "index.json" {
		// Leaf: <version>.json
		want := NormalizeVersion(strings.TrimSuffix(segs[1], ".json"))
		for _, it := range items {
			if NormalizeVersion(ver(it)) == want {
				leaf := map[string]any{"@id": it["@id"], "@type": []string{"Package", "http://schema.nuget.org/catalog#Permalink"},
					"catalogEntry": it["catalogEntry"], "listed": true, "packageContent": it["packageContent"], "registration": h.base(r) + "/v3/registration/" + id + "/index.json"}
				writeJSON(w, 200, leaf)
				return
			}
		}
		writeJSON(w, 404, map[string]any{"error": "not found"})
		return
	}
	b := h.base(r)
	page := map[string]any{"@id": b + "/v3/registration/" + id + "/index.json#page/" + ver(items[0]) + "/" + ver(items[len(items)-1]),
		"count": len(items), "items": items, "lower": ver(items[0]), "upper": ver(items[len(items)-1]), "parent": b + "/v3/registration/" + id + "/index.json"}
	writeJSON(w, 200, map[string]any{"@id": b + "/v3/registration/" + id + "/index.json", "count": 1, "items": []any{page}})
}

// ---------------------------------------------------------------- search

func (h *handler) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if h.repo.Type == model.Group {
		// Local (hosted member) results first, then the first proxy member's upstream.
		local := h.localSearch(r, q.Get("q"), q.Get("prerelease") == "true", 1000, 0)
		var upstream map[string]any
		members, _ := h.d.Engine.Members(r.Context(), h.repo)
		for _, m := range members {
			if m.Type != model.Proxy {
				continue
			}
			if doc := (&handler{repo: m, d: h.d}).upstreamSearch(r, q); doc != nil {
				upstream = doc
				h.rewriteRegistration(r, "", upstream, "", "")
				break
			}
		}
		data := local
		total := len(local)
		if upstream != nil {
			for _, d := range asSlice(upstream["data"]) {
				if m, ok := d.(map[string]any); ok {
					data = append(data, m)
				}
			}
			if th, ok := upstream["totalHits"].(float64); ok {
				total += int(th)
			}
		}
		writeJSON(w, 200, map[string]any{"totalHits": total, "data": data})
		return
	}
	if h.repo.Type == model.Proxy {
		if doc := h.upstreamSearch(r, q); doc != nil {
			up := h.upstream(r.Context(), h.repo)
			h.rewriteRegistration(r, "", doc, up.Registration, up.Flat)
			writeJSON(w, 200, doc)
			return
		}
		writeJSON(w, 502, map[string]any{"error": "upstream search unavailable"})
		return
	}
	take, _ := strconv.Atoi(q.Get("take"))
	if take <= 0 || take > 100 {
		take = 20
	}
	skip, _ := strconv.Atoi(q.Get("skip"))
	data := h.localSearch(r, q.Get("q"), q.Get("prerelease") == "true", take, skip)
	writeJSON(w, 200, map[string]any{"totalHits": len(data), "data": data})
}

// upstreamSearch queries a proxy's upstream SearchQueryService (URLs are not rewritten).
func (h *handler) upstreamSearch(r *http.Request, q url.Values) map[string]any {
	up := h.upstream(r.Context(), h.repo)
	if up.Search == "" {
		return nil
	}
	resp, err := h.d.Engine.Upstream(r.Context(), h.repo, http.MethodGet, up.Search+"?"+q.Encode(), nil, nil, repo.Policy{Kind: repo.NoCache})
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var doc map[string]any
	if json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&doc) != nil {
		return nil
	}
	return doc
}

// localSearch searches hosted packages of this repository (or, for a group,
// of its hosted members).
func (h *handler) localSearch(r *http.Request, text string, pre bool, take, skip int) []map[string]any {
	repos := []*model.Repository{h.repo}
	if h.repo.Type == model.Group {
		repos, _ = h.d.Engine.Members(r.Context(), h.repo)
	}
	var hits []content.SearchHit
	for _, rp := range repos {
		if rp.Type != model.Hosted {
			continue
		}
		hs, _ := h.d.Content.Search(r.Context(), content.SearchQuery{Q: text, Format: Name, Repo: rp.Name, Limit: 500})
		hits = append(hits, hs...)
	}
	return h.searchDocs(r, hits, pre, take, skip)
}

// searchDocs turns package hits into NuGet search result entries.
func (h *handler) searchDocs(r *http.Request, hits []content.SearchHit, pre bool, take, skip int) []map[string]any {
	byID := map[string][]content.SearchHit{}
	var order []string
	for _, hit := range hits {
		if _, ok := byID[hit.Name]; !ok {
			order = append(order, hit.Name)
		}
		byID[hit.Name] = append(byID[hit.Name], hit)
	}
	data := []map[string]any{}
	for i, id := range order {
		if i < skip || len(data) >= take {
			continue
		}
		var versions []map[string]any
		var latest content.SearchHit
		for _, v := range byID[id] {
			if !pre && IsPrerelease(v.Version) {
				continue
			}
			versions = append(versions, map[string]any{"version": v.Version, "downloads": 0, "@id": h.base(r) + "/v3/registration/" + id + "/" + v.Version + ".json"})
			if latest.Version == "" || VersionLess(latest.Version, v.Version) {
				latest = v
			}
		}
		if len(versions) == 0 {
			continue
		}
		var attrs struct {
			Nuspec *Nuspec `json:"nuspec"`
		}
		json.Unmarshal(latest.Attrs, &attrs)
		e := map[string]any{"@type": "Package", "registration": h.base(r) + "/v3/registration/" + id + "/index.json", "id": id, "version": latest.Version, "versions": versions, "totalDownloads": 0, "verified": false}
		if attrs.Nuspec != nil {
			e["id"] = attrs.Nuspec.ID
			e["description"] = attrs.Nuspec.Description
			e["authors"] = strings.Split(attrs.Nuspec.Authors, ",")
			e["tags"] = strings.Fields(attrs.Nuspec.Tags)
		}
		data = append(data, e)
	}
	return data
}

func (h *handler) autocomplete(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	hits, _ := h.d.Content.Search(r.Context(), content.SearchQuery{Q: q.Get("q"), Format: Name, Limit: 100})
	seen := map[string]bool{}
	data := []string{}
	for _, hit := range hits {
		if !seen[hit.Name] {
			seen[hit.Name] = true
			data = append(data, hit.Name)
		}
	}
	writeJSON(w, 200, map[string]any{"totalHits": len(data), "data": data})
}

// ---------------------------------------------------------------- publish

// publish handles PUT api/v2/package (push) and DELETE api/v2/package/{id}/{version}.
func (h *handler) publish(w http.ResponseWriter, r *http.Request, segs []string) {
	// NuGet sends the API key in X-NuGet-ApiKey; accept our user tokens there.
	if k := r.Header.Get("X-NuGet-ApiKey"); k != "" && r.Header.Get("Authorization") == "" {
		if p, err := h.d.Auth.Login(r.Context(), auth.ClientIP(r), "", k); err == nil {
			r = r.WithContext(auth.WithPrincipal(r.Context(), p))
		}
	}
	switch r.Method {
	case http.MethodPut, http.MethodPost:
		if !format.Authorize(w, r, h.repo, auth.Write, challenge) {
			return
		}
		if h.repo.Type != model.Hosted {
			writeJSON(w, 400, map[string]any{"error": "only hosted repositories accept pushes"})
			return
		}
		var data []byte
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
			if err := r.ParseMultipartForm(512 << 20); err != nil {
				writeJSON(w, 400, map[string]any{"error": "bad multipart"})
				return
			}
			var found bool
			for _, fhs := range r.MultipartForm.File {
				for _, fh := range fhs {
					f, err := fh.Open()
					if err != nil {
						continue
					}
					data, _ = io.ReadAll(io.LimitReader(f, 512<<20))
					f.Close()
					found = true
					break
				}
				if found {
					break
				}
			}
			if !found {
				writeJSON(w, 400, map[string]any{"error": "no package in request"})
				return
			}
		} else {
			data, _ = io.ReadAll(io.LimitReader(r.Body, 512<<20))
		}
		ns, nuspecRaw, err := ReadNupkg(data)
		if err != nil {
			writeJSON(w, 400, map[string]any{"error": "invalid nupkg: " + err.Error()})
			return
		}
		id := strings.ToLower(ns.ID)
		ver := NormalizeVersion(ns.Version)
		base := "v3/flatcontainer/" + id + "/" + ver + "/"
		pkg := packageFor(id, ver, ns)
		if _, err := h.d.Engine.Put(r.Context(), h.repo, base+id+"."+ver+".nupkg", bytes.NewReader(data), repo.PutOptions{ContentType: "application/octet-stream", Package: pkg}); err != nil {
			if errors.Is(err, repo.ErrRedeploy) {
				writeJSON(w, 409, map[string]any{"error": "package version already exists"})
				return
			}
			mapErr(w, err, h.d)
			return
		}
		h.d.Engine.Put(r.Context(), h.repo, base+id+".nuspec", bytes.NewReader(nuspecRaw), repo.PutOptions{ContentType: "application/xml", AllowRedeploy: true})
		w.WriteHeader(http.StatusCreated)
	case http.MethodDelete:
		if !format.Authorize(w, r, h.repo, auth.Delete, challenge) {
			return
		}
		if len(segs) != 5 {
			writeJSON(w, 404, map[string]any{"error": "not found"})
			return
		}
		id, ver := strings.ToLower(segs[3]), NormalizeVersion(segs[4])
		p, err := h.d.Content.Package(r.Context(), h.repo.ID, "", id, ver)
		if err != nil {
			writeJSON(w, 404, map[string]any{"error": "not found"})
			return
		}
		h.d.Content.DeletePackage(r.Context(), p.ID)
		h.d.Engine.Delete(r.Context(), h.repo, "v3/flatcontainer/"+id+"/"+ver+"/"+id+".nuspec")
		w.WriteHeader(http.StatusNoContent)
	default:
		writeJSON(w, 405, map[string]any{"error": "method not allowed"})
	}
}

func mapErr(w http.ResponseWriter, err error, d format.Deps) {
	if errors.Is(err, repo.ErrNotFound) || errors.Is(err, content.ErrNotFound) {
		writeJSON(w, 404, map[string]any{"error": "not found"})
		return
	}
	format.MapError(w, err, d.Log)
}
