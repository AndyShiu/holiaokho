package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/holiaokho/holiaokho/internal/auth"
	"github.com/holiaokho/holiaokho/internal/backup"
	"github.com/holiaokho/holiaokho/internal/config"
	"github.com/holiaokho/holiaokho/internal/format/alpine"
	"github.com/holiaokho/holiaokho/internal/model"
	"github.com/holiaokho/holiaokho/internal/notify"
	"github.com/holiaokho/holiaokho/internal/pkgsign"
	"github.com/holiaokho/holiaokho/internal/repo"
	"github.com/holiaokho/holiaokho/internal/task"
)

// LogSource exposes the in-memory log buffer.
type LogSource interface {
	Tail(n int) []string
}

// adminRoutes mounts routing rules, content selectors, webhooks, email,
// task schedules, system endpoints.
func (a *API) adminRoutes(r chi.Router) {
	r.Route("/routing-rules", func(r chi.Router) {
		r.Get("/", a.need("app:repositories", auth.Read, a.listRoutingRules))
		r.Post("/", a.need("app:repositories", auth.Write, a.createRoutingRule))
		r.Put("/{id}", a.need("app:repositories", auth.Write, a.updateRoutingRule))
		r.Delete("/{id}", a.need("app:repositories", auth.Delete, a.deleteRoutingRule))
		r.Post("/test", a.need("app:repositories", auth.Read, a.testRoutingRule))
	})
	r.Route("/content-selectors", func(r chi.Router) {
		r.Get("/", a.need("app:roles", auth.Read, a.listSelectors))
		r.Post("/", a.need("app:roles", auth.Write, a.createSelector))
		r.Put("/{id}", a.need("app:roles", auth.Write, a.updateSelector))
		r.Delete("/{id}", a.need("app:roles", auth.Delete, a.deleteSelector))
		r.Post("/test", a.need("app:roles", auth.Read, a.testSelector))
	})
	r.Route("/webhooks", func(r chi.Router) {
		r.Get("/", a.need("app:system", auth.Read, a.listWebhooks))
		r.Post("/", a.need("app:system", auth.Write, a.createWebhook))
		r.Put("/{id}", a.need("app:system", auth.Write, a.updateWebhook))
		r.Delete("/{id}", a.need("app:system", auth.Delete, a.deleteWebhook))
		r.Post("/{id}/test", a.need("app:system", auth.Write, a.testWebhook))
	})
	r.Route("/email", func(r chi.Router) {
		r.Get("/", a.need("app:system", auth.Read, a.getEmail))
		r.Put("/", a.need("app:system", auth.Write, a.putEmail))
		r.Post("/test", a.need("app:system", auth.Write, a.testEmail))
	})
	r.Put("/tasks/{name}/schedule", a.need("app:tasks", auth.Write, a.setTaskSchedule))
	r.Post("/cleanup-policies/{id}/preview", a.need("app:repositories", auth.Read, a.previewCleanup))
	r.Put("/storages/{name}", a.need("app:storages", auth.Write, a.updateStorage))
	r.Route("/system", func(r chi.Router) {
		r.Get("/logs", a.need("app:system", auth.Read, a.systemLogs))
		r.Get("/log-level", a.need("app:system", auth.Read, a.getLogLevel))
		r.Put("/log-level", a.need("app:system", auth.Write, a.setLogLevel))
		r.Get("/info", a.need("app:system", auth.Read, a.systemInfo))
		r.Get("/support-zip", a.need("app:system", auth.Read, a.supportZip))
		r.Get("/config", a.need("app:system", auth.Read, a.systemConfig))
		r.Post("/pgp-key", a.need("app:system", auth.Write, a.generatePGPKey))
		r.Post("/rsa-key", a.need("app:system", auth.Write, a.generateRSAKey))
		r.Get("/backup", a.need("app:system", auth.Admin, a.downloadBackup))
		r.Post("/restore", a.need("app:system", auth.Admin, a.restoreBackup))
	})
}

// ----------------------------------------------------------- routing rules

func (a *API) listRoutingRules(w http.ResponseWriter, r *http.Request) {
	rules, err := repo.ListRoutingRules(r.Context(), a.Content.DB.Pool)
	if err != nil {
		a.fail(w, err)
		return
	}
	if rules == nil {
		rules = []repo.RoutingRule{}
	}
	writeJSON(w, 200, rules)
}

func (a *API) createRoutingRule(w http.ResponseWriter, r *http.Request) {
	var rule repo.RoutingRule
	if err := readJSON(r, &rule); err != nil {
		writeErr(w, 400, "body.invalid", "invalid body")
		return
	}
	if rule.Name == "" {
		writeErr(w, 400, "validation", "name required")
		return
	}
	if rule.Mode == "" {
		rule.Mode = "block"
	}
	if err := rule.Compile(); err != nil {
		writeErr(w, 400, "validation", "%s", err.Error())
		return
	}
	rule.ID = uuid.New()
	m, _ := json.Marshal(rule.Matchers)
	if _, err := a.Content.DB.Pool.Exec(r.Context(), `INSERT INTO routing_rules(id,name,description,mode,matchers) VALUES ($1,$2,$3,$4,$5)`, rule.ID, rule.Name, rule.Description, rule.Mode, m); err != nil {
		if strings.Contains(err.Error(), "23505") {
			writeErr(w, 409, "conflict", "already exists")
			return
		}
		a.fail(w, err)
		return
	}
	a.Engine.InvalidateRouting()
	a.audit_(r, "routing_rule.create", "routing_rule", rule.Name, rule)
	writeJSON(w, 201, rule)
}

func (a *API) updateRoutingRule(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "validation", "invalid id")
		return
	}
	var rule repo.RoutingRule
	if err := readJSON(r, &rule); err != nil {
		writeErr(w, 400, "body.invalid", "invalid body")
		return
	}
	if err := rule.Compile(); err != nil {
		writeErr(w, 400, "validation", "%s", err.Error())
		return
	}
	m, _ := json.Marshal(rule.Matchers)
	tag, err := a.Content.DB.Pool.Exec(r.Context(), `UPDATE routing_rules SET name=$2, description=$3, mode=$4, matchers=$5 WHERE id=$1`, id, rule.Name, rule.Description, rule.Mode, m)
	if err != nil {
		a.fail(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		writeErr(w, 404, "not_found", "not found")
		return
	}
	a.Engine.InvalidateRouting()
	rule.ID = id
	writeJSON(w, 200, rule)
}

func (a *API) deleteRoutingRule(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "validation", "invalid id")
		return
	}
	a.Content.DB.Pool.Exec(r.Context(), `DELETE FROM routing_rules WHERE id=$1`, id)
	a.Engine.InvalidateRouting()
	a.Content.Invalidate()
	w.WriteHeader(204)
}

func (a *API) testRoutingRule(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Mode     string   `json:"mode"`
		Matchers []string `json:"matchers"`
		Path     string   `json:"path"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "body.invalid", "invalid body")
		return
	}
	rule := repo.RoutingRule{Mode: in.Mode, Matchers: in.Matchers}
	if rule.Mode == "" {
		rule.Mode = "block"
	}
	if err := rule.Compile(); err != nil {
		writeErr(w, 400, "validation", "%s", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"allowed": rule.Allows(in.Path)})
}

// -------------------------------------------------------- content selectors

func (a *API) listSelectors(w http.ResponseWriter, r *http.Request) {
	list, err := a.Auth.ListSelectors(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	if list == nil {
		list = []auth.ContentSelector{}
	}
	writeJSON(w, 200, list)
}

func (a *API) createSelector(w http.ResponseWriter, r *http.Request) {
	var c auth.ContentSelector
	if err := readJSON(r, &c); err != nil {
		writeErr(w, 400, "body.invalid", "invalid body")
		return
	}
	if err := a.Auth.SaveSelector(r.Context(), &c, true); err != nil {
		if err == auth.ErrConflict {
			a.fail(w, err)
			return
		}
		writeErr(w, 400, "validation", "%s", err.Error())
		return
	}
	a.audit_(r, "content_selector.create", "content_selector", c.Name, c.Expression)
	writeJSON(w, 201, c)
}

func (a *API) updateSelector(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "validation", "invalid id")
		return
	}
	var c auth.ContentSelector
	if err := readJSON(r, &c); err != nil {
		writeErr(w, 400, "body.invalid", "invalid body")
		return
	}
	c.ID = id
	if err := a.Auth.SaveSelector(r.Context(), &c, false); err != nil {
		if err == auth.ErrNotFound {
			a.fail(w, err)
			return
		}
		writeErr(w, 400, "validation", "%s", err.Error())
		return
	}
	writeJSON(w, 200, c)
}

func (a *API) deleteSelector(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "validation", "invalid id")
		return
	}
	if err := a.Auth.DeleteSelector(r.Context(), id); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(204)
}

func (a *API) testSelector(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Expression string            `json:"expression"`
		Format     string            `json:"format"`
		Path       string            `json:"path"`
		Coordinate map[string]string `json:"coordinate"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "body.invalid", "invalid body")
		return
	}
	c := auth.ContentSelector{Expression: in.Expression}
	if err := c.Compile(); err != nil {
		writeErr(w, 400, "validation", "%s", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"matches": c.Match(auth.SelectorInput{Format: in.Format, Path: in.Path, Coord: in.Coordinate})})
}

// ---------------------------------------------------------------- webhooks

func (a *API) listWebhooks(w http.ResponseWriter, r *http.Request) {
	list, err := a.Notify.ListWebhooks(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	if list == nil {
		list = []notify.Webhook{}
	}
	for i := range list {
		list[i].Secret = ""
	}
	writeJSON(w, 200, list)
}

func (a *API) createWebhook(w http.ResponseWriter, r *http.Request) {
	var h notify.Webhook
	if err := readJSON(r, &h); err != nil {
		writeErr(w, 400, "body.invalid", "invalid body")
		return
	}
	h.Enabled = true
	if err := a.Notify.SaveWebhook(r.Context(), &h, true); err != nil {
		writeErr(w, 400, "validation", "%s", err.Error())
		return
	}
	a.audit_(r, "webhook.create", "webhook", h.Name, map[string]any{"url": h.URL, "events": h.Events})
	h.Secret = ""
	writeJSON(w, 201, h)
}

func (a *API) updateWebhook(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "validation", "invalid id")
		return
	}
	var h notify.Webhook
	if err := readJSON(r, &h); err != nil {
		writeErr(w, 400, "body.invalid", "invalid body")
		return
	}
	h.ID = id
	if err := a.Notify.SaveWebhook(r.Context(), &h, false); err != nil {
		writeErr(w, 400, "validation", "%s", err.Error())
		return
	}
	h.Secret = ""
	writeJSON(w, 200, h)
}

func (a *API) deleteWebhook(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "validation", "invalid id")
		return
	}
	a.Notify.DeleteWebhook(r.Context(), id)
	w.WriteHeader(204)
}

func (a *API) testWebhook(w http.ResponseWriter, r *http.Request) {
	a.Notify.Emit(notify.Event{Event: "test", Actor: auth.PrincipalFrom(r.Context()).Username, Data: map[string]any{"message": "hello from Holiaokho"}})
	writeJSON(w, 202, map[string]any{"queued": true})
}

// ------------------------------------------------------------------- email

func (a *API) getEmail(w http.ResponseWriter, r *http.Request) {
	e, _ := a.Notify.EmailConfig(r.Context())
	e.Password = ""
	writeJSON(w, 200, e)
}

func (a *API) putEmail(w http.ResponseWriter, r *http.Request) {
	var e notify.Email
	if err := readJSON(r, &e); err != nil {
		writeErr(w, 400, "body.invalid", "invalid body")
		return
	}
	if e.Password == "" {
		if cur, _ := a.Notify.EmailConfig(r.Context()); cur.Password != "" {
			e.Password = cur.Password
		}
	}
	if err := a.Notify.SaveEmailConfig(r.Context(), e); err != nil {
		a.fail(w, err)
		return
	}
	a.audit_(r, "email.update", "settings", "email", map[string]any{"host": e.Host, "enabled": e.Enabled})
	e.Password = ""
	writeJSON(w, 200, e)
}

func (a *API) testEmail(w http.ResponseWriter, r *http.Request) {
	var in struct {
		To string `json:"to"`
	}
	readJSON(r, &in)
	var to []string
	if in.To != "" {
		to = []string{in.To}
	}
	if err := a.Notify.SendMail(r.Context(), to, "[Holiaokho] test message", "This is a test message from Holiaokho."); err != nil {
		writeErr(w, 502, "email.failed", "%s", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"sent": true})
}

// ------------------------------------------------------------ tasks/cleanup

func (a *API) setTaskSchedule(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Cron    string `json:"cron"`
		Enabled *bool  `json:"enabled"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "body.invalid", "invalid body")
		return
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	if err := a.Tasks.SetSchedule(r.Context(), chi.URLParam(r, "name"), in.Cron, enabled); err != nil {
		writeErr(w, 400, "validation", "%s", err.Error())
		return
	}
	a.audit_(r, "task.schedule", "task", chi.URLParam(r, "name"), in)
	w.WriteHeader(204)
}

func (a *API) previewCleanup(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 400, "validation", "invalid id")
		return
	}
	var p model.CleanupPolicy
	var crit []byte
	if err := a.Content.DB.Pool.QueryRow(r.Context(), `SELECT id, name, format, criteria FROM cleanup_policies WHERE id=$1`, id).Scan(&p.ID, &p.Name, &p.Format, &crit); err != nil {
		writeErr(w, 404, "not_found", "not found")
		return
	}
	json.Unmarshal(crit, &p.Criteria)
	rp, err := a.Content.Repo(r.Context(), r.URL.Query().Get("repository"))
	if err != nil {
		writeErr(w, 404, "repo.not_found", "repository not found")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 200
	}
	pkgs, err := task.PreviewCleanupPolicy(r.Context(), a.Content, rp, &p, limit)
	if err != nil {
		a.fail(w, err)
		return
	}
	if pkgs == nil {
		pkgs = []model.Package{}
	}
	writeJSON(w, 200, map[string]any{"policy": p.Name, "repository": rp.Name, "wouldDelete": len(pkgs), "packages": pkgs})
}

func (a *API) updateStorage(w http.ResponseWriter, r *http.Request) {
	var in struct {
		QuotaBytes *int64 `json:"quotaBytes"`
	}
	if err := readJSON(r, &in); err != nil || in.QuotaBytes == nil {
		writeErr(w, 400, "body.invalid", "quotaBytes required")
		return
	}
	tag, err := a.Content.DB.Pool.Exec(r.Context(), `UPDATE storages SET quota_bytes=$2 WHERE name=$1`, chi.URLParam(r, "name"), *in.QuotaBytes)
	if err != nil {
		a.fail(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		writeErr(w, 404, "not_found", "not found")
		return
	}
	w.WriteHeader(204)
}

// ------------------------------------------------------------------ system

func (a *API) systemLogs(w http.ResponseWriter, r *http.Request) {
	n, _ := strconv.Atoi(r.URL.Query().Get("tail"))
	if n <= 0 {
		n = 200
	}
	var lines []string
	if a.Logs != nil {
		lines = a.Logs.Tail(n)
	}
	if lines == nil {
		lines = []string{}
	}
	if strings.Contains(r.Header.Get("Accept"), "text/plain") {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(strings.Join(lines, "\n") + "\n"))
		return
	}
	writeJSON(w, 200, lines)
}

func (a *API) getLogLevel(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"level": strings.ToLower(a.LogLevel.Level().String())})
}

func (a *API) setLogLevel(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Level string `json:"level"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "body.invalid", "invalid body")
		return
	}
	var lv slog.Level
	if err := lv.UnmarshalText([]byte(in.Level)); err != nil {
		writeErr(w, 400, "validation", "level must be debug, info, warn or error")
		return
	}
	a.LogLevel.Set(lv)
	a.audit_(r, "system.log_level", "system", in.Level, nil)
	writeJSON(w, 200, map[string]any{"level": strings.ToLower(lv.String())})
}

func (a *API) systemInfo(w http.ResponseWriter, r *http.Request) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	host, _ := os.Hostname()
	info := map[string]any{
		"version": a.Version, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH, "cpus": runtime.NumCPU(),
		"goroutines": runtime.NumGoroutine(), "hostname": host, "pid": os.Getpid(), "uptime": time.Since(a.Started).Round(time.Second).String(),
		"memory":   map[string]any{"heapAllocBytes": ms.HeapAlloc, "sysBytes": ms.Sys, "numGC": ms.NumGC},
		"database": a.dbInfo(r), "storages": a.storageInfo(r), "formats": a.Formats.Names(),
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		info["module"] = bi.Main.Path
		var deps []string
		for _, d := range bi.Deps {
			deps = append(deps, d.Path+"@"+d.Version)
		}
		info["dependencies"] = deps
	}
	writeJSON(w, 200, info)
}

func (a *API) dbInfo(r *http.Request) map[string]any {
	var ver string
	a.Content.DB.Pool.QueryRow(r.Context(), `SELECT version()`).Scan(&ver)
	st := a.Content.DB.Pool.Stat()
	var counts = map[string]int64{}
	for _, t := range []string{"repositories", "packages", "assets", "blobs", "users"} {
		var n int64
		a.Content.DB.Pool.QueryRow(r.Context(), `SELECT count(*) FROM `+t).Scan(&n)
		counts[t] = n
	}
	return map[string]any{"server": ver, "connections": map[string]any{"total": st.TotalConns(), "idle": st.IdleConns(), "max": st.MaxConns()}, "rows": counts}
}

func (a *API) storageInfo(r *http.Request) []map[string]any {
	stores, _ := a.Content.ListStorages(r.Context())
	var out []map[string]any
	for _, st := range stores {
		used, count, _ := a.Content.StorageUsage(r.Context(), st.ID)
		_, err := a.Content.Store(st.ID)
		out = append(out, map[string]any{"name": st.Name, "type": st.Type, "usedBytes": used, "blobs": count, "quotaBytes": st.QuotaBytes, "available": err == nil})
	}
	return out
}

func (a *API) systemConfig(w http.ResponseWriter, r *http.Request) {
	c := a.Config
	c.Database.URL = redactURL(c.Database.URL)
	c.Storage.S3.SecretKey = redact(c.Storage.S3.SecretKey)
	c.Auth.AdminPassword = redact(c.Auth.AdminPassword)
	writeJSON(w, 200, c)
}

// generatePGPKey creates a signing key pair for APT/YUM hosted repositories.
func (a *API) generatePGPKey(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	readJSON(r, &in)
	if in.Name == "" {
		in.Name = "Holiaokho"
	}
	if in.Email == "" {
		in.Email = "repo@holiaokho.local"
	}
	priv, pub, err := pkgsign.GenerateKey(in.Name, in.Email)
	if err != nil {
		a.fail(w, err)
		return
	}
	a.audit_(r, "system.pgp_key", "system", in.Email, nil)
	writeJSON(w, 201, map[string]any{"privateKey": priv, "publicKey": pub})
}

// downloadBackup streams a database backup archive (?blobs=true includes blobs).
func (a *API) downloadBackup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="holiaokho-%s.tar.gz"`, time.Now().UTC().Format("20060102-150405")))
	a.audit_(r, "system.backup", "system", "", nil)
	if err := backup.Write(r.Context(), a.Content, a.Version, r.URL.Query().Get("blobs") == "true", w, func(string, ...any) {}); err != nil {
		a.Deps.Log.Error("backup", "err", err)
	}
}

// restoreBackup loads an uploaded archive (body = tar.gz).
func (a *API) restoreBackup(w http.ResponseWriter, r *http.Request) {
	var lines []string
	logf := func(f string, args ...any) { lines = append(lines, fmt.Sprintf(f, args...)) }
	if err := backup.Restore(r.Context(), a.Content, r.Body, logf); err != nil {
		writeErr(w, 400, "restore.failed", "%s", err.Error())
		return
	}
	a.audit_(r, "system.restore", "system", "", lines)
	writeJSON(w, 200, map[string]any{"restored": true, "log": lines})
}

// generateRSAKey creates an RSA key pair for Alpine APKINDEX signing.
func (a *API) generateRSAKey(w http.ResponseWriter, r *http.Request) {
	priv, pub, err := alpine.GenerateKey()
	if err != nil {
		a.fail(w, err)
		return
	}
	a.audit_(r, "system.rsa_key", "system", "", nil)
	writeJSON(w, 201, map[string]any{"privateKey": priv, "publicKey": pub})
}

func redact(s string) string {
	if s == "" {
		return ""
	}
	return "***"
}

func redactURL(u string) string {
	if i := strings.Index(u, "://"); i >= 0 {
		if j := strings.Index(u[i+3:], "@"); j >= 0 {
			return u[:i+3] + "***@" + u[i+3+j+1:]
		}
	}
	return u
}

// supportZip bundles system info, config, health, recent logs and audit
// entries for troubleshooting (Nexus "Support ZIP").
func (a *API) supportZip(w http.ResponseWriter, r *http.Request) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name string, v any) {
		f, _ := zw.Create(name)
		switch x := v.(type) {
		case []byte:
			f.Write(x)
		case string:
			f.Write([]byte(x))
		default:
			b, _ := json.MarshalIndent(v, "", "  ")
			f.Write(b)
		}
	}
	rec := func(h http.HandlerFunc) []byte {
		rw := &captureWriter{hdr: http.Header{}}
		h(rw, r)
		return rw.buf.Bytes()
	}
	add("info.json", rec(a.systemInfo))
	add("config.json", rec(a.systemConfig))
	add("health.json", rec(a.statusCheck))
	add("repositories.json", rec(a.listRepos))
	add("tasks.json", rec(a.listTasks))
	if a.Logs != nil {
		add("logs.txt", strings.Join(a.Logs.Tail(2000), "\n"))
	}
	add("audit.json", rec(a.audit))
	zw.Close()
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="holiaokho-support-%s.zip"`, time.Now().Format("20060102-150405")))
	w.Write(buf.Bytes())
}

type captureWriter struct {
	hdr http.Header
	buf bytes.Buffer
}

func (c *captureWriter) Header() http.Header         { return c.hdr }
func (c *captureWriter) Write(b []byte) (int, error) { return c.buf.Write(b) }
func (c *captureWriter) WriteHeader(int)             {}

var _ = config.Config{}
