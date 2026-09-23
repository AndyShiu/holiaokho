package docker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// authTransport adds registry bearer tokens to upstream requests, handling
// the WWW-Authenticate: Bearer challenge flow used by Docker Hub, GHCR, etc.
// Tokens are cached per scope.
type authTransport struct {
	base     http.RoundTripper
	username string
	password string

	mu     sync.Mutex
	tokens map[string]cachedToken // scope -> token
}

type cachedToken struct {
	token   string
	expires time.Time
}

func newAuthClient(base *http.Client, username, password string) *http.Client {
	c := *base
	c.Transport = &authTransport{base: base.Transport, username: username, password: password, tokens: map[string]cachedToken{}}
	return &c
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil && req.Body != http.NoBody {
		// Bodies cannot be replayed after a challenge; upstream pushes are not supported.
		return t.base.RoundTrip(req)
	}
	scope := scopeFor(req)
	if tok := t.cached(scope); tok != "" {
		r2 := req.Clone(req.Context())
		r2.Header.Set("Authorization", "Bearer "+tok)
		resp, err := t.base.RoundTrip(r2)
		if err != nil || resp.StatusCode != http.StatusUnauthorized {
			return resp, err
		}
		resp.Body.Close()
	}
	resp, err := t.base.RoundTrip(req)
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		return resp, err
	}
	challenge := resp.Header.Get("WWW-Authenticate")
	resp.Body.Close()
	if !strings.HasPrefix(strings.ToLower(challenge), "bearer ") {
		if strings.HasPrefix(strings.ToLower(challenge), "basic ") && t.username != "" {
			r2 := req.Clone(req.Context())
			r2.SetBasicAuth(t.username, t.password)
			return t.base.RoundTrip(r2)
		}
		return t.base.RoundTrip(req)
	}
	params := parseChallenge(challenge)
	tok, exp, err := t.fetchToken(req, params)
	var denied *tokenDenied
	if errors.As(err, &denied) {
		// The registry answered, and the answer is no. GHCR refuses a token
		// for a repository that does not exist, so this is how a mistyped
		// image name looks from here. Returned as an error it would count as
		// the upstream being down, and one typo would block every image on
		// the registry for everyone. As a response it is what it is: a
		// refusal, which the engine already knows how to treat.
		return denied.response(req), nil
	}
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	t.tokens[scope] = cachedToken{token: tok, expires: exp}
	t.mu.Unlock()
	r2 := req.Clone(req.Context())
	r2.Header.Set("Authorization", "Bearer "+tok)
	return t.base.RoundTrip(r2)
}

func (t *authTransport) cached(scope string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	c, ok := t.tokens[scope]
	if !ok || c.expires.Before(time.Now().Add(10*time.Second)) {
		return ""
	}
	return c.token
}

// scopeFor derives "repository:<name>:pull" from the request path.
func scopeFor(req *http.Request) string {
	p := req.URL.Path
	i := strings.Index(p, "/v2/")
	if i < 0 {
		return ""
	}
	rest := p[i+4:]
	for _, marker := range []string{"/manifests/", "/blobs/", "/tags/"} {
		if j := strings.Index(rest, marker); j > 0 {
			return "repository:" + rest[:j] + ":pull"
		}
	}
	return ""
}

func parseChallenge(h string) map[string]string {
	out := map[string]string{}
	h = strings.TrimSpace(h[len("Bearer "):])
	for _, part := range splitParams(h) {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			out[strings.ToLower(strings.TrimSpace(kv[0]))] = strings.Trim(strings.TrimSpace(kv[1]), `"`)
		}
	}
	return out
}

func splitParams(s string) []string {
	var out []string
	var cur strings.Builder
	inq := false
	for _, r := range s {
		switch {
		case r == '"':
			inq = !inq
			cur.WriteRune(r)
		case r == ',' && !inq:
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func (t *authTransport) fetchToken(orig *http.Request, params map[string]string) (string, time.Time, error) {
	realm := params["realm"]
	if realm == "" {
		return "", time.Time{}, fmt.Errorf("bearer challenge without realm")
	}
	u := realm
	sep := "?"
	if strings.Contains(u, "?") {
		sep = "&"
	}
	if s := params["service"]; s != "" {
		u += sep + "service=" + s
		sep = "&"
	}
	if s := params["scope"]; s != "" {
		u += sep + "scope=" + s
	}
	req, err := http.NewRequestWithContext(orig.Context(), http.MethodGet, u, nil)
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("User-Agent", orig.Header.Get("User-Agent"))
	if t.username != "" {
		req.SetBasicAuth(t.username, t.password)
	}
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return "", time.Time{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return "", time.Time{}, &tokenDenied{status: resp.StatusCode, body: b}
		}
		return "", time.Time{}, fmt.Errorf("token endpoint %s returned %d: %s", realm, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var body struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return "", time.Time{}, err
	}
	tok := body.Token
	if tok == "" {
		tok = body.AccessToken
	}
	if tok == "" {
		return "", time.Time{}, fmt.Errorf("token endpoint returned no token")
	}
	exp := body.ExpiresIn
	if exp <= 0 {
		exp = 60
	}
	return tok, time.Now().Add(time.Duration(exp) * time.Second), nil
}

// tokenDenied is a token endpoint refusing the requested scope.
type tokenDenied struct {
	status int
	body   []byte
}

func (d *tokenDenied) Error() string {
	return fmt.Sprintf("token endpoint returned %d", d.status)
}

func (d *tokenDenied) response(req *http.Request) *http.Response {
	return &http.Response{
		StatusCode:    d.status,
		Status:        fmt.Sprintf("%d %s", d.status, http.StatusText(d.status)),
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        http.Header{"Content-Type": {"application/json"}},
		Body:          io.NopCloser(bytes.NewReader(d.body)),
		ContentLength: int64(len(d.body)),
		Request:       req,
	}
}
