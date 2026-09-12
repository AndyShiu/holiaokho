package auth

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"

	"github.com/holiaokho/holiaokho/internal/model"
)

// ldapLogin authenticates username/password against the configured
// directory and returns the (synchronised) local user row.
func (s *Service) ldapLogin(ctx context.Context, cfg LDAPConfig, username, password string) (*model.User, error) {
	if !cfg.Enabled || cfg.URL == "" || password == "" {
		return nil, ErrInvalidCreds
	}
	conn, err := ldapDial(cfg)
	if err != nil {
		return nil, fmt.Errorf("ldap connect: %w", err)
	}
	defer conn.Close()
	if cfg.BindDN != "" {
		if err := conn.Bind(cfg.BindDN, cfg.BindPassword); err != nil {
			return nil, fmt.Errorf("ldap service bind: %w", err)
		}
	}
	filter := strings.ReplaceAll(cfg.UserFilter, "{username}", ldap.EscapeFilter(username))
	scope := ldap.ScopeWholeSubtree
	if !cfg.UserSubtree {
		scope = ldap.ScopeSingleLevel
	}
	attrs := []string{"dn"}
	for _, a := range []string{cfg.EmailAttr, cfg.DisplayNameAttr, cfg.MemberOfAttr} {
		if a != "" {
			attrs = append(attrs, a)
		}
	}
	res, err := conn.Search(ldap.NewSearchRequest(cfg.UserBaseDN, scope, ldap.NeverDerefAliases, 2, cfg.Timeout, false, filter, attrs, nil))
	if err != nil {
		return nil, fmt.Errorf("ldap user search: %w", err)
	}
	if len(res.Entries) != 1 {
		return nil, ErrInvalidCreds
	}
	entry := res.Entries[0]
	// Verify the password by binding as the user.
	if err := conn.Bind(entry.DN, password); err != nil {
		return nil, ErrInvalidCreds
	}
	// Groups.
	var groups []string
	if cfg.MemberOfAttr != "" {
		groups = entry.GetAttributeValues(cfg.MemberOfAttr)
	} else if cfg.GroupBaseDN != "" && cfg.GroupFilter != "" {
		if cfg.BindDN != "" {
			conn.Bind(cfg.BindDN, cfg.BindPassword)
		}
		gf := strings.NewReplacer("{dn}", ldap.EscapeFilter(entry.DN), "{username}", ldap.EscapeFilter(username)).Replace(cfg.GroupFilter)
		gattr := cfg.GroupNameAttr
		if gattr == "" {
			gattr = "cn"
		}
		gres, err := conn.Search(ldap.NewSearchRequest(cfg.GroupBaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, cfg.Timeout, false, gf, []string{gattr}, nil))
		if err == nil {
			for _, g := range gres.Entries {
				groups = append(groups, g.GetAttributeValue(gattr), g.DN)
			}
		}
	}
	roles := mapGroups(groups, cfg.RoleMapping, cfg.DefaultRoles)
	u := &model.User{Username: username, Source: "ldap", Active: true, Roles: roles}
	if cfg.EmailAttr != "" {
		u.Email = entry.GetAttributeValue(cfg.EmailAttr)
	}
	if cfg.DisplayNameAttr != "" {
		u.DisplayName = entry.GetAttributeValue(cfg.DisplayNameAttr)
	}
	return s.syncExternalUser(ctx, u)
}

func ldapDial(cfg LDAPConfig) (*ldap.Conn, error) {
	u, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, err
	}
	tlsCfg := &tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify, ServerName: u.Hostname()}
	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	conn, err := ldap.DialURL(cfg.URL, ldap.DialWithTLSConfig(tlsCfg), ldap.DialWithDialer(&net.Dialer{Timeout: timeout}))
	if err != nil {
		return nil, err
	}
	conn.SetTimeout(timeout)
	if cfg.StartTLS && u.Scheme == "ldap" {
		if err := conn.StartTLS(tlsCfg); err != nil {
			conn.Close()
			return nil, err
		}
	}
	return conn, nil
}

// mapGroups converts directory groups (names or DNs, case-insensitive) to
// role ids via the mapping, then appends default roles.
func mapGroups(groups []string, mapping map[string]string, defaults []string) []string {
	seen := map[string]bool{}
	var roles []string
	add := func(r string) {
		if r != "" && !seen[r] {
			seen[r] = true
			roles = append(roles, r)
		}
	}
	for _, g := range groups {
		for k, v := range mapping {
			if strings.EqualFold(k, g) {
				add(v)
			}
		}
	}
	for _, r := range defaults {
		add(r)
	}
	return roles
}

// syncExternalUser upserts a user authenticated by an external realm and
// keeps roles in sync with what the realm reported. Roles that do not exist
// locally are dropped (with a log line) rather than failing the login.
func (s *Service) syncExternalUser(ctx context.Context, u *model.User) (*model.User, error) {
	roles, err := s.roleMap(ctx)
	if err != nil {
		return nil, err
	}
	var valid []string
	for _, r := range u.Roles {
		if _, ok := roles[r]; ok {
			valid = append(valid, r)
		} else {
			s.Log.Warn("external realm mapped to unknown role", "user", u.Username, "role", r)
		}
	}
	u.Roles = valid
	existing, err := s.User(ctx, u.Username)
	switch {
	case errors.Is(err, ErrNotFound):
		if err := s.CreateUser(ctx, u); err != nil {
			return nil, err
		}
		return s.User(ctx, u.Username)
	case err != nil:
		return nil, err
	}
	if existing.Source == "local" {
		// A local account with the same name wins; do not let the directory
		// take it over silently.
		return nil, ErrInvalidCreds
	}
	if !existing.Active {
		return nil, ErrInvalidCreds
	}
	existing.Email, existing.DisplayName, existing.Roles = u.Email, u.DisplayName, u.Roles
	if err := s.UpdateUser(ctx, existing); err != nil {
		return nil, err
	}
	return existing, nil
}

// TestLDAP checks connectivity, service bind and optionally a user login.
func (s *Service) TestLDAP(ctx context.Context, cfg LDAPConfig, username, password string) (map[string]any, error) {
	if cfg.BindPassword == "" {
		cur := s.Settings(ctx)
		cfg.BindPassword = cur.LDAP.BindPassword
	}
	cfg.Enabled = true
	conn, err := ldapDial(cfg)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()
	if cfg.BindDN != "" {
		if err := conn.Bind(cfg.BindDN, cfg.BindPassword); err != nil {
			return nil, fmt.Errorf("service bind: %w", err)
		}
	}
	out := map[string]any{"connected": true}
	if username != "" {
		u, err := s.ldapLogin(ctx, cfg, username, password)
		if err != nil {
			return out, fmt.Errorf("user login: %w", err)
		}
		out["user"] = u
	}
	return out, nil
}
