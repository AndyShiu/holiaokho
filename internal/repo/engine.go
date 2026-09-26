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
	"github.com/holiaokho/holiaokho/internal/logx"
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
	// ErrBlocked is an ErrUpstream: the upstream is blocked, by an
	// administrator or after failures, so it was not asked. Distinct so
	// callers merging group members need not log it on every request — the
	// block itself is logged once, when it starts.
	ErrBlocked = fmt.Errorf("%w: upstream blocked", ErrUpstream)
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
	// KeepHeaders lists upstream response headers to store in the asset
	// attributes ("headers" map) so the format can replay them.
	KeepHeaders []string
	// Stream lets a cache miss be served while it downloads, instead of
	// after. Only for content the caller hands straight to the client: the
	// Result carries no Asset until the download has finished, so a caller
	// that parses the body or reads asset attributes must leave it off.
	Stream bool
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
	// spools are the streamed downloads in progress, by repo and path.
	spoolMu sync.Mutex
	spools  map[string]*spool
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
		Proxy: http.ProxyFromEnvironment,
		// Providing DialContext (or TLSClientConfig, below) stops net/http from
		// enabling HTTP/2 on its own, so ask for it explicitly: every upstream
		// that matters here (npmjs, Maven Central, Docker Hub, PyPI) serves h2,
		// and multiplexing is what makes a CI burst cheap.
		ForceAttemptHTTP2: true,
		DialContext:       (&net.Dialer{Timeout: pc.ConnectTimeout, KeepAlive: 30 * time.Second}).DialContext,
		// A CI run fetches hundreds of artifacts from a handful of hosts. With
		// a small per-host idle pool most of them pay for a fresh TLS handshake.
		MaxIdleConns:          256,
		MaxIdleConnsPerHost:   64,
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
		spools:  map[string]*spool{},
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

// DeployTarget is where a deployment sent to group lands: its first hosted
// member, looking through nested groups in member order, as Nexus Pro's
// group deployment does. nil when the group has no hosted member.
func (e *Engine) DeployTarget(ctx context.Context, group *model.Repository) *model.Repository {
	seen := map[string]bool{}
	var walk func(g *model.Repository) *model.Repository
	walk = func(g *model.Repository) *model.Repository {
		if seen[g.Name] {
			return nil
		}
		seen[g.Name] = true
		ms, _ := e.Members(ctx, g)
		for _, m := range ms {
			switch m.Type {
			case model.Hosted:
				return m
			case model.Group:
				if t := walk(m); t != nil {
					return t
				}
			}
		}
		return nil
	}
	return walk(group)
}

func (e *Engine) fetchGroup(ctx context.Context, group *model.Repository, path string, pol Policy) (*Result, error) {
	members, err := e.Members(ctx, group)
	if err != nil {
		return nil, err
	}
	lastErr := ErrNotFound
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
		// Not ErrNotFound: nobody asked the upstream, so nobody knows. A
		// client told "manifest unknown" goes looking for a typo; one told
		// the upstream is unavailable waits and retries, which is right.
		return nil, fmt.Errorf("%w: %s", ErrBlocked, repo.Name)
	}
	var stale *model.Asset
	if cached {
		stale = a
	}
	if pol.Stream {
		if res, err := e.fetchStreamed(ctx, repo, path, pol, stale); !errors.Is(err, errNoSpool) {
			return res, err
		}
	}
	// Coalesce concurrent misses for the same asset.
	key := repo.Name + "\x00" + path
	v, err, _ := e.sf.Do(key, func() (any, error) {
		// Detach from the first caller's context: other requests may be
		// waiting on this fetch and must not fail if the first one leaves.
		fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), e.client.Timeout)
		defer cancel()
		return e.refresh(fctx, repo, path, pol, stale, nil)
	})
	if err != nil {
		return e.refreshFailed(ctx, repo, path, stale, err)
	}
	return e.refreshed(ctx, repo, v.(*model.Asset))
}

// refreshFailed serves the stale copy when there is one and the failure was
// the upstream's, rather than an answer from it.
func (e *Engine) refreshFailed(ctx context.Context, repo *model.Repository, path string, stale *model.Asset, err error) (*Result, error) {
	if stale != nil && !stale.Negative && stale.BlobDigest != nil && errors.Is(err, ErrUpstream) {
		e.Log.Warn("upstream failed, serving stale", "repo", repo.Name, "path", path, "err", err)
		return e.open(ctx, repo, stale, false)
	}
	return nil, err
}

func (e *Engine) refreshed(ctx context.Context, repo *model.Repository, na *model.Asset) (*Result, error) {
	if na.Negative || na.BlobDigest == nil {
		return nil, ErrNotFound
	}
	return e.open(ctx, repo, na, true)
}

var errNoSpool = errors.New("no spool")

// fetchStreamed is fetchProxy's cache miss for Policy.Stream: one download
// per asset however many clients ask, each of them reading it as it lands.
func (e *Engine) fetchStreamed(ctx context.Context, repo *model.Repository, path string, pol Policy, stale *model.Asset) (*Result, error) {
	key := repo.Name + "\x00" + path
	e.spoolMu.Lock()
	sp := e.spools[key]
	if sp == nil {
		var err error
		if sp, err = newSpool(); err != nil {
			e.spoolMu.Unlock()
			// Nowhere to spool to is not a reason to fail the request; it
			// is a reason to fall back to fetching the whole thing first.
			e.Log.Warn("cannot spool upstream download", "err", err)
			return nil, errNoSpool
		}
		e.spools[key] = sp
		go func() {
			// Detached, like the singleflight path: the download belongs to
			// everyone reading it, and finishing it is what fills the cache.
			na, err := e.refresh(context.WithoutCancel(ctx), repo, path, pol, stale, sp)
			sp.finish(na, err)
			e.spoolMu.Lock()
			delete(e.spools, key)
			e.spoolMu.Unlock()
			sp.release()
		}()
	}
	sp.acquire()
	e.spoolMu.Unlock()

	if err := sp.wait(ctx); err != nil {
		sp.release()
		return nil, err
	}
	sp.mu.Lock()
	done, na, err, size, ct := sp.done, sp.asset, sp.err, sp.size, sp.ct
	sp.mu.Unlock()

	// Finished by the time we looked — either it never streamed (a 404, a
	// 304, a refusal) or it already completed. Serve what it produced, from
	// storage, with everything a stored asset brings (ranges, ETag).
	if done {
		sp.release()
		if err != nil {
			return e.refreshFailed(ctx, repo, path, stale, err)
		}
		return e.refreshed(ctx, repo, na)
	}
	return &Result{Repo: repo, Body: sp.reader(), Size: size, ContentType: ct, Upstream: true}, nil
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

func (e *Engine) doUpstream(req *http.Request, pol Policy, streamed bool) (*http.Response, error) {
	c := e.client
	if pol.Client != nil {
		c = pol.Client
	}
	if streamed {
		// http.Client.Timeout covers reading the body too. A copy shares
		// the transport, so connection reuse and cached registry tokens are
		// unaffected; connect and response-header timeouts still apply.
		cc := *c
		cc.Timeout = 0
		c = &cc
	}
	return c.Do(req)
}

// refresh fetches path from upstream and stores it. stale, if non-nil, is
// the expired cached asset used for conditional requests.
//
// sp, when non-nil, receives the body as it is stored. The transfer then has
// no total time limit: it is resumed where it stopped when it stalls, and
// given up only when it stops making progress altogether.
func (e *Engine) refresh(ctx context.Context, repo *model.Repository, path string, pol Policy, stale *model.Asset, sp *spool) (*model.Asset, error) {
	u := e.upstreamURL(repo, path, pol)
	reqCtx := ctx
	var rb *resumingBody
	if sp != nil {
		rb = newResumingBody(ctx)
		defer rb.timer.Stop()
		reqCtx = rb.attempt()
	}
	req, err := e.newUpstreamRequest(reqCtx, repo, http.MethodGet, u, pol)
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
	resp, err := e.doUpstream(req, pol, sp != nil)
	if err != nil {
		e.noteFailure(repo)
		if logx.Disconnected(err) {
			e.Log.Debug("upstream request cancelled", "repo", repo.Name, "url", u)
		} else {
			e.Log.Warn("upstream request failed", "repo", repo.Name, "url", u, "err", err)
		}
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

	ct := pol.ContentType
	if ct == "" {
		ct = resp.Header.Get("Content-Type")
	}
	var body io.Reader = resp.Body
	if sp != nil {
		rb.body = resp.Body
		rb.open = func(ctx context.Context, off int64) (io.ReadCloser, error) {
			return e.resumeUpstream(ctx, repo, u, pol, off)
		}
		defer rb.Close()
		sp.begin(resp.ContentLength, ct)
		body = io.TeeReader(rb, sp)
	}
	info, err := e.Content.PutBlob(ctx, repo.StorageID, body, pol.ExpectedDigest)
	if err != nil {
		if rb != nil && rb.err != nil {
			e.Log.Warn("upstream transfer gave up", "repo", repo.Name, "url", u, "received", rb.off, "err", rb.err)
			return nil, fmt.Errorf("%w: transfer from %s stopped after %d bytes", ErrUpstream, u, rb.off)
		}
		return nil, fmt.Errorf("store upstream body: %w", err)
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
	if len(pol.KeepHeaders) > 0 {
		kept := map[string]string{}
		for _, k := range pol.KeepHeaders {
			if v := resp.Header.Get(k); v != "" {
				kept[k] = v
			}
		}
		at["headers"] = kept
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

// resumeUpstream asks for the rest of u from byte off.
func (e *Engine) resumeUpstream(ctx context.Context, repo *model.Repository, u string, pol Policy, off int64) (io.ReadCloser, error) {
	e.Log.Info("resuming upstream transfer", "repo", repo.Name, "url", u, "from", off)
	req, err := e.newUpstreamRequest(ctx, repo, http.MethodGet, u, pol)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-", off))
	resp, err := e.doUpstream(req, pol, true)
	if err != nil {
		return nil, err
	}
	// Anything but the exact continuation would splice the wrong bytes into
	// the stream. Storage would catch it at the end by digest; better not to
	// send them to the client in the first place.
	if resp.StatusCode != http.StatusPartialContent ||
		!strings.HasPrefix(resp.Header.Get("Content-Range"), fmt.Sprintf("bytes %d-", off)) {
		resp.Body.Close()
		return nil, fmt.Errorf("upstream cannot resume at %d: %s", off, resp.Status)
	}
	return resp.Body, nil
}

// passthrough proxies a request upstream without caching.
func (e *Engine) passthrough(ctx context.Context, repo *model.Repository, path string, pol Policy) (*Result, error) {
	u := e.upstreamURL(repo, path, pol)
	req, err := e.newUpstreamRequest(ctx, repo, http.MethodGet, u, pol)
	if err != nil {
		return nil, err
	}
	resp, err := e.doUpstream(req, pol, false)
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
	return e.doUpstream(req, pol, false)
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
