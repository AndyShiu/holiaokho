// Package api implements the management REST API under /api/v1. The Web UI
// and the holiao CLI are both clients of this API; nothing is UI-only.
// Errors are {"code": "<stable.key>", "message": "<english>", "params": ...}
// so clients can localise them.
package api

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/holiaokho/holiaokho/internal/auth"
	"github.com/holiaokho/holiaokho/internal/config"
	"github.com/holiaokho/holiaokho/internal/content"
	"github.com/holiaokho/holiaokho/internal/format"
	"github.com/holiaokho/holiaokho/internal/model"
	"github.com/holiaokho/holiaokho/internal/notify"
	"github.com/holiaokho/holiaokho/internal/repo"
	"github.com/holiaokho/holiaokho/internal/secrets"
	"github.com/holiaokho/holiaokho/internal/task"
)

//go:embed openapi.yaml
var openAPISpec []byte

type API struct {
	Content *content.Service
	Engine  *repo.Engine
	Auth    *auth.Service
	Formats *format.Registry
	Tasks   *task.Scheduler
	Deps    format.Deps
	Version string
	Started time.Time
	// OnRepoChange is called after repositories are created/updated/deleted
	// (the server uses it to (re)start Docker port listeners).
	OnRepoChange func()
	Notify       *notify.Service
	Logs         LogSource
	LogLevel     *slog.LevelVar
	Config       config.Config
}

// passwordChangeOnly blocks an account whose password somebody else chose.
//
// This lives in middleware, not in the UI, because a check the client
// performs is a check an attacker skips: the whole point is that the
// bootstrap password may already be known to more people than it should be.
// Only the handful of endpoints needed to see who you are, change the
// password and log out stay reachable.
//
// Package endpoints (/repository, /v2) are deliberately not covered — they
// are mounted elsewhere, and locking them would break CI for everyone the
// moment an administrator resets one developer's password.
func (a *API) passwordChangeOnly(next http.Handler) http.Handler {
	allowed := map[string]string{
		"/session":      "", // whoami, login and logout (any method)
		"/me/password":  http.MethodPut,
		"/auth/methods": http.MethodGet,
		"/status":       http.MethodGet,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFrom(r.Context())
		if p == nil || !p.MustChangePassword {
			next.ServeHTTP(w, r)
			return
		}
		path := r.URL.Path
		if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePath != "" {
			path = rc.RoutePath
		}
		if m, ok := allowed[strings.TrimSuffix(path, "/")]; ok && (m == "" || m == r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		writeErr(w, 403, "auth.password_change_required", "the password must be changed before this account can be used")
	})
}

func (a *API) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(a.passwordChangeOnly)
	r.Get("/status", a.status)
	r.Get("/status/check", a.need("app:status", auth.Read, a.statusCheck))
	r.Get("/formats", a.formats)
	r.Get("/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		w.Write(openAPISpec)
	})

	r.Post("/session", a.login)
	r.Get("/session", a.whoami)
	r.Delete("/session", a.logout)

	r.Route("/repositories", func(r chi.Router) {
		r.Get("/", a.listRepos)
		r.Post("/", a.need("app:repositories", auth.Write, a.createRepo))
		r.Get("/{name}", a.need("app:repositories", auth.Read, a.getRepo))
		r.Put("/{name}", a.need("app:repositories", auth.Write, a.updateRepo))
		r.Delete("/{name}", a.need("app:repositories", auth.Delete, a.deleteRepo))
		r.Post("/{name}/invalidate-cache", a.need("app:repositories", auth.Write, a.invalidateCache))
		r.Post("/{name}/purge-content", a.need("app:repositories", auth.Delete, a.purgeContent))
		r.Get("/{name}/browse", a.browse)
		r.Post("/{name}/upload", a.upload)
		r.Get("/{name}/packages", a.listPackages)
	})
	r.Route("/storages", func(r chi.Router) {
		r.Get("/", a.need("app:storages", auth.Read, a.listStorages))
		r.Post("/", a.need("app:storages", auth.Write, a.createStorage))
		r.Post("/test", a.need("app:storages", auth.Write, a.testStorage))
	})
	r.Route("/users", func(r chi.Router) {
		r.Get("/", a.need("app:users", auth.Read, a.listUsers))
		r.Post("/", a.need("app:users", auth.Write, a.createUser))
		r.Get("/{username}", a.need("app:users", auth.Read, a.getUser))
		r.Put("/{username}", a.need("app:users", auth.Write, a.updateUser))
		r.Delete("/{username}", a.need("app:users", auth.Delete, a.deleteUser))
		r.Put("/{username}/password", a.need("app:users", auth.Write, a.setPassword))
		r.Get("/{username}/tokens", a.need("app:users", auth.Read, a.listTokens))
		r.Post("/{username}/tokens", a.need("app:users", auth.Write, a.createToken))
		r.Delete("/{username}/tokens/{id}", a.need("app:users", auth.Write, a.deleteToken))
	})
	r.Route("/me", func(r chi.Router) {
		r.Put("/password", a.authed(a.changeMyPassword))
		r.Get("/tokens", a.authed(a.listTokens))
		r.Post("/tokens", a.authed(a.createToken))
		r.Delete("/tokens/{id}", a.authed(a.deleteToken))
	})
	r.Route("/roles", func(r chi.Router) {
		r.Get("/", a.need("app:roles", auth.Read, a.listRoles))
		r.Post("/", a.need("app:roles", auth.Write, a.createRole))
		r.Get("/{id}", a.need("app:roles", auth.Read, a.getRole))
		r.Put("/{id}", a.need("app:roles", auth.Write, a.updateRole))
		r.Delete("/{id}", a.need("app:roles", auth.Delete, a.deleteRole))
	})
	r.Get("/search", a.need("app:search", auth.Read, a.search))
	r.Route("/packages", func(r chi.Router) {
		r.Get("/{id}", a.getPackage)
		r.Delete("/{id}", a.deletePackage)
		r.Get("/{id}/assets", a.packageAssets)
		r.Get("/{id}/referrers", a.packageReferrers)
	})
	r.Route("/assets", func(r chi.Router) {
		r.Get("/{id}", a.getAsset)
		r.Delete("/{id}", a.deleteAsset)
	})
	r.Route("/tasks", func(r chi.Router) {
		r.Get("/", a.need("app:tasks", auth.Read, a.listTasks))
		r.Post("/{name}/run", a.need("app:tasks", auth.Write, a.runTask))
		r.Get("/runs", a.need("app:tasks", auth.Read, a.taskRuns))
	})
	r.Route("/cleanup-policies", func(r chi.Router) {
		r.Get("/", a.need("app:repositories", auth.Read, a.listCleanup))
		r.Post("/", a.need("app:repositories", auth.Write, a.createCleanup))
		r.Put("/{id}", a.need("app:repositories", auth.Write, a.updateCleanup))
		r.Delete("/{id}", a.need("app:repositories", auth.Delete, a.deleteCleanup))
		r.Put("/{id}/repositories/{name}", a.need("app:repositories", auth.Write, a.assignCleanup))
		r.Delete("/{id}/repositories/{name}", a.need("app:repositories", auth.Write, a.unassignCleanup))
	})
	r.Get("/audit", a.need("app:system", auth.Read, a.audit))
	a.adminRoutes(r)
	a.authRoutes(r)
	r.NotFound(func(w http.ResponseWriter, r *http.Request) { writeErr(w, 404, "not_found", "not found") })
	return r
}

// ----------------------------------------------------------------- helpers

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, msg string, params ...any) {
	format.WriteError(w, status, code, msg, params...)
}

func readJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 8<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func (a *API) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, content.ErrNotFound), errors.Is(err, auth.ErrNotFound):
		writeErr(w, 404, "not_found", "not found")
	case errors.Is(err, content.ErrConflict), errors.Is(err, auth.ErrConflict):
		writeErr(w, 409, "conflict", "already exists")
	case errors.Is(err, auth.ErrInvalidCreds):
		writeErr(w, 401, "auth.invalid", "invalid credentials")
	case errors.Is(err, auth.ErrRateLimited):
		writeErr(w, 429, "auth.rate_limited", "too many failed login attempts")
	case errors.Is(err, auth.ErrPolicy):
		var pe *auth.PolicyError
		errors.As(err, &pe)
		writeErr(w, 400, pe.Code, pe.Format, pe.Params...)
	default:
		var ve validationError
		if errors.As(err, &ve) {
			writeErr(w, 400, "validation", "%s", ve.Error())
			return
		}
		a.Deps.Log.Error("api error", "err", err)
		writeErr(w, 500, "internal", "internal error")
	}
}

type validationError struct{ msg string }

func (v validationError) Error() string   { return v.msg }
func invalid(f string, args ...any) error { return validationError{fmt.Sprintf(f, args...)} }

// isHTTPS reports whether the client reached us over TLS (directly or via a
// forwarding proxy), so cookies can carry the Secure flag.
func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// need wraps a handler with an application-level permission check.
func (a *API) need(target, action string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFrom(r.Context())
		if p == nil || !p.Can(target, action) {
			if p == nil || p.Anonymous {
				// No WWW-Authenticate here: this API is consumed by the Web UI,
				// and the header makes browsers pop up their native Basic-auth
				// dialog. Package clients authenticate on /repository and /v2,
				// which still send a challenge.
				writeErr(w, 401, "auth.required", "authentication required")
				return
			}
			writeErr(w, 403, "auth.forbidden", "permission denied")
			return
		}
		h(w, r)
	}
}

// authed requires any non-anonymous principal.
func (a *API) authed(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFrom(r.Context())
		if p == nil || p.Anonymous {
			writeErr(w, 401, "auth.required", "authentication required")
			return
		}
		h(w, r)
	}
}

func (a *API) requireRepo(w http.ResponseWriter, r *http.Request, name, action string) *model.Repository {
	rp, err := a.Content.Repo(r.Context(), name)
	if err != nil {
		a.fail(w, err)
		return nil
	}
	if !format.Authorize(w, r, rp, action, `Basic realm="Holiaokho"`) {
		return nil
	}
	return rp
}

func (a *API) audit_(r *http.Request, action, targetType, targetID string, detail any) {
	p := auth.PrincipalFrom(r.Context())
	actor := "anonymous"
	if p != nil {
		actor = p.Username
	}
	d, _ := json.Marshal(detail)
	if d == nil {
		d = []byte("{}")
	}
	a.Content.DB.Pool.Exec(r.Context(), `INSERT INTO audit_log(actor,action,target_type,target_id,detail) VALUES ($1,$2,$3,$4,$5)`, actor, action, targetType, targetID, d)
	if a.Notify != nil {
		repoName := ""
		if targetType == "repository" {
			repoName = targetID
		}
		a.Notify.Emit(notify.Event{Event: action, Repository: repoName, Actor: actor, Data: map[string]any{"targetType": targetType, "targetId": targetID, "detail": json.RawMessage(d)}})
	}
}

// ------------------------------------------------------------------ status

func (a *API) status(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"name": "Holiaokho", "version": a.Version, "uptime": time.Since(a.Started).Round(time.Second).String(),
		"formats": a.Formats.Names(),
	})
}

func (a *API) statusCheck(w http.ResponseWriter, r *http.Request) {
	// Each unhealthy check carries a stable "code" alongside the English
	// message so clients can localise it.
	checks := map[string]any{}
	ok := true
	if err := a.Content.DB.Pool.Ping(r.Context()); err != nil {
		checks["database"] = map[string]any{"healthy": false, "code": "db_unreachable", "message": err.Error()}
		ok = false
	} else {
		checks["database"] = map[string]any{"healthy": true}
	}
	stores, err := a.Content.ListStorages(r.Context())
	if err == nil {
		for _, st := range stores {
			if _, err := a.Content.Store(st.ID); err != nil {
				checks["storage:"+st.Name] = map[string]any{"healthy": false, "code": "storage_unavailable", "message": err.Error()}
				ok = false
				continue
			}
			used, _, _ := a.Content.StorageUsage(r.Context(), st.ID)
			c := map[string]any{"healthy": true, "type": st.Type, "usedBytes": used}
			if st.QuotaBytes > 0 {
				c["quotaBytes"] = st.QuotaBytes
				if used >= st.QuotaBytes {
					c["healthy"], c["code"], c["message"] = false, "quota_exceeded", "quota exceeded"
					ok = false
				} else if used*10 >= st.QuotaBytes*9 {
					c["code"], c["message"] = "quota_warning", "above 90% of quota"
				}
			}
			checks["storage:"+st.Name] = c
		}
	}
	if u, err := a.Auth.User(r.Context(), "admin"); err == nil {
		if okpw, _, _ := auth.VerifyPassword(u.PasswordHash, "admin123"); okpw {
			checks["default_admin_password"] = map[string]any{"healthy": false, "code": "default_admin_password", "message": "admin still uses the default password"}
		} else {
			checks["default_admin_password"] = map[string]any{"healthy": true}
		}
	}
	checks["scheduler"] = map[string]any{"healthy": a.Tasks != nil}
	writeJSON(w, 200, map[string]any{"healthy": ok, "checks": checks})
}

func (a *API) formats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, a.Formats.Names())
}

// ----------------------------------------------------------------- session

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "body.invalid", "invalid body")
		return
	}
	p, err := a.Auth.Login(r.Context(), auth.ClientIP(r), in.Username, in.Password)
	if err != nil {
		a.fail(w, err)
		return
	}
	id, exp, err := a.Auth.CreateSession(r.Context(), p.Username)
	if err != nil {
		a.fail(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: auth.SessionCookie, Value: id, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Expires: exp, Secure: isHTTPS(r)})
	a.audit_(r, "login", "user", p.Username, nil)
	writeJSON(w, 200, map[string]any{"username": p.Username, "roles": p.Roles, "expiresAt": exp,
		"mustChangePassword": p.MustChangePassword})
}

func (a *API) whoami(w http.ResponseWriter, r *http.Request) {
	p := auth.PrincipalFrom(r.Context())
	writeJSON(w, 200, map[string]any{"username": p.Username, "roles": p.Roles, "anonymous": p.Anonymous, "via": p.Via, "privileges": p.Privileges,
		"mustChangePassword": p.MustChangePassword})
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(auth.SessionCookie); err == nil {
		a.Auth.DeleteSession(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: auth.SessionCookie, Value: "", Path: "/", MaxAge: -1})
	w.WriteHeader(204)
}

// ------------------------------------------------------------ repositories

type repoView struct {
	*model.Repository
	Stats content.RepoStats `json:"stats"`
	URL   string            `json:"url"`
}

func (a *API) repoURL(r *http.Request, name string) string {
	return a.Deps.BaseURL(r) + "/repository/" + name
}

// redactRepo returns a copy with secret attributes replaced by "***".
func redactRepo(rp *model.Repository) *model.Repository {
	c := *rp
	c.Attributes = secrets.RedactPaths(rp.Attributes, secrets.RepositoryPaths)
	return &c
}

// keepRedacted restores stored secret values where the client sent "***".
func keepRedacted(existing json.RawMessage, attrs map[string]json.RawMessage) {
	var old map[string]map[string]any
	json.Unmarshal(existing, &old)
	for _, p := range secrets.RepositoryPaths {
		block, field, _ := strings.Cut(p, ".")
		raw, ok := attrs[block]
		if !ok {
			continue
		}
		var m map[string]any
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		if v, _ := m[field].(string); v == secrets.Redacted {
			if ov, ok := old[block][field].(string); ok {
				m[field] = ov
			} else {
				delete(m, field)
			}
			nb, _ := json.Marshal(m)
			attrs[block] = nb
		}
	}
}

// listRepos is open to anyone (Browse works anonymously) but only returns the
// repositories the caller may read. Managers with app:repositories see all.
func (a *API) listRepos(w http.ResponseWriter, r *http.Request) {
	repos, err := a.Content.ListRepos(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	p := auth.PrincipalFrom(r.Context())
	manager := p != nil && p.Can("app:repositories", auth.Read)
	out := make([]repoView, 0, len(repos))
	for _, rp := range repos {
		if !manager && (p == nil || !p.CanRepo(rp.Name, rp.Format, auth.Read)) {
			continue
		}
		st, _ := a.Content.RepoStats(r.Context(), rp.ID)
		out = append(out, repoView{redactRepo(rp), st, a.repoURL(r, rp.Name)})
	}
	writeJSON(w, 200, out)
}

func (a *API) getRepo(w http.ResponseWriter, r *http.Request) {
	rp, err := a.Content.Repo(r.Context(), chi.URLParam(r, "name"))
	if err != nil {
		a.fail(w, err)
		return
	}
	st, _ := a.Content.RepoStats(r.Context(), rp.ID)
	writeJSON(w, 200, repoView{redactRepo(rp), st, a.repoURL(r, rp.Name)})
}

type repoInput struct {
	Name       string                     `json:"name"`
	Format     string                     `json:"format"`
	Type       model.RepoType             `json:"type"`
	Storage    string                     `json:"storage"`
	Online     *bool                      `json:"online"`
	Attributes map[string]json.RawMessage `json:"attributes"`
	// RoutingRule is the rule name ("" = none). Pointer distinguishes "unset" from "clear".
	RoutingRule *string `json:"routingRule"`
}

func (a *API) validateRepo(rp *model.Repository, attrs map[string]json.RawMessage) error {
	f, ok := a.Formats.Get(rp.Format)
	if !ok {
		return invalid("unknown format %q", rp.Format)
	}
	switch rp.Type {
	case model.Hosted, model.Proxy, model.Group:
	default:
		return invalid("type must be hosted, proxy or group")
	}
	if attrs == nil {
		attrs = map[string]json.RawMessage{}
	}
	if rp.Type == model.Proxy {
		var p model.ProxyAttrs
		if raw, ok := attrs["proxy"]; ok {
			json.Unmarshal(raw, &p)
		}
		if !strings.HasPrefix(p.RemoteURL, "http://") && !strings.HasPrefix(p.RemoteURL, "https://") {
			return invalid("proxy.remoteUrl must be an http(s) URL")
		}
		if p.NegativeCacheTTL == 0 {
			p.NegativeCacheTTL = 1
		}
		if p.MetadataMaxAge == 0 {
			p.MetadataMaxAge = 1440
		}
		if p.ContentMaxAge == 0 {
			p.ContentMaxAge = 1440
		}
		raw, _ := json.Marshal(p)
		attrs["proxy"] = raw
	}
	if rp.Type == model.Group {
		var g model.GroupAttrs
		if raw, ok := attrs["group"]; ok {
			json.Unmarshal(raw, &g)
		}
		raw, _ := json.Marshal(g)
		attrs["group"] = raw
	}
	if rp.Type == model.Hosted {
		var h model.HostedAttrs
		if raw, ok := attrs["hosted"]; ok {
			json.Unmarshal(raw, &h)
		}
		switch h.WritePolicy {
		case "":
			h.WritePolicy = model.WriteAllow
		case model.WriteAllow, model.WriteAllowOnce, model.WriteDeny:
		default:
			return invalid("hosted.writePolicy must be allow, allow_once or deny")
		}
		raw, _ := json.Marshal(h)
		attrs["hosted"] = raw
	}
	// Let the core decode first so format validators see Proxy/Hosted.
	tmp, _ := json.Marshal(attrs)
	rp.Attributes = tmp
	probe := *rp
	if err := probeDecode(&probe); err != nil {
		return invalid("%v", err)
	}
	if err := f.ValidateAttributes(&probe, attrs); err != nil {
		return invalid("%v", err)
	}
	rp.Attributes, _ = json.Marshal(attrs)
	return nil
}

func (a *API) resolveRoutingRule(r *http.Request, rp *model.Repository, name string) error {
	if name == "" {
		rp.RoutingRuleID = nil
		return nil
	}
	var id uuid.UUID
	if err := a.Content.DB.Pool.QueryRow(r.Context(), `SELECT id FROM routing_rules WHERE name=$1`, name).Scan(&id); err != nil {
		return invalid("routing rule %q not found", name)
	}
	rp.RoutingRuleID = &id
	return nil
}

func probeDecode(r *model.Repository) error {
	var a struct {
		Proxy  *model.ProxyAttrs  `json:"proxy"`
		Hosted *model.HostedAttrs `json:"hosted"`
	}
	if err := json.Unmarshal(r.Attributes, &a); err != nil {
		return err
	}
	r.Proxy, r.Hosted = a.Proxy, a.Hosted
	return nil
}

func (a *API) createRepo(w http.ResponseWriter, r *http.Request) {
	var in repoInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "body.invalid", "invalid body: %v", err.Error())
		return
	}
	rp := &model.Repository{Name: in.Name, Format: in.Format, Type: in.Type, Online: true}
	if in.Online != nil {
		rp.Online = *in.Online
	}
	if in.Storage != "" {
		st, err := a.Content.StorageByName(r.Context(), in.Storage)
		if err != nil {
			writeErr(w, 400, "storage.unknown", "unknown storage %q", in.Storage)
			return
		}
		rp.StorageID = st.ID
	}
	if err := a.validateRepo(rp, in.Attributes); err != nil {
		a.fail(w, err)
		return
	}
	if in.RoutingRule != nil {
		if err := a.resolveRoutingRule(r, rp, *in.RoutingRule); err != nil {
			a.fail(w, err)
			return
		}
	}
	if err := a.Content.CreateRepo(r.Context(), rp); err != nil {
		if errors.Is(err, content.ErrConflict) {
			a.fail(w, err)
			return
		}
		writeErr(w, 400, "repo.invalid", "%v", err.Error())
		return
	}
	a.audit_(r, "repository.create", "repository", rp.Name, map[string]any{"format": rp.Format, "type": rp.Type})
	if a.OnRepoChange != nil {
		a.OnRepoChange()
	}
	writeJSON(w, 201, redactRepo(rp))
}

func (a *API) updateRepo(w http.ResponseWriter, r *http.Request) {
	rp, err := a.Content.Repo(r.Context(), chi.URLParam(r, "name"))
	if err != nil {
		a.fail(w, err)
		return
	}
	var in repoInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "body.invalid", "invalid body: %v", err.Error())
		return
	}
	upd := *rp
	if in.Online != nil {
		upd.Online = *in.Online
	}
	attrs := in.Attributes
	if attrs == nil {
		json.Unmarshal(rp.Attributes, &attrs)
	} else {
		keepRedacted(rp.Attributes, attrs)
	}
	if err := a.validateRepo(&upd, attrs); err != nil {
		a.fail(w, err)
		return
	}
	if in.RoutingRule != nil {
		if err := a.resolveRoutingRule(r, &upd, *in.RoutingRule); err != nil {
			a.fail(w, err)
			return
		}
	}
	if err := a.Content.UpdateRepo(r.Context(), &upd); err != nil {
		writeErr(w, 400, "repo.invalid", "%v", err.Error())
		return
	}
	a.audit_(r, "repository.update", "repository", rp.Name, map[string]any{"online": upd.Online})
	if a.OnRepoChange != nil {
		a.OnRepoChange()
	}
	writeJSON(w, 200, redactRepo(&upd))
}

func (a *API) deleteRepo(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := a.Content.DeleteRepo(r.Context(), name); err != nil {
		a.fail(w, err)
		return
	}
	a.audit_(r, "repository.delete", "repository", name, nil)
	if a.OnRepoChange != nil {
		a.OnRepoChange()
	}
	w.WriteHeader(204)
}

// purgeContent deletes everything a repository holds. For a proxy this is the
// "really empty the cache" companion to invalidate-cache, which only marks
// entries stale.
func (a *API) purgeContent(w http.ResponseWriter, r *http.Request) {
	rp, err := a.Content.Repo(r.Context(), chi.URLParam(r, "name"))
	if err != nil {
		a.fail(w, err)
		return
	}
	assets, packages, err := a.Content.PurgeRepoContent(r.Context(), rp.ID)
	if err != nil {
		a.fail(w, err)
		return
	}
	a.audit_(r, "repository.purge_content", "repository", rp.Name, map[string]any{"assets": assets, "packages": packages})
	writeJSON(w, 200, map[string]any{"assets": assets, "packages": packages})
}

func (a *API) invalidateCache(w http.ResponseWriter, r *http.Request) {
	rp, err := a.Content.Repo(r.Context(), chi.URLParam(r, "name"))
	if err != nil {
		a.fail(w, err)
		return
	}
	if rp.Type != model.Proxy {
		writeErr(w, 400, "repo.not_proxy", "only proxy repositories have a cache")
		return
	}
	_, err = a.Content.DB.Pool.Exec(r.Context(), `UPDATE assets SET cache_expires_at=now() WHERE repo_id=$1 AND cache_expires_at IS NOT NULL`, rp.ID)
	if err != nil {
		a.fail(w, err)
		return
	}
	a.Content.DB.Pool.Exec(r.Context(), `DELETE FROM assets WHERE repo_id=$1 AND negative`, rp.ID)
	a.audit_(r, "repository.invalidate_cache", "repository", rp.Name, nil)
	w.WriteHeader(204)
}

func (a *API) browse(w http.ResponseWriter, r *http.Request) {
	rp := a.requireRepo(w, r, chi.URLParam(r, "name"), auth.Read)
	if rp == nil {
		return
	}
	dir := strings.Trim(r.URL.Query().Get("path"), "/")
	repos := []*model.Repository{rp}
	if rp.Type == model.Group {
		repos, _ = a.Engine.Members(r.Context(), rp)
	}
	dirs := map[string]bool{}
	files := map[string]*model.Asset{}
	for _, m := range repos {
		ds, fs, err := a.Content.ListChildren(r.Context(), m.ID, dir)
		if err != nil {
			a.fail(w, err)
			return
		}
		for _, d := range ds {
			dirs[d] = true
		}
		for _, f := range fs {
			if _, ok := files[f.Path]; !ok {
				files[f.Path] = f
			}
		}
	}
	var dl []string
	for d := range dirs {
		dl = append(dl, d)
	}
	var fl []*model.Asset
	for _, f := range files {
		fl = append(fl, f)
	}
	sortStrings(dl)
	sortAssets(fl)
	writeJSON(w, 200, map[string]any{"path": dir, "directories": dl, "files": fl})
}

// upload accepts multipart/form-data with fields "file" and optional "path".
func (a *API) upload(w http.ResponseWriter, r *http.Request) {
	rp := a.requireRepo(w, r, chi.URLParam(r, "name"), auth.Write)
	if rp == nil {
		return
	}
	if rp.Type != model.Hosted {
		writeErr(w, 400, "repo.read_only", "only hosted repositories accept uploads")
		return
	}
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeErr(w, 400, "body.invalid", "multipart form expected")
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, 400, "body.invalid", "file field required")
		return
	}
	defer f.Close()
	p := r.FormValue("path")
	if p == "" {
		p = hdr.Filename
	}
	p, err = repo.CleanPath(p)
	if err != nil {
		writeErr(w, 400, "path.invalid", "invalid path")
		return
	}
	fm, _ := a.Formats.Get(rp.Format)
	opt := repo.PutOptions{ContentType: hdr.Header.Get("Content-Type"), AllowRedeploy: r.FormValue("overwrite") == "true"}
	if fm != nil {
		opt.Package = fm.Parse(p)
	}
	asset, err := a.Engine.Put(r.Context(), rp, p, f, opt)
	if err != nil {
		format.MapError(w, err, a.Deps.Log)
		return
	}
	a.audit_(r, "asset.upload", "asset", p, map[string]any{"repository": rp.Name})
	writeJSON(w, 201, asset)
}

func (a *API) listPackages(w http.ResponseWriter, r *http.Request) {
	rp := a.requireRepo(w, r, chi.URLParam(r, "name"), auth.Read)
	if rp == nil {
		return
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	hits, err := a.Content.Search(r.Context(), content.SearchQuery{Repo: rp.Name, Q: q.Get("q"), Name: q.Get("name"), Namespace: q.Get("namespace"), Limit: limit, Offset: offset})
	if err != nil {
		a.fail(w, err)
		return
	}
	if hits == nil {
		hits = []content.SearchHit{}
	}
	writeJSON(w, 200, hits)
}

// ---------------------------------------------------------------- storages

func (a *API) listStorages(w http.ResponseWriter, r *http.Request) {
	st, err := a.Content.ListStorages(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	type view struct {
		model.Storage
		UsedBytes int64 `json:"usedBytes"`
		Blobs     int64 `json:"blobs"`
		Available bool  `json:"available"`
	}
	out := make([]view, 0, len(st))
	for _, s := range st {
		used, n, _ := a.Content.StorageUsage(r.Context(), s.ID)
		_, err := a.Content.Store(s.ID)
		if s.Type == "s3" {
			s.Config = redactS3(s.Config)
		}
		out = append(out, view{s, used, n, err == nil})
	}
	writeJSON(w, 200, out)
}

func redactS3(raw json.RawMessage) json.RawMessage {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return raw
	}
	if _, ok := m["secretKey"]; ok {
		m["secretKey"] = "***"
	}
	b, _ := json.Marshal(m)
	return b
}

// testStorage validates a storage definition (saved or not) by writing,
// reading and deleting a probe blob. "***" secrets are taken from the saved
// storage with the same name.
func (a *API) testStorage(w http.ResponseWriter, r *http.Request) {
	var st model.Storage
	if err := readJSON(r, &st); err != nil {
		writeErr(w, 400, "body.invalid", "invalid body")
		return
	}
	if st.Type == "" {
		writeErr(w, 400, "validation", "type required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	res, err := a.Content.TestStorage(ctx, st)
	if err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "type": st.Type, "message": err.Error()})
		return
	}
	writeJSON(w, 200, res)
}

func (a *API) createStorage(w http.ResponseWriter, r *http.Request) {
	var st model.Storage
	if err := readJSON(r, &st); err != nil {
		writeErr(w, 400, "body.invalid", "invalid body")
		return
	}
	if st.Name == "" || st.Type == "" {
		writeErr(w, 400, "validation", "name and type required")
		return
	}
	if err := a.Content.CreateStorage(r.Context(), &st); err != nil {
		if errors.Is(err, content.ErrConflict) {
			a.fail(w, err)
			return
		}
		writeErr(w, 400, "storage.invalid", "%v", err.Error())
		return
	}
	a.audit_(r, "storage.create", "storage", st.Name, nil)
	writeJSON(w, 201, st)
}

// ------------------------------------------------------------------- users

type userInput struct {
	Username    string   `json:"username"`
	Email       string   `json:"email"`
	DisplayName string   `json:"displayName"`
	Password    string   `json:"password"`
	Active      *bool    `json:"active"`
	Roles       []string `json:"roles"`
}

func (a *API) listUsers(w http.ResponseWriter, r *http.Request) {
	us, err := a.Auth.ListUsers(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, 200, us)
}

func (a *API) getUser(w http.ResponseWriter, r *http.Request) {
	u, err := a.Auth.User(r.Context(), chi.URLParam(r, "username"))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, 200, u)
}

func (a *API) createUser(w http.ResponseWriter, r *http.Request) {
	var in userInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "body.invalid", "invalid body")
		return
	}
	if in.Username == "" || in.Password == "" {
		writeErr(w, 400, "validation", "username and password required")
		return
	}
	if err := a.Auth.CheckPassword(r.Context(), in.Username, in.Password); err != nil {
		a.fail(w, err)
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		a.fail(w, err)
		return
	}
	u := &model.User{Username: in.Username, Email: in.Email, DisplayName: in.DisplayName, PasswordHash: hash, Active: true, Roles: in.Roles}
	if in.Active != nil {
		u.Active = *in.Active
	}
	if err := a.Auth.CreateUser(r.Context(), u); err != nil {
		if errors.Is(err, auth.ErrConflict) {
			a.fail(w, err)
			return
		}
		writeErr(w, 400, "user.invalid", "%v", err.Error())
		return
	}
	a.audit_(r, "user.create", "user", u.Username, map[string]any{"roles": u.Roles})
	writeJSON(w, 201, u)
}

func (a *API) updateUser(w http.ResponseWriter, r *http.Request) {
	u, err := a.Auth.User(r.Context(), chi.URLParam(r, "username"))
	if err != nil {
		a.fail(w, err)
		return
	}
	var in userInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "body.invalid", "invalid body")
		return
	}
	if in.Email != "" {
		u.Email = in.Email
	}
	if in.DisplayName != "" {
		u.DisplayName = in.DisplayName
	}
	if in.Active != nil {
		u.Active = *in.Active
	}
	if in.Roles != nil {
		u.Roles = in.Roles
	}
	if err := a.Auth.UpdateUser(r.Context(), u); err != nil {
		writeErr(w, 400, "user.invalid", "%v", err.Error())
		return
	}
	if in.Password != "" {
		if err := a.Auth.CheckPassword(r.Context(), u.Username, in.Password); err != nil {
			a.fail(w, err)
			return
		}
		if err := a.Auth.SetPassword(r.Context(), u.Username, in.Password, auth.PasswordByAdmin); err != nil {
			a.fail(w, err)
			return
		}
	}
	a.audit_(r, "user.update", "user", u.Username, map[string]any{"roles": u.Roles, "active": u.Active})
	writeJSON(w, 200, u)
}

func (a *API) deleteUser(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "username")
	if err := a.Auth.DeleteUser(r.Context(), name); err != nil {
		a.fail(w, err)
		return
	}
	a.audit_(r, "user.delete", "user", name, nil)
	w.WriteHeader(204)
}

func (a *API) setPassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
	}
	if err := readJSON(r, &in); err != nil || in.Password == "" {
		writeErr(w, 400, "validation", "password required")
		return
	}
	if err := a.Auth.CheckPassword(r.Context(), chi.URLParam(r, "username"), in.Password); err != nil {
		a.fail(w, err)
		return
	}
	if err := a.Auth.SetPassword(r.Context(), chi.URLParam(r, "username"), in.Password, auth.PasswordByAdmin); err != nil {
		a.fail(w, err)
		return
	}
	a.audit_(r, "user.password", "user", chi.URLParam(r, "username"), nil)
	w.WriteHeader(204)
}

func (a *API) changeMyPassword(w http.ResponseWriter, r *http.Request) {
	p := auth.PrincipalFrom(r.Context())
	var in struct {
		Current  string `json:"current"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &in); err != nil || in.Password == "" {
		writeErr(w, 400, "validation", "password required")
		return
	}
	if _, err := a.Auth.Login(r.Context(), auth.ClientIP(r), p.Username, in.Current); err != nil {
		a.fail(w, err)
		return
	}
	// Otherwise an account that owes a password change can satisfy the
	// requirement by typing the same password back, clearing the flag while
	// leaving everyone who already knows it able to log in.
	if in.Password == in.Current {
		writeErr(w, 400, "auth.password_reused", "the new password must be different from the current one")
		return
	}
	if err := a.Auth.CheckPassword(r.Context(), p.Username, in.Password); err != nil {
		a.fail(w, err)
		return
	}
	if err := a.Auth.SetPassword(r.Context(), p.Username, in.Password, auth.PasswordByOwner); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(204)
}

func (a *API) tokenUser(r *http.Request) string {
	if u := chi.URLParam(r, "username"); u != "" {
		return u
	}
	return auth.PrincipalFrom(r.Context()).Username
}

func (a *API) listTokens(w http.ResponseWriter, r *http.Request) {
	ts, err := a.Auth.ListTokens(r.Context(), a.tokenUser(r))
	if err != nil {
		a.fail(w, err)
		return
	}
	if ts == nil {
		ts = []*model.Token{}
	}
	writeJSON(w, 200, ts)
}

func (a *API) createToken(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name      string     `json:"name"`
		ExpiresAt *time.Time `json:"expiresAt"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "body.invalid", "invalid body")
		return
	}
	if in.Name == "" {
		in.Name = "token"
	}
	secret, t, err := a.Auth.CreateToken(r.Context(), a.tokenUser(r), in.Name, in.ExpiresAt)
	if err != nil {
		a.fail(w, err)
		return
	}
	a.audit_(r, "token.create", "user", a.tokenUser(r), map[string]any{"name": in.Name})
	writeJSON(w, 201, map[string]any{"token": t, "secret": secret})
}

func (a *API) deleteToken(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "validation", "invalid id")
		return
	}
	if err := a.Auth.DeleteToken(r.Context(), a.tokenUser(r), id); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(204)
}

// ------------------------------------------------------------------- roles

func (a *API) listRoles(w http.ResponseWriter, r *http.Request) {
	rs, err := a.Auth.ListRoles(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, 200, rs)
}

func (a *API) getRole(w http.ResponseWriter, r *http.Request) {
	role, err := a.Auth.Role(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, 200, role)
}

func (a *API) createRole(w http.ResponseWriter, r *http.Request) {
	var role model.Role
	if err := readJSON(r, &role); err != nil {
		writeErr(w, 400, "body.invalid", "invalid body")
		return
	}
	if err := a.Auth.SaveRole(r.Context(), &role, true); err != nil {
		a.fail(w, err)
		return
	}
	a.audit_(r, "role.create", "role", role.ID, role.Privileges)
	writeJSON(w, 201, role)
}

func (a *API) updateRole(w http.ResponseWriter, r *http.Request) {
	var role model.Role
	if err := readJSON(r, &role); err != nil {
		writeErr(w, 400, "body.invalid", "invalid body")
		return
	}
	role.ID = chi.URLParam(r, "id")
	if err := a.Auth.SaveRole(r.Context(), &role, false); err != nil {
		a.fail(w, err)
		return
	}
	a.audit_(r, "role.update", "role", role.ID, role.Privileges)
	writeJSON(w, 200, role)
}

func (a *API) deleteRole(w http.ResponseWriter, r *http.Request) {
	if err := a.Auth.DeleteRole(r.Context(), chi.URLParam(r, "id")); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(204)
}

// ------------------------------------------------------------------ search

func (a *API) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	hits, err := a.Content.Search(r.Context(), content.SearchQuery{Q: q.Get("q"), Format: q.Get("format"), Repo: q.Get("repository"),
		Namespace: q.Get("namespace"), Name: q.Get("name"), Version: q.Get("version"), Limit: limit, Offset: offset})
	if err != nil {
		a.fail(w, err)
		return
	}
	// Filter by repo read permission.
	p := auth.PrincipalFrom(r.Context())
	out := make([]content.SearchHit, 0, len(hits))
	for _, h := range hits {
		if p.CanRepo(h.RepoName, h.Format, auth.Read) {
			out = append(out, h)
		}
	}
	writeJSON(w, 200, out)
}

// -------------------------------------------------------- packages/assets

func (a *API) pkgRepo(w http.ResponseWriter, r *http.Request, action string) (*model.Package, *model.Repository) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeErr(w, 400, "validation", "invalid id")
		return nil, nil
	}
	p, err := a.Content.PackageByID(r.Context(), id)
	if err != nil {
		a.fail(w, err)
		return nil, nil
	}
	var rp *model.Repository
	for _, cand := range a.reposByID(r) {
		if cand.ID == p.RepoID {
			rp = cand
		}
	}
	if rp == nil {
		writeErr(w, 404, "not_found", "not found")
		return nil, nil
	}
	if !format.Authorize(w, r, rp, action, `Basic realm="Holiaokho"`) {
		return nil, nil
	}
	return p, rp
}

func (a *API) reposByID(r *http.Request) []*model.Repository {
	repos, _ := a.Content.ListRepos(r.Context())
	return repos
}

func (a *API) getPackage(w http.ResponseWriter, r *http.Request) {
	p, rp := a.pkgRepo(w, r, auth.Read)
	if p == nil {
		return
	}
	assets, _ := a.Content.PackageAssets(r.Context(), p.ID)
	writeJSON(w, 200, map[string]any{"package": p, "repository": rp.Name, "format": rp.Format, "assets": assets})
}

func (a *API) packageAssets(w http.ResponseWriter, r *http.Request) {
	p, _ := a.pkgRepo(w, r, auth.Read)
	if p == nil {
		return
	}
	assets, err := a.Content.PackageAssets(r.Context(), p.ID)
	if err != nil {
		a.fail(w, err)
		return
	}
	if assets == nil {
		assets = []*model.Asset{}
	}
	writeJSON(w, 200, assets)
}

// packageReferrers lists the artifacts attached to a Docker image — cosign
// signatures, SBOMs, build attestations.
//
// The registry protocol already exposes this at /v2/<name>/referrers/<digest>,
// but that endpoint speaks in image names and digests the UI does not hold,
// and answers with an OCI index rather than something a page can render. This
// is the same data addressed the way the rest of the API is: by package id.
func (a *API) packageReferrers(w http.ResponseWriter, r *http.Request) {
	p, rp := a.pkgRepo(w, r, auth.Read)
	if p == nil {
		return
	}
	out := []map[string]any{}
	if rp.Format != "docker" {
		writeJSON(w, 200, out)
		return
	}

	// The image name lives on the package, the digest on its manifest asset —
	// a tag can be repointed, so the digest has to come from what is stored
	// rather than from the version string.
	var pattrs map[string]any
	json.Unmarshal(p.Attrs, &pattrs)
	image, _ := pattrs["image"].(string)
	if image == "" {
		writeJSON(w, 200, out)
		return
	}
	assets, err := a.Content.PackageAssets(r.Context(), p.ID)
	if err != nil {
		a.fail(w, err)
		return
	}
	var digest string
	for _, as := range assets {
		var aattrs map[string]any
		json.Unmarshal(as.Attrs, &aattrs)
		if d, _ := aattrs["digest"].(string); d != "" {
			digest = d
			break
		}
	}
	if digest == "" {
		writeJSON(w, 200, out)
		return
	}

	list, err := a.Content.Referrers(r.Context(), rp.ID, image, digest, "")
	if err != nil {
		a.fail(w, err)
		return
	}
	for _, l := range list {
		out = append(out, map[string]any{
			"digest":       l.Digest,
			"mediaType":    l.MediaType,
			"artifactType": l.ArtifactType,
			"size":         l.Size,
			"annotations":  l.Annotations,
			// Classified here rather than in the UI: the mapping from an
			// artifact type to "this is a signature" is knowledge about the
			// ecosystem, and belongs with the code that already knows it.
			"kind": referrerKind(l.ArtifactType, l.MediaType),
		})
	}
	writeJSON(w, 200, out)
}

// referrerKind gives a stable label the UI can translate, instead of asking a
// page to pattern-match media types it should not need to know about.
func referrerKind(artifactType, mediaType string) string {
	t := strings.ToLower(artifactType + " " + mediaType)
	switch {
	case strings.Contains(t, "cosign") && strings.Contains(t, "sign"):
		return "signature"
	case strings.Contains(t, "in-toto"), strings.Contains(t, "attestation"):
		return "attestation"
	case strings.Contains(t, "spdx"), strings.Contains(t, "cyclonedx"), strings.Contains(t, "sbom"):
		return "sbom"
	case strings.Contains(t, "sig"):
		return "signature"
	}
	return "other"
}

func (a *API) deletePackage(w http.ResponseWriter, r *http.Request) {
	p, rp := a.pkgRepo(w, r, auth.Delete)
	if p == nil {
		return
	}
	if err := a.Content.DeletePackage(r.Context(), p.ID); err != nil {
		a.fail(w, err)
		return
	}
	a.audit_(r, "package.delete", "package", strconv.FormatInt(p.ID, 10), map[string]any{"repository": rp.Name, "name": p.Name, "version": p.Version})
	if hook, ok := a.Formats.Get(rp.Format); ok {
		if pd, ok := hook.(interface {
			AfterDelete(r *http.Request, d format.Deps, rp *model.Repository, p *model.Package)
		}); ok {
			pd.AfterDelete(r, a.Deps, rp, p)
		}
	}
	w.WriteHeader(204)
}

func (a *API) assetRepo(w http.ResponseWriter, r *http.Request, action string) (*model.Asset, *model.Repository) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeErr(w, 400, "validation", "invalid id")
		return nil, nil
	}
	as, err := a.Content.AssetByID(r.Context(), id)
	if err != nil {
		a.fail(w, err)
		return nil, nil
	}
	for _, rp := range a.reposByID(r) {
		if rp.ID == as.RepoID {
			if !format.Authorize(w, r, rp, action, `Basic realm="Holiaokho"`) {
				return nil, nil
			}
			return as, rp
		}
	}
	writeErr(w, 404, "not_found", "not found")
	return nil, nil
}

func (a *API) getAsset(w http.ResponseWriter, r *http.Request) {
	as, rp := a.assetRepo(w, r, auth.Read)
	if as == nil {
		return
	}
	writeJSON(w, 200, map[string]any{"asset": as, "repository": rp.Name, "downloadUrl": a.repoURL(r, rp.Name) + "/" + as.Path})
}

func (a *API) deleteAsset(w http.ResponseWriter, r *http.Request) {
	as, rp := a.assetRepo(w, r, auth.Delete)
	if as == nil {
		return
	}
	if err := a.Content.DeleteAsset(r.Context(), rp.ID, as.Path); err != nil {
		a.fail(w, err)
		return
	}
	a.audit_(r, "asset.delete", "asset", as.Path, map[string]any{"repository": rp.Name})
	w.WriteHeader(204)
}

// ------------------------------------------------------------------- tasks

func (a *API) listTasks(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, a.Tasks.List())
}

func (a *API) runTask(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := a.Tasks.RunNow(name); err != nil {
		writeErr(w, 404, "task.unknown", "%v", err.Error())
		return
	}
	a.audit_(r, "task.run", "task", name, nil)
	writeJSON(w, 202, map[string]any{"started": name})
}

func (a *API) taskRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := a.Tasks.Runs(r.Context(), r.URL.Query().Get("task"), 50)
	if err != nil {
		a.fail(w, err)
		return
	}
	if runs == nil {
		runs = []model.TaskRun{}
	}
	writeJSON(w, 200, runs)
}

// ---------------------------------------------------------- cleanup policy

func (a *API) listCleanup(w http.ResponseWriter, r *http.Request) {
	ps, err := task.ListCleanupPolicies(r.Context(), a.Content.DB)
	if err != nil {
		a.fail(w, err)
		return
	}
	if ps == nil {
		ps = []task.CleanupPolicyView{}
	}
	writeJSON(w, 200, ps)
}

func (a *API) createCleanup(w http.ResponseWriter, r *http.Request) {
	var p model.CleanupPolicy
	if err := readJSON(r, &p); err != nil {
		writeErr(w, 400, "body.invalid", "invalid body")
		return
	}
	if p.Name == "" {
		writeErr(w, 400, "validation", "name required")
		return
	}
	p.ID = uuid.New()
	if err := task.SaveCleanupPolicy(r.Context(), a.Content.DB, &p, true); err != nil {
		a.fail(w, err)
		return
	}
	a.audit_(r, "cleanup.create", "cleanup_policy", p.Name, p.Criteria)
	writeJSON(w, 201, p)
}

func (a *API) updateCleanup(w http.ResponseWriter, r *http.Request) {
	var p model.CleanupPolicy
	if err := readJSON(r, &p); err != nil {
		writeErr(w, 400, "body.invalid", "invalid body")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "validation", "invalid id")
		return
	}
	p.ID = id
	if err := task.SaveCleanupPolicy(r.Context(), a.Content.DB, &p, false); err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, 200, p)
}

func (a *API) deleteCleanup(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "validation", "invalid id")
		return
	}
	if _, err := a.Content.DB.Pool.Exec(r.Context(), `DELETE FROM cleanup_policies WHERE id=$1`, id); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(204)
}

func (a *API) assignCleanup(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "validation", "invalid id")
		return
	}
	rp, err := a.Content.Repo(r.Context(), chi.URLParam(r, "name"))
	if err != nil {
		a.fail(w, err)
		return
	}
	if _, err := a.Content.DB.Pool.Exec(r.Context(), `INSERT INTO repo_cleanup_policies(repo_id,policy_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, rp.ID, id); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(204)
}

func (a *API) unassignCleanup(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "validation", "invalid id")
		return
	}
	rp, err := a.Content.Repo(r.Context(), chi.URLParam(r, "name"))
	if err != nil {
		a.fail(w, err)
		return
	}
	a.Content.DB.Pool.Exec(r.Context(), `DELETE FROM repo_cleanup_policies WHERE repo_id=$1 AND policy_id=$2`, rp.ID, id)
	w.WriteHeader(204)
}

// ------------------------------------------------------------------- audit

func (a *API) audit(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := a.Content.DB.Pool.Query(r.Context(), `SELECT id, at, actor, action, target_type, target_id, detail FROM audit_log ORDER BY at DESC LIMIT $1`, limit)
	if err != nil {
		a.fail(w, err)
		return
	}
	defer rows.Close()
	type entry struct {
		ID         int64           `json:"id"`
		At         time.Time       `json:"at"`
		Actor      string          `json:"actor"`
		Action     string          `json:"action"`
		TargetType string          `json:"targetType"`
		TargetID   string          `json:"targetId"`
		Detail     json.RawMessage `json:"detail"`
	}
	out := []entry{}
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.ID, &e.At, &e.Actor, &e.Action, &e.TargetType, &e.TargetID, &e.Detail); err != nil {
			a.fail(w, err)
			return
		}
		out = append(out, e)
	}
	writeJSON(w, 200, out)
}
