package vuln

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/holiaokho/holiaokho/internal/content"
)

// Report is what every export format is written from: the findings grouped
// by package, which is how a team acts on them — "upgrade log4j-core to
// 2.17.1" is one task however many advisories it closes.
type Report struct {
	GeneratedAt time.Time  `json:"generatedAt"`
	Version     string     `json:"holiaokhoVersion"`
	Source      string     `json:"source"`
	Filters     Filters    `json:"filters"`
	Coverage    Coverage   `json:"coverage"`
	Summary     Counts     `json:"summary"`
	Packages    []PkgEntry `json:"packages"`
}

type Filters struct {
	MinSeverity string `json:"minSeverity,omitempty"`
	Severity    string `json:"severity,omitempty"`
	Repository  string `json:"repository,omitempty"`
	Format      string `json:"format,omitempty"`
	Query       string `json:"query,omitempty"`
}

// Coverage says how much was looked at, so an empty report is read as what
// it is. See Summary.
type Coverage struct {
	Scanned    int        `json:"scanned"`
	Pending    int        `json:"pending"`
	NotCovered int        `json:"notCovered"`
	Excluded   int        `json:"excluded"`
	LastScan   *time.Time `json:"lastScan,omitempty"`
}

type Counts struct {
	Packages        int            `json:"packages"`
	Vulnerabilities int            `json:"vulnerabilities"`
	BySeverity      map[string]int `json:"bySeverity"`
}

// PkgEntry is one package version, wherever it is stored.
type PkgEntry struct {
	PURL         string   `json:"purl"`
	Format       string   `json:"format"`
	Name         string   `json:"name"` // as its ecosystem writes it: group:artifact, @scope/name
	Version      string   `json:"version"`
	Repositories []string `json:"repositories"`
	Severity     string   `json:"highestSeverity"`
	// UpgradeTo is the lowest version that fixes every vulnerability listed
	// that has a fix; FixesAll says whether that is all of them.
	UpgradeTo       string       `json:"upgradeTo,omitempty"`
	FixesAll        bool         `json:"fixesAll"`
	Vulnerabilities []ReportVuln `json:"vulnerabilities"`
}

type ReportVuln struct {
	ID        string     `json:"id"`
	Aliases   []string   `json:"aliases"`
	Severity  string     `json:"severity"`
	Score     *float64   `json:"cvss,omitempty"`
	Summary   string     `json:"summary"`
	FixedIn   []string   `json:"fixedIn"`
	FixTo     string     `json:"fixTo,omitempty"` // the upgrade from this version
	Published *time.Time `json:"published,omitempty"`
	FirstSeen time.Time  `json:"firstSeen"`
	URL       string     `json:"url"`
}

// exportLimit bounds one report. Far above what any instance has had; it is
// there so a report is never an unbounded query.
const exportLimit = 100000

// BuildReport collects the findings f selects into a Report.
func BuildReport(ctx context.Context, c *content.Service, f Filter, enabled bool, version string) (*Report, error) {
	f.Limit, f.Offset = exportLimit, 0
	found, _, err := List(ctx, c, f)
	if err != nil {
		return nil, err
	}
	s, err := Summarise(ctx, c, f.RepoIDs, enabled)
	if err != nil {
		return nil, err
	}
	if s.LastOKAt != nil {
		t := s.LastOKAt.UTC()
		s.LastOKAt = &t
	}
	r := &Report{
		GeneratedAt: time.Now().UTC(),
		Version:     version,
		Source:      "OSV.dev",
		Filters:     Filters{MinSeverity: f.MinSeverity, Severity: f.Severity, Repository: f.Repository, Format: f.Format, Query: f.Q},
		Coverage:    Coverage{Scanned: s.Scanned, Pending: s.Pending, NotCovered: s.NotCovered, Excluded: s.Excluded, LastScan: s.LastOKAt},
	}
	assemble(r, found)
	return r, nil
}

// assemble groups findings by package into r, working out for each package
// the version to upgrade to.
func assemble(r *Report, found []Finding) {
	r.Summary = Counts{BySeverity: map[string]int{}}
	byKey := map[string]*PkgEntry{}
	seen := map[string]map[string]bool{} // package key -> vulnerability ids
	var order []string
	for _, x := range found {
		key := x.Format + "\x00" + x.Namespace + "\x00" + x.Name + "\x00" + x.Version
		p := byKey[key]
		if p == nil {
			purl, _ := PURL(x.Format, x.Namespace, x.Name, x.Version, x.attrs)
			p = &PkgEntry{PURL: purl, Format: x.Format, Name: packageName(x), Version: x.Version, Severity: SeverityUnknown}
			byKey[key] = p
			seen[key] = map[string]bool{}
			order = append(order, key)
		}
		if !contains(p.Repositories, x.Repository) {
			p.Repositories = append(p.Repositories, x.Repository)
		}
		// The same package in two repositories is the same finding twice.
		if seen[key][x.VulnID] {
			continue
		}
		seen[key][x.VulnID] = true
		p.Vulnerabilities = append(p.Vulnerabilities, ReportVuln{
			ID: x.VulnID, Aliases: nonNil(x.Aliases), Severity: x.Severity, Score: x.Score, Summary: x.Summary,
			FixedIn: nonNil(x.FixedIn), FixTo: fixFor(x.Version, x.FixedIn),
			Published: x.Published, FirstSeen: x.FirstSeen, URL: "https://osv.dev/vulnerability/" + x.VulnID,
		})
		if SeverityRank(x.Severity) > SeverityRank(p.Severity) {
			p.Severity = x.Severity
		}
	}

	ids := map[string]bool{}
	for _, key := range order {
		p := byKey[key]
		p.FixesAll = true
		for _, v := range p.Vulnerabilities {
			ids[v.ID] = true
			r.Summary.BySeverity[v.Severity]++
			if v.FixTo == "" {
				p.FixesAll = false
			} else if p.UpgradeTo == "" || compareVersions(v.FixTo, p.UpgradeTo) > 0 {
				p.UpgradeTo = v.FixTo
			}
		}
		sort.Strings(p.Repositories)
		r.Packages = append(r.Packages, *p)
	}
	r.Summary.Packages = len(r.Packages)
	r.Summary.Vulnerabilities = len(ids)
	// Worst first, then by name: the order someone works through them.
	sort.SliceStable(r.Packages, func(i, j int) bool {
		a, b := r.Packages[i], r.Packages[j]
		if SeverityRank(a.Severity) != SeverityRank(b.Severity) {
			return SeverityRank(a.Severity) > SeverityRank(b.Severity)
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return compareVersions(a.Version, b.Version) < 0
	})
}

// packageName writes a package's name the way its own ecosystem does.
func packageName(f Finding) string {
	if f.Format == "nuget" {
		if id := NuGetID(f.attrs); id != "" {
			return id // as published, not the lower-case form it is stored in
		}
	}
	switch {
	case f.Namespace == "" || f.Format == "rubygems" || f.Format == "r":
		return f.Name
	case f.Format == "maven":
		return f.Namespace + ":" + f.Name
	default:
		return strings.TrimSuffix(f.Namespace, "/") + "/" + f.Name
	}
}
