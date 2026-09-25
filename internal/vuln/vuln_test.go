package vuln

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPURL(t *testing.T) {
	nuspec := json.RawMessage(`{"nuspec":{"id":"Newtonsoft.Json"}}`)
	resolved := json.RawMessage(`{"canonicalId":"Serilog"}`)
	cases := []struct {
		format, ns, name, ver string
		attrs                 json.RawMessage
		want                  string
	}{
		{"maven", "org.apache.logging.log4j", "log4j-core", "2.14.1", nil, "pkg:maven/org.apache.logging.log4j/log4j-core@2.14.1"},
		{"npm", "", "lodash", "4.17.20", nil, "pkg:npm/lodash@4.17.20"},
		{"npm", "@babel", "traverse", "7.22.0", nil, "pkg:npm/@babel/traverse@7.22.0"},
		{"pypi", "", "django", "3.2.0", nil, "pkg:pypi/django@3.2.0"},
		{"go", "golang.org/x", "text", "v0.3.5", nil, "pkg:golang/golang.org/x/text@v0.3.5"},
		{"go", "github.com/docker", "docker", "v20.10.0+incompatible", nil, "pkg:golang/github.com/docker/docker@v20.10.0+incompatible"},
		{"rubygems", "java", "rack", "2.0.0", nil, "pkg:gem/rack@2.0.0"},
		{"cargo", "", "tokio", "1.0.0", nil, "pkg:cargo/tokio@1.0.0"},
		{"composer", "laravel", "framework", "8.0.0", nil, "pkg:composer/laravel/framework@8.0.0"},
		{"pub", "", "http", "0.12.0", nil, "pkg:pub/http@0.12.0"},
		{"r", "src/contrib", "commonmark", "1.8.0", nil, "pkg:cran/commonmark@1.8.0"},
		{"nuget", "", "newtonsoft.json", "12.0.1", nuspec, "pkg:nuget/Newtonsoft.Json@12.0.1"},
		{"nuget", "", "serilog", "2.0.0", resolved, "pkg:nuget/Serilog@2.0.0"},
	}
	for _, c := range cases {
		got, ok := PURL(c.format, c.ns, c.name, c.ver, c.attrs)
		if !ok || got != c.want {
			t.Errorf("%s %s/%s@%s: got %q ok=%v, want %q", c.format, c.ns, c.name, c.ver, got, ok, c.want)
		}
	}
}

// Every one of these would be a silent false negative if it produced a URL.
func TestPURLRefusesWhatItCannotName(t *testing.T) {
	cases := []struct {
		format, name string
		attrs        json.RawMessage
	}{
		{"docker", "alpine", nil},
		{"apt", "openssl", nil},
		{"conan", "openssl", nil},
		{"nuget", "newtonsoft.json", nil}, // casing unknown
		{"nuget", "newtonsoft.json", json.RawMessage(`{"canonicalId":"Other.Package"}`)}, // not this package
	}
	for _, c := range cases {
		if got, ok := PURL(c.format, "", c.name, "1.0.0", c.attrs); ok {
			t.Errorf("%s %s: got %q, want no purl", c.format, c.name, got)
		}
	}
}

func TestCVSS3Score(t *testing.T) {
	cases := map[string]float64{
		"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:C/C:H/I:H/A:H": 10.0,
		"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H": 9.8,
		"CVSS:3.1/AV:N/AC:H/PR:N/UI:N/S:U/C:H/I:N/A:N": 5.9,
		"CVSS:3.1/AV:L/AC:L/PR:L/UI:N/S:U/C:H/I:N/A:N": 5.5,
		"CVSS:3.1/AV:N/AC:L/PR:N/UI:R/S:C/C:L/I:L/A:N": 6.1,
		"CVSS:3.0/AV:N/AC:L/PR:L/UI:N/S:U/C:H/I:H/A:H": 8.8,
		"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:N": 0,
		// Log4Shell as GitHub publishes it, temporal metrics included.
		"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:C/C:H/I:H/A:H/E:H": 10.0,
	}
	for v, want := range cases {
		got, ok := cvss3Score(v)
		if !ok || got != want {
			t.Errorf("%s: got %v ok=%v, want %v", v, got, ok, want)
		}
	}
	for _, bad := range []string{"", "CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:H/VI:H/VA:H/SC:N/SI:N/SA:N", "CVSS:3.1/AV:N/AC:L"} {
		if _, ok := cvss3Score(bad); ok {
			t.Errorf("%q: want not ok", bad)
		}
	}
}

func TestRatingPrefersTheAdvisory(t *testing.T) {
	var r Record
	json.Unmarshal([]byte(`{"database_specific":{"severity":"MODERATE"},
		"severity":[{"type":"CVSS_V3","score":"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"}]}`), &r)
	sev, score := r.rating()
	if sev != SeverityModerate || score == nil || *score != 9.8 {
		t.Fatalf("got %s %v", sev, score)
	}
	var v Record
	json.Unmarshal([]byte(`{"severity":[{"type":"CVSS_V3","score":"CVSS:3.1/AV:N/AC:H/PR:N/UI:N/S:U/C:H/I:N/A:N"}]}`), &v)
	if sev, _ := v.rating(); sev != SeverityModerate {
		t.Fatalf("computed rating: got %s", sev)
	}
	var none Record
	if sev, _ := none.rating(); sev != SeverityUnknown {
		t.Fatalf("no data: got %s", sev)
	}
}

func TestFixedVersionsMatchEitherSpellingOfAScope(t *testing.T) {
	var r Record
	json.Unmarshal([]byte(`{"affected":[
		{"package":{"purl":"pkg:npm/%40babel/traverse"},"ranges":[{"events":[{"introduced":"0"},{"fixed":"7.23.2"},{"introduced":"8.0.0-alpha.0"},{"fixed":"8.0.0-alpha.4"}]}]},
		{"package":{"purl":"pkg:npm/other"},"ranges":[{"events":[{"fixed":"9.9.9"}]}]}]}`), &r)
	fixed := r.fixedVersions()[samePURL("pkg:npm/@babel/traverse")]
	if strings.Join(fixed, ",") != "7.23.2,8.0.0-alpha.4" {
		t.Fatalf("got %v", fixed)
	}
}

func TestQueryBatchFollowsPages(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req struct {
			Queries []struct {
				Package   struct{ PURL string } `json:"package"`
				PageToken string                `json:"page_token"`
			} `json:"queries"`
		}
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &req)
		type res struct {
			Vulns []match `json:"vulns"`
			Next  string  `json:"next_page_token,omitempty"`
		}
		var out struct {
			Results []res `json:"results"`
		}
		for _, q := range req.Queries {
			switch {
			case q.Package.PURL == "pkg:npm/big@1" && q.PageToken == "":
				out.Results = append(out.Results, res{[]match{{ID: "A"}}, "page2"})
			case q.Package.PURL == "pkg:npm/big@1":
				out.Results = append(out.Results, res{[]match{{ID: "B"}}, ""})
			case q.Package.PURL == "pkg:npm/small@1":
				out.Results = append(out.Results, res{[]match{{ID: "C"}}, ""})
			default:
				out.Results = append(out.Results, res{})
			}
		}
		json.NewEncoder(w).Encode(out)
	}))
	defer srv.Close()
	o := &OSV{BaseURL: srv.URL, HTTP: srv.Client()}
	got, err := o.QueryBatch(context.Background(), []string{"pkg:npm/big@1", "pkg:npm/small@1", "pkg:npm/clean@1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got[0]) != 2 || len(got[1]) != 1 || len(got[2]) != 0 {
		t.Fatalf("got %+v", got)
	}
	if calls != 2 {
		t.Fatalf("%d requests, want 2 (the second only for the paged query)", calls)
	}
}

// Three databases, one bug: the count must be one, represented by the
// advisory with a rating.
func TestCollapseCountsAliasesOnce(t *testing.T) {
	rs := &recordSet{byID: map[string]stored{
		"GO-2021-0113":        {aliases: []string{"CVE-2021-38561", "GHSA-ppp9-7jff-5vj2"}, severity: SeverityUnknown},
		"GHSA-ppp9-7jff-5vj2": {aliases: []string{"CVE-2021-38561"}, severity: SeverityHigh},
		"GO-2022-0001":        {aliases: []string{"CVE-2022-1"}, severity: SeverityModerate},
	}}
	s := &Scanner{}
	got := s.collapse(context.Background(), []match{{ID: "GO-2021-0113"}, {ID: "GHSA-ppp9-7jff-5vj2"}, {ID: "GO-2022-0001"}}, rs)
	if len(got) != 2 {
		t.Fatalf("got %d groups: %+v", len(got), got)
	}
	reps := []string{got[0].rep, got[1].rep}
	if reps[0] != "GHSA-ppp9-7jff-5vj2" || reps[1] != "GO-2022-0001" {
		t.Fatalf("representatives %v", reps)
	}
}

// A record with no rating borrows one from its GitHub alias, fetched for
// the purpose, and the alias joins the group.
func TestCollapseBorrowsARating(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/GHSA-xxxx-yyyy-zzzz") {
			w.Write([]byte(`{"id":"GHSA-xxxx-yyyy-zzzz","aliases":["CVE-2024-1"],"modified":"2024-01-01T00:00:00Z","database_specific":{"severity":"CRITICAL"}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	rs := &recordSet{byID: map[string]stored{
		"PYSEC-2024-1": {aliases: []string{"CVE-2024-1", "GHSA-xxxx-yyyy-zzzz"}, severity: SeverityUnknown},
	}}
	s := &Scanner{OSV: &OSV{BaseURL: srv.URL, HTTP: srv.Client()}, Content: nil}
	s.store = func(context.Context, *Record, string, *float64, map[string][]string) error { return nil }
	got := s.collapse(context.Background(), []match{{ID: "PYSEC-2024-1", Modified: time.Now()}}, rs)
	if len(got) != 1 || got[0].rep != "GHSA-xxxx-yyyy-zzzz" || rs.byID[got[0].rep].severity != SeverityCritical {
		t.Fatalf("got %+v", got)
	}
}
