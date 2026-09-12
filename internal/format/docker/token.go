package docker

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/holiaokho/holiaokho/internal/auth"
)

// TokenIssuer implements the Docker registry token endpoint (/v2/token) and
// validates the opaque bearer tokens it issues. Tokens live in memory —
// acceptable for a single node; a clustered deployment would move this to
// the database.
type TokenIssuer struct {
	Auth *auth.Service
	TTL  time.Duration

	mu     sync.Mutex
	tokens map[string]issued
}

type issued struct {
	username string
	expires  time.Time
}

func NewTokenIssuer(a *auth.Service) *TokenIssuer {
	return &TokenIssuer{Auth: a, TTL: time.Hour, tokens: map[string]issued{}}
}

// ServeHTTP handles GET /v2/token?service=...&scope=repository:name:pull,push.
// Credentials may be passed as Basic auth; otherwise the anonymous principal
// is used. The registry endpoints enforce permissions on every request, so
// the token merely binds an identity.
func (t *TokenIssuer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	username := auth.UserAnonymous
	if user, pass, ok := r.BasicAuth(); ok {
		p, err := t.Auth.Login(ctx, auth.ClientIP(r), user, pass)
		if err != nil {
			status := http.StatusUnauthorized
			if err == auth.ErrRateLimited {
				status = http.StatusTooManyRequests
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			json.NewEncoder(w).Encode(map[string]any{"errors": []map[string]any{{"code": "UNAUTHORIZED", "message": "invalid credentials"}}})
			return
		}
		username = p.Username
	} else if p := auth.PrincipalFrom(ctx); p != nil && !p.Anonymous {
		username = p.Username
	}
	raw := make([]byte, 32)
	rand.Read(raw)
	tok := base64.RawURLEncoding.EncodeToString(raw)
	exp := time.Now().Add(t.TTL)
	t.mu.Lock()
	t.tokens[tok] = issued{username: username, expires: exp}
	// Opportunistic cleanup.
	if len(t.tokens) > 10000 {
		now := time.Now()
		for k, v := range t.tokens {
			if v.expires.Before(now) {
				delete(t.tokens, k)
			}
		}
	}
	t.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"token": tok, "access_token": tok, "expires_in": int(t.TTL.Seconds()), "issued_at": time.Now().UTC().Format(time.RFC3339),
	})
}

// Principal resolves the bearer token in the request, if any, to a principal.
// Returns nil when no docker token is present.
func (t *TokenIssuer) Principal(ctx context.Context, r *http.Request) *auth.Principal {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return nil
	}
	tok := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	if strings.HasPrefix(tok, auth.TokenPrefix) {
		return nil // user tokens are resolved by the generic middleware
	}
	t.mu.Lock()
	is, ok := t.tokens[tok]
	t.mu.Unlock()
	if !ok || is.expires.Before(time.Now()) {
		return nil
	}
	p, err := t.Auth.PrincipalFor(ctx, is.username)
	if err != nil {
		return nil
	}
	return p
}
