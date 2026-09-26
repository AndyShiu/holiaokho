package repo

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/holiaokho/holiaokho/internal/model"
)

func retryEngine(retries int) *Engine {
	return &Engine{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), retries: retries}
}

func fastRetries(t *testing.T) {
	old := retryBase
	retryBase = time.Millisecond
	t.Cleanup(func() { retryBase = old })
}

// upstream answers with statuses in turn, then 200 "ok".
func upstream(t *testing.T, statuses ...int) (*httptest.Server, *atomic.Int32) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := int(n.Add(1)) - 1
		if i < len(statuses) {
			w.WriteHeader(statuses[i])
			return
		}
		io.WriteString(w, "ok")
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

func get(t *testing.T, e *Engine, repo *model.Repository, method, url string, body io.Reader) (*http.Response, error) {
	req, _ := http.NewRequest(method, url, body)
	return e.doWithRetry(http.DefaultClient, repo, req)
}

func TestRetryAfterTransientStatuses(t *testing.T) {
	fastRetries(t)
	srv, n := upstream(t, 503, 502)
	resp, err := get(t, retryEngine(2), &model.Repository{Name: "p"}, "GET", srv.URL, nil)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("got %v, %v; want 200 after two retries", resp, err)
	}
	if n.Load() != 3 {
		t.Fatalf("upstream asked %d times, want 3", n.Load())
	}
}

func TestRetriesRunOut(t *testing.T) {
	fastRetries(t)
	srv, n := upstream(t, 503, 503, 503, 503)
	resp, _ := get(t, retryEngine(2), &model.Repository{Name: "p"}, "GET", srv.URL, nil)
	if resp.StatusCode != 503 || n.Load() != 3 {
		t.Fatalf("got %d after %d tries, want the last 503 after 3", resp.StatusCode, n.Load())
	}
}

func TestAnswersAreNotRetried(t *testing.T) {
	fastRetries(t)
	for _, code := range []int{404, 401, 403, 500} {
		srv, n := upstream(t, code)
		resp, _ := get(t, retryEngine(2), &model.Repository{Name: "p"}, "GET", srv.URL, nil)
		if resp.StatusCode != code || n.Load() != 1 {
			t.Errorf("%d: got %d after %d tries, want it passed on after 1", code, resp.StatusCode, n.Load())
		}
	}
}

func TestOnlySafeRequestsAreRetried(t *testing.T) {
	fastRetries(t)
	srv, n := upstream(t, 503)
	resp, _ := get(t, retryEngine(2), &model.Repository{Name: "p"}, "POST", srv.URL, strings.NewReader("{}"))
	if resp.StatusCode != 503 || n.Load() != 1 {
		t.Fatalf("POST got %d after %d tries, want no retry", resp.StatusCode, n.Load())
	}
}

func TestRepositoryOverridesRetries(t *testing.T) {
	fastRetries(t)
	srv, n := upstream(t, 503, 503)
	zero := 0
	resp, _ := get(t, retryEngine(5), &model.Repository{Name: "p", Proxy: &model.ProxyAttrs{Retries: &zero}}, "GET", srv.URL, nil)
	if resp.StatusCode != 503 || n.Load() != 1 {
		t.Fatalf("with retries 0 got %d after %d tries", resp.StatusCode, n.Load())
	}
}

func TestRetryAfterConnectionRefused(t *testing.T) {
	fastRetries(t)
	// A port that was just listening and now is not: the connection is refused.
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := l.Addr().String()
	l.Close()
	var tries atomic.Int32
	go func() {
		// Come back up while the proxy is retrying.
		time.Sleep(20 * time.Millisecond)
		l2, err := net.Listen("tcp", addr)
		if err != nil {
			return
		}
		http.Serve(l2, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { tries.Add(1); io.WriteString(w, "ok") }))
	}()
	old := retryBase
	retryBase = 30 * time.Millisecond
	defer func() { retryBase = old }()
	resp, err := get(t, retryEngine(3), &model.Repository{Name: "p"}, "GET", "http://"+addr+"/x", nil)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("got %v, %v; want 200 once the upstream is back", resp, err)
	}
}

func TestLongRetryAfterIsNotWaitedFor(t *testing.T) {
	fastRetries(t)
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(429)
	}))
	defer srv.Close()
	start := time.Now()
	resp, _ := get(t, retryEngine(2), &model.Repository{Name: "p"}, "GET", srv.URL, nil)
	if resp.StatusCode != 429 || n.Load() != 1 || time.Since(start) > time.Second {
		t.Fatalf("got %d after %d tries in %v, want the 429 passed on at once", resp.StatusCode, n.Load(), time.Since(start))
	}
}
