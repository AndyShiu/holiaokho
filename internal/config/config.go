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
	Log      Log      `yaml:"log"`
}

type Server struct {
	// Listen is the main HTTP listen address, e.g. ":8081".
	Listen string `yaml:"listen"`
	// BaseURL is the externally visible URL; used when rewriting
	// URLs in format metadata (npm tarball links, docker token realm).
	// If empty it is derived from the request (Host + X-Forwarded-*).
	BaseURL string `yaml:"base_url"`
	// TrustForwarded honours X-Forwarded-Proto/Host/For from a reverse proxy.
	TrustForwarded bool          `yaml:"trust_forwarded"`
	ReadTimeout    time.Duration `yaml:"read_timeout"`
	WriteTimeout   time.Duration `yaml:"write_timeout"`
	// UIDir optionally overrides the embedded web UI with a directory on disk.
	UIDir string `yaml:"ui_dir"`
}

type Database struct {
	URL string `yaml:"url"`
	// MaxConns is the pgx pool size.
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

type Log struct {
	Level  string `yaml:"level"`  // debug|info|warn|error
	Format string `yaml:"format"` // json|text
}

func Default() Config {
	return Config{
		Server: Server{
			Listen:         ":8081",
			TrustForwarded: true,
			ReadTimeout:    0,
			WriteTimeout:   0,
		},
		Database: Database{
			URL:      "postgres://holiaokho:holiaokho@localhost:5432/holiaokho?sslmode=disable",
			MaxConns: 20,
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
		Log: Log{Level: "info", Format: "text"},
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
	str("UI_DIR", &c.Server.UIDir)
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
	str("LOG_LEVEL", &c.Log.Level)
	str("LOG_FORMAT", &c.Log.Format)
}
