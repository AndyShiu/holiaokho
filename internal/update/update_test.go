package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"1.2.1", "1.2.0", true},
		{"1.10.0", "1.9.9", true}, // numeric, not string, order
		{"2.0.0", "1.99.99", true},
		{"v1.3.0", "1.2.0", true},
		{"1.2.0", "1.2.0", false},
		{"1.1.9", "1.2.0", false},
		{"1.3.0", "dev", false},       // a development build is never told to upgrade
		{"1.3.0", "scan", false},      // the CI scan build
		{"1.3.0-rc1", "1.2.0", false}, // not a release
		{"", "1.2.0", false},
	}
	for _, c := range cases {
		if got := Newer(c.latest, c.current); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
}

func TestFetchSendsOnlyAUserAgentAndRejectsPrereleases(t *testing.T) {
	var ua, cookie, auth string
	body := `{"tag_name":"v1.3.0","html_url":"https://github.com/x/y/releases/tag/v1.3.0","published_at":"2026-10-01T00:00:00Z"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua, cookie, auth = r.Header.Get("User-Agent"), r.Header.Get("Cookie"), r.Header.Get("Authorization")
		w.Write([]byte(body))
	}))
	defer srv.Close()
	c := &Checker{HTTP: srv.Client(), URL: srv.URL, Current: "1.2.0", Enabled: true}
	rel, err := c.fetch(context.Background())
	if err != nil || rel.version() != "1.3.0" {
		t.Fatalf("got %+v, %v", rel, err)
	}
	if ua != "Holiaokho/1.2.0" || cookie != "" || auth != "" {
		t.Fatalf("request carried more than it should: ua=%q cookie=%q auth=%q", ua, cookie, auth)
	}

	body = `{"tag_name":"v1.4.0-rc1","prerelease":true}`
	if _, err := c.fetch(context.Background()); err == nil {
		t.Fatal("a pre-release was accepted")
	}
}
