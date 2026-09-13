package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/holiaokho/holiaokho/internal/model"
)

// OIDC login: GET /api/v1/auth/oidc/login → provider → GET .../callback →
// session cookie → redirect to "/". State is kept in a short-lived cookie.

const oidcStateCookie = "holiaokho_oidc_state"

func (s *Service) oidcProvider(ctx context.Context, cfg OIDCConfig) (*gooidc.Provider, error) {
	s.oidcMu.Lock()
	defer s.oidcMu.Unlock()
	if s.oidcProv != nil && s.oidcIssuer == cfg.Issuer {
		return s.oidcProv, nil
	}
	if cfg.InsecureSkipIssuerVerify {
		ctx = gooidc.InsecureIssuerURLContext(ctx, cfg.Issuer)
	}
	p, err := gooidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	s.oidcProv, s.oidcIssuer = p, cfg.Issuer
	return p, nil
}

func (s *Service) oauthConfig(p *gooidc.Provider, cfg OIDCConfig, redirect string) *oauth2.Config {
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{gooidc.ScopeOpenID, "profile", "email"}
	}
	return &oauth2.Config{ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, Endpoint: p.Endpoint(), RedirectURL: redirect, Scopes: scopes}
}

// OIDCLogin starts the authorisation code flow.
func (s *Service) OIDCLogin(w http.ResponseWriter, r *http.Request, baseURL string) {
	cfg := s.Settings(r.Context()).OIDC
	if !cfg.Enabled {
		http.Error(w, "OIDC is not enabled", http.StatusNotFound)
		return
	}
	p, err := s.oidcProvider(r.Context(), cfg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	redirect := cfg.RedirectURL
	if redirect == "" {
		redirect = baseURL + "/api/v1/auth/oidc/callback"
	}
	raw := make([]byte, 24)
	rand.Read(raw)
	state := base64.RawURLEncoding.EncodeToString(raw)
	next := r.URL.Query().Get("next")
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") {
		next = "/"
	}
	http.SetCookie(w, &http.Cookie{Name: oidcStateCookie, Value: state + "|" + next, Path: "/api/v1/auth/oidc", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 600, Secure: r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")})
	http.Redirect(w, r, s.oauthConfig(p, cfg, redirect).AuthCodeURL(state), http.StatusFound)
}

// OIDCCallback completes the flow and returns the authenticated user.
func (s *Service) OIDCCallback(w http.ResponseWriter, r *http.Request, baseURL string) (*model.User, string, error) {
	cfg := s.Settings(r.Context()).OIDC
	if !cfg.Enabled {
		return nil, "", errors.New("OIDC is not enabled")
	}
	c, err := r.Cookie(oidcStateCookie)
	if err != nil {
		return nil, "", errors.New("missing state cookie")
	}
	state, next, _ := strings.Cut(c.Value, "|")
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") {
		next = "/"
	}
	if r.URL.Query().Get("state") != state {
		return nil, "", errors.New("state mismatch")
	}
	http.SetCookie(w, &http.Cookie{Name: oidcStateCookie, Value: "", Path: "/api/v1/auth/oidc", MaxAge: -1})
	if e := r.URL.Query().Get("error"); e != "" {
		return nil, "", fmt.Errorf("provider error: %s %s", e, r.URL.Query().Get("error_description"))
	}
	p, err := s.oidcProvider(r.Context(), cfg)
	if err != nil {
		return nil, "", err
	}
	redirect := cfg.RedirectURL
	if redirect == "" {
		redirect = baseURL + "/api/v1/auth/oidc/callback"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	tok, err := s.oauthConfig(p, cfg, redirect).Exchange(ctx, r.URL.Query().Get("code"))
	if err != nil {
		return nil, "", fmt.Errorf("token exchange: %w", err)
	}
	rawID, _ := tok.Extra("id_token").(string)
	if rawID == "" {
		return nil, "", errors.New("no id_token in response")
	}
	idt, err := p.Verifier(&gooidc.Config{ClientID: cfg.ClientID, SkipIssuerCheck: cfg.InsecureSkipIssuerVerify}).Verify(ctx, rawID)
	if err != nil {
		return nil, "", fmt.Errorf("verify id_token: %w", err)
	}
	claims := map[string]any{}
	idt.Claims(&claims)
	// Some providers only put groups/profile in userinfo.
	if ui, err := p.UserInfo(ctx, oauth2.StaticTokenSource(tok)); err == nil {
		extra := map[string]any{}
		if ui.Claims(&extra) == nil {
			for k, v := range extra {
				if _, ok := claims[k]; !ok {
					claims[k] = v
				}
			}
		}
	}
	u, err := s.userFromClaims(r.Context(), cfg, claims)
	return u, next, err
}

func (s *Service) userFromClaims(ctx context.Context, cfg OIDCConfig, claims map[string]any) (*model.User, error) {
	uc := cfg.UsernameClaim
	if uc == "" {
		uc = "preferred_username"
	}
	username, _ := claims[uc].(string)
	if username == "" {
		username, _ = claims["email"].(string)
	}
	if username == "" {
		username, _ = claims["sub"].(string)
	}
	if username == "" {
		return nil, errors.New("no usable username claim")
	}
	var groups []string
	if cfg.GroupsClaim != "" {
		if gs, ok := claims[cfg.GroupsClaim].([]any); ok {
			for _, g := range gs {
				if str, ok := g.(string); ok {
					groups = append(groups, str)
				}
			}
		}
	}
	u := &model.User{Username: username, Source: "oidc", Active: true, Roles: mapGroups(groups, cfg.RoleMapping, cfg.DefaultRoles)}
	u.Email, _ = claims["email"].(string)
	u.DisplayName, _ = claims["name"].(string)
	return s.syncExternalUser(ctx, u)
}

// oidc state on the service
type oidcState struct {
	oidcMu     sync.Mutex
	oidcProv   *gooidc.Provider
	oidcIssuer string
}
