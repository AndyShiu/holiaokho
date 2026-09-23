package docker

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// A registry refusing a token is answering, not failing. GHCR does this for
// any repository name that does not exist; if it surfaced as a transport
// error the engine would count it as an outage and block the whole upstream.
func TestTokenRefusalIsAResponseNotAnError(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"errors":[{"code":"DENIED"}]}`))
		default:
			w.Header().Set("WWW-Authenticate", `Bearer realm="`+srv.URL+`/token",service="test",scope="repository:nope:pull"`)
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer srv.Close()

	c := newAuthClient(srv.Client(), "", "")
	resp, err := c.Get(srv.URL + "/v2/nope/manifests/latest")
	if err != nil {
		t.Fatalf("refusal surfaced as an error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d, want 403", resp.StatusCode)
	}
}
