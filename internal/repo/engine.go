// Package repo is the format-agnostic repository engine: it resolves a path
// inside a hosted, proxy or group repository to content, handling upstream
// fetching, caching, negative caching, stale-if-error and deployment
// policies. Format plugins call into it and layer format semantics on top.
package repo

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/holiaokho/holiaokho/internal/config"
	"github.com/holiaokho/holiaokho/internal/content"
	"github.com/holiaokho/holiaokho/internal/model"
	"github.com/holiaokho/holiaokho/internal/storage"
)

var (
	ErrNotFound       = errors.New("not found")
	ErrOffline        = errors.New("repository offline")
	ErrWriteDenied    = errors.New("deployment not allowed by write policy")
	ErrRedeploy       = errors.New("redeployment not allowed")
	ErrReadOnly       = errors.New("repository is read-only")
	ErrUpstream       = errors.New("upstream error")
	ErrInvalidPath    = errors.New("invalid path")
	ErrUpstreamDenied = errors.New("upstream denied")
	ErrRouted         = errors.New("path blocked by routing rule")
)

// Kind tells the engine which TTL applies to a proxied path.
type Kind int

const (
	// Content is immutable artifact data (jars, tarballs, layers).
	Content Kind = iota
	// Metadata is a mutable index document (maven-metadata.xml, npm package doc).
	Metadata
	// NoCache is passed through to upstream every time (audits, searches).
	NoCache
)

// Policy is supplied by the format plugin per request.
type Policy struct {
	Kind Kind
	// UpstreamPath overrides the path appended to the remote URL.
	UpstreamPath string
	// Client overrides the HTTP client used for upstream (docker token auth).
	Client *http.Client
	// Headers are added to the upstream request (e.g. Accept for manifests).
	Headers http.Header
	// ContentType overrides the type recorded for the stored asset.
	ContentType string
	// Immutable forces "cache forever" regardless of repo settings.
	Immutable bool
	// Package, if set, is upserted and linked to the stored asset.
	Package *model.Package
	// Attrs are merged into the asset attributes.
	Attrs map[string]any
	// ExpectedDigest, when known (Docker blobs/manifests by digest), lets the
	// engine reuse an already-stored blob without contacting upstream, and
	// verifies downloaded content.
	ExpectedDigest storage.Digest
}

// Result is a resolved asset with an open body.
type Result struct {
	Repo        *model.Repository // the repository that actually served it
	Asset       *model.Asset
	Body        io.ReadCloser
	Size        int64
	ContentType string
	// Upstream is true when the bytes came from a live upstream fetch.
	Upstream bool
	// Headers carries selected upstream headers for pass-through (NoCache).
	Headers http.Header
	// Status for NoCache pass-through responses.
	Status int
}

func (r *Result) Close() {
	if r != nil && r.Body != nil {
		r.Body.Close()
	}
}

// RepoResolver looks up repositories by name (for group members).
type RepoResolver func(ctx context.Context, name string) (*model.Repository, error)

type Engine struct {
	Content *content.Service
	Log     *slog.Logger
	Repos   RepoResolver
	client  *http.Client
	sf      singleflight.Group
	ua      string
	// autoBlock tracks upstream failures per repo (name -> until).
	blockedMu sync.Mutex
	blocked   map[string]time.Time
	routing   routing
	// OnEvent, when set, receives content events (asset.created, asset.deleted).
	OnEvent func(Event)
}

// Event describes a content change for webhooks.
type Event struct {
	Name       string
	Repository string
	Data       map[string]any
}

func (e *Engine) emit(name string, repo *model.Repository, data map[string]any) {
	if e.OnEvent != nil {
		e.OnEvent(Event{Name: name, Repository: repo.Name, Data: data})
	}
}

func NewEngine(c *content.Service, log *slog.Logger, repos RepoResolver, pc config.Proxy) (*Engine, error) {
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: pc.ConnectTimeout, KeepAlive: 30 * time.Second}).DialContext,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   20,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
	}
	if pc.HTTPProxy != "" {
		pu, err := url.Parse(pc.HTTPProxy)
		if err != nil {
			return nil, fmt.Errorf("proxy url: %w", err)
		}
		tr.Proxy = http.ProxyURL(pu)
		if pc.NoProxy != "" {
			os.Setenv("NO_PROXY", pc.NoProxy)
		}
	}
	if pc.CACertFile != "" {
		pem, err := os.ReadFile(pc.CACertFile)
		if err != nil {
			return nil, fmt.Errorf("ca cert: %w", err)
		}
		pool, _ := x509.SystemCertPool()
		if pool == nil {
			pool = x509.NewCertPool()
		}
		pool.AppendCertsFromPEM(pem)
		tr.TLSClientConfig = &tls.Config{RootCAs: pool}
	}
	return &Engine{
		Content: c, Log: log, Repos: repos,
		client:  &http.Client{Transport: tr, Timeout: pc.Timeout},
		ua:      pc.UserAgent,
		blocked: map[string]time.Time{},
	}, nil
}

// HTTPClient exposes the shared upstream client (formats wrap it).
func (e *Engine) HTTPClient() *http.Client { return e.client }
func (e *Engine) UserAgent() string        { return e.ua }

// CleanPath validates and normalises a repository-relative path.
func CleanPath(p string) (string, error) {
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return "", ErrInvalidPath
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", ErrInvalidPath
		}
	}
	if strings.ContainsAny(p, "\x00\\") {
		return "", ErrInvalidPath
	}
	return p, nil
}

// ------------------------------------------------------------------- Fetch

// Fetch resolves path in repo according to its type.
func (e *Engine) Fetch(ctx context.Context, repo *model.Repository, path string, pol Policy) (*Result, error) {
	if !repo.Online {
		return nil, ErrOffline
	}
	if rule := e.RuleFor(ctx, repo); rule != nil && !rule.Allows(path) {
		return nil, ErrRouted
	}
	switch repo.Type {
	case model.Hosted:
		return e.fetchLocal(ctx, repo, path)
	case model.Proxy:
		return e.fetchProxy(ctx, repo, path, pol)
	case model.Group:
		return e.fetchGroup(ctx, repo, path, pol)
	}
	return nil, fmt.Errorf("unknown repository type %q", repo.Type)
}

// Members resolves the group's member repositories in order.
func (e *Engine) Members(ctx context.Context, group *model.Repository) ([]*model.Repository, error) {
	var out []*model.Repository
	for _, name := range group.Members {
		m, err := e.Repos(ctx, name)
		if err != nil {
			e.Log.Warn("group member missing", "group", group.Name, "member", name)
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

func (e *Engine) fetchGroup(ctx context.Context, group *model.Repository, path string, pol Policy) (*Result, error) {
	members, err := e.Members(ctx, group)
	if err != nil {
		return nil, err
	}
	var lastErr error = ErrNotFound
	for _, m := range members {
		res, err := e.Fetch(ctx, m, path, pol)
		if err == nil {
			return res, nil
		}
		// A member that cannot serve the path (missing, offline, routed away,
		// or the upstream refused it) must not mask other members.
		if !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrOffline) && !errors.Is(err, ErrUpstreamDenied) && !errors.Is(err, ErrRouted) {
			lastErr = err
		}
	}
	return nil, lastErr
}

func (e *Engine) fetchLocal(ctx context.Context, repo *model.Repository, path string) (*Result, error) {
	a, err := e.Content.Asset(ctx, repo.ID, path)
	if errors.Is(err, content.ErrNotFound) || (err == nil && (a.Negative || a.BlobDigest == nil)) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return e.open(ctx, repo, a, false)
}

func (e *Engine) open(ctx context.Context, repo *model.Repository, a *model.Asset, upstream bool) (*Result, error) {
	rc, size, err := e.Content.OpenBlob(ctx, storage.Digest(*a.BlobDigest))
	if errors.Is(err, content.ErrNotFound) {
		e.Log.Error("asset points to missing blob", "repo", repo.Name, "path", a.Path, "digest", *a.BlobDigest)
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	go e.Content.TouchDownloaded(context.WithoutCancel(ctx), a.ID)
	return &Result{Repo: repo, Asset: a, Body: rc, Size: size, ContentType: a.ContentType, Upstream: upstream}, nil
}

func (e *Engine) ttl(repo *model.Repository, pol Policy) *time.Time {
	if pol.Immutable {
		return nil
	}
	var mins int
	switch pol.Kind {
	case Metadata:
		mins = repo.Proxy.MetadataMaxAge
	default:
		mins = repo.Proxy.ContentMaxAge
	}
	if mins < 0 {
		return nil
	}
	t := time.Now().Add(time.Duration(mins) * time.Minute)
	return &t
}

func fresh(a *model.Asset) bool {
	return a.CacheExpiresAt == nil || a.CacheExpiresAt.After(time.Now())
}

func (e *Engine) fetchProxy(ctx context.Context, repo *model.Repository, path string, pol Policy) (*Result, error) {
	if pol.Kind == NoCache {
		return e.passthrough(ctx, repo, path, pol)
	}
	a, err := e.Content.Asset(ctx, repo.ID, path)
	if err != nil && !errors.Is(err, content.ErrNotFound) {
		return nil, err
	}
	cached := err == nil
	if cached && fresh(a) {
		if a.Negative {
			return nil, ErrNotFound
		}
		if a.BlobDigest != nil {
			return e.open(ctx, repo, a, false)
		}
	}
	// Content-addressed shortcut: the bytes may already be here via another
	// repository (Docker layers are shared across images and registries).
	if pol.ExpectedDigest != "" && (!cached || a.BlobDigest == nil) {
		if size, ok, _ := e.Content.BlobExists(ctx, pol.ExpectedDigest); ok {
			d := string(pol.ExpectedDigest)
			na := &model.Asset{RepoID: repo.ID, Path: path, Size: size, ContentType: pol.ContentType}
			na.BlobDigest = &d
			na.Attrs, _ = json.Marshal(map[string]any{"dedup": true})
			if err := e.Content.UpsertAsset(ctx, na); err == nil {
				return e.open(ctx, repo, na, false)
			}
		}
	}
	if repo.Proxy.Blocked || e.isAutoBlocked(repo) {
		if cached && !a.Negative && a.BlobDigest != nil {
			return e.open(ctx, repo, a, false) // stale but better than nothing
		}
		return nil, ErrNotFound
	}
	// Coalesce concurrent misses for the same asset.
	key := repo.Name + "\x00" + path
	v, err, _ := e.sf.Do(key, func() (any, error) {
		var stale *model.Asset
		if cached {
			stale = a
		}
		// Detach from the first caller's context: other requests may be
		// waiting on this fetch and must not fail if the first one leaves.
		fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), e.client.Timeout)
		defer cancel()
		return e.refresh(fctx, repo, path, pol, stale)
	})
	if err != nil {
		if cached && !a.Negative && a.BlobDigest != nil && errors.Is(err, ErrUpstream) {
			e.Log.Warn("upstream failed, serving stale", "repo", repo.Name, "path", path, "err", err)
			return e.open(ctx, repo, a, false)
		}
		return nil, err
	}
	na := v.(*model.Asset)
	if na.Negative || na.BlobDigest == nil {
		return nil, ErrNotFound
	}
	return e.open(ctx, repo, na, true)
}

func (e *Engine) upstreamURL(repo *model.Repository, path string, pol Policy) string {
	base := strings.TrimSuffix(repo.Proxy.RemoteURL, "/")
	p := path
	if pol.UpstreamPath != "" {
		p = pol.UpstreamPath
	}
	if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
		return p
	}
	return base + "/" + strings.TrimPrefix(p, "/")
}

func (e *Engine) newUpstreamRequest(ctx context.Context, repo *model.Repository, method, u string, pol Policy) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", e.ua)
	for k, vs := range pol.Headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	if repo.Proxy.Username != "" {
		req.SetBasicAuth(repo.Proxy.Username, repo.Proxy.Password)
	}
	return req, nil
}

func (e *Engine) doUpstream(req *http.Request, pol Policy) (*http.Response, error) {
	c := e.client
	if pol.Client != nil {
		c = pol.Client
	}
	return c.Do(req)
}

// refresh fetches path from upstream and stores it. stale, if non-nil, is
// the expired cached asset used for conditional requests.
func (e *Engine) refresh(ctx context.Context, repo *model.Repository, path string, pol Policy, stale *model.Asset) (*model.Asset, error) {
	u := e.upstreamURL(repo, path, pol)
	req, err := e.newUpstreamRequest(ctx, repo, http.MethodGet, u, pol)
	if err != nil {
		return nil, err
	}
	var attrs map[string]any
	if stale != nil && !stale.Negative && stale.BlobDigest != nil {
		json.Unmarshal(stale.Attrs, &attrs)
		if et, _ := attrs["etag"].(string); et != "" {
			req.Header.Set("If-None-Match", et)
		}
		if lm, _ := attrs["lastModified"].(string); lm != "" {
			req.Header.Set("If-Modified-Since", lm)
		}
	}
	resp, err := e.doUpstream(req, pol)
	if err != nil {
		e.noteFailure(repo)
		e.Log.Warn("upstream request failed", "repo", repo.Name, "url", u, "err", err)
		return nil, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	defer resp.Body.Close()
	e.noteSuccess(repo)
	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusNotFound {
		e.Log.Warn("upstream returned error", "repo", repo.Name, "url", u, "status", resp.StatusCode)
	}

	switch {
	case resp.StatusCode == http.StatusNotModified && stale != nil:
		stale.CacheExpiresAt = e.ttl(repo, pol)
		if err := e.Content.UpsertAsset(ctx, stale); err != nil {
			return nil, err
		}
		return stale, nil
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		if stale != nil && !stale.Negative && stale.BlobDigest != nil {
			// Upstream dropped something we already have (e.g. a removed
			// release). Keep serving our copy rather than losing it.
			e.Log.Warn("upstream returned 404 for cached asset; keeping local copy", "repo", repo.Name, "path", path)
			stale.CacheExpiresAt = e.ttl(repo, pol)
			if err := e.Content.UpsertAsset(ctx, stale); err != nil {
				return nil, err
			}
			return stale, nil
		}
		neg := &model.Asset{RepoID: repo.ID, Path: path, Negative: true}
		if repo.Proxy.NegativeCacheTTL > 0 {
			t := time.Now().Add(time.Duration(repo.Proxy.NegativeCacheTTL) * time.Minute)
			neg.CacheExpiresAt = &t
			if err := e.Content.UpsertAsset(ctx, neg); err != nil {
				return nil, err
			}
		}
		return neg, nil
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("%w: upstream %s returned %d", ErrUpstreamDenied, u, resp.StatusCode)
	case resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests:
		e.noteFailure(repo)
		return nil, fmt.Errorf("%w: upstream %s returned %d", ErrUpstream, u, resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%w: upstream %s returned %d", ErrUpstream, u, resp.StatusCode)
	}

	info, err := e.Content.PutBlob(ctx, repo.StorageID, resp.Body, pol.ExpectedDigest)
	if err != nil {
		return nil, fmt.Errorf("store upstream body: %w", err)
	}
	ct := pol.ContentType
	if ct == "" {
		ct = resp.Header.Get("Content-Type")
	}
	na := &model.Asset{RepoID: repo.ID, Path: path, Size: info.Size, ContentType: ct, CacheExpiresAt: e.ttl(repo, pol)}
	d := string(info.Digest)
	na.BlobDigest = &d
	at := map[string]any{"upstreamUrl": u}
	if et := resp.Header.Get("ETag"); et != "" {
		at["etag"] = et
	}
	if lm := resp.Header.Get("Last-Modified"); lm != "" {
		at["lastModified"] = lm
	}
	for k, v := range pol.Attrs {
		at[k] = v
	}
	na.Attrs, _ = json.Marshal(at)
	if pol.Package != nil {
		pkg := *pol.Package
		pkg.RepoID = repo.ID
		if err := e.Content.UpsertPackage(ctx, &pkg); err != nil {
			return nil, err
		}
		na.PackageID = &pkg.ID
	}
	if err := e.Content.UpsertAsset(ctx, na); err != nil {
		return nil, err
	}
	return na, nil
}

// passthrough proxies a request upstream without caching.
func (e *Engine) passthrough(ctx context.Context, repo *model.Repository, path string, pol Policy) (*Result, error) {
	u := e.upstreamURL(repo, path, pol)
	req, err := e.newUpstreamRequest(ctx, repo, http.MethodGet, u, pol)
	if err != nil {
		return nil, err
	}
	resp, err := e.doUpstream(req, pol)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, ErrNotFound
	}
	return &Result{Repo: repo, Body: resp.Body, Size: resp.ContentLength, ContentType: resp.Header.Get("Content-Type"),
		Upstream: true, Headers: resp.Header, Status: resp.StatusCode}, nil
}

// Upstream performs an arbitrary upstream request for a proxy repo (used by
// formats for POST pass-through such as npm audit).
func (e *Engine) Upstream(ctx context.Context, repo *model.Repository, method, path string, body io.Reader, hdr http.Header, pol Policy) (*http.Response, error) {
	u := e.upstreamURL(repo, path, pol)
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", e.ua)
	for k, vs := range hdr {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	if repo.Proxy.Username != "" {
		req.SetBasicAuth(repo.Proxy.Username, repo.Proxy.Password)
	}
	return e.doUpstream(req, pol)
}

// ---------------------------------------------------------------- autoBlock

func (e *Engine) isAutoBlocked(repo *model.Repository) bool {
	if !repo.Proxy.AutoBlock {
		return false
	}
	e.blockedMu.Lock()
	defer e.blockedMu.Unlock()
	until, ok := e.blocked[repo.Name]
	return ok && until.After(time.Now())
}

func (e *Engine) noteFailure(repo *model.Repository) {
	if repo.Proxy.AutoBlock {
		e.blockedMu.Lock()
		e.blocked[repo.Name] = time.Now().Add(30 * time.Second)
		e.blockedMu.Unlock()
		e.Log.Warn("upstream auto-blocked", "repo", repo.Name, "for", "30s")
	}
}

func (e *Engine) noteSuccess(repo *model.Repository) {
	e.blockedMu.Lock()
	delete(e.blocked, repo.Name)
	e.blockedMu.Unlock()
}

// --------------------------------------------------------------------- Put

// PutOptions controls a hosted deployment.
type PutOptions struct {
	ContentType string
	Package     *model.Package
	Attrs       map[string]any
	// Digest, if known, is verified against the uploaded bytes.
	Digest storage.Digest
	// AllowRedeploy bypasses ALLOW_ONCE (e.g. snapshots, metadata, checksums).
	AllowRedeploy bool
}

// Put stores uploaded content into a hosted repository.
func (e *Engine) Put(ctx context.Context, repo *model.Repository, path string, body io.Reader, opt PutOptions) (*model.Asset, error) {
	if !repo.Online {
		return nil, ErrOffline
	}
	if repo.Type != model.Hosted {
		return nil, ErrReadOnly
	}
	existing, err := e.Content.Asset(ctx, repo.ID, path)
	if err != nil && !errors.Is(err, content.ErrNotFound) {
		return nil, err
	}
	switch repo.Hosted.WritePolicy {
	case model.WriteDeny:
		return nil, ErrWriteDenied
	case model.WriteAllowOnce:
		if existing != nil && existing.BlobDigest != nil && !opt.AllowRedeploy {
			return nil, ErrRedeploy
		}
	}
	info, err := e.Content.PutBlob(ctx, repo.StorageID, body, opt.Digest)
	if err != nil {
		return nil, err
	}
	a := &model.Asset{RepoID: repo.ID, Path: path, Size: info.Size, ContentType: opt.ContentType}
	d := string(info.Digest)
	a.BlobDigest = &d
	if opt.Package != nil {
		pkg := *opt.Package
		pkg.RepoID = repo.ID
		if err := e.Content.UpsertPackage(ctx, &pkg); err != nil {
			return nil, err
		}
		a.PackageID = &pkg.ID
	}
	if opt.Attrs != nil {
		a.Attrs, _ = json.Marshal(opt.Attrs)
	}
	if err := e.Content.UpsertAsset(ctx, a); err != nil {
		return nil, err
	}
	ev := map[string]any{"path": path, "size": info.Size, "digest": d}
	if opt.Package != nil {
		ev["package"] = map[string]any{"namespace": opt.Package.Namespace, "name": opt.Package.Name, "version": opt.Package.Version}
	}
	e.emit("asset.created", repo, ev)
	return a, nil
}

// Delete removes an asset from a hosted repository (or the cache of a proxy).
func (e *Engine) Delete(ctx context.Context, repo *model.Repository, path string) error {
	if repo.Type == model.Group {
		return ErrReadOnly
	}
	err := e.Content.DeleteAsset(ctx, repo.ID, path)
	if errors.Is(err, content.ErrNotFound) {
		return ErrNotFound
	}
	if err == nil {
		e.emit("asset.deleted", repo, map[string]any{"path": path})
	}
	return err
}

// ReadAll fetches and fully reads a (small) document, for metadata merging.
func (e *Engine) ReadAll(ctx context.Context, repo *model.Repository, path string, pol Policy, limit int64) ([]byte, *Result, error) {
	res, err := e.Fetch(ctx, repo, path, pol)
	if err != nil {
		return nil, nil, err
	}
	defer res.Close()
	if limit <= 0 {
		limit = 32 << 20
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, limit))
	return b, res, err
}
