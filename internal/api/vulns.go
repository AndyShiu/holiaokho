package api

import (
	"bytes"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/holiaokho/holiaokho/internal/auth"
	"github.com/holiaokho/holiaokho/internal/content"
	"github.com/holiaokho/holiaokho/internal/vuln"
)

// readableRepoIDs is every repository the caller may read. Findings are
// filtered in the query by this list rather than afterwards, so counts and
// pages are right: a vulnerability in a package you cannot see is not yours
// to count.
func (a *API) readableRepoIDs(r *http.Request) []uuid.UUID {
	p := auth.PrincipalFrom(r.Context())
	ids := []uuid.UUID{}
	if p == nil {
		return ids
	}
	for _, rp := range a.reposByID(r) {
		if p.CanRepo(rp.Name, rp.Format, auth.Read) {
			ids = append(ids, rp.ID)
		}
	}
	return ids
}

func (a *API) vulnSummary(w http.ResponseWriter, r *http.Request) {
	s, err := vuln.Summarise(r.Context(), a.Content, a.readableRepoIDs(r), a.Config.Vulns.Enabled)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, 200, s)
}

func (a *API) vulnList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	items, total, err := vuln.List(r.Context(), a.Content, vuln.Filter{
		RepoIDs:     a.readableRepoIDs(r),
		Repository:  q.Get("repository"),
		Format:      q.Get("format"),
		MinSeverity: q.Get("severity"),
		Severity:    q.Get("level"),
		Q:           q.Get("q"),
		Limit:       limit,
		Offset:      max(offset, 0),
		Sort:        content.ParseSort(q.Get("sort"), q.Get("order")),
	})
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": total})
}

// packageVulns lists one package's findings, and whether it could be
// checked at all — "none found" and "never looked" must not look the same.
func (a *API) packageVulns(w http.ResponseWriter, r *http.Request) {
	p, rp := a.pkgRepo(w, r, auth.Read)
	if p == nil {
		return
	}
	items, err := vuln.Findings(r.Context(), a.Content, vuln.Filter{RepoIDs: []uuid.UUID{rp.ID}, PackageID: p.ID})
	if err != nil {
		a.fail(w, err)
		return
	}
	var scanned *time.Time
	var covered *bool
	a.Content.DB.Pool.QueryRow(r.Context(), `SELECT vuln_scanned_at, vuln_covered FROM packages WHERE id = $1`, p.ID).Scan(&scanned, &covered)
	status := "pending"
	switch {
	case !a.Config.Vulns.Enabled:
		status = "disabled"
	case !vuln.Covered(rp.Format):
		status = "notCovered"
	case !vuln.ScanEnabled(rp.Attributes):
		status = "excluded"
	case covered == nil:
		status = "pending"
	case !*covered:
		status = "notCovered"
	default:
		status = "scanned"
	}
	writeJSON(w, 200, map[string]any{"status": status, "scannedAt": scanned, "items": items})
}

// exportTypes are the report formats, with what they are served as.
var exportTypes = map[string]struct{ ext, contentType string }{
	"pdf":  {"pdf", "application/pdf"},
	"xlsx": {"xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
	"csv":  {"csv", "text/csv; charset=utf-8"},
	"json": {"json", "application/json"},
}

// vulnExport downloads the findings the vulnerabilities page is showing —
// same filters, same repositories the caller may read — as a report.
// "as" picks the format; "format" is already the package-format filter.
func (a *API) vulnExport(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	as := q.Get("as")
	t, ok := exportTypes[as]
	if !ok {
		writeErr(w, 400, "validation", "as must be one of pdf, xlsx, csv, json")
		return
	}
	rep, err := vuln.BuildReport(r.Context(), a.Content, vuln.Filter{
		RepoIDs:     a.readableRepoIDs(r),
		Repository:  q.Get("repository"),
		Format:      q.Get("format"),
		MinSeverity: q.Get("severity"),
		Severity:    q.Get("level"),
		Severities:  levels(q.Get("levels")),
		Q:           q.Get("q"),
	}, a.Config.Vulns.Enabled, a.Version)
	if err != nil {
		a.fail(w, err)
		return
	}
	lang := vuln.Lang(q.Get("lang"))
	// Written to memory first, so a failure halfway is an error response
	// rather than a truncated file the browser saves anyway.
	var buf bytes.Buffer
	switch as {
	case "pdf":
		err = vuln.WritePDF(&buf, rep, lang)
	case "xlsx":
		err = vuln.WriteXLSX(&buf, rep, lang)
	case "csv":
		err = vuln.WriteCSV(&buf, rep)
	case "json":
		err = vuln.WriteJSON(&buf, rep)
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	a.audit_(r, "vulnerabilities.export", "vulnerabilities", as, map[string]any{"packages": rep.Summary.Packages, "filters": rep.Filters})
	name := "holiaokho-vulnerabilities-" + rep.GeneratedAt.Format("20060102-1504")
	if slug := rep.Filters.Slug(); slug != "" {
		name += "-" + slug
	}
	name += "." + t.ext
	w.Header().Set("Content-Type", t.contentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	w.Header().Set("Cache-Control", "no-store")
	w.Write(buf.Bytes())
}

// levels parses the comma-separated severities chosen for an export,
// keeping only known ones.
func levels(v string) []string {
	var out []string
	for _, s := range strings.Split(strings.ToUpper(v), ",") {
		switch s = strings.TrimSpace(s); s {
		case vuln.SeverityCritical, vuln.SeverityHigh, vuln.SeverityModerate, vuln.SeverityLow, vuln.SeverityUnknown:
			out = append(out, s)
		}
	}
	return out
}

// blockedList lists refused downloads of malicious packages, in the
// repositories the caller can read, most recent first.
func (a *API) blockedList(w http.ResponseWriter, r *http.Request) {
	p := auth.PrincipalFrom(r.Context())
	var names []string
	for _, rp := range a.reposByID(r) {
		if p.CanRepo(rp.Name, rp.Format, auth.Read) {
			names = append(names, rp.Name)
		}
	}
	type allowed struct {
		Reason string    `json:"reason"`
		By     string    `json:"by"`
		At     time.Time `json:"at"`
	}
	type row struct {
		PURL       string    `json:"purl"`
		Repository string    `json:"repository"`
		Format     string    `json:"format"`
		Name       string    `json:"name"`
		Version    string    `json:"version"`
		VulnID     string    `json:"id"`
		Summary    string    `json:"summary"`
		Attempts   int       `json:"attempts"`
		FirstAt    time.Time `json:"firstAt"`
		LastAt     time.Time `json:"lastAt"`
		LastUser   string    `json:"lastUser"`
		Allowed    *allowed  `json:"allowed"`
	}
	rows, err := a.Content.DB.Pool.Query(r.Context(), `
		SELECT b.purl, b.repository, b.format, b.name, b.version, b.vuln_id, b.summary, b.attempts,
		       b.first_at, b.last_at, b.last_user, al.reason, al.allowed_by, al.allowed_at
		  FROM malware_blocks b LEFT JOIN malware_allowed al ON al.purl = b.purl
		 WHERE b.repository = ANY($1)
		 ORDER BY b.last_at DESC LIMIT 1000`, names)
	if err != nil {
		a.fail(w, err)
		return
	}
	defer rows.Close()
	out := []row{}
	for rows.Next() {
		var x row
		var reason, by *string
		var at *time.Time
		if err := rows.Scan(&x.PURL, &x.Repository, &x.Format, &x.Name, &x.Version, &x.VulnID, &x.Summary, &x.Attempts,
			&x.FirstAt, &x.LastAt, &x.LastUser, &reason, &by, &at); err != nil {
			a.fail(w, err)
			return
		}
		if reason != nil {
			x.Allowed = &allowed{*reason, *by, *at}
		}
		out = append(out, x)
	}
	writeJSON(w, 200, map[string]any{"blocking": a.Guard != nil, "items": out})
}

// allowPackage lets one package version through the malicious-package
// check, in every repository, with the reason on record.
func (a *API) allowPackage(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PURL   string `json:"purl"`
		Reason string `json:"reason"`
	}
	if err := readJSON(r, &in); err != nil || in.PURL == "" || strings.TrimSpace(in.Reason) == "" {
		writeErr(w, 400, "validation", "purl and reason are required")
		return
	}
	user := auth.PrincipalFrom(r.Context()).Username
	if _, err := a.Content.DB.Pool.Exec(r.Context(), `
		INSERT INTO malware_allowed (purl, reason, allowed_by) VALUES ($1, $2, $3)
		ON CONFLICT (purl) DO UPDATE SET reason = EXCLUDED.reason, allowed_by = EXCLUDED.allowed_by, allowed_at = now()`,
		in.PURL, strings.TrimSpace(in.Reason), user); err != nil {
		a.fail(w, err)
		return
	}
	if a.Guard != nil {
		a.Guard.Forget(in.PURL)
	}
	a.audit_(r, "package.allow_malicious", "package", in.PURL, map[string]any{"reason": in.Reason})
	w.WriteHeader(http.StatusNoContent)
}

// disallowPackage withdraws an allowance: the package is refused again.
func (a *API) disallowPackage(w http.ResponseWriter, r *http.Request) {
	purl := r.URL.Query().Get("purl")
	if purl == "" {
		writeErr(w, 400, "validation", "purl is required")
		return
	}
	if _, err := a.Content.DB.Pool.Exec(r.Context(), `DELETE FROM malware_allowed WHERE purl = $1`, purl); err != nil {
		a.fail(w, err)
		return
	}
	if a.Guard != nil {
		a.Guard.Forget(purl)
	}
	a.audit_(r, "package.disallow_malicious", "package", purl, nil)
	w.WriteHeader(http.StatusNoContent)
}
