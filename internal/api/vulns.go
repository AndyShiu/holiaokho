package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/holiaokho/holiaokho/internal/auth"
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
