package vuln

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	SeverityCritical = "CRITICAL"
	SeverityHigh     = "HIGH"
	SeverityModerate = "MODERATE"
	SeverityLow      = "LOW"
	SeverityUnknown  = "UNKNOWN"
)

// SeverityRank orders severities; higher is worse, UNKNOWN is 0.
func SeverityRank(s string) int {
	switch strings.ToUpper(s) {
	case SeverityCritical:
		return 4
	case SeverityHigh:
		return 3
	case SeverityModerate, "MEDIUM":
		return 2
	case SeverityLow:
		return 1
	}
	return 0
}

// batchSize is OSV's limit on queries per querybatch request.
const batchSize = 1000

// OSV is a client for the OSV API (https://google.github.io/osv.dev/api/).
type OSV struct {
	BaseURL string
	HTTP    *http.Client
	UA      string
}

// match is one vulnerability id a querybatch returned for a package.
type match struct {
	ID       string    `json:"id"`
	Modified time.Time `json:"modified"`
}

// QueryBatch returns, for each purl, the ids of the vulnerabilities affecting
// it. The result is index-aligned with purls.
func (o *OSV) QueryBatch(ctx context.Context, purls []string) ([][]match, error) {
	out := make([][]match, len(purls))
	for start := 0; start < len(purls); start += batchSize {
		end := min(start+batchSize, len(purls))
		type query struct {
			Package struct {
				PURL string `json:"purl"`
			} `json:"package"`
			PageToken string `json:"page_token,omitempty"`
		}
		qs := make([]query, end-start)
		for i := range qs {
			qs[i].Package.PURL = purls[start+i]
		}
		// A query with more matches than fit in one page comes back with a
		// token; only those are asked again, until none are left.
		pending := make([]int, len(qs))
		for i := range pending {
			pending[i] = i
		}
		for len(pending) > 0 {
			req := struct {
				Queries []query `json:"queries"`
			}{}
			for _, i := range pending {
				req.Queries = append(req.Queries, qs[i])
			}
			var resp struct {
				Results []struct {
					Vulns         []match `json:"vulns"`
					NextPageToken string  `json:"next_page_token"`
				} `json:"results"`
			}
			if err := o.post(ctx, "/v1/querybatch", req, &resp); err != nil {
				return nil, err
			}
			if len(resp.Results) != len(pending) {
				return nil, fmt.Errorf("osv: %d results for %d queries", len(resp.Results), len(pending))
			}
			var next []int
			for k, r := range resp.Results {
				i := pending[k]
				out[start+i] = append(out[start+i], r.Vulns...)
				if r.NextPageToken != "" {
					qs[i].PageToken = r.NextPageToken
					next = append(next, i)
				}
			}
			pending = next
		}
	}
	return out, nil
}

// Record is the part of an OSV vulnerability record that is kept.
type Record struct {
	ID               string     `json:"id"`
	Aliases          []string   `json:"aliases"`
	Summary          string     `json:"summary"`
	Details          string     `json:"details"`
	Published        time.Time  `json:"published"`
	Modified         time.Time  `json:"modified"`
	Withdrawn        *time.Time `json:"withdrawn"`
	DatabaseSpecific struct {
		Severity string   `json:"severity"`
		CWEIDs   []string `json:"cwe_ids"`
	} `json:"database_specific"`
	Severity []struct {
		Type  string `json:"type"`
		Score string `json:"score"`
	} `json:"severity"`
	Affected []struct {
		Package struct {
			Name      string `json:"name"`
			Ecosystem string `json:"ecosystem"`
			PURL      string `json:"purl"`
		} `json:"package"`
		Ranges []struct {
			Events []map[string]string `json:"events"`
		} `json:"ranges"`
		EcosystemSpecific struct {
			Severity string `json:"severity"`
		} `json:"ecosystem_specific"`
	} `json:"affected"`
}

// Get fetches one vulnerability record.
func (o *OSV) Get(ctx context.Context, id string) (*Record, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(o.BaseURL, "/")+"/v1/vulns/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, err
	}
	var r Record
	if err := o.do(req, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (o *OSV) post(ctx context.Context, path string, body, into any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(o.BaseURL, "/")+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return o.do(req, into)
}

func (o *OSV) do(req *http.Request, into any) error {
	if o.UA != "" {
		req.Header.Set("User-Agent", o.UA)
	}
	resp, err := o.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("osv %s: %s: %s", req.URL.Path, resp.Status, strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(into)
}

// rating works out a record's severity and, when it comes from a CVSS
// vector, its score. The advisory's own rating wins over a computed one:
// it is what the people who triaged the issue decided.
// Malicious reports whether the record describes a malicious package rather
// than a bug: an entry from the OpenSSF malicious-packages database (MAL-…),
// or one aliased to it, or an advisory classed CWE-506, embedded malicious
// code. Such a package has no safe version to upgrade to; it is to be
// removed, and not downloaded in the first place.
func (r *Record) Malicious() bool {
	if strings.HasPrefix(r.ID, "MAL-") {
		return true
	}
	for _, a := range r.Aliases {
		if strings.HasPrefix(a, "MAL-") {
			return true
		}
	}
	return contains(r.DatabaseSpecific.CWEIDs, "CWE-506")
}

// Query returns the records OSV has for one package version, in full.
func (o *OSV) Query(ctx context.Context, purl string) ([]Record, error) {
	var out struct {
		Vulns []Record `json:"vulns"`
	}
	if err := o.post(ctx, "/v1/query", map[string]any{"package": map[string]string{"purl": purl}}, &out); err != nil {
		return nil, err
	}
	return out.Vulns, nil
}

func (r *Record) rating() (severity string, score *float64) {
	defer func() {
		// Malicious code is critical whatever else the record says, and MAL-
		// records carry no rating at all.
		if r.Malicious() {
			severity = SeverityCritical
		}
	}()
	for _, s := range r.Severity {
		if strings.HasPrefix(s.Type, "CVSS_V3") {
			if v, ok := cvss3Score(s.Score); ok {
				score = &v
				break
			}
		}
	}
	if SeverityRank(r.DatabaseSpecific.Severity) > 0 {
		return normalise(r.DatabaseSpecific.Severity), score
	}
	if score != nil {
		return ratingFor(*score), score
	}
	for _, a := range r.Affected {
		if SeverityRank(a.EcosystemSpecific.Severity) > 0 {
			return normalise(a.EcosystemSpecific.Severity), nil
		}
	}
	return SeverityUnknown, nil
}

func normalise(s string) string {
	s = strings.ToUpper(s)
	if s == "MEDIUM" {
		return SeverityModerate
	}
	return s
}

// fixedVersions lists, per affected package, the versions that fix this
// record, keyed by samePURL of the package's purl.
func (r *Record) fixedVersions() map[string][]string {
	out := map[string][]string{}
	for _, a := range r.Affected {
		if a.Package.PURL == "" {
			continue
		}
		key := samePURL(a.Package.PURL)
		for _, rg := range a.Ranges {
			for _, ev := range rg.Events {
				if f := ev["fixed"]; f != "" && !contains(out[key], f) {
					out[key] = append(out[key], f)
				}
			}
		}
	}
	return out
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// samePURL reduces a purl to a form two spellings of the same package agree
// on: OSV writes an npm scope as %40babel, and a purl built here may not.
func samePURL(p string) string {
	if u, err := url.PathUnescape(p); err == nil {
		p = u
	}
	return strings.ToLower(p)
}
