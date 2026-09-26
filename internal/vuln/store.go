package vuln

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/holiaokho/holiaokho/internal/content"
)

// Filter narrows a findings query. RepoIDs is required and limits results
// to repositories the caller may read; an empty list means none.
type Filter struct {
	RepoIDs     []uuid.UUID
	Repository  string
	Format      string
	MinSeverity string
	Severity    string   // exactly this one
	Severities  []string // any of these; chosen in the export dialog
	Q           string   // package name or vulnerability id / alias
	PackageID   int64
	Limit       int
	Offset      int
	Sort        content.Sort

	unnotified bool
}

// listSort are the columns the findings list sorts by.
var listSort = map[string]content.SortColumn{
	"severity":   {Exprs: []string{severityOrder, "v.score"}},
	"id":         {Exprs: []string{"v.id"}},
	"package":    {Exprs: []string{"p.namespace", "p.name", "p.version"}, Version: true},
	"version":    {Exprs: []string{"p.version"}, Version: true},
	"repository": {Exprs: []string{"r.name"}},
	"summary":    {Exprs: []string{"v.summary"}},
	"lastUsed":   {Exprs: []string{"coalesce(p.last_downloaded_at, p.created_at)"}},
	"firstSeen":  {Exprs: []string{"pv.first_seen"}},
	"published":  {Exprs: []string{"v.published"}},
}

const optedIn = `coalesce((r.attributes -> 'vulnerabilities' ->> 'scan')::boolean, true)`

const severityOrder = `CASE v.severity WHEN 'CRITICAL' THEN 4 WHEN 'HIGH' THEN 3 WHEN 'MODERATE' THEN 2 WHEN 'LOW' THEN 1 ELSE 0 END`

func (f Filter) where() (string, []any) {
	// An opted-out repository is not scanned any more, so what was found
	// in it before is not current; it is left out rather than shown stale.
	conds := []string{"p.repo_id = ANY($1)", optedIn}
	args := []any{f.RepoIDs}
	add := func(cond string, v any) {
		args = append(args, v)
		conds = append(conds, fmt.Sprintf(cond, len(args)))
	}
	if f.Repository != "" {
		add("r.name = $%d", f.Repository)
	}
	if f.Format != "" {
		add("r.format = $%d", f.Format)
	}
	if n := SeverityRank(f.MinSeverity); n > 0 {
		add(severityOrder+" >= $%d", n)
	}
	if f.Severity != "" {
		add("v.severity = $%d", strings.ToUpper(f.Severity))
	}
	if len(f.Severities) > 0 {
		add("v.severity = ANY($%d)", f.Severities)
	}
	if f.PackageID > 0 {
		add("p.id = $%d", f.PackageID)
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		add(`(p.name ILIKE $%[1]d OR p.namespace ILIKE $%[1]d OR v.id ILIKE $%[1]d OR array_to_string(v.aliases, ' ') ILIKE $%[1]d)`, "%"+escapeLike(q)+"%")
	}
	if f.unnotified {
		conds = append(conds, "pv.notified_at IS NULL")
	}
	return strings.Join(conds, " AND "), args
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// List returns findings, worst first, and how many match in total.
func List(ctx context.Context, c *content.Service, f Filter) ([]Finding, int, error) {
	where, args := f.where()
	from := `FROM package_vulnerabilities pv
		JOIN packages p ON p.id = pv.package_id
		JOIN repositories r ON r.id = p.repo_id
		JOIN vulnerabilities v ON v.id = pv.vuln_id
		WHERE ` + where
	var total int
	if err := c.DB.Pool.QueryRow(ctx, `SELECT count(*) `+from, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	q := `SELECT pv.package_id, r.name, r.format, p.namespace, p.name, p.version,
			v.id, v.aliases, v.summary, v.severity, v.score::float8, pv.fixed_in, v.published, pv.first_seen, p.attrs,
			coalesce(p.last_downloaded_at, p.created_at) ` + from
	// Worst first unless asked otherwise.
	q += c.OrderBy(ctx, f.Sort, listSort,
		severityOrder+` DESC, v.score DESC NULLS LAST, v.published DESC NULLS LAST, p.name, p.version`, "pv.package_id, v.id")
	if f.Limit > 0 {
		args = append(args, f.Limit, f.Offset)
		q += fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	}
	rows, err := c.DB.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Finding{}
	for rows.Next() {
		var x Finding
		if err := rows.Scan(&x.PackageID, &x.Repository, &x.Format, &x.Namespace, &x.Name, &x.Version,
			&x.VulnID, &x.Aliases, &x.Summary, &x.Severity, &x.Score, &x.FixedIn, &x.Published, &x.FirstSeen, &x.attrs, &x.LastUsed); err != nil {
			return nil, 0, err
		}
		out = append(out, x)
	}
	return out, total, rows.Err()
}

// Summary is the overview the dashboard and the vulnerabilities page show.
type Summary struct {
	Enabled bool `json:"enabled"`
	// Findings per severity: one per vulnerable package version and
	// vulnerability, the way a team has to fix them.
	Counts map[string]int `json:"counts"`
	// Vulnerabilities is how many distinct vulnerabilities those are.
	Vulnerabilities  int `json:"vulnerabilities"`
	AffectedPackages int `json:"affectedPackages"`

	// Coverage. Every package is in exactly one of these, so a clean result
	// can always be read against how much was actually checked.
	Scanned    int `json:"scanned"`    // checked with OSV
	Pending    int `json:"pending"`    // checkable, not checked yet
	NotCovered int `json:"notCovered"` // OSV cannot be asked about it
	Excluded   int `json:"excluded"`   // its repository opted out

	LastRunAt *time.Time `json:"lastRunAt"`
	LastOKAt  *time.Time `json:"lastOkAt"`
	LastError string     `json:"lastError,omitempty"`

	// CoveredFormats are the formats that can be checked at all.
	CoveredFormats []string `json:"coveredFormats"`
}

// Summarise builds the Summary over the given repositories.
func Summarise(ctx context.Context, c *content.Service, repoIDs []uuid.UUID, enabled bool) (Summary, error) {
	s := Summary{Enabled: enabled, Counts: map[string]int{}, CoveredFormats: CoveredFormats()}
	sort.Strings(s.CoveredFormats)
	rows, err := c.DB.Pool.Query(ctx, `
		SELECT v.severity, count(*), count(DISTINCT v.id), count(DISTINCT pv.package_id)
		FROM package_vulnerabilities pv
		JOIN packages p ON p.id = pv.package_id
		JOIN repositories r ON r.id = p.repo_id
		JOIN vulnerabilities v ON v.id = pv.vuln_id
		WHERE p.repo_id = ANY($1) AND `+optedIn+`
		GROUP BY GROUPING SETS ((v.severity), ())`, repoIDs)
	if err != nil {
		return s, err
	}
	for rows.Next() {
		var sev *string
		var n, vulns, pkgs int
		if err := rows.Scan(&sev, &n, &vulns, &pkgs); err != nil {
			rows.Close()
			return s, err
		}
		if sev == nil {
			s.Vulnerabilities, s.AffectedPackages = vulns, pkgs
		} else {
			s.Counts[*sev] = n
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return s, err
	}
	err = c.DB.Pool.QueryRow(ctx, `
		SELECT
		  count(*) FILTER (WHERE NOT opted_out AND covered_format AND p.vuln_covered),
		  count(*) FILTER (WHERE NOT opted_out AND covered_format AND p.vuln_covered IS NULL),
		  count(*) FILTER (WHERE NOT opted_out AND (NOT covered_format OR p.vuln_covered = false)),
		  count(*) FILTER (WHERE opted_out)
		FROM packages p
		JOIN LATERAL (
		  SELECT NOT coalesce((r.attributes -> 'vulnerabilities' ->> 'scan')::boolean, true) AS opted_out,
		         r.format = ANY($2) AS covered_format
		  FROM repositories r WHERE r.id = p.repo_id
		) r ON true
		WHERE p.repo_id = ANY($1)`, repoIDs, CoveredFormats()).Scan(&s.Scanned, &s.Pending, &s.NotCovered, &s.Excluded)
	if err != nil {
		return s, err
	}
	err = c.DB.Pool.QueryRow(ctx, `SELECT last_run_at, last_ok_at, last_error FROM vuln_scan_state WHERE id = 1`).
		Scan(&s.LastRunAt, &s.LastOKAt, &s.LastError)
	return s, err
}

// Findings is List without paging, for internal use.
func Findings(ctx context.Context, c *content.Service, f Filter) ([]Finding, error) {
	f.Limit = 0
	out, _, err := List(ctx, c, f)
	return out, err
}
