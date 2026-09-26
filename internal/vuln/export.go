package vuln

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// CSVColumns are the CSV header, fixed and in English: the file is meant
// for scripts and AI agents matching findings against a project's own
// dependency files, and purl is the column to match on.
var CSVColumns = []string{
	"purl", "ecosystem", "package", "version", "repositories",
	"severity", "cvss", "vulnerability_id", "aliases", "summary",
	"fixed_in", "fix_to", "package_upgrade_to", "package_fixes_all",
	"package_last_used", "published", "first_seen", "url",
}

// WriteCSV writes one row per vulnerability in each package.
func WriteCSV(w io.Writer, r *Report) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(CSVColumns); err != nil {
		return err
	}
	for _, p := range r.Packages {
		for _, v := range p.Vulnerabilities {
			row := []string{
				p.PURL, p.Format, p.Name, p.Version, strings.Join(p.Repositories, ";"),
				v.Severity, score(v.Score), v.ID, strings.Join(v.Aliases, ";"), v.Summary,
				strings.Join(v.FixedIn, ";"), v.FixTo, p.UpgradeTo, fmt.Sprint(p.FixesAll),
				p.LastUsed.UTC().Format(time.RFC3339), date(v.Published), v.FirstSeen.UTC().Format(time.RFC3339), v.URL,
			}
			for i := range row {
				row[i] = noFormula(row[i])
			}
			if err := cw.Write(row); err != nil {
				return err
			}
		}
	}
	cw.Flush()
	return cw.Error()
}

// WriteJSON writes the report grouped by package, which is the shape an
// agent reasons about: one entry per dependency to check, with everything
// known about it.
func WriteJSON(w io.Writer, r *Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// noFormula stops a spreadsheet from treating a cell as a formula. The
// summaries come from outside (OSV), and a CSV is opened in Excel as often
// as it is read by a script; "=HYPERLINK(…)" in a summary must stay text.
func noFormula(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

func score(f *float64) string {
	if f == nil {
		return ""
	}
	return fmt.Sprintf("%.1f", *f)
}

func date(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format("2006-01-02")
}
