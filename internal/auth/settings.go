package auth

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/holiaokho/holiaokho/internal/secrets"
)

// Settings is the persisted authentication configuration (settings.key='auth').
type Settings struct {
	// Realms is the order in which password logins are attempted: local, ldap.
	Realms []string `json:"realms"`
	// DefaultRoles are granted to every authenticated user (Nexus "Default Role").
	DefaultRoles []string `json:"defaultRoles"`
	// Password is the complexity policy for local passwords.
	Password PasswordPolicy `json:"password"`
	// Anonymous enables unauthenticated access. nil means "not set here", in
	// which case auth.anonymous_enabled from the config file applies.
	Anonymous *bool `json:"anonymous,omitempty"`
	LDAP     LDAPConfig     `json:"ldap"`
	OIDC     OIDCConfig     `json:"oidc"`
	Rut      RutConfig      `json:"rut"`
}

type LDAPConfig struct {
	Enabled            bool   `json:"enabled"`
	URL                string `json:"url"` // ldap://host:389 or ldaps://host:636
	StartTLS           bool   `json:"startTls"`
	InsecureSkipVerify bool   `json:"insecureSkipVerify"`
	BindDN             string `json:"bindDn"`
	BindPassword       string `json:"bindPassword,omitempty"`
	UserBaseDN         string `json:"userBaseDn"`
	UserFilter         string `json:"userFilter"` // e.g. (uid={username}) or (sAMAccountName={username})
	UserSubtree        bool   `json:"userSubtree"`
	EmailAttr          string `json:"emailAttr"`
	DisplayNameAttr    string `json:"displayNameAttr"`
	GroupBaseDN        string `json:"groupBaseDn"`
	GroupFilter        string `json:"groupFilter"` // e.g. (member={dn}) or (memberUid={username})
	GroupNameAttr      string `json:"groupNameAttr"`
	// MemberOfAttr reads groups from the user entry instead of searching (AD memberOf).
	MemberOfAttr string            `json:"memberOfAttr"`
	RoleMapping  map[string]string `json:"roleMapping"` // ldap group name (or DN) -> role id
	DefaultRoles []string          `json:"defaultRoles"`
	Timeout      int               `json:"timeoutSeconds"`
}

type OIDCConfig struct {
	Enabled       bool              `json:"enabled"`
	Issuer        string            `json:"issuer"`
	ClientID      string            `json:"clientId"`
	ClientSecret  string            `json:"clientSecret,omitempty"`
	Scopes        []string          `json:"scopes"`
	UsernameClaim string            `json:"usernameClaim"` // default preferred_username, then email
	GroupsClaim   string            `json:"groupsClaim"`   // e.g. groups
	RoleMapping   map[string]string `json:"roleMapping"`
	DefaultRoles  []string          `json:"defaultRoles"`
	// RedirectURL overrides the derived <base>/api/v1/auth/oidc/callback.
	RedirectURL string `json:"redirectUrl"`
	// InsecureSkipIssuerVerify tolerates issuer mismatches (some proxies).
	InsecureSkipIssuerVerify bool `json:"insecureSkipIssuerVerify"`
}

type RutConfig struct {
	Enabled bool   `json:"enabled"`
	Header  string `json:"header"` // e.g. X-Forwarded-User / REMOTE_USER
	// TrustedProxies are CIDRs allowed to assert the header; empty = any.
	TrustedProxies []string `json:"trustedProxies"`
	AutoCreate     bool     `json:"autoCreate"`
	DefaultRoles   []string `json:"defaultRoles"`
}

var settingsCache struct {
	mu sync.RWMutex
	v  Settings
	at time.Time
}

func defaultSettings() Settings {
	return Settings{Realms: []string{"local", "ldap"}, Password: defaultPasswordPolicy(), LDAP: LDAPConfig{UserFilter: "(uid={username})", UserSubtree: true, EmailAttr: "mail", DisplayNameAttr: "cn", GroupNameAttr: "cn", Timeout: 10},
		OIDC: OIDCConfig{Scopes: []string{"openid", "profile", "email"}, UsernameClaim: "preferred_username"}, Rut: RutConfig{Header: "X-Forwarded-User"}}
}

// Settings returns the cached auth settings (reloaded every 30s).
func (s *Service) Settings(ctx context.Context) Settings {
	settingsCache.mu.RLock()
	if time.Since(settingsCache.at) < 30*time.Second {
		v := settingsCache.v
		settingsCache.mu.RUnlock()
		return v
	}
	settingsCache.mu.RUnlock()
	v := defaultSettings()
	var raw []byte
	if err := s.DB.Pool.QueryRow(ctx, `SELECT value FROM settings WHERE key='auth'`).Scan(&raw); err == nil {
		json.Unmarshal(raw, &v)
		if pw, err := secrets.Decrypt(v.LDAP.BindPassword); err == nil {
			v.LDAP.BindPassword = pw
		} else {
			s.Log.Error("decrypt ldap bind password", "err", err)
		}
		if cs, err := secrets.Decrypt(v.OIDC.ClientSecret); err == nil {
			v.OIDC.ClientSecret = cs
		} else {
			s.Log.Error("decrypt oidc client secret", "err", err)
		}
	}
	if len(v.Realms) == 0 {
		v.Realms = []string{"local", "ldap"}
	}
	settingsCache.mu.Lock()
	settingsCache.v, settingsCache.at = v, time.Now()
	settingsCache.mu.Unlock()
	return v
}

func (s *Service) SaveSettings(ctx context.Context, v Settings) error {
	var err error
	if v.LDAP.BindPassword, err = secrets.Encrypt(v.LDAP.BindPassword); err != nil {
		return err
	}
	if v.OIDC.ClientSecret, err = secrets.Encrypt(v.OIDC.ClientSecret); err != nil {
		return err
	}
	raw, _ := json.Marshal(v)
	_, err = s.DB.Pool.Exec(ctx, `INSERT INTO settings(key,value) VALUES ('auth',$1) ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, raw)
	settingsCache.mu.Lock()
	settingsCache.at = time.Time{}
	settingsCache.mu.Unlock()
	s.oidcMu.Lock()
	s.oidcProv = nil
	s.oidcMu.Unlock()
	return err
}
