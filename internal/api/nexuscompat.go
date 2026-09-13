package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/holiaokho/holiaokho/internal/auth"
	"github.com/holiaokho/holiaokho/internal/content"
	"github.com/holiaokho/holiaokho/internal/model"
	"github.com/holiaokho/holiaokho/internal/repo"
)

// NexusCompatRouter serves the subset of the Nexus Repository REST API
// (/service/rest/v1) that CI scripts commonly call, so pipelines written
// against Nexus keep working after a swap. Everything else answers 501
// with a pointer to the native API.
func (a *API) NexusCompatRouter() http.Handler {
	r := chi.NewRouter()
	r.Get("/status", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	r.Get("/status/writable", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	r.Get("/status/check", a.need("app:status", auth.Read, a.statusCheck))
	r.Get("/repositories", a.nxRepositories)
	r.Get("/repositorySettings", a.need("app:repositories", auth.Read, a.nxRepositories))
	r.Get("/search", a.need("app:search", auth.Read, a.nxSearch(false, false)))
	r.Get("/search/assets", a.need("app:search", auth.Read, a.nxSearch(true, false)))
	r.Get("/search/assets/download", a.need("app:search", auth.Read, a.nxSearch(true, true)))
	r.Get("/components", a.nxComponents)
	r.Get("/components/{id}", a.nxComponent)
	r.Delete("/components/{id}", a.deletePackage)
	r.Post("/components", a.nxUpload)
	r.Get("/assets", a.nxAssets)
	r.Get("/assets/{id}", a.nxAsset)
	r.Delete("/assets/{id}", a.deleteAsset)
	r.Get("/formats/upload-specs", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, []any{}) })
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, 501, "nexus.unsupported", "this Nexus endpoint is not emulated; use /api/v1 (see /api/v1/openapi.yaml)")
	})
	return r
}

func nxFormat(f string) string {
	if f == "maven" {
		return "maven2"
	}
	return f
}

func (a *API) nxRepositories(w http.ResponseWriter, r *http.Request) {
	repos, err := a.Content.ListRepos(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	p := auth.PrincipalFrom(r.Context())
	out := []map[string]any{}
	for _, rp := range repos {
		if p != nil && !p.CanRepo(rp.Name, rp.Format, auth.Read) {
			continue
		}
		e := map[string]any{"name": rp.Name, "format": nxFormat(rp.Format), "type": string(rp.Type), "url": a.repoURL(r, rp.Name), "online": rp.Online, "attributes": map[string]any{}}
		if rp.Proxy != nil {
			e["attributes"] = map[string]any{"proxy": map[string]any{"remoteUrl": rp.Proxy.RemoteURL}}
		}
		if rp.Type == model.Group {
			e["group"] = map[string]any{"memberNames": rp.Members}
		}
		out = append(out, e)
	}
	writeJSON(w, 200, out)
}

// nxComponent converts a package to the Nexus component shape.
func (a *API) nxComponent_(r *http.Request, h content.SearchHit) map[string]any {
	assets, _ := a.Content.PackageAssets(r.Context(), h.ID)
	var al []map[string]any
	for _, as := range assets {
		al = append(al, a.nxAsset_(r, as, h.RepoName, h.Format))
	}
	if al == nil {
		al = []map[string]any{}
	}
	group := h.Namespace
	return map[string]any{"id": nxID(h.RepoName, "pkg", h.ID), "repository": h.RepoName, "format": nxFormat(h.Format), "group": group, "name": h.Name, "version": h.Version, "assets": al}
}

func (a *API) nxAsset_(r *http.Request, as *model.Asset, repoName, format string) map[string]any {
	sum := map[string]any{}
	if as.BlobDigest != nil {
		sum["sha256"] = strings.TrimPrefix(*as.BlobDigest, "sha256:")
	}
	return map[string]any{"id": nxID(repoName, "asset", as.ID), "repository": repoName, "format": nxFormat(format), "path": as.Path,
		"downloadUrl": a.repoURL(r, repoName) + "/" + as.Path, "contentType": as.ContentType, "fileSize": as.Size, "checksum": sum,
		"lastModified": as.UpdatedAt, "lastDownloaded": as.LastDownloadedAt, "blobCreated": as.CreatedAt}
}

// nxID mimics Nexus' opaque ids (base64 of repo:kind:id) while staying
// decodable by our own handlers.
func nxID(repoName, kind string, id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(repoName + ":" + kind + ":" + strconv.FormatInt(id, 10)))
}

func decodeNxID(s string) (string, string, int64) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return "", "", n
		}
		return "", "", 0
	}
	parts := strings.SplitN(string(b), ":", 3)
	if len(parts) != 3 {
		return "", "", 0
	}
	n, _ := strconv.ParseInt(parts[2], 10, 64)
	return parts[0], parts[1], n
}

func (a *API) nxQuery(r *http.Request) content.SearchQuery {
	q := r.URL.Query()
	sq := content.SearchQuery{Q: q.Get("q"), Repo: q.Get("repository"), Format: q.Get("format"), Namespace: q.Get("group"), Name: q.Get("name"), Version: q.Get("version"), Limit: 50}
	if sq.Format == "maven2" {
		sq.Format = "maven"
	}
	for _, k := range []string{"maven.groupId", "npm.scope", "docker.imageName"} {
		if v := q.Get(k); v != "" && sq.Namespace == "" {
			sq.Namespace = v
		}
	}
	for _, k := range []string{"maven.artifactId", "docker.imageTag"} {
		if v := q.Get(k); v != "" && sq.Name == "" {
			sq.Name = v
		}
	}
	if v := q.Get("maven.baseVersion"); v != "" {
		sq.Version = v
	}
	if tok := q.Get("continuationToken"); tok != "" {
		sq.Offset, _ = strconv.Atoi(tok)
	}
	return sq
}

func (a *API) nxSearch(assets, download bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sq := a.nxQuery(r)
		hits, err := a.Content.Search(r.Context(), sq)
		if err != nil {
			a.fail(w, err)
			return
		}
		p := auth.PrincipalFrom(r.Context())
		var items []map[string]any
		var sort = r.URL.Query().Get("sort")
		for _, h := range hits {
			if !p.CanRepo(h.RepoName, h.Format, auth.Read) {
				continue
			}
			if assets {
				al, _ := a.Content.PackageAssets(r.Context(), h.ID)
				for _, as := range al {
					if ext := r.URL.Query().Get("maven.extension"); ext != "" && !strings.HasSuffix(as.Path, "."+ext) {
						continue
					}
					items = append(items, a.nxAsset_(r, as, h.RepoName, h.Format))
				}
			} else {
				items = append(items, a.nxComponent_(r, h))
			}
		}
		if items == nil {
			items = []map[string]any{}
		}
		if download {
			if len(items) == 0 {
				writeErr(w, 404, "not_found", "no asset matched")
				return
			}
			pick := items[0]
			if sort == "version" && len(items) > 1 {
				pick = items[len(items)-1]
			}
			http.Redirect(w, r, pick["downloadUrl"].(string), http.StatusFound)
			return
		}
		var next any
		if len(hits) == sq.Limit {
			next = strconv.Itoa(sq.Offset + sq.Limit)
		}
		writeJSON(w, 200, map[string]any{"items": items, "continuationToken": next})
	}
}

func (a *API) nxComponents(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("repository")
	if name == "" {
		writeErr(w, 422, "validation", "repository is required")
		return
	}
	rp := a.requireRepo(w, r, name, auth.Read)
	if rp == nil {
		return
	}
	sq := a.nxQuery(r)
	sq.Repo = name
	hits, err := a.Content.Search(r.Context(), sq)
	if err != nil {
		a.fail(w, err)
		return
	}
	items := []map[string]any{}
	for _, h := range hits {
		items = append(items, a.nxComponent_(r, h))
	}
	var next any
	if len(hits) == sq.Limit {
		next = strconv.Itoa(sq.Offset + sq.Limit)
	}
	writeJSON(w, 200, map[string]any{"items": items, "continuationToken": next})
}

func (a *API) nxComponent(w http.ResponseWriter, r *http.Request) {
	_, _, id := decodeNxID(chi.URLParam(r, "id"))
	rctx := chi.RouteContext(r.Context())
	rctx.URLParams.Add("id", strconv.FormatInt(id, 10))
	p, rp := a.pkgRepo(w, r, auth.Read)
	if p == nil {
		return
	}
	writeJSON(w, 200, a.nxComponent_(r, content.SearchHit{Package: *p, RepoName: rp.Name, Format: rp.Format}))
}

func (a *API) nxAssets(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("repository")
	rp := a.requireRepo(w, r, name, auth.Read)
	if rp == nil {
		return
	}
	assets, err := a.Content.ListAssets(r.Context(), rp.ID, "", 200)
	if err != nil {
		a.fail(w, err)
		return
	}
	items := []map[string]any{}
	for _, as := range assets {
		items = append(items, a.nxAsset_(r, as, rp.Name, rp.Format))
	}
	writeJSON(w, 200, map[string]any{"items": items, "continuationToken": nil})
}

func (a *API) nxAsset(w http.ResponseWriter, r *http.Request) {
	_, _, id := decodeNxID(chi.URLParam(r, "id"))
	chi.RouteContext(r.Context()).URLParams.Add("id", strconv.FormatInt(id, 10))
	as, rp := a.assetRepo(w, r, auth.Read)
	if as == nil {
		return
	}
	writeJSON(w, 200, a.nxAsset_(r, as, rp.Name, rp.Format))
}

// nxUpload implements POST /components?repository=x (multipart) for the
// raw and maven forms Nexus documents: raw.directory + raw.assetN /
// raw.assetN.filename, or maven2.groupId/artifactId/version + maven2.assetN
// with maven2.assetN.extension.
func (a *API) nxUpload(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("repository")
	rp := a.requireRepo(w, r, name, auth.Write)
	if rp == nil {
		return
	}
	if err := r.ParseMultipartForm(256 << 20); err != nil {
		writeErr(w, 400, "body.invalid", "multipart expected")
		return
	}
	fm, _ := a.Formats.Get(rp.Format)
	n := 0
	for i := 1; i <= 20; i++ {
		var key string
		switch rp.Format {
		case "raw":
			key = "raw.asset" + strconv.Itoa(i)
		case "maven":
			key = "maven2.asset" + strconv.Itoa(i)
		default:
			key = rp.Format + ".asset"
			if i > 1 {
				key += strconv.Itoa(i)
			}
		}
		fhs := r.MultipartForm.File[key]
		if len(fhs) == 0 {
			if i == 1 && rp.Format != "raw" && rp.Format != "maven" {
				continue
			}
			break
		}
		f, err := fhs[0].Open()
		if err != nil {
			continue
		}
		var p string
		switch rp.Format {
		case "raw":
			fn := r.FormValue(key + ".filename")
			if fn == "" {
				fn = fhs[0].Filename
			}
			p = strings.Trim(r.FormValue("raw.directory"), "/") + "/" + fn
		case "maven":
			g, art, v := r.FormValue("maven2.groupId"), r.FormValue("maven2.artifactId"), r.FormValue("maven2.version")
			ext := r.FormValue(key + ".extension")
			cls := r.FormValue(key + ".classifier")
			fn := art + "-" + v
			if cls != "" {
				fn += "-" + cls
			}
			p = strings.ReplaceAll(g, ".", "/") + "/" + art + "/" + v + "/" + fn + "." + ext
		default:
			p = fhs[0].Filename
		}
		cp, err := repo.CleanPath(p)
		if err != nil {
			f.Close()
			writeErr(w, 400, "path.invalid", "invalid path %q", p)
			return
		}
		opt := repo.PutOptions{ContentType: fhs[0].Header.Get("Content-Type")}
		if fm != nil {
			opt.Package = fm.Parse(cp)
		}
		if _, err := a.Engine.Put(r.Context(), rp, cp, f, opt); err != nil {
			f.Close()
			writeErr(w, 400, "upload.failed", "%s", err.Error())
			return
		}
		f.Close()
		n++
	}
	if n == 0 {
		writeErr(w, 400, "validation", "no assets in request")
		return
	}
	a.audit_(r, "asset.upload", "repository", rp.Name, map[string]any{"count": n, "via": "nexus-compat"})
	w.WriteHeader(204)
}

var _ = json.Marshal
