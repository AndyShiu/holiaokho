// Package server wires configuration, database, storage, auth, formats and
// the HTTP routers into a running Holiaokho instance.
package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/holiaokho/holiaokho/internal/api"
	"github.com/holiaokho/holiaokho/internal/auth"
	"github.com/holiaokho/holiaokho/internal/backup"
	"github.com/holiaokho/holiaokho/internal/config"
	"github.com/holiaokho/holiaokho/internal/content"
	"github.com/holiaokho/holiaokho/internal/db"
	"github.com/holiaokho/holiaokho/internal/format"
	"github.com/holiaokho/holiaokho/internal/format/alpine"
	"github.com/holiaokho/holiaokho/internal/format/ansible"
	"github.com/holiaokho/holiaokho/internal/format/apt"
	"github.com/holiaokho/holiaokho/internal/format/cargo"
	"github.com/holiaokho/holiaokho/internal/format/cocoapods"
	"github.com/holiaokho/holiaokho/internal/format/composer"
	"github.com/holiaokho/holiaokho/internal/format/conan"
	"github.com/holiaokho/holiaokho/internal/format/conda"
	"github.com/holiaokho/holiaokho/internal/format/cran"
	"github.com/holiaokho/holiaokho/internal/format/docker"
	"github.com/holiaokho/holiaokho/internal/format/gitlfs"
	"github.com/holiaokho/holiaokho/internal/format/goproxy"
	"github.com/holiaokho/holiaokho/internal/format/helm"
	"github.com/holiaokho/holiaokho/internal/format/huggingface"
	"github.com/holiaokho/holiaokho/internal/format/maven"
	"github.com/holiaokho/holiaokho/internal/format/npm"
	"github.com/holiaokho/holiaokho/internal/format/nuget"
	"github.com/holiaokho/holiaokho/internal/format/p2"
	"github.com/holiaokho/holiaokho/internal/format/pub"
	"github.com/holiaokho/holiaokho/internal/format/pypi"
	"github.com/holiaokho/holiaokho/internal/format/raw"
	"github.com/holiaokho/holiaokho/internal/format/rubygems"
	"github.com/holiaokho/holiaokho/internal/format/swift"
	"github.com/holiaokho/holiaokho/internal/format/terraform"
	"github.com/holiaokho/holiaokho/internal/format/yum"
	"github.com/holiaokho/holiaokho/internal/model"
	"github.com/holiaokho/holiaokho/internal/notify"
	"github.com/holiaokho/holiaokho/internal/repo"
	"github.com/holiaokho/holiaokho/internal/secrets"
	"github.com/holiaokho/holiaokho/internal/task"
	"github.com/holiaokho/holiaokho/internal/update"
	"github.com/holiaokho/holiaokho/internal/vuln"
	"github.com/holiaokho/holiaokho/web"
)

// Version is the build's version string. Releases override it at link time
// with -ldflags "-X .../internal/server.Version=..." so the running binary can
// say which build it is; the fallback below only applies to local builds.
var Version = "1.3.5"

type Server struct {
	Updates *update.Checker
	Cfg     config.Config
	Log     *slog.Logger
	DB      *db.DB
	Content *content.Service
	Auth    *auth.Service
	Engine  *repo.Engine
	Formats *format.Registry
	Tasks   *task.Scheduler
	API     *api.API
	Deps    format.Deps
	Docker  *docker.Format
	Tokens  *docker.TokenIssuer
	Notify  *notify.Service
	Sys     *System

	main    *http.Server
	tlsSrv  *http.Server
	metrics metrics

	mu         sync.Mutex
	listeners  map[int]*http.Server // docker port connectors
	subdomains map[string]string    // docker subdomain label -> repo name
}

type metrics struct {
	requests [6]atomic.Int64 // by status class 0..5 (1xx..5xx)
	started  time.Time
}

func New(ctx context.Context, cfg config.Config, log *slog.Logger, sys *System) (*Server, error) {
	if sys == nil {
		_, sys = NewSystemLogger(cfg.Log.Level, cfg.Log.Format)
	}
	if err := auth.SetTrustedProxies(cfg.Server.TrustedProxies); err != nil {
		return nil, err
	}
	kr, err := secrets.Init(cfg.Secrets.Key, cfg.Secrets.KeyFile, cfg.Secrets.PreviousKeys)
	if err != nil {
		return nil, fmt.Errorf("secrets: %w", err)
	}
	if kr.Generated {
		log.Warn("generated a new secret encryption key; back it up or set HOLIAOKHO_SECRET_KEY", "file", cfg.Secrets.KeyFile)
	}
	d, err := db.Open(ctx, cfg.Database.URL, cfg.Database.MaxConns)
	if err != nil {
		return nil, err
	}
	if err := d.Migrate(ctx, log); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	c := content.New(d, log)
	if err := c.EnsureDefaultStorage(ctx, cfg.Storage.Type, defaultStorageConfig(cfg.Storage)); err != nil {
		return nil, err
	}
	if err := c.LoadStorages(ctx); err != nil {
		return nil, err
	}
	if err := c.EnsureDefaultRepos(ctx); err != nil {
		return nil, fmt.Errorf("default repositories: %w", err)
	}
	a := auth.New(d, log, auth.Config{AnonymousEnabled: cfg.Auth.AnonymousEnabled, SessionTTL: cfg.Auth.SessionTTL,
		LoginMaxFailures: cfg.Auth.LoginMaxFailures, LoginWindow: cfg.Auth.LoginWindow})
	if err := a.Bootstrap(ctx, cfg.Auth.AdminPassword); err != nil {
		return nil, fmt.Errorf("bootstrap auth: %w", err)
	}
	auth.SelectorLookup = a.Selector
	eng, err := repo.NewEngine(c, log, c.Repo, cfg.Proxy)
	if err != nil {
		return nil, err
	}
	s := &Server{Cfg: cfg, Log: log, DB: d, Content: c, Auth: a, Engine: eng, Formats: format.NewRegistry(), listeners: map[int]*http.Server{}, Sys: sys}
	s.metrics.started = time.Now()
	s.Notify = notify.New(d.Pool, log)
	eng.OnEvent = func(ev repo.Event) {
		s.Notify.Emit(notify.Event{Event: ev.Name, Repository: ev.Repository, Data: ev.Data})
	}
	s.Deps = format.Deps{Content: c, Engine: eng, Auth: a, Log: log, BaseURL: s.baseURL}
	s.Tokens = docker.NewTokenIssuer(a)
	s.Docker = docker.New(s.Tokens)
	s.Formats.Register(maven.Format{})
	s.Formats.Register(npm.Format{})
	s.Formats.Register(raw.Format{})
	s.Formats.Register(pypi.Format{})
	s.Formats.Register(nuget.Format{})
	s.Formats.Register(helm.Format{})
	s.Formats.Register(goproxy.Format{})
	s.Formats.Register(apt.Format{})
	s.Formats.Register(yum.Format{})
	s.Formats.Register(alpine.Format{})
	s.Formats.Register(rubygems.Format{})
	s.Formats.Register(cargo.Format{})
	s.Formats.Register(composer.Format{})
	s.Formats.Register(conda.Format{})
	s.Formats.Register(cran.Format{})
	s.Formats.Register(p2.Format{})
	s.Formats.Register(cocoapods.Format{})
	s.Formats.Register(terraform.Format{})
	s.Formats.Register(pub.Format{})
	s.Formats.Register(gitlfs.Format{})
	s.Formats.Register(huggingface.Format{})
	s.Formats.Register(ansible.Format{})
	s.Formats.Register(conan.Format{})
	s.Formats.Register(swift.Format{})
	s.Formats.Register(s.Docker)

	s.Tasks = task.NewScheduler(d, log)
	s.Tasks.Notify = func(name string, err error, logText string) {
		s.Notify.Emit(notify.Event{Event: "task.failed", Data: map[string]any{"task": name, "error": err.Error()}})
		if mailErr := s.Notify.SendMail(context.Background(), nil, "[Holiaokho] task "+name+" failed", err.Error()+"\n\n"+logText); mailErr != nil {
			log.Debug("task failure mail not sent", "err", mailErr)
		}
	}
	task.RegisterBuiltins(s.Tasks, c)
	// The backup task is always registered; it reads its settings at run time
	// so they can be changed from the UI without a restart.
	s.Tasks.Register("backup", "Write a scheduled backup archive to the configured directory", 24*time.Hour, func(ctx context.Context, logf func(string, ...any)) error {
		b := backup.Load(ctx, d, backup.Settings{Enabled: cfg.Backup.Dir != "", Dir: cfg.Backup.Dir, WithBlobs: cfg.Backup.WithBlobs, Keep: cfg.Backup.Keep})
		if !b.Enabled || b.Dir == "" {
			logf("scheduled backup is not configured; set a directory in Backup / Restore")
			return nil
		}
		_, err := backup.WriteFile(ctx, c, Version, b.Dir, b.WithBlobs, b.Keep, logf)
		return err
	})
	s.Tasks.Register("re-encrypt-secrets", "Re-encrypt stored secrets with the current key (run after rotating secrets.key)", 24*time.Hour, func(ctx context.Context, logf func(string, ...any)) error {
		return s.reencryptSecrets(ctx, logf)
	})
	// Hourly, so a package that arrives is checked within the hour; each
	// run only takes packages never checked or last checked a day ago.
	scanner := &vuln.Scanner{
		Content:   c,
		OSV:       &vuln.OSV{BaseURL: cfg.Vulns.OSVURL, HTTP: eng.HTTPClient(), UA: eng.UserAgent()},
		Log:       log,
		MinNotify: cfg.Vulns.NotifyMinSeverity,
		Announce:  s.announceVulnerabilities,
	}
	s.Tasks.Register("scan-vulnerabilities", "Check stored packages against OSV for known vulnerabilities (new packages first; each rechecked daily)", time.Hour, func(ctx context.Context, logf func(string, ...any)) error {
		if !cfg.Vulns.Enabled {
			logf("vulnerability scanning is disabled in the configuration")
			return nil
		}
		return scanner.Run(ctx, logf)
	})
	s.Updates = &update.Checker{DB: d.Pool, HTTP: eng.HTTPClient(), URL: cfg.Updates.URL, Current: Version, Enabled: cfg.Updates.Check}
	s.Tasks.Register("check-for-updates", "Ask GitHub whether a newer Holiaokho release has been published (can be turned off: updates.check)", 24*time.Hour, func(ctx context.Context, logf func(string, ...any)) error {
		if !cfg.Updates.Check {
			logf("update checks are turned off in the configuration")
			return nil
		}
		if err := s.Updates.Check(ctx); err != nil {
			return err
		}
		st := s.Updates.Status(ctx)
		switch {
		case st.Error != "":
			logf("could not check: %s", st.Error)
		case st.Available:
			logf("%s is available (running %s)", st.Latest, st.Current)
		default:
			logf("up to date (%s)", st.Current)
		}
		return nil
	})
	s.Tasks.Register("rebuild-indexes", "Regenerate hosted repository indexes (Maven metadata, APT/YUM/apk/CRAN/Conda index files)", 7*24*time.Hour, func(ctx context.Context, logf func(string, ...any)) error {
		return s.rebuildIndexes(ctx, logf)
	})
	task.VersionLess = func(f, x, y string) bool {
		if fm, ok := s.Formats.Get(f); ok {
			return fm.VersionLess(x, y)
		}
		return x < y
	}
	s.API = &api.API{Content: c, Engine: eng, Auth: a, Formats: s.Formats, Tasks: s.Tasks, Deps: s.Deps, Version: Version, Started: time.Now(), OnRepoChange: s.syncDockerListeners,
		Notify: s.Notify, Logs: sys.Buffer, LogLevel: sys.Level, Config: cfg, Updates: s.Updates}
	return s, nil
}

// defaultStorageConfig builds the JSON config of the "default" store from
// the server configuration.
func defaultStorageConfig(st config.Storage) string {
	if st.Type == "s3" {
		b, _ := json.Marshal(map[string]any{"endpoint": st.S3.Endpoint, "region": st.S3.Region, "bucket": st.S3.Bucket, "prefix": st.S3.Prefix,
			"accessKey": st.S3.AccessKey, "secretKey": st.S3.SecretKey, "pathStyle": st.S3.PathStyle})
		return string(b)
	}
	b, _ := json.Marshal(map[string]any{"path": st.Path})
	return string(b)
}

// baseURL derives the external base URL for a request.
func (s *Server) baseURL(r *http.Request) string {
	if s.Cfg.Server.BaseURL != "" {
		return strings.TrimSuffix(s.Cfg.Server.BaseURL, "/")
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host := r.Host
	if s.Cfg.Server.TrustForwarded {
		if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
			scheme = p
		}
		if h := r.Header.Get("X-Forwarded-Host"); h != "" {
			host = h
		}
	}
	return scheme + "://" + host
}

// ------------------------------------------------------------ middleware

type statusWriter struct {
	http.ResponseWriter
	status int
	n      int64
}

func (w *statusWriter) WriteHeader(c int) { w.status = c; w.ResponseWriter.WriteHeader(c) }
func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	n, err := w.ResponseWriter.Write(b)
	w.n += int64(n)
	return n, err
}
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		defer func() {
			if rec := recover(); rec != nil {
				s.Log.Error("panic", "err", rec, "path", r.URL.Path)
				if sw.status == 0 {
					format.WriteError(sw, 500, "internal", "internal error")
				}
			}
			if sw.status == 0 {
				sw.status = 200
			}
			if c := sw.status / 100; c >= 1 && c <= 5 {
				s.metrics.requests[c].Add(1)
			}
			// Successful requests are the firehose and stay at debug. Failures
			// are rare and are what you need when something goes wrong, so
			// they are visible at the default level: a CI run that logs
			// nothing at info still shows its 4xx and 5xx here.
			args := []any{"method", r.Method, "path", r.URL.Path, "status", sw.status, "bytes", sw.n, "dur", time.Since(start).Round(time.Millisecond), "ip", auth.ClientIP(r)}
			switch {
			case sw.status >= 500:
				s.Log.Error("http", args...)
			case sw.status >= 400:
				s.Log.Info("http", args...)
			default:
				s.Log.Debug("http", args...)
			}
		}()
		// Browser hardening. Repository content is served in a sandbox so an
		// uploaded HTML file cannot run scripts in Holiaokho's origin.
		sw.Header().Set("X-Content-Type-Options", "nosniff")
		if strings.HasPrefix(r.URL.Path, "/repository/") || strings.HasPrefix(r.URL.Path, "/v2/") {
			sw.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
		} else {
			sw.Header().Set("X-Frame-Options", "DENY")
			sw.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/service/") {
				sw.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
			} else if r.URL.Path == "/ui" || strings.HasPrefix(r.URL.Path, "/ui/") {
				// Ant Design injects <style> tags (CSS-in-JS); web fonts come from Google Fonts.
				sw.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src 'self' data: https://fonts.gstatic.com; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'")
			}
		}
		p, presented, err := s.Auth.FromRequest(r)
		if err != nil {
			if errors.Is(err, auth.ErrRateLimited) {
				format.WriteError(sw, 429, "auth.rate_limited", "too many failed login attempts")
				return
			}
			if presented {
				if !strings.HasPrefix(r.URL.Path, "/api/") {
					sw.Header().Set("WWW-Authenticate", `Basic realm="Holiaokho"`)
				}
				format.WriteError(sw, 401, "auth.invalid", "invalid credentials")
				return
			}
		}
		ctx := auth.WithPrincipal(r.Context(), p)
		next.ServeHTTP(sw, r.WithContext(ctx))
	})
}

// --------------------------------------------------------------- routing

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(s.middleware)
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := s.DB.Pool.Ping(r.Context()); err != nil {
			http.Error(w, "database: "+err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte("ready"))
	})
	r.Get("/metrics", s.metricsHandler)
	r.Mount("/api/v1", s.API.Router())
	r.Mount("/service/rest/v1", s.API.NexusCompatRouter())
	r.Get("/service/metrics/prometheus", s.metricsHandler)
	r.HandleFunc("/repository/{name}/*", s.repositoryHandler)
	r.HandleFunc("/repository/{name}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.Path+"/", http.StatusMovedPermanently)
	})
	// Terraform service discovery is host-level: point it at the first
	// terraform group (or any terraform repository).
	r.Get("/.well-known/terraform.json", func(w http.ResponseWriter, req *http.Request) {
		repos, _ := s.Content.ListRepos(req.Context())
		var pick *model.Repository
		for _, rp := range repos {
			if rp.Format == "terraform" && (pick == nil || rp.Type == model.Group) {
				pick = rp
			}
		}
		if pick == nil {
			format.WriteError(w, 404, "not_found", "no terraform repository configured")
			return
		}
		base := s.baseURL(req) + "/repository/" + pick.Name
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"providers.v1":"%s/v1/providers/","modules.v1":"%s/v1/modules/"}`, base, base)
	})
	r.Handle("/v2/token", s.Tokens)
	r.HandleFunc("/v2", s.dockerPathHandler)
	r.HandleFunc("/v2/*", s.dockerPathHandler)
	r.Handle("/*", s.uiHandler())
	// Subdomain connectors: "<label>.<host>" serves that Docker repository.
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if label, ok := strings.CutPrefix(req.Host, ""); ok {
			if i := strings.IndexByte(label, '.'); i > 0 {
				s.mu.Lock()
				name, hit := s.subdomains[label[:i]]
				s.mu.Unlock()
				if hit && (strings.HasPrefix(req.URL.Path, "/v2/") || req.URL.Path == "/v2") {
					if req.URL.Path == "/v2/token" {
						s.middleware(s.Tokens).ServeHTTP(w, req)
						return
					}
					s.middleware(s.dockerPortHandler(name)).ServeHTTP(w, req)
					return
				}
			}
		}
		r.ServeHTTP(w, req)
	})
}

func (s *Server) repositoryHandler(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	rp, err := s.Content.Repo(r.Context(), name)
	if err != nil {
		format.WriteError(w, 404, "repo.not_found", "repository %q not found", name)
		return
	}
	f, ok := s.Formats.Get(rp.Format)
	if !ok {
		format.WriteError(w, 501, "format.unsupported", "format %q is not supported by this build", rp.Format)
		return
	}
	prefix := "/repository/" + name
	if rp.Format == docker.Name {
		// Docker clients may address the registry as /repository/<name>/v2/...
		prefix += "/v2"
		if !strings.HasPrefix(r.URL.Path, prefix) {
			format.WriteError(w, 404, "not_found", "docker repositories are served under /v2")
			return
		}
	}
	h := f.Handler(rp, s.Deps)
	http.StripPrefix(prefix, h).ServeHTTP(w, r)
}

// dockerPathHandler serves /v2/<repo>/<image>/... on the main port (path
// mode) and the bare /v2/ version check.
func (s *Server) dockerPathHandler(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, "/v2")
	p = strings.TrimPrefix(p, "/")
	w.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")
	if p == "" {
		s.Docker.Ping(w, r, s.baseURL(r))
		return
	}
	first := p
	if i := strings.IndexByte(p, '/'); i >= 0 {
		first = p[:i]
	}
	rp, err := s.Content.Repo(r.Context(), first)
	if err != nil || rp.Format != docker.Name || !docker.AttrsOf(rp).PathEnabled {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		w.Write([]byte(`{"errors":[{"code":"NAME_UNKNOWN","message":"repository name not known to registry"}]}`))
		return
	}
	h := s.Docker.Handler(rp, s.Deps)
	http.StripPrefix("/v2/"+first, h).ServeHTTP(w, r)
}

func (s *Server) uiHandler() http.Handler {
	var root fs.FS
	if dir := s.Cfg.Server.UIDir; dir != "" {
		root = os.DirFS(dir)
	} else {
		sub, err := fs.Sub(web.Dist, "dist")
		if err != nil {
			panic(err)
		}
		root = sub
	}
	fileServer := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The SPA lives under /ui/ (Vite base). "/" redirects there; anything
		// else outside /ui/ is not a UI route.
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/ui/", http.StatusFound)
			return
		}
		if r.URL.Path != "/ui" && !strings.HasPrefix(r.URL.Path, "/ui/") {
			http.NotFound(w, r)
			return
		}
		p := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/ui"), "/")
		if p == "" {
			p = "index.html"
		}
		r2 := r.Clone(r.Context())
		if st, err := fs.Stat(root, p); p == "index.html" || err != nil || st.IsDir() {
			// SPA fallback: client-side routes get index.html (FileServer
			// serves it for "/" and would redirect an explicit /index.html).
			r2.URL.Path = "/"
			w.Header().Set("Cache-Control", "no-cache")
		} else {
			r2.URL.Path = "/" + p
			if strings.HasPrefix(p, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
		}
		fileServer.ServeHTTP(w, r2)
	})
}

func (s *Server) metricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "# HELP holiaokho_http_requests_total HTTP requests by status class\n# TYPE holiaokho_http_requests_total counter\n")
	for c := 1; c <= 5; c++ {
		fmt.Fprintf(w, "holiaokho_http_requests_total{class=\"%dxx\"} %d\n", c, s.metrics.requests[c].Load())
	}
	fmt.Fprintf(w, "# HELP holiaokho_uptime_seconds Seconds since start\n# TYPE holiaokho_uptime_seconds gauge\nholiaokho_uptime_seconds %d\n", int(time.Since(s.metrics.started).Seconds()))
	var blobs, size int64
	s.DB.Pool.QueryRow(r.Context(), `SELECT count(*), coalesce(sum(size),0) FROM blobs`).Scan(&blobs, &size)
	fmt.Fprintf(w, "# TYPE holiaokho_blobs_total gauge\nholiaokho_blobs_total %d\n# TYPE holiaokho_blob_bytes gauge\nholiaokho_blob_bytes %d\n", blobs, size)
	var pool = s.DB.Pool.Stat()
	fmt.Fprintf(w, "# TYPE holiaokho_db_connections gauge\nholiaokho_db_connections{state=\"total\"} %d\nholiaokho_db_connections{state=\"idle\"} %d\n", pool.TotalConns(), pool.IdleConns())
}

// ------------------------------------------------------- docker listeners

// syncDockerListeners starts/stops per-repository Docker port connectors to
// match the current repository configuration.
func (s *Server) syncDockerListeners() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repos, err := s.Content.ListRepos(ctx)
	if err != nil {
		s.Log.Error("list repos for docker listeners", "err", err)
		return
	}
	want := map[int]*model.Repository{}
	tlsPorts := map[int]docker.Attrs{}
	subs := map[string]string{}
	for _, rp := range repos {
		if rp.Format != docker.Name {
			continue
		}
		a := docker.AttrsOf(rp)
		for _, port := range []int{a.HTTPPort, a.HTTPSPort} {
			if port <= 0 {
				continue
			}
			if other, dup := want[port]; dup {
				s.Log.Warn("docker port used by two repositories", "port", port, "a", other.Name, "b", rp.Name)
				continue
			}
			want[port] = rp
			if port == a.HTTPSPort {
				tlsPorts[port] = a
			}
		}
		if a.Subdomain != "" {
			subs[a.Subdomain] = rp.Name
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subdomains = subs
	for port, srv := range s.listeners {
		if _, ok := want[port]; !ok {
			s.Log.Info("stopping docker connector", "port", port)
			srv.Close()
			delete(s.listeners, port)
		}
	}
	for port, rp := range want {
		if _, ok := s.listeners[port]; ok {
			continue
		}
		name := rp.Name
		mux := chi.NewRouter()
		mux.Use(s.middleware)
		mux.Handle("/v2/token", s.Tokens)
		mux.HandleFunc("/v2", s.dockerPortHandler(name))
		mux.HandleFunc("/v2/*", s.dockerPortHandler(name))
		mux.NotFound(func(w http.ResponseWriter, r *http.Request) {
			format.WriteError(w, 404, "not_found", "this port serves the Docker registry API for %q", name)
		})
		srv := &http.Server{Addr: fmt.Sprintf(":%d", port), Handler: mux, ReadHeaderTimeout: 30 * time.Second, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
		ln, err := net.Listen("tcp", srv.Addr)
		if err != nil {
			s.Log.Error("docker connector listen", "port", port, "repo", name, "err", err)
			continue
		}
		if a, tls := tlsPorts[port]; tls {
			s.listeners[port] = srv
			s.Log.Info("docker connector listening (TLS)", "port", port, "repo", name)
			go func() {
				if err := srv.ServeTLS(ln, a.TLSCert, a.TLSKey); err != nil && !errors.Is(err, http.ErrServerClosed) {
					s.Log.Error("docker TLS connector", "port", port, "err", err)
				}
			}()
			continue
		}
		s.listeners[port] = srv
		s.Log.Info("docker connector listening", "port", port, "repo", name)
		go srv.Serve(ln)
	}
}

func (s *Server) dockerPortHandler(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rp, err := s.Content.Repo(r.Context(), name)
		if err != nil {
			format.WriteError(w, 404, "repo.not_found", "repository not found")
			return
		}
		h := s.Docker.Handler(rp, s.Deps)
		http.StripPrefix("/v2", h).ServeHTTP(w, r)
	}
}

// ------------------------------------------------------------------- run

func (s *Server) Run(ctx context.Context) error {
	s.Tasks.Start()
	// The daily task's first run is a day away; an administrator who has
	// just installed or upgraded should not wait that long to hear.
	go func() {
		select {
		case <-time.After(time.Minute):
			if err := s.Updates.Check(ctx); err != nil {
				s.Log.Debug("update check", "err", err)
			}
		case <-ctx.Done():
		}
	}()
	s.syncDockerListeners()
	s.main = &http.Server{Addr: s.Cfg.Server.Listen, Handler: s.Router(), ReadHeaderTimeout: 30 * time.Second,
		ReadTimeout: s.Cfg.Server.ReadTimeout, WriteTimeout: s.Cfg.Server.WriteTimeout, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	ln, err := net.Listen("tcp", s.Cfg.Server.Listen)
	if err != nil {
		return err
	}
	errc := make(chan error, 1)
	cfg := s.Cfg.Server
	switch {
	case cfg.TLSCert != "" && cfg.TLSListen != "":
		// HTTP on Listen plus HTTPS on TLSListen.
		s.Log.Info("holiaokho listening", "addr", cfg.Listen, "https", cfg.TLSListen, "version", Version)
		go func() { errc <- s.main.Serve(ln) }()
		tlsSrv := &http.Server{Addr: cfg.TLSListen, Handler: s.main.Handler, ReadHeaderTimeout: 30 * time.Second, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
		tln, err := net.Listen("tcp", cfg.TLSListen)
		if err != nil {
			return err
		}
		s.tlsSrv = tlsSrv
		go func() { errc <- tlsSrv.ServeTLS(tln, cfg.TLSCert, cfg.TLSKey) }()
	case cfg.TLSCert != "":
		s.Log.Info("holiaokho listening (TLS)", "addr", cfg.Listen, "version", Version)
		go func() { errc <- s.main.ServeTLS(ln, cfg.TLSCert, cfg.TLSKey) }()
	default:
		s.Log.Info("holiaokho listening", "addr", cfg.Listen, "version", Version)
		go func() { errc <- s.main.Serve(ln) }()
	}
	select {
	case <-ctx.Done():
		s.Log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		s.main.Shutdown(shutdownCtx)
		if s.tlsSrv != nil {
			s.tlsSrv.Shutdown(shutdownCtx)
		}
		s.mu.Lock()
		for _, l := range s.listeners {
			l.Shutdown(shutdownCtx)
		}
		s.mu.Unlock()
		s.Tasks.Stop()
		s.DB.Close()
		return nil
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// Rebuilder is implemented by formats whose hosted repositories keep
// derived index files.
type Rebuilder interface {
	Rebuild(ctx context.Context, d format.Deps, rp *model.Repository) error
}

func (s *Server) rebuildIndexes(ctx context.Context, logf func(string, ...any)) error {
	repos, err := s.Content.ListRepos(ctx)
	if err != nil {
		return err
	}
	n := 0
	for _, rp := range repos {
		if rp.Type != model.Hosted {
			continue
		}
		f, ok := s.Formats.Get(rp.Format)
		if !ok {
			continue
		}
		rb, ok := f.(Rebuilder)
		if !ok {
			continue
		}
		if err := rb.Rebuild(ctx, s.Deps, rp); err != nil {
			logf("%s: %v", rp.Name, err)
			continue
		}
		n++
	}
	logf("rebuilt indexes for %d repositories", n)
	return nil
}

// reencryptSecrets rewrites every stored secret (repository attributes,
// storage configs, auth and email settings) with the current key. Values
// already on the current key are left untouched.
func (s *Server) reencryptSecrets(ctx context.Context, logf func(string, ...any)) error {
	n := 0
	rows, err := s.DB.Pool.Query(ctx, `SELECT name, attributes FROM repositories`)
	if err != nil {
		return err
	}
	type rr struct {
		name string
		raw  []byte
	}
	var repos []rr
	for rows.Next() {
		var x rr
		rows.Scan(&x.name, &x.raw)
		repos = append(repos, x)
	}
	rows.Close()
	for _, x := range repos {
		if !anyNeeds(x.raw, secrets.RepositoryPaths) {
			continue
		}
		dec, err := secrets.DecryptPaths(x.raw, secrets.RepositoryPaths)
		if err != nil {
			logf("repository %s: %v", x.name, err)
			continue
		}
		enc, _ := secrets.EncryptPaths(dec, secrets.RepositoryPaths)
		if _, err := s.DB.Pool.Exec(ctx, `UPDATE repositories SET attributes=$2 WHERE name=$1`, x.name, enc); err == nil {
			n++
		}
	}
	srows, err := s.DB.Pool.Query(ctx, `SELECT name, config FROM storages`)
	if err != nil {
		return err
	}
	var stores []rr
	for srows.Next() {
		var x rr
		srows.Scan(&x.name, &x.raw)
		stores = append(stores, x)
	}
	srows.Close()
	for _, x := range stores {
		if !anyNeeds(x.raw, secrets.StorageConfigPaths) {
			continue
		}
		dec, err := secrets.DecryptPaths(x.raw, secrets.StorageConfigPaths)
		if err != nil {
			logf("storage %s: %v", x.name, err)
			continue
		}
		enc, _ := secrets.EncryptPaths(dec, secrets.StorageConfigPaths)
		if _, err := s.DB.Pool.Exec(ctx, `UPDATE storages SET config=$2 WHERE name=$1`, x.name, enc); err == nil {
			n++
		}
	}
	// Settings: reading decrypts, saving encrypts with the current key.
	st := s.Auth.Settings(ctx)
	if err := s.Auth.SaveSettings(ctx, st); err == nil {
		n++
	}
	if em, err := s.Notify.EmailConfig(ctx); err == nil {
		if err := s.Notify.SaveEmailConfig(ctx, em); err == nil {
			n++
		}
	}
	s.Content.Invalidate()
	logf("re-encrypted %d records", n)
	return nil
}

func anyNeeds(raw []byte, paths []string) bool {
	needs := false
	secrets.Transform(raw, paths, func(v string) (string, error) {
		if secrets.NeedsReencrypt(v) {
			needs = true
		}
		return v, nil
	})
	return needs
}
