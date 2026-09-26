package repo

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/holiaokho/holiaokho/internal/model"
)

// maxRetryWait caps how long one retry waits. An upstream asking for longer
// (Retry-After: 60) is answered with its own response rather than holding
// the client's request open for a minute.
const maxRetryWait = 5 * time.Second

// retryBase is the first backoff; each later attempt doubles it.
var retryBase = 500 * time.Millisecond

func (e *Engine) retriesFor(repo *model.Repository) int {
	if repo != nil && repo.Proxy != nil && repo.Proxy.Retries != nil {
		return *repo.Proxy.Retries
	}
	return e.retries
}

// doWithRetry sends req, trying again after failures that a second attempt
// can fix: a dropped or refused connection, a timeout, or an upstream saying
// it is briefly unavailable (429, 502, 503, 504). A proxy is a client of a
// public registry across the internet, and one reset connection should not
// become a failed build.
//
// Only requests that are safe to send twice are retried: GET and HEAD with
// no body. Answers are not retried — a 404 or 401 is what the upstream
// means, and an invalid certificate will not become valid in a second.
func (e *Engine) doWithRetry(c *http.Client, repo *model.Repository, req *http.Request) (*http.Response, error) {
	retries := e.retriesFor(repo)
	if (req.Method != http.MethodGet && req.Method != http.MethodHead) || req.Body != nil && req.Body != http.NoBody {
		retries = 0
	}
	for attempt := 0; ; attempt++ {
		resp, err := c.Do(req)
		if attempt >= retries || !retryable(req.Context(), resp, err) {
			return resp, err
		}
		wait, ok := retryWait(attempt, resp)
		if !ok {
			return resp, err
		}
		reason := ""
		if err != nil {
			reason = err.Error()
		} else {
			reason = resp.Status
			io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
			resp.Body.Close()
		}
		e.Log.Debug("retrying upstream request", "repo", repo.Name, "url", req.URL.Redacted(), "attempt", attempt+1, "of", retries, "after", wait, "reason", reason)
		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(wait):
		}
	}
}

func retryable(ctx context.Context, resp *http.Response, err error) bool {
	if ctx.Err() != nil {
		return false // the client gave up; nobody is waiting for the answer
	}
	if err != nil {
		var cert x509.UnknownAuthorityError
		var host x509.HostnameError
		var inv x509.CertificateInvalidError
		if errors.As(err, &cert) || errors.As(err, &host) || errors.As(err, &inv) {
			return false
		}
		var dns *net.DNSError
		if errors.As(err, &dns) && dns.IsNotFound {
			return false // a typo in the remote URL stays a typo
		}
		return true
	}
	switch resp.StatusCode {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// retryWait is the pause before the next attempt: exponential backoff with
// jitter, or the upstream's Retry-After when it gives one. ok is false when
// the upstream asks for longer than maxRetryWait.
func retryWait(attempt int, resp *http.Response) (time.Duration, bool) {
	if resp != nil {
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			var d time.Duration
			if n, err := strconv.Atoi(ra); err == nil {
				d = time.Duration(n) * time.Second
			} else if t, err := http.ParseTime(ra); err == nil {
				d = time.Until(t)
			}
			if d > maxRetryWait {
				return 0, false
			}
			if d > 0 {
				return d, true
			}
		}
	}
	d := retryBase << attempt
	d += time.Duration(rand.Int64N(int64(retryBase / 2)))
	return min(d, maxRetryWait), true
}
