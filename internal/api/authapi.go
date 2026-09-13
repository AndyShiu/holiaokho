package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/holiaokho/holiaokho/internal/auth"
)

func (a *API) authRoutes(r chi.Router) {
	r.Route("/auth", func(r chi.Router) {
		r.Get("/settings", a.need("app:system", auth.Read, a.getAuthSettings))
		r.Put("/settings", a.need("app:system", auth.Write, a.putAuthSettings))
		r.Post("/ldap/test", a.need("app:system", auth.Write, a.testLDAP))
		r.Get("/oidc/login", a.oidcLogin)
		r.Get("/oidc/callback", a.oidcCallback)
		r.Get("/methods", a.authMethods)
	})
}

// authMethods tells the UI which login options to show (public).
func (a *API) authMethods(w http.ResponseWriter, r *http.Request) {
	st := a.Auth.Settings(r.Context())
	writeJSON(w, 200, map[string]any{
		"local": true, "ldap": st.LDAP.Enabled, "oidc": st.OIDC.Enabled,
		"oidcLoginUrl": "/api/v1/auth/oidc/login", "anonymous": a.Auth.Cfg.AnonymousEnabled,
		"passwordPolicy": st.Password,
	})
}

func (a *API) getAuthSettings(w http.ResponseWriter, r *http.Request) {
	st := a.Auth.Settings(r.Context())
	st.LDAP.BindPassword = redact(st.LDAP.BindPassword)
	st.OIDC.ClientSecret = redact(st.OIDC.ClientSecret)
	writeJSON(w, 200, st)
}

func (a *API) putAuthSettings(w http.ResponseWriter, r *http.Request) {
	var in auth.Settings
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "body.invalid", "invalid body: %s", err.Error())
		return
	}
	cur := a.Auth.Settings(r.Context())
	if in.Password.MinLength == 0 {
		// password block omitted: keep the current policy
		in.Password = cur.Password
	}
	if in.LDAP.BindPassword == "" || in.LDAP.BindPassword == "***" {
		in.LDAP.BindPassword = cur.LDAP.BindPassword
	}
	if in.OIDC.ClientSecret == "" || in.OIDC.ClientSecret == "***" {
		in.OIDC.ClientSecret = cur.OIDC.ClientSecret
	}
	for _, realm := range in.Realms {
		if realm != "local" && realm != "ldap" {
			writeErr(w, 400, "validation", "unknown realm %q", realm)
			return
		}
	}
	if in.Password.MinLength < 8 {
		writeErr(w, 400, "validation", "password.minLength must be at least 8")
		return
	}
	if in.Password.MaxLength != 0 && in.Password.MaxLength < in.Password.MinLength {
		writeErr(w, 400, "validation", "password.maxLength must be >= minLength")
		return
	}
	if err := a.Auth.SaveSettings(r.Context(), in); err != nil {
		a.fail(w, err)
		return
	}
	a.audit_(r, "auth.settings", "settings", "auth", map[string]any{"realms": in.Realms, "ldap": in.LDAP.Enabled, "oidc": in.OIDC.Enabled, "rut": in.Rut.Enabled})
	a.getAuthSettings(w, r)
}

func (a *API) testLDAP(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Config   *auth.LDAPConfig `json:"config"`
		Username string           `json:"username"`
		Password string           `json:"password"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, "body.invalid", "invalid body: %s", err.Error())
		return
	}
	cfg := a.Auth.Settings(r.Context()).LDAP
	if in.Config != nil {
		cfg = *in.Config
	}
	out, err := a.Auth.TestLDAP(r.Context(), cfg, in.Username, in.Password)
	if err != nil {
		writeErr(w, 502, "ldap.failed", "%s", err.Error())
		return
	}
	writeJSON(w, 200, out)
}

func (a *API) oidcLogin(w http.ResponseWriter, r *http.Request) {
	a.Auth.OIDCLogin(w, r, a.Deps.BaseURL(r))
}

func (a *API) oidcCallback(w http.ResponseWriter, r *http.Request) {
	u, next, err := a.Auth.OIDCCallback(w, r, a.Deps.BaseURL(r))
	if err != nil {
		a.Deps.Log.Warn("oidc callback", "err", err)
		writeErr(w, 401, "oidc.failed", "%s", err.Error())
		return
	}
	id, exp, err := a.Auth.CreateSession(r.Context(), u.Username)
	if err != nil {
		a.fail(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: auth.SessionCookie, Value: id, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Expires: exp, Secure: isHTTPS(r)})
	a.audit_(r, "login", "user", u.Username, map[string]any{"via": "oidc"})
	http.Redirect(w, r, next, http.StatusFound)
}
