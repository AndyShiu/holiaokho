// Package notify delivers events to webhooks and e-mail. Deliveries are
// asynchronous with a bounded queue; failures are logged, never block callers.
package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/smtp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/holiaokho/holiaokho/internal/secrets"
)

type Webhook struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	URL        string    `json:"url"`
	Secret     string    `json:"secret,omitempty"`
	Events     []string  `json:"events"`
	Repository string    `json:"repository"`
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"createdAt"`
}

// Event is what gets POSTed: {"event":"asset.created","at":...,"repository":"x","data":{...}}.
type Event struct {
	Event      string    `json:"event"`
	At         time.Time `json:"at"`
	Repository string    `json:"repository,omitempty"`
	Actor      string    `json:"actor,omitempty"`
	Data       any       `json:"data,omitempty"`
}

type Email struct {
	Enabled  bool   `json:"enabled"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
	From     string `json:"from"`
	StartTLS bool   `json:"startTls"`
	SSL      bool   `json:"ssl"`
	// Recipients for system notifications (task failures, quota warnings).
	Recipients []string `json:"recipients"`
}

type Service struct {
	Pool *pgxpool.Pool
	Log  *slog.Logger

	queue  chan job
	client *http.Client
	mu     sync.RWMutex
	hooks  []Webhook
	at     time.Time
}

type job struct {
	hook Webhook
	body []byte
}

func New(pool *pgxpool.Pool, log *slog.Logger) *Service {
	s := &Service{Pool: pool, Log: log, queue: make(chan job, 1000), client: &http.Client{Timeout: 15 * time.Second}}
	for i := 0; i < 4; i++ {
		go s.worker()
	}
	return s
}

// ---------------------------------------------------------------- webhooks

func (s *Service) ListWebhooks(ctx context.Context) ([]Webhook, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id, name, url, secret, events, repository, enabled, created_at FROM webhooks ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Webhook
	for rows.Next() {
		var w Webhook
		var ev []byte
		if err := rows.Scan(&w.ID, &w.Name, &w.URL, &w.Secret, &ev, &w.Repository, &w.Enabled, &w.CreatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal(ev, &w.Events)
		out = append(out, w)
	}
	return out, nil
}

func (s *Service) SaveWebhook(ctx context.Context, w *Webhook, create bool) error {
	if w.Name == "" || !strings.HasPrefix(w.URL, "http") {
		return errors.New("name and http(s) url required")
	}
	if w.Events == nil {
		w.Events = []string{}
	}
	ev, _ := json.Marshal(w.Events)
	defer s.invalidate()
	if create {
		w.ID = uuid.New()
		_, err := s.Pool.Exec(ctx, `INSERT INTO webhooks(id,name,url,secret,events,repository,enabled) VALUES ($1,$2,$3,$4,$5,$6,$7)`, w.ID, w.Name, w.URL, w.Secret, ev, w.Repository, w.Enabled)
		return err
	}
	_, err := s.Pool.Exec(ctx, `UPDATE webhooks SET name=$2,url=$3,secret=$4,events=$5,repository=$6,enabled=$7 WHERE id=$1`, w.ID, w.Name, w.URL, w.Secret, ev, w.Repository, w.Enabled)
	return err
}

func (s *Service) DeleteWebhook(ctx context.Context, id uuid.UUID) error {
	defer s.invalidate()
	_, err := s.Pool.Exec(ctx, `DELETE FROM webhooks WHERE id=$1`, id)
	return err
}

func (s *Service) invalidate() {
	s.mu.Lock()
	s.at = time.Time{}
	s.mu.Unlock()
}

func (s *Service) hooksCached() []Webhook {
	s.mu.RLock()
	if time.Since(s.at) < 30*time.Second {
		h := s.hooks
		s.mu.RUnlock()
		return h
	}
	s.mu.RUnlock()
	hooks, err := s.ListWebhooks(context.Background())
	if err != nil {
		return nil
	}
	s.mu.Lock()
	s.hooks, s.at = hooks, time.Now()
	s.mu.Unlock()
	return hooks
}

// Emit fans an event out to every matching enabled webhook.
func (s *Service) Emit(ev Event) {
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	}
	hooks := s.hooksCached()
	if len(hooks) == 0 {
		return
	}
	body, _ := json.Marshal(ev)
	for _, h := range hooks {
		if !h.Enabled || !matches(h, ev) {
			continue
		}
		select {
		case s.queue <- job{hook: h, body: body}:
		default:
			s.Log.Warn("webhook queue full, dropping event", "hook", h.Name, "event", ev.Event)
		}
	}
}

func matches(h Webhook, ev Event) bool {
	if h.Repository != "" && h.Repository != ev.Repository {
		return false
	}
	if len(h.Events) == 0 {
		return true
	}
	for _, e := range h.Events {
		if e == "*" || e == ev.Event || (strings.HasSuffix(e, ".*") && strings.HasPrefix(ev.Event, strings.TrimSuffix(e, "*"))) {
			return true
		}
	}
	return false
}

func (s *Service) worker() {
	for j := range s.queue {
		for attempt := 1; attempt <= 3; attempt++ {
			if err := s.deliver(j); err == nil {
				break
			} else if attempt == 3 {
				s.Log.Warn("webhook delivery failed", "hook", j.hook.Name, "url", j.hook.URL, "err", err)
			} else {
				time.Sleep(time.Duration(attempt*attempt) * time.Second)
			}
		}
	}
}

func (s *Service) deliver(j job) error {
	req, err := http.NewRequest(http.MethodPost, j.hook.URL, bytes.NewReader(j.body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Holiaokho-Webhook/1.0")
	req.Header.Set("X-Holiaokho-Event", "true")
	if j.hook.Secret != "" {
		mac := hmac.New(sha256.New, []byte(j.hook.Secret))
		mac.Write(j.body)
		req.Header.Set("X-Holiaokho-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

// ------------------------------------------------------------------- email

func (s *Service) EmailConfig(ctx context.Context) (Email, error) {
	var e Email
	var raw []byte
	err := s.Pool.QueryRow(ctx, `SELECT value FROM settings WHERE key='email'`).Scan(&raw)
	if err != nil {
		return e, nil // not configured
	}
	json.Unmarshal(raw, &e)
	if pw, err := secrets.Decrypt(e.Password); err == nil {
		e.Password = pw
	} else {
		return e, err
	}
	return e, nil
}

func (s *Service) SaveEmailConfig(ctx context.Context, e Email) error {
	var err error
	if e.Password, err = secrets.Encrypt(e.Password); err != nil {
		return err
	}
	raw, _ := json.Marshal(e)
	_, err = s.Pool.Exec(ctx, `INSERT INTO settings(key,value) VALUES ('email',$1) ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, raw)
	return err
}

// SendMail sends a plain-text message using the stored configuration.
func (s *Service) SendMail(ctx context.Context, to []string, subject, body string) error {
	cfg, err := s.EmailConfig(ctx)
	if err != nil {
		return err
	}
	if !cfg.Enabled || cfg.Host == "" {
		return errors.New("email is not configured")
	}
	if len(to) == 0 {
		to = cfg.Recipients
	}
	if len(to) == 0 {
		return errors.New("no recipients")
	}
	port := cfg.Port
	if port == 0 {
		port = 25
	}
	addr := fmt.Sprintf("%s:%d", cfg.Host, port)
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nDate: %s\r\n\r\n%s",
		cfg.From, strings.Join(to, ", "), subject, time.Now().Format(time.RFC1123Z), body)
	var conn net.Conn
	d := net.Dialer{Timeout: 15 * time.Second}
	if cfg.SSL {
		conn, err = tls.DialWithDialer(&d, "tcp", addr, &tls.Config{ServerName: cfg.Host})
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return err
	}
	c, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return err
	}
	defer c.Close()
	if cfg.StartTLS && !cfg.SSL {
		if err := c.StartTLS(&tls.Config{ServerName: cfg.Host}); err != nil {
			return err
		}
	}
	if cfg.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)); err != nil {
			return err
		}
	}
	if err := c.Mail(cfg.From); err != nil {
		return err
	}
	for _, r := range to {
		if err := c.Rcpt(r); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}
