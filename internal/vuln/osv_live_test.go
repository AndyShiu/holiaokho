package vuln

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"testing"
	"time"
)

// Asks the real OSV about one package known to be vulnerable in every
// covered format, and fails if any comes back clean.
//
// A wrong purl is not an error to OSV — it answers "no vulnerabilities" — so
// a mapping that drifts out of step with OSV would make every package of
// that format look safe. This is the check that notices.
//
//	HOLIAOKHO_TEST_OSV=1 go test ./internal/vuln/ -run Live
func TestLiveEveryCoveredFormatFindsAKnownVulnerability(t *testing.T) {
	if os.Getenv("HOLIAOKHO_TEST_OSV") == "" {
		t.Skip("HOLIAOKHO_TEST_OSV not set")
	}
	known := map[string]struct {
		ns, name, ver string
		attrs         json.RawMessage
	}{
		"maven":    {"org.apache.logging.log4j", "log4j-core", "2.14.1", nil},
		"npm":      {"@babel", "traverse", "7.22.0", nil},
		"pypi":     {"", "django", "3.2.0", nil},
		"go":       {"golang.org/x", "text", "v0.3.5", nil},
		"nuget":    {"", "newtonsoft.json", "12.0.1", json.RawMessage(`{"canonicalId":"Newtonsoft.Json"}`)},
		"rubygems": {"", "rack", "2.0.0", nil},
		"cargo":    {"", "tokio", "1.0.0", nil},
		"composer": {"laravel", "framework", "8.0.0", nil},
		"pub":      {"", "http", "0.12.0", nil},
		"r":        {"src/contrib", "commonmark", "1.8.0", nil},
	}
	formats := CoveredFormats()
	sort.Strings(formats)
	var purls []string
	for _, f := range formats {
		k, ok := known[f]
		if !ok {
			t.Fatalf("format %s is covered but has no known-vulnerable package in this test", f)
		}
		p, ok := PURL(f, k.ns, k.name, k.ver, k.attrs)
		if !ok {
			t.Fatalf("%s: no purl", f)
		}
		purls = append(purls, p)
	}
	o := &OSV{BaseURL: "https://api.osv.dev", HTTP: &http.Client{Timeout: time.Minute}}
	got, err := o.QueryBatch(context.Background(), purls)
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range purls {
		if len(got[i]) == 0 {
			t.Errorf("%s: OSV found nothing — the purl for %s no longer matches", p, formats[i])
		} else {
			t.Logf("%-3d %s", len(got[i]), p)
		}
	}
}
