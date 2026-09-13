// Package auth implements users, roles, tokens, sessions and request
// authentication. Authorisation decisions are made with Principal.Can.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/holiaokho/holiaokho/internal/db"
	"github.com/holiaokho/holiaokho/internal/model"
)

var (
	ErrNotFound     = errors.New("not found")
	ErrConflict     = errors.New("already exists")
	ErrInvalidCreds = errors.New("invalid credentials")
	ErrRateLimited  = errors.New("too many failed logins")
)

const (
	RoleAdmin     = "admin"
	RoleAnonymous = "anonymous"
	UserAnonymous = "anonymous"
	TokenPrefix   = "hlk_"
	SessionCookie = "holiaokho_session"
)

type Config struct {
	AnonymousEnabled bool
	SessionTTL       time.Duration
	LoginMaxFailures int
	LoginWindow      time.Duration
}

type Service struct {
	DB  *db.DB
	Log *slog.Logger
	Cfg Config

	oidcState

	mu       sync.Mutex
	failures map[string][]time.Time // ip -> failure timestamps
	// roleCache avoids a DB hit per request.
	roles   map[string]*model.Role
	rolesAt time.Time
}

func New(d *db.DB, log *slog.Logger, cfg Config) *Service {
	if cfg.SessionTTL == 0 {
		cfg.SessionTTL = 30 * time.Minute
	}
	if cfg.LoginMaxFailures == 0 {
		cfg.LoginMaxFailures = 5
	}
	if cfg.LoginWindow == 0 {
		cfg.LoginWindow = time.Minute
	}
	return &Service{DB: d, Log: log, Cfg: cfg, failures: map[string][]time.Time{}}
}

// Bootstrap creates default roles and the admin user on an empty database.
func (s *Service) Bootstrap(ctx context.Context, adminPassword string) error {
	defaults := []model.Role{
		{ID: RoleAdmin, Name: "Administrator", Description: "Full access", Privileges: []model.Privilege{{Target: "*", Actions: []string{"*"}}}},
		{ID: RoleAnonymous, Name: "Anonymous", Description: "Read access to all repositories", Privileges: []model.Privilege{
			{Target: "repo:*", Actions: []string{Read}}, {Target: "app:search", Actions: []string{Read}}, {Target: "app:status", Actions: []string{Read}}}},
		{ID: "developer", Name: "Developer", Description: "Read and deploy to all repositories", Privileges: []model.Privilege{
			{Target: "repo:*", Actions: []string{Read, Write}}, {Target: "app:search", Actions: []string{Read}}, {Target: "app:status", Actions: []string{Read}}}},
	}
	for _, r := range defaults {
		privs, _ := json.Marshal(r.Privileges)
		if _, err := s.DB.Pool.Exec(ctx, `INSERT INTO roles(id,name,description,privileges) VALUES ($1,$2,$3,$4) ON CONFLICT (id) DO NOTHING`,
			r.ID, r.Name, r.Description, privs); err != nil {
			return err
		}
	}
	var n int
	if err := s.DB.Pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE username <> $1`, UserAnonymous).Scan(&n); err != nil {
		return err
	}
	if _, err := s.DB.Pool.Exec(ctx, `INSERT INTO users(id,username,display_name,source,active) VALUES ($1,$2,'Anonymous','local',true) ON CONFLICT (username) DO NOTHING`,
		uuid.New(), UserAnonymous); err != nil {
		return err
	}
	if _, err := s.DB.Pool.Exec(ctx, `INSERT INTO user_roles SELECT id, $2 FROM users WHERE username=$1 ON CONFLICT DO NOTHING`, UserAnonymous, RoleAnonymous); err != nil {
		return err
	}
	if n == 0 {
		if err := defaultPasswordPolicy().Validate(adminPassword, "admin"); err != nil {
			s.Log.Warn("bootstrap admin password violates the password policy; change it after first login", "reason", err.Error())
		}
		hash, err := HashPassword(adminPassword)
		if err != nil {
			return err
		}
		u := &model.User{Username: "admin", DisplayName: "Administrator", PasswordHash: hash, Source: "local", Active: true, Roles: []string{RoleAdmin}}
		if err := s.CreateUser(ctx, u); err != nil && !errors.Is(err, ErrConflict) {
			return err
		}
		s.Log.Warn("created default admin user; change the password", "username", "admin")
	}
	return nil
}

// ------------------------------------------------------------------- users

const userCols = `u.id, u.username, u.email, u.display_name, u.password_hash, u.source, u.active, u.created_at,
	coalesce((SELECT array_agg(role_id ORDER BY role_id) FROM user_roles ur WHERE ur.user_id=u.id), '{}')`

func scanUser(row pgx.Row) (*model.User, error) {
	var u model.User
	if err := row.Scan(&u.ID, &u.Username, &u.Email, &u.DisplayName, &u.PasswordHash, &u.Source, &u.Active, &u.CreatedAt, &u.Roles); err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *Service) User(ctx context.Context, username string) (*model.User, error) {
	u, err := scanUser(s.DB.Pool.QueryRow(ctx, `SELECT `+userCols+` FROM users u WHERE u.username=$1`, username))
	if db.IsNoRows(err) {
		return nil, ErrNotFound
	}
	return u, err
}

func (s *Service) ListUsers(ctx context.Context) ([]*model.User, error) {
	rows, err := s.DB.Pool.Query(ctx, `SELECT `+userCols+` FROM users u ORDER BY u.username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, nil
}

func (s *Service) CreateUser(ctx context.Context, u *model.User) error {
	if u.Username == "" {
		return errors.New("username required")
	}
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	if u.Source == "" {
		u.Source = "local"
	}
	return s.DB.Tx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO users(id,username,email,display_name,password_hash,source,active) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			u.ID, u.Username, u.Email, u.DisplayName, u.PasswordHash, u.Source, u.Active)
		if err != nil {
			if strings.Contains(err.Error(), "23505") {
				return ErrConflict
			}
			return err
		}
		return setRolesTx(ctx, tx, u.ID, u.Roles)
	})
}

func (s *Service) UpdateUser(ctx context.Context, u *model.User) error {
	return s.DB.Tx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE users SET email=$2, display_name=$3, active=$4 WHERE username=$1`, u.Username, u.Email, u.DisplayName, u.Active)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE username=$1`, u.Username).Scan(&id); err != nil {
			return err
		}
		return setRolesTx(ctx, tx, id, u.Roles)
	})
}

func setRolesTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, roles []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM user_roles WHERE user_id=$1`, userID); err != nil {
		return err
	}
	for _, r := range roles {
		if _, err := tx.Exec(ctx, `INSERT INTO user_roles(user_id, role_id) VALUES ($1,$2)`, userID, r); err != nil {
			return fmt.Errorf("role %q: %w", r, err)
		}
	}
	return nil
}

// CheckPassword validates pw against the configured policy.
func (s *Service) CheckPassword(ctx context.Context, username, pw string) error {
	return s.Settings(ctx).Password.Validate(pw, username)
}

func (s *Service) SetPassword(ctx context.Context, username, pw string) error {
	hash, err := HashPassword(pw)
	if err != nil {
		return err
	}
	tag, err := s.DB.Pool.Exec(ctx, `UPDATE users SET password_hash=$2 WHERE username=$1`, username, hash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetPasswordHash stores an already-hashed password (Nexus import).
func (s *Service) SetPasswordHash(ctx context.Context, username, hash string) error {
	_, err := s.DB.Pool.Exec(ctx, `UPDATE users SET password_hash=$2 WHERE username=$1`, username, hash)
	return err
}

func (s *Service) DeleteUser(ctx context.Context, username string) error {
	if username == UserAnonymous {
		return errors.New("cannot delete the anonymous user")
	}
	tag, err := s.DB.Pool.Exec(ctx, `DELETE FROM users WHERE username=$1`, username)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ------------------------------------------------------------------- roles

func (s *Service) ListRoles(ctx context.Context) ([]*model.Role, error) {
	rows, err := s.DB.Pool.Query(ctx, `SELECT id,name,description,privileges,created_at FROM roles ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Role
	for rows.Next() {
		var r model.Role
		var privs []byte
		if err := rows.Scan(&r.ID, &r.Name, &r.Description, &privs, &r.CreatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal(privs, &r.Privileges)
		out = append(out, &r)
	}
	return out, nil
}

func (s *Service) Role(ctx context.Context, id string) (*model.Role, error) {
	var r model.Role
	var privs []byte
	err := s.DB.Pool.QueryRow(ctx, `SELECT id,name,description,privileges,created_at FROM roles WHERE id=$1`, id).
		Scan(&r.ID, &r.Name, &r.Description, &privs, &r.CreatedAt)
	if db.IsNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	json.Unmarshal(privs, &r.Privileges)
	return &r, nil
}

func (s *Service) SaveRole(ctx context.Context, r *model.Role, create bool) error {
	if r.ID == "" {
		return errors.New("role id required")
	}
	privs, _ := json.Marshal(r.Privileges)
	if create {
		_, err := s.DB.Pool.Exec(ctx, `INSERT INTO roles(id,name,description,privileges) VALUES ($1,$2,$3,$4)`, r.ID, r.Name, r.Description, privs)
		if err != nil && strings.Contains(err.Error(), "23505") {
			return ErrConflict
		}
		s.invalidateRoles()
		return err
	}
	tag, err := s.DB.Pool.Exec(ctx, `UPDATE roles SET name=$2, description=$3, privileges=$4 WHERE id=$1`, r.ID, r.Name, r.Description, privs)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	s.invalidateRoles()
	return nil
}

func (s *Service) DeleteRole(ctx context.Context, id string) error {
	if id == RoleAdmin || id == RoleAnonymous {
		return errors.New("built-in role cannot be deleted")
	}
	tag, err := s.DB.Pool.Exec(ctx, `DELETE FROM roles WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	s.invalidateRoles()
	return nil
}

func (s *Service) invalidateRoles() {
	s.mu.Lock()
	s.roles = nil
	s.mu.Unlock()
}

func (s *Service) roleMap(ctx context.Context) (map[string]*model.Role, error) {
	s.mu.Lock()
	if s.roles != nil && time.Since(s.rolesAt) < 30*time.Second {
		m := s.roles
		s.mu.Unlock()
		return m, nil
	}
	s.mu.Unlock()
	list, err := s.ListRoles(ctx)
	if err != nil {
		return nil, err
	}
	m := map[string]*model.Role{}
	for _, r := range list {
		m[r.ID] = r
	}
	s.mu.Lock()
	s.roles, s.rolesAt = m, time.Now()
	s.mu.Unlock()
	return m, nil
}

func (s *Service) principalFor(ctx context.Context, u *model.User, via string) (*Principal, error) {
	roles, err := s.roleMap(ctx)
	if err != nil {
		return nil, err
	}
	p := &Principal{Username: u.Username, Roles: u.Roles, Via: via, Anonymous: u.Username == UserAnonymous}
	ids := u.Roles
	if !p.Anonymous {
		// "Default Role" realm: every authenticated user gets these too.
		for _, d := range s.Settings(ctx).DefaultRoles {
			ids = append(ids, d)
		}
	}
	for _, rid := range ids {
		if r, ok := roles[rid]; ok {
			p.Privileges = append(p.Privileges, r.Privileges...)
		}
	}
	return p, nil
}

// Anonymous returns the anonymous principal (or an empty one when disabled).
func (s *Service) Anonymous(ctx context.Context) *Principal {
	if !s.Cfg.AnonymousEnabled {
		return &Principal{Username: UserAnonymous, Anonymous: true, Via: "anonymous"}
	}
	u, err := s.User(ctx, UserAnonymous)
	if err != nil {
		return &Principal{Username: UserAnonymous, Anonymous: true, Via: "anonymous"}
	}
	p, err := s.principalFor(ctx, u, "anonymous")
	if err != nil {
		return &Principal{Username: UserAnonymous, Anonymous: true, Via: "anonymous"}
	}
	return p
}

// ------------------------------------------------------------------ tokens

// CreateToken issues a new user token; the secret is only returned once.
func (s *Service) CreateToken(ctx context.Context, username, name string, expires *time.Time) (secret string, t *model.Token, err error) {
	u, err := s.User(ctx, username)
	if err != nil {
		return "", nil, err
	}
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	body := base64.RawURLEncoding.EncodeToString(raw)
	secret = TokenPrefix + body
	prefix := secret[:12]
	t = &model.Token{ID: uuid.New(), UserID: u.ID, Name: name, Prefix: prefix, ExpiresAt: expires}
	_, err = s.DB.Pool.Exec(ctx, `INSERT INTO tokens(id,user_id,name,prefix,hash,expires_at) VALUES ($1,$2,$3,$4,$5,$6)`,
		t.ID, t.UserID, t.Name, t.Prefix, hashToken(secret), t.ExpiresAt)
	if err != nil {
		return "", nil, err
	}
	t.CreatedAt = time.Now()
	return secret, t, nil
}

func hashToken(secret string) string {
	h := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(h[:])
}

func (s *Service) ListTokens(ctx context.Context, username string) ([]*model.Token, error) {
	rows, err := s.DB.Pool.Query(ctx, `SELECT t.id,t.user_id,t.name,t.prefix,t.created_at,t.expires_at,t.last_used_at FROM tokens t JOIN users u ON u.id=t.user_id WHERE u.username=$1 ORDER BY t.created_at`, username)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Token
	for rows.Next() {
		var t model.Token
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.Prefix, &t.CreatedAt, &t.ExpiresAt, &t.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, &t)
	}
	return out, nil
}

func (s *Service) DeleteToken(ctx context.Context, username string, id uuid.UUID) error {
	tag, err := s.DB.Pool.Exec(ctx, `DELETE FROM tokens t USING users u WHERE t.user_id=u.id AND u.username=$1 AND t.id=$2`, username, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) userByToken(ctx context.Context, secret string) (*model.User, error) {
	var username string
	err := s.DB.Pool.QueryRow(ctx, `UPDATE tokens t SET last_used_at=now() FROM users u WHERE t.user_id=u.id AND t.hash=$1 AND (t.expires_at IS NULL OR t.expires_at > now()) AND u.active RETURNING u.username`, hashToken(secret)).Scan(&username)
	if db.IsNoRows(err) {
		return nil, ErrInvalidCreds
	}
	if err != nil {
		return nil, err
	}
	return s.User(ctx, username)
}

// ---------------------------------------------------------------- sessions

func (s *Service) CreateSession(ctx context.Context, username string) (string, time.Time, error) {
	u, err := s.User(ctx, username)
	if err != nil {
		return "", time.Time{}, err
	}
	raw := make([]byte, 32)
	rand.Read(raw)
	id := base64.RawURLEncoding.EncodeToString(raw)
	exp := time.Now().Add(s.Cfg.SessionTTL)
	_, err = s.DB.Pool.Exec(ctx, `INSERT INTO sessions(id,user_id,expires_at) VALUES ($1,$2,$3)`, id, u.ID, exp)
	return id, exp, err
}

func (s *Service) DeleteSession(ctx context.Context, id string) {
	s.DB.Pool.Exec(ctx, `DELETE FROM sessions WHERE id=$1`, id)
}

func (s *Service) userBySession(ctx context.Context, id string) (*model.User, error) {
	var username string
	err := s.DB.Pool.QueryRow(ctx, `UPDATE sessions s SET expires_at=$2 FROM users u WHERE s.user_id=u.id AND s.id=$1 AND s.expires_at > now() AND u.active RETURNING u.username`,
		id, time.Now().Add(s.Cfg.SessionTTL)).Scan(&username)
	if db.IsNoRows(err) {
		return nil, ErrInvalidCreds
	}
	if err != nil {
		return nil, err
	}
	return s.User(ctx, username)
}

// ---------------------------------------------------------- authentication

// Login verifies username/password (or username/token) with rate limiting
// keyed by client IP.
func (s *Service) Login(ctx context.Context, ip, username, password string) (*Principal, error) {
	if s.rateLimited(ip) {
		return nil, ErrRateLimited
	}
	p, err := s.authenticate(ctx, username, password)
	if errors.Is(err, ErrInvalidCreds) {
		s.recordFailure(ip)
	}
	return p, err
}

func (s *Service) authenticate(ctx context.Context, username, password string) (*Principal, error) {
	if strings.HasPrefix(password, TokenPrefix) {
		u, err := s.userByToken(ctx, password)
		if err != nil {
			return nil, err
		}
		if username != "" && username != u.Username {
			return nil, ErrInvalidCreds
		}
		return s.principalFor(ctx, u, "token")
	}
	if username == UserAnonymous {
		return nil, ErrInvalidCreds
	}
	settings := s.Settings(ctx)
	for _, realm := range settings.Realms {
		switch realm {
		case "local":
			p, err := s.localLogin(ctx, username, password)
			if err == nil {
				return p, nil
			}
			if !errors.Is(err, ErrInvalidCreds) {
				return nil, err
			}
		case "ldap":
			if !settings.LDAP.Enabled {
				continue
			}
			u, err := s.ldapLogin(ctx, settings.LDAP, username, password)
			if err == nil {
				return s.principalFor(ctx, u, "ldap")
			}
			if !errors.Is(err, ErrInvalidCreds) {
				s.Log.Warn("ldap login error", "user", username, "err", err)
			}
		}
	}
	return nil, ErrInvalidCreds
}

func (s *Service) localLogin(ctx context.Context, username, password string) (*Principal, error) {
	u, err := s.User(ctx, username)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			// Burn time comparably to a real verification.
			VerifyPassword("$argon2id$v=19$m=65536,t=2,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", password)
			return nil, ErrInvalidCreds
		}
		return nil, err
	}
	if !u.Active || u.Source != "local" {
		return nil, ErrInvalidCreds
	}
	ok, rehash, err := VerifyPassword(u.PasswordHash, password)
	if err != nil || !ok {
		return nil, ErrInvalidCreds
	}
	if rehash {
		if err := s.SetPassword(ctx, username, password); err != nil {
			s.Log.Warn("rehash password", "user", username, "err", err)
		}
	}
	return s.principalFor(ctx, u, "basic")
}

func (s *Service) rateLimited(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	cut := time.Now().Add(-s.Cfg.LoginWindow)
	var keep []time.Time
	for _, t := range s.failures[ip] {
		if t.After(cut) {
			keep = append(keep, t)
		}
	}
	s.failures[ip] = keep
	return len(keep) >= s.Cfg.LoginMaxFailures
}

func (s *Service) recordFailure(ip string) {
	s.mu.Lock()
	s.failures[ip] = append(s.failures[ip], time.Now())
	s.mu.Unlock()
}

// FromRequest resolves the principal for an HTTP request. It never returns
// nil: unauthenticated requests get the anonymous principal. The bool
// reports whether credentials were presented (so callers can distinguish
// "no credentials" from "bad credentials").
func (s *Service) FromRequest(r *http.Request) (*Principal, bool, error) {
	ctx := r.Context()
	ip := ClientIP(r)
	if h := r.Header.Get("Authorization"); h != "" {
		switch {
		case strings.HasPrefix(h, "Basic "):
			user, pass, ok := r.BasicAuth()
			if !ok {
				return s.Anonymous(ctx), true, ErrInvalidCreds
			}
			p, err := s.Login(ctx, ip, user, pass)
			if err != nil {
				return s.Anonymous(ctx), true, err
			}
			return p, true, nil
		case strings.HasPrefix(h, "Bearer "):
			tok := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
			if strings.HasPrefix(tok, TokenPrefix) {
				p, err := s.Login(ctx, ip, "", tok)
				if err != nil {
					return s.Anonymous(ctx), true, err
				}
				return p, true, nil
			}
			// Other bearer tokens (docker) are validated by the format handler.
			return s.Anonymous(ctx), false, nil
		}
	}
	if p, ok := s.rutAuth(r); ok {
		return p, true, nil
	}
	if c, err := r.Cookie(SessionCookie); err == nil && c.Value != "" {
		u, err := s.userBySession(ctx, c.Value)
		if err == nil {
			p, err := s.principalFor(ctx, u, "session")
			if err == nil {
				return p, true, nil
			}
		}
	}
	return s.Anonymous(ctx), false, nil
}

// rutAuth trusts a username header set by a reverse proxy (Nexus "Rut Auth").
func (s *Service) rutAuth(r *http.Request) (*Principal, bool) {
	cfg := s.Settings(r.Context()).Rut
	if !cfg.Enabled || cfg.Header == "" {
		return nil, false
	}
	username := strings.TrimSpace(r.Header.Get(cfg.Header))
	if username == "" || username == UserAnonymous {
		return nil, false
	}
	if len(cfg.TrustedProxies) == 0 {
		// Never accept an identity header from arbitrary peers.
		s.Log.Warn("rut auth enabled without trustedProxies; ignoring header")
		return nil, false
	}
	{
		host := remoteIP(r)
		ip := net.ParseIP(host)
		trusted := false
		for _, c := range cfg.TrustedProxies {
			if _, n, err := net.ParseCIDR(c); err == nil && ip != nil && n.Contains(ip) {
				trusted = true
				break
			} else if c == host {
				trusted = true
				break
			}
		}
		if !trusted {
			s.Log.Warn("rut auth header from untrusted address", "remote", r.RemoteAddr)
			return nil, false
		}
	}
	u, err := s.User(r.Context(), username)
	if errors.Is(err, ErrNotFound) {
		if !cfg.AutoCreate {
			return nil, false
		}
		nu := &model.User{Username: username, Source: "rut", Active: true, Roles: cfg.DefaultRoles}
		if u, err = s.syncExternalUser(r.Context(), nu); err != nil {
			return nil, false
		}
	} else if err != nil || !u.Active {
		return nil, false
	}
	p, err := s.principalFor(r.Context(), u, "rut")
	if err != nil {
		return nil, false
	}
	return p, true
}

// TrustedProxies is the set of networks whose X-Forwarded-For is trusted;
// set by the server from configuration.
var TrustedProxies []*net.IPNet

// SetTrustedProxies parses CIDRs (or single IPs) into TrustedProxies.
func SetTrustedProxies(cidrs []string) error {
	var out []*net.IPNet
	for _, c := range cidrs {
		if !strings.Contains(c, "/") {
			if strings.Contains(c, ":") {
				c += "/128"
			} else {
				c += "/32"
			}
		}
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			return fmt.Errorf("trusted proxy %q: %w", c, err)
		}
		out = append(out, n)
	}
	TrustedProxies = out
	return nil
}

func remoteIP(r *http.Request) string {
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.Trim(host, "[]")
}

// ClientIP returns the client address. X-Forwarded-For is only believed
// when the direct peer is a trusted proxy; the rightmost untrusted hop wins,
// so clients cannot spoof their address past the proxy.
func ClientIP(r *http.Request) string {
	peer := remoteIP(r)
	ip := net.ParseIP(peer)
	if ip == nil || !inTrusted(ip) {
		return peer
	}
	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return peer
	}
	hops := strings.Split(xff, ",")
	for i := len(hops) - 1; i >= 0; i-- {
		h := strings.TrimSpace(hops[i])
		hip := net.ParseIP(h)
		if hip == nil {
			return peer
		}
		if !inTrusted(hip) {
			return h
		}
	}
	return strings.TrimSpace(hops[0])
}

func inTrusted(ip net.IP) bool {
	for _, n := range TrustedProxies {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

type ctxKey struct{}

func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

func PrincipalFrom(ctx context.Context) *Principal {
	p, _ := ctx.Value(ctxKey{}).(*Principal)
	return p
}

// PrincipalFor resolves a principal for a known username (docker tokens).
func (s *Service) PrincipalFor(ctx context.Context, username string) (*Principal, error) {
	if username == "" || username == UserAnonymous {
		return s.Anonymous(ctx), nil
	}
	u, err := s.User(ctx, username)
	if err != nil {
		return nil, err
	}
	if !u.Active {
		return nil, ErrInvalidCreds
	}
	return s.principalFor(ctx, u, "token")
}
