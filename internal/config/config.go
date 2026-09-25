// Package config loads server configuration from a YAML file with
// environment-variable overrides (HOLIAOKHO_* keys).
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   Server   `yaml:"server"`
	Database Database `yaml:"database"`
	Storage  Storage  `yaml:"storage"`
	Auth     Auth     `yaml:"auth"`
	Proxy    Proxy    `yaml:"proxy"`
	Backup   Backup   `yaml:"backup"`
	Secrets  Secrets  `yaml:"secrets"`
	Log      Log      `yaml:"log"`
	Vulns    Vulns    `yaml:"vulnerabilities"`
	Updates  Updates  `yaml:"updates"`
}

type Server struct {
	// Listen is the main HTTP listen address, e.g. ":8081".
	Listen string `yaml:"listen"`
	// BaseURL is the externally visible URL; used when rewriting
	// URLs in format metadata (npm tarball links, docker token realm).
	// If empty it is derived from the request (Host + X-Forwarded-*).
	BaseURL string `yaml:"base_url"`
	// TrustForwarded honours X-Forwarded-Proto/Host/For from a reverse proxy.
	TrustForwarded bool `yaml:"trust_forwarded"`
	// TrustedProxies are CIDRs whose X-Forwarded-For is believed for client
	// IP (login rate limiting, audit). Default: private ranges + loopback.
	TrustedProxies []string      `yaml:"trusted_proxies"`
	ReadTimeout    time.Duration `yaml:"read_timeout"`
	WriteTimeout   time.Duration `yaml:"write_timeout"`
	// UIDir optionally overrides the embedded web UI with a directory on disk.
	UIDir string `yaml:"ui_dir"`
	// TLSCert/TLSKey (PEM paths) serve the main listener over HTTPS.
	TLSCert string `yaml:"tls_cert"`
	TLSKey  string `yaml:"tls_key"`
	// TLSListen is an additional HTTPS listener while Listen stays HTTP
	// (e.g. ":8443" next to ":8081"). Ignored when empty.
	TLSListen string `yaml:"tls_listen"`
}

type Database struct {
	URL string `yaml:"url"`
	// MaxConns is the pgx pool size. A CI burst fetching hundreds of
	// artifacts saturated a pool of 20, so the default leaves headroom;
	// keep it well under the server's own max_connections.
	MaxConns int32 `yaml:"max_conns"`
}

type Storage struct {
	// Type is "fs" or "s3". Additional named stores can be created via the API.
	Type string `yaml:"type"`
	Path string `yaml:"path"`
	S3   S3     `yaml:"s3"`
}

type S3 struct {
	Endpoint  string `yaml:"endpoint"`
	Region    string `yaml:"region"`
	Bucket    string `yaml:"bucket"`
	Prefix    string `yaml:"prefix"`
	AccessKey string `yaml:"access_key"`
	SecretKey string `yaml:"secret_key"`
	PathStyle bool   `yaml:"path_style"`
}

type Auth struct {
	// AdminPassword bootstraps the admin user on first start.
	AdminPassword string `yaml:"admin_password"`
	// AnonymousRead grants unauthenticated read access to all repositories
	// unless overridden per role. Mirrors Nexus "anonymous access".
	AnonymousEnabled bool          `yaml:"anonymous_enabled"`
	SessionTTL       time.Duration `yaml:"session_ttl"`
	// LoginRateLimit: after N failures from an IP within Window, respond 429.
	LoginMaxFailures int           `yaml:"login_max_failures"`
	LoginWindow      time.Duration `yaml:"login_window"`
}

type Proxy struct {
	// Outbound HTTP proxy for upstream fetches, e.g. "http://proxy:3128".
	HTTPProxy      string        `yaml:"http_proxy"`
	NoProxy        string        `yaml:"no_proxy"`
	UserAgent      string        `yaml:"user_agent"`
	ConnectTimeout time.Duration `yaml:"connect_timeout"`
	Timeout        time.Duration `yaml:"timeout"`
	// CACertFile is a PEM bundle appended to the system trust store.
	CACertFile string `yaml:"ca_cert_file"`
}

// Secrets controls encryption of stored credentials (upstream passwords,
// signing keys, LDAP/OIDC/SMTP secrets, S3 keys).
type Secrets struct {
	// Key is the current key (base64 32 bytes, hex, or any passphrase).
	// Empty: read/create KeyFile.
	Key     string `yaml:"key"`
	KeyFile string `yaml:"key_file"`
	// PreviousKeys still decrypt old values after a rotation; run the
	// re-encrypt-secrets task, then remove them.
	PreviousKeys []string `yaml:"previous_keys"`
}

type Backup struct {
	// Dir enables the scheduled backup task, writing archives here.
	Dir       string `yaml:"dir"`
	WithBlobs bool   `yaml:"with_blobs"`
	Keep      int    `yaml:"keep"`
}

// Vulns configures scanning stored packages for known vulnerabilities.
type Vulns struct {
	// Enabled turns the scan task on. Individual repositories can still be
	// excluded from their own settings.
	Enabled bool `yaml:"enabled"`
	// OSVURL is the OSV API to query; point it at a mirror when the server
	// cannot reach api.osv.dev directly.
	OSVURL string `yaml:"osv_url"`
	// NotifyMinSeverity is the lowest severity worth an email or webhook
	// when newly found: CRITICAL, HIGH, MODERATE or LOW.
	NotifyMinSeverity string `yaml:"notify_min_severity"`
}

// Updates configures checking for a newer release.
type Updates struct {
	// Check asks GitHub once a day whether a newer release exists, and tells
	// administrators in the UI. The request carries only a User-Agent with
	// the version, but it does tell GitHub this instance exists; turn it off
	// where that is not wanted, or where there is no route out anyway.
	Check bool `yaml:"check"`
	// URL is the releases API to ask; for forks and mirrors.
	URL string `yaml:"url"`
}

type Log struct {
	Level  string `yaml:"level"`  // debug|info|warn|error
	Format string `yaml:"format"` // json|text
}

func Default() Config {
	return Config{
		Server: Server{
			Listen:         ":8081",
			TrustForwarded: true,
			TrustedProxies: []string{"127.0.0.0/8", "::1/128", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"},
			ReadTimeout:    0,
			WriteTimeout:   0,
		},
		Database: Database{
			URL:      "postgres://holiaokho:holiaokho@localhost:5432/holiaokho?sslmode=disable",
			MaxConns: 50,
		},
		Storage: Storage{Type: "fs", Path: "./data/blobs"},
		Auth: Auth{
			AdminPassword:    "admin123",
			AnonymousEnabled: true,
			SessionTTL:       30 * time.Minute,
			LoginMaxFailures: 10,
			LoginWindow:      time.Minute,
		},
		Proxy: Proxy{
			UserAgent:      "Holiaokho/0.1",
			ConnectTimeout: 20 * time.Second,
			Timeout:        10 * time.Minute,
		},
		Backup:  Backup{Keep: 7},
		Secrets: Secrets{KeyFile: "./data/secret.key"},
		Log:     Log{Level: "info", Format: "text"},
		Vulns:   Vulns{Enabled: true, OSVURL: "https://api.osv.dev", NotifyMinSeverity: "HIGH"},
		Updates: Updates{Check: true, URL: "https://api.github.com/repos/AndyShiu/holiaokho/releases/latest"},
	}
}

// Load reads the YAML file (if path != "") then applies env overrides.
func Load(path string) (Config, error) {
	cfg := Default()
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return cfg, fmt.Errorf("read config: %w", err)
		}
		if err := yaml.Unmarshal(b, &cfg); err != nil {
			return cfg, fmt.Errorf("parse config: %w", err)
		}
	}
	applyEnv(&cfg)
	return cfg, nil
}

func applyEnv(c *Config) {
	str := func(key string, dst *string) {
		if v, ok := os.LookupEnv("HOLIAOKHO_" + key); ok {
			*dst = v
		}
	}
	boolean := func(key string, dst *bool) {
		if v, ok := os.LookupEnv("HOLIAOKHO_" + key); ok {
			*dst = strings.EqualFold(v, "true") || v == "1"
		}
	}
	dur := func(key string, dst *time.Duration) {
		if v, ok := os.LookupEnv("HOLIAOKHO_" + key); ok {
			if d, err := time.ParseDuration(v); err == nil {
				*dst = d
			}
		}
	}
	str("LISTEN", &c.Server.Listen)
	str("BASE_URL", &c.Server.BaseURL)
	boolean("TRUST_FORWARDED", &c.Server.TrustForwarded)
	if v, ok := os.LookupEnv("HOLIAOKHO_TRUSTED_PROXIES"); ok {
		c.Server.TrustedProxies = nil
		for _, x := range strings.Split(v, ",") {
			if x = strings.TrimSpace(x); x != "" {
				c.Server.TrustedProxies = append(c.Server.TrustedProxies, x)
			}
		}
	}
	str("UI_DIR", &c.Server.UIDir)
	str("TLS_CERT", &c.Server.TLSCert)
	str("TLS_KEY", &c.Server.TLSKey)
	str("TLS_LISTEN", &c.Server.TLSListen)
	str("DATABASE_URL", &c.Database.URL)
	if v, ok := os.LookupEnv("HOLIAOKHO_DATABASE_MAX_CONNS"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			c.Database.MaxConns = int32(n)
		}
	}
	str("STORAGE_TYPE", &c.Storage.Type)
	str("STORAGE_PATH", &c.Storage.Path)
	str("S3_ENDPOINT", &c.Storage.S3.Endpoint)
	str("S3_REGION", &c.Storage.S3.Region)
	str("S3_BUCKET", &c.Storage.S3.Bucket)
	str("S3_PREFIX", &c.Storage.S3.Prefix)
	str("S3_ACCESS_KEY", &c.Storage.S3.AccessKey)
	str("S3_SECRET_KEY", &c.Storage.S3.SecretKey)
	boolean("S3_PATH_STYLE", &c.Storage.S3.PathStyle)
	str("ADMIN_PASSWORD", &c.Auth.AdminPassword)
	boolean("ANONYMOUS_ENABLED", &c.Auth.AnonymousEnabled)
	dur("SESSION_TTL", &c.Auth.SessionTTL)
	str("HTTP_PROXY", &c.Proxy.HTTPProxy)
	str("NO_PROXY", &c.Proxy.NoProxy)
	str("CA_CERT_FILE", &c.Proxy.CACertFile)
	str("SECRET_KEY", &c.Secrets.Key)
	str("SECRET_KEY_FILE", &c.Secrets.KeyFile)
	if v, ok := os.LookupEnv("HOLIAOKHO_SECRET_PREVIOUS_KEYS"); ok && v != "" {
		c.Secrets.PreviousKeys = strings.Split(v, ",")
	}
	str("BACKUP_DIR", &c.Backup.Dir)
	boolean("BACKUP_WITH_BLOBS", &c.Backup.WithBlobs)
	str("LOG_LEVEL", &c.Log.Level)
	str("LOG_FORMAT", &c.Log.Format)
	boolean("VULNERABILITIES_ENABLED", &c.Vulns.Enabled)
	str("VULNERABILITIES_OSV_URL", &c.Vulns.OSVURL)
	str("VULNERABILITIES_NOTIFY_MIN_SEVERITY", &c.Vulns.NotifyMinSeverity)
	boolean("UPDATES_CHECK", &c.Updates.Check)
	str("UPDATES_URL", &c.Updates.URL)
}
