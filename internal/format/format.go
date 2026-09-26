// Package format defines the plugin interface every repository format
// implements, plus HTTP helpers shared by plugins.
package format

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/holiaokho/holiaokho/internal/auth"
	"github.com/holiaokho/holiaokho/internal/content"
	"github.com/holiaokho/holiaokho/internal/model"
	"github.com/holiaokho/holiaokho/internal/repo"
	"github.com/holiaokho/holiaokho/internal/storage"
)

// Deps is everything a plugin may use. Plugins never touch the DB directly.
type Deps struct {
	Content *content.Service
	Engine  *repo.Engine
	Auth    *auth.Service
	Log     *slog.Logger
	// BaseURL returns the externally visible base URL for a request
	// (scheme://host[:port], no trailing slash).
	BaseURL func(r *http.Request) string
}

// Format is a repository format plugin.
type Format interface {
	// Name is the format identifier used in repository.format ("maven", "npm").
	Name() string
	// Handler serves requests whose path is relative to /repository/<name>/.
	// The handler must perform authorization via Deps (see Authorize).
	Handler(r *model.Repository, d Deps) http.Handler
	// ValidateAttributes checks format-specific repository attributes on
	// create/update and may fill defaults. attrs is the full attributes object.
	ValidateAttributes(r *model.Repository, attrs map[string]json.RawMessage) error
	// VersionLess reports whether version a sorts before b (cleanup "keep N").
	VersionLess(a, b string) bool
	// Parse extracts package coordinates from an uploaded asset path, or
	// returns nil when the path is not a package artifact. Used by the
	// generic upload API and the Nexus importer.
	Parse(path string) *model.Package
}

// GroupDeployFilter is implemented by formats whose clients send writes to a
// group that are not deployments — logins, security audits, batch lookups.
// IsDeploy reports whether a PUT, POST or PATCH (path relative to the
// repository) is a deployment. Formats without it treat every such request
// as one.
type GroupDeployFilter interface {
	IsDeploy(r *http.Request) bool
}

// WriteAuthorizer is implemented by formats that authenticate their
// clients their own way (Docker bearer tokens) and so must check a write to
// a group themselves. It writes the 401/403 and returns false on refusal.
type WriteAuthorizer interface {
	AuthorizeWrite(w http.ResponseWriter, r *http.Request, rp *model.Repository, d Deps) bool
}

// IsGroupDeploy reports whether r, addressed to a group of format f, is a
// deployment to hand to the group's first hosted member.
func IsGroupDeploy(f Format, r *http.Request) bool {
	switch r.Method {
	case http.MethodPut, http.MethodPost, http.MethodPatch:
	default:
		return false
	}
	if gf, ok := f.(GroupDeployFilter); ok {
		return gf.IsDeploy(r)
	}
	return true
}

// AuthorizeGroupWrite checks that the caller may write to the group rp.
func AuthorizeGroupWrite(f Format, w http.ResponseWriter, r *http.Request, rp *model.Repository, d Deps) bool {
	if a, ok := f.(WriteAuthorizer); ok {
		return a.AuthorizeWrite(w, r, rp, d)
	}
	return Authorize(w, r, rp, auth.Write, `Basic realm="Holiaokho"`)
}

// Registry maps format names to plugins.
type Registry struct {
	mu      sync.RWMutex
	formats map[string]Format
}

func NewRegistry() *Registry { return &Registry{formats: map[string]Format{}} }

func (r *Registry) Register(f Format) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.formats[f.Name()] = f
}

func (r *Registry) Get(name string) (Format, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, ok := r.formats[name]
	return f, ok
}

func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.formats))
	for n := range r.formats {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// ----------------------------------------------------------- HTTP helpers

// ActionFor maps an HTTP method to an RBAC action.
func ActionFor(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return auth.Read
	case http.MethodDelete:
		return auth.Delete
	default:
		return auth.Write
	}
}

// Authorize checks the request principal against the repository. On failure
// it writes 401 (no credentials) or 403 and returns false. challenge is the
// WWW-Authenticate value used for 401.
func Authorize(w http.ResponseWriter, r *http.Request, repo *model.Repository, action, challenge string) bool {
	p := auth.PrincipalFrom(r.Context())
	if p != nil && p.CanContent(repo.Name, repo.Format, r.URL.Path, action) {
		return true
	}
	if p == nil || p.Anonymous {
		if challenge != "" {
			w.Header().Set("WWW-Authenticate", challenge)
		}
		WriteError(w, http.StatusUnauthorized, "auth.required", "authentication required")
		return false
	}
	WriteError(w, http.StatusForbidden, "auth.forbidden", "permission denied")
	return false
}

// WriteError emits the standard error body: {"code": ..., "message": ...}.
func WriteError(w http.ResponseWriter, status int, code, msg string, params ...any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := map[string]any{"code": code, "message": msg}
	if len(params) > 0 {
		body["message"] = fmt.Sprintf(msg, params...)
		body["params"] = params
	}
	json.NewEncoder(w).Encode(body)
}

// MapError translates engine errors to HTTP responses.
func MapError(w http.ResponseWriter, err error, log *slog.Logger) {
	switch {
	case errors.Is(err, repo.ErrNotFound), errors.Is(err, content.ErrNotFound), errors.Is(err, repo.ErrRouted):
		WriteError(w, http.StatusNotFound, "not_found", "not found")
	case errors.Is(err, repo.ErrOffline):
		WriteError(w, http.StatusServiceUnavailable, "repo.offline", "repository is offline")
	case errors.Is(err, repo.ErrRedeploy):
		WriteError(w, http.StatusBadRequest, "repo.redeploy_denied", "redeployment is not allowed by the repository write policy")
	case errors.Is(err, repo.ErrWriteDenied), errors.Is(err, repo.ErrReadOnly):
		WriteError(w, http.StatusForbidden, "repo.read_only", "deployment not allowed")
	case errors.Is(err, content.ErrQuota):
		WriteError(w, http.StatusInsufficientStorage, "storage.quota", "%s", err.Error())
	case errors.Is(err, repo.ErrInvalidPath):
		WriteError(w, http.StatusBadRequest, "path.invalid", "invalid path")
	case errors.Is(err, repo.ErrUpstreamDenied):
		WriteError(w, http.StatusBadGateway, "upstream.denied", "%s", err.Error())
	case errors.Is(err, repo.ErrUpstream):
		WriteError(w, http.StatusBadGateway, "upstream.error", "%s", err.Error())
	default:
		if log != nil {
			log.Error("internal error", "err", err)
		}
		WriteError(w, http.StatusInternalServerError, "internal", "internal error")
	}
}

// ServeResult writes a fetched asset with Content-Type/Length, ETag,
// Last-Modified, and single-range support. It closes the result.
func ServeResult(w http.ResponseWriter, r *http.Request, d Deps, res *repo.Result) {
	defer res.Close()
	if res.Status != 0 && res.Headers != nil { // pass-through
		for _, h := range []string{"Content-Type", "Content-Length", "Cache-Control", "ETag", "Last-Modified"} {
			if v := res.Headers.Get(h); v != "" {
				w.Header().Set(h, v)
			}
		}
		w.WriteHeader(res.Status)
		if r.Method != http.MethodHead {
			io.Copy(w, res.Body)
		}
		return
	}
	ct := res.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Accept-Ranges", "bytes")
	if res.Asset != nil {
		if res.Asset.BlobDigest != nil {
			w.Header().Set("ETag", `"`+strings.TrimPrefix(*res.Asset.BlobDigest, "sha256:")+`"`)
			w.Header().Set("X-Checksum-Sha256", strings.TrimPrefix(*res.Asset.BlobDigest, "sha256:"))
		}
		w.Header().Set("Last-Modified", res.Asset.UpdatedAt.UTC().Format(http.TimeFormat))
		if inm := r.Header.Get("If-None-Match"); inm != "" && res.Asset.BlobDigest != nil &&
			strings.Trim(inm, `"`) == strings.TrimPrefix(*res.Asset.BlobDigest, "sha256:") {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	// Range: only "bytes=a-b" / "bytes=a-" single ranges.
	if rng := r.Header.Get("Range"); rng != "" && res.Asset != nil && res.Asset.BlobDigest != nil && res.Size > 0 {
		if start, end, ok := parseRange(rng, res.Size); ok {
			res.Body.Close()
			rc, err := d.Content.OpenBlobRange(r.Context(), storage.Digest(*res.Asset.BlobDigest), start, end-start+1)
			if err == nil {
				defer rc.Close()
				w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, res.Size))
				w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
				w.WriteHeader(http.StatusPartialContent)
				if r.Method != http.MethodHead {
					io.Copy(w, rc)
				}
				return
			}
		}
	}
	if res.Size >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(res.Size, 10))
	}
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		io.Copy(w, res.Body)
	}
}

func parseRange(h string, size int64) (int64, int64, bool) {
	if !strings.HasPrefix(h, "bytes=") || strings.Contains(h, ",") {
		return 0, 0, false
	}
	spec := strings.TrimPrefix(h, "bytes=")
	i := strings.IndexByte(spec, '-')
	if i < 0 {
		return 0, 0, false
	}
	var start, end int64
	var err error
	if spec[:i] == "" {
		n, err := strconv.ParseInt(spec[i+1:], 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, false
		}
		if n > size {
			n = size
		}
		return size - n, size - 1, true
	}
	if start, err = strconv.ParseInt(spec[:i], 10, 64); err != nil || start >= size {
		return 0, 0, false
	}
	if spec[i+1:] == "" {
		end = size - 1
	} else if end, err = strconv.ParseInt(spec[i+1:], 10, 64); err != nil || end < start {
		return 0, 0, false
	}
	if end >= size {
		end = size - 1
	}
	return start, end, true
}

// ReplayHeaders sets response headers stored by Policy.KeepHeaders.
func ReplayHeaders(w http.ResponseWriter, a *model.Asset) {
	if a == nil {
		return
	}
	var attrs struct {
		Headers map[string]string `json:"headers"`
	}
	json.Unmarshal(a.Attrs, &attrs)
	for k, v := range attrs.Headers {
		w.Header().Set(k, v)
	}
}

// ServeBytes writes an in-memory document (merged metadata).
func ServeBytes(w http.ResponseWriter, r *http.Request, contentType string, b []byte, modified time.Time) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	if !modified.IsZero() {
		w.Header().Set("Last-Modified", modified.UTC().Format(http.TimeFormat))
	}
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		w.Write(b)
	}
}

// FetchAndServe is the common GET/HEAD path for file-like assets.
func FetchAndServe(w http.ResponseWriter, r *http.Request, d Deps, rp *model.Repository, path string, pol repo.Policy) {
	res, err := d.Engine.Fetch(r.Context(), rp, path, pol)
	if err != nil {
		MapError(w, err, d.Log)
		return
	}
	ServeResult(w, r, d, res)
}

// AttrBlock decodes a named block from repository attributes.
func AttrBlock(rp *model.Repository, name string, dst any) error {
	var m map[string]json.RawMessage
	if len(rp.Attributes) == 0 {
		return nil
	}
	if err := json.Unmarshal(rp.Attributes, &m); err != nil {
		return err
	}
	raw, ok := m[name]
	if !ok {
		return nil
	}
	return json.Unmarshal(raw, dst)
}

// Ctx is a shorthand for a request context with a timeout for upstream work.
func Ctx(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}
