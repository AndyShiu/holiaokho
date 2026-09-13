// Package server wires configuration, database, storage, auth, formats and
// the HTTP routers into a running Holiaokho instance.
package server

import (
	"context"
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
	"github.com/holiaokho/holiaokho/internal/config"
	"github.com/holiaokho/holiaokho/internal/content"
	"github.com/holiaokho/holiaokho/internal/db"
	"github.com/holiaokho/holiaokho/internal/format"
	"github.com/holiaokho/holiaokho/internal/format/alpine"
	"github.com/holiaokho/holiaokho/internal/format/apt"
	"github.com/holiaokho/holiaokho/internal/format/cargo"
	"github.com/holiaokho/holiaokho/internal/format/cocoapods"
	"github.com/holiaokho/holiaokho/internal/format/composer"
	"github.com/holiaokho/holiaokho/internal/format/conda"
	"github.com/holiaokho/holiaokho/internal/format/cran"
	"github.com/holiaokho/holiaokho/internal/format/docker"
	"github.com/holiaokho/holiaokho/internal/format/goproxy"
	"github.com/holiaokho/holiaokho/internal/format/helm"
	"github.com/holiaokho/holiaokho/internal/format/maven"
	"github.com/holiaokho/holiaokho/internal/format/npm"
	"github.com/holiaokho/holiaokho/internal/format/nuget"
	"github.com/holiaokho/holiaokho/internal/format/p2"
	"github.com/holiaokho/holiaokho/internal/format/pypi"
	"github.com/holiaokho/holiaokho/internal/format/raw"
	"github.com/holiaokho/holiaokho/internal/format/rubygems"
	"github.com/holiaokho/holiaokho/internal/format/terraform"
	"github.com/holiaokho/holiaokho/internal/format/yum"
	"github.com/holiaokho/holiaokho/internal/model"
	"github.com/holiaokho/holiaokho/internal/notify"
	"github.com/holiaokho/holiaokho/internal/repo"
	"github.com/holiaokho/holiaokho/internal/task"
	"github.com/holiaokho/holiaokho/web"
)

const Version = "0.1.0-dev"

type Server struct {
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
	upstream atomic.Int64
	started  time.Time
}

func New(ctx context.Context, cfg config.Config, log *slog.Logger, sys *System) (*Server, error) {
	if sys == nil {
		_, sys = NewSystemLogger(cfg.Log.Level, cfg.Log.Format)
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
	s.Formats.Register(s.Docker)

	s.Tasks = task.NewScheduler(d, log)
	s.Tasks.Notify = func(name string, err error, logText string) {
		s.Notify.Emit(notify.Event{Event: "task.failed", Data: map[string]any{"task": name, "error": err.Error()}})
		if mailErr := s.Notify.SendMail(context.Background(), nil, "[Holiaokho] task "+name+" failed", err.Error()+"\n\n"+logText); mailErr != nil {
			log.Debug("task failure mail not sent", "err", mailErr)
		}
	}
	task.RegisterBuiltins(s.Tasks, c)
	task.VersionLess = func(f, x, y string) bool {
		if fm, ok := s.Formats.Get(f); ok {
			return fm.VersionLess(x, y)
		}
		return x < y
	}
	s.API = &api.API{Content: c, Engine: eng, Auth: a, Formats: s.Formats, Tasks: s.Tasks, Deps: s.Deps, Version: Version, Started: time.Now(), OnRepoChange: s.syncDockerListeners,
		Notify: s.Notify, Logs: sys.Buffer, LogLevel: sys.Level, Config: cfg}
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
			s.Log.Debug("http", "method", r.Method, "path", r.URL.Path, "status", sw.status, "bytes", sw.n, "dur", time.Since(start).Round(time.Millisecond), "ip", auth.ClientIP(r))
		}()
		p, presented, err := s.Auth.FromRequest(r)
		if err != nil {
			if errors.Is(err, auth.ErrRateLimited) {
				format.WriteError(sw, 429, "auth.rate_limited", "too many failed login attempts")
				return
			}
			if presented {
				sw.Header().Set("WWW-Authenticate", `Basic realm="Holiaokho"`)
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
			http.Error(w, "database: "+err.Error(), 503)
			return
		}
		w.Write([]byte("ready"))
	})
	r.Get("/metrics", s.metricsHandler)
	r.Mount("/api/v1", s.API.Router())
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
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(root, p); err != nil {
			// SPA fallback.
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}
		fileServer.ServeHTTP(w, r)
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
		srv := &http.Server{Addr: fmt.Sprintf(":%d", port), Handler: mux, ReadHeaderTimeout: 30 * time.Second}
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
	s.syncDockerListeners()
	s.main = &http.Server{Addr: s.Cfg.Server.Listen, Handler: s.Router(), ReadHeaderTimeout: 30 * time.Second,
		ReadTimeout: s.Cfg.Server.ReadTimeout, WriteTimeout: s.Cfg.Server.WriteTimeout}
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
		tlsSrv := &http.Server{Addr: cfg.TLSListen, Handler: s.main.Handler, ReadHeaderTimeout: 30 * time.Second}
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
