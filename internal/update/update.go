// Package update checks whether a newer release has been published.
//
// It is one GET a day to the GitHub releases API, carrying nothing but a
// User-Agent naming the product and its version. That request still tells
// GitHub that an instance exists at this address, which some organisations
// do not allow, so it is a configuration switch and says so in the example
// configuration.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DefaultURL is the upstream project's latest release.
const DefaultURL = "https://api.github.com/repos/AndyShiu/holiaokho/releases/latest"

// Status is what the UI shows.
type Status struct {
	Enabled     bool       `json:"enabled"`
	Current     string     `json:"current"`
	Latest      string     `json:"latest,omitempty"`
	Available   bool       `json:"available"`
	URL         string     `json:"url,omitempty"` // the release page
	PublishedAt *time.Time `json:"publishedAt,omitempty"`
	CheckedAt   *time.Time `json:"checkedAt,omitempty"`
	Error       string     `json:"error,omitempty"`
}

// stored is what is kept in settings between checks.
type stored struct {
	Latest      string     `json:"latest"`
	URL         string     `json:"url"`
	PublishedAt *time.Time `json:"publishedAt,omitempty"`
	CheckedAt   time.Time  `json:"checkedAt"`
	Error       string     `json:"error,omitempty"`
}

type Checker struct {
	DB      *pgxpool.Pool
	HTTP    *http.Client
	URL     string
	Current string
	Enabled bool
}

// Check asks for the latest release and records the answer. A failure is
// recorded, not returned: an instance with no route to GitHub is normal and
// should not fail a task every day.
func (c *Checker) Check(ctx context.Context) error {
	if !c.Enabled {
		return nil
	}
	s := stored{CheckedAt: time.Now().UTC()}
	if rel, err := c.fetch(ctx); err != nil {
		prev, _ := c.load(ctx)
		s.Latest, s.URL, s.PublishedAt = prev.Latest, prev.URL, prev.PublishedAt
		s.Error = err.Error()
	} else {
		s.Latest, s.URL, s.PublishedAt = rel.version(), rel.HTMLURL, rel.PublishedAt
	}
	raw, _ := json.Marshal(s)
	_, err := c.DB.Exec(ctx, `INSERT INTO settings(key, value) VALUES ('update_check', $1)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, raw)
	return err
}

type release struct {
	TagName     string     `json:"tag_name"`
	HTMLURL     string     `json:"html_url"`
	PublishedAt *time.Time `json:"published_at"`
	Draft       bool       `json:"draft"`
	Prerelease  bool       `json:"prerelease"`
}

func (r release) version() string { return strings.TrimPrefix(r.TagName, "v") }

func (c *Checker) fetch(ctx context.Context) (*release, error) {
	url := c.URL
	if url == "" {
		url = DefaultURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Holiaokho/"+c.Current)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	var rel release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return nil, err
	}
	// "latest" never returns drafts or pre-releases, but a mirror might.
	if rel.Draft || rel.Prerelease || parse(rel.version()) == nil {
		return nil, fmt.Errorf("%s: no usable release (tag %q)", url, rel.TagName)
	}
	return &rel, nil
}

func (c *Checker) load(ctx context.Context) (stored, error) {
	var s stored
	var raw []byte
	err := c.DB.QueryRow(ctx, `SELECT value FROM settings WHERE key = 'update_check'`).Scan(&raw)
	if err == nil {
		err = json.Unmarshal(raw, &s)
	}
	return s, err
}

// Status reports the last check against the running version.
func (c *Checker) Status(ctx context.Context) Status {
	st := Status{Enabled: c.Enabled, Current: c.Current}
	if !c.Enabled {
		return st
	}
	s, err := c.load(ctx)
	if err != nil {
		return st
	}
	st.Latest, st.URL, st.PublishedAt, st.Error = s.Latest, s.URL, s.PublishedAt, s.Error
	st.CheckedAt = &s.CheckedAt
	st.Available = Newer(s.Latest, c.Current)
	return st
}

// Newer reports whether latest is a higher release than current. Anything
// that is not a plain release number on either side — a development build,
// a pre-release — is never "newer", so a developer is not told to upgrade
// away from the branch they are working on.
func Newer(latest, current string) bool {
	l, c := parse(latest), parse(current)
	if l == nil || c == nil {
		return false
	}
	for i := range l {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

func parse(v string) []int {
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) != 3 {
		return nil
	}
	out := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil
		}
		out[i] = n
	}
	return out
}
