package vuln

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/holiaokho/holiaokho/internal/content"
	"github.com/holiaokho/holiaokho/internal/format/nuget"
	"github.com/holiaokho/holiaokho/internal/storage"
)

// Rescan is how often a package is checked again. Packages do not change,
// but the list of known vulnerabilities does, daily.
const Rescan = 24 * time.Hour

// rescanSlack is how much earlier than Rescan a result counts as due.
//
// Without it a daily schedule checks each package every other day: a run
// stamps its packages as it finishes — 02:00:40, say — and the next day's
// run starts at 02:00:00, when they are 23h59m old and not yet due, so they
// wait for the day after. Anything from hourly to daily finds yesterday's
// results due with a few hours' margin.
const rescanSlack = 4 * time.Hour

// dueBefore is the check time before which a package is due again.
func dueBefore(now time.Time) time.Time { return now.Add(-(Rescan - rescanSlack)) }

// Finding is one vulnerability in one stored package.
type Finding struct {
	PackageID  int64      `json:"packageId"`
	Repository string     `json:"repository"`
	Format     string     `json:"format"`
	Namespace  string     `json:"namespace"`
	Name       string     `json:"name"`
	Version    string     `json:"version"`
	VulnID     string     `json:"id"`
	Aliases    []string   `json:"aliases"`
	Summary    string     `json:"summary"`
	Severity   string     `json:"severity"`
	Score      *float64   `json:"score"`
	FixedIn    []string   `json:"fixedIn"`
	Published  *time.Time `json:"published"`
	FirstSeen  time.Time  `json:"firstSeen"`
	// LastUsed is when the package was last downloaded, or stored if it
	// never has been: whether anyone still depends on it.
	LastUsed time.Time `json:"lastUsed"`

	attrs json.RawMessage // for the purl: a NuGet id's casing lives here
}

// Scanner checks stored packages against OSV.
type Scanner struct {
	Content *content.Service
	OSV     *OSV
	Log     *slog.Logger
	// PerRun caps how many packages one run checks, so a first run on a
	// large instance is spread over several instead of one very long one.
	// Newly arrived packages always go first.
	PerRun int
	// MinNotify is the lowest severity passed to Announce.
	MinNotify string
	// Announce receives findings seen for the first time, at or above
	// MinNotify, once per run. Nil disables announcements.
	Announce func(ctx context.Context, found []Finding) error

	// store saves a fetched record; nil means the database. Tests replace it.
	store func(ctx context.Context, rec *Record, severity string, score *float64, fixed map[string][]string) error
}

type due struct {
	id                          int64
	format, ns, name, ver, purl string
	attrs                       json.RawMessage
}

// Run checks the packages that are due. It fails quietly — recording the
// error for the UI rather than failing the task — when OSV cannot be
// reached: a repository manager inside a network with no route out is a
// normal deployment, and one that should not raise an alarm every hour.
func (s *Scanner) Run(ctx context.Context, logf func(string, ...any)) error {
	db := s.Content.DB.Pool
	db.Exec(ctx, `UPDATE vuln_scan_state SET last_run_at = now()`)

	pkgs, err := s.due(ctx)
	if err != nil {
		return err
	}
	if len(pkgs) == 0 {
		logf("nothing due")
		db.Exec(ctx, `UPDATE vuln_scan_state SET last_ok_at = now(), last_error = ''`)
		return s.announce(ctx, logf)
	}

	// Build each package's purl; one query per distinct purl however many
	// repositories hold the same package.
	byPURL := map[string][]int64{}
	var uncovered []int64
	for i := range pkgs {
		p := &pkgs[i]
		if p.format == "nuget" && NuGetID(p.attrs) == "" {
			if id := s.nugetID(ctx, p.id); id != "" {
				p.attrs, _ = json.Marshal(map[string]string{"canonicalId": id})
				db.Exec(ctx, `UPDATE packages SET attrs = attrs || jsonb_build_object('canonicalId', $2::text) WHERE id = $1`, p.id, id)
			}
		}
		purl, ok := PURL(p.format, p.ns, p.name, p.ver, p.attrs)
		if !ok {
			uncovered = append(uncovered, p.id)
			continue
		}
		p.purl = purl
		byPURL[purl] = append(byPURL[purl], p.id)
	}
	purls := make([]string, 0, len(byPURL))
	for u := range byPURL {
		purls = append(purls, u)
	}

	matches, err := s.OSV.QueryBatch(ctx, purls)
	if err != nil {
		s.unreachable(ctx, err, logf)
		return nil
	}

	// Fetch the records not seen before, or changed since.
	want := map[string]time.Time{}
	for _, ms := range matches {
		for _, m := range ms {
			if t, ok := want[m.ID]; !ok || m.Modified.After(t) {
				want[m.ID] = m.Modified
			}
		}
	}
	recs, err := s.records(ctx, want)
	if err != nil {
		s.unreachable(ctx, err, logf)
		return nil
	}

	// Results, per package.
	var clean []int64
	batch := &pgx.Batch{}
	var withFindings, total int
	for i, u := range purls {
		groups := s.collapse(ctx, matches[i], recs)
		ids := byPURL[u]
		if len(groups) == 0 {
			clean = append(clean, ids...)
			continue
		}
		withFindings += len(ids)
		total += len(groups) * len(ids)
		base := samePURL(u[:strings.LastIndexByte(u, '@')])
		type row struct {
			V string   `json:"v"`
			F []string `json:"f"`
		}
		rows := make([]row, 0, len(groups))
		keep := make([]string, 0, len(groups))
		for _, g := range groups {
			rows = append(rows, row{g.rep, recs.fixed(g.members, base)})
			keep = append(keep, g.rep)
		}
		js, _ := json.Marshal(rows)
		for _, id := range ids {
			batch.Queue(`DELETE FROM package_vulnerabilities WHERE package_id = $1 AND NOT (vuln_id = ANY($2))`, id, keep)
			batch.Queue(`INSERT INTO package_vulnerabilities (package_id, vuln_id, fixed_in)
				SELECT $1, x.v, ARRAY(SELECT jsonb_array_elements_text(x.f))
				FROM jsonb_to_recordset($2::jsonb) AS x(v text, f jsonb)
				ON CONFLICT (package_id, vuln_id) DO UPDATE SET fixed_in = EXCLUDED.fixed_in`, id, string(js))
			batch.Queue(`UPDATE packages SET vuln_scanned_at = now(), vuln_covered = true WHERE id = $1`, id)
		}
	}
	if batch.Len() > 0 {
		if err := db.SendBatch(ctx, batch).Close(); err != nil {
			return err
		}
	}
	// The common case — no findings — in two statements, not two per package.
	if len(clean) > 0 {
		if _, err := db.Exec(ctx, `DELETE FROM package_vulnerabilities WHERE package_id = ANY($1)`, clean); err != nil {
			return err
		}
		if _, err := db.Exec(ctx, `UPDATE packages SET vuln_scanned_at = now(), vuln_covered = true WHERE id = ANY($1)`, clean); err != nil {
			return err
		}
	}
	if len(uncovered) > 0 {
		if _, err := db.Exec(ctx, `UPDATE packages SET vuln_scanned_at = now(), vuln_covered = false WHERE id = ANY($1)`, uncovered); err != nil {
			return err
		}
	}
	db.Exec(ctx, `UPDATE vuln_scan_state SET last_ok_at = now(), last_error = ''`)
	logf("checked %d packages (%d distinct): %d with known vulnerabilities, %d findings; %d could not be checked; %d records fetched",
		len(pkgs), len(purls), withFindings, total, len(uncovered), recs.fetched)
	return s.announce(ctx, logf)
}

func (s *Scanner) unreachable(ctx context.Context, err error, logf func(string, ...any)) {
	msg := err.Error()
	s.Content.DB.Pool.Exec(ctx, `UPDATE vuln_scan_state SET last_error = $1`, msg)
	s.Log.Warn("vulnerability scan could not reach OSV", "err", err)
	logf("could not reach OSV, will try again next run: %s", msg)
}

// due returns the packages to check this run, newest arrivals first.
func (s *Scanner) due(ctx context.Context) ([]due, error) {
	limit := s.PerRun
	if limit <= 0 {
		limit = 10000
	}
	rows, err := s.Content.DB.Pool.Query(ctx, `
		SELECT p.id, r.format, p.namespace, p.name, p.version, p.attrs
		FROM packages p JOIN repositories r ON r.id = p.repo_id
		WHERE r.type IN ('proxy', 'hosted')
		  AND r.format = ANY($1)
		  AND coalesce((r.attributes -> 'vulnerabilities' ->> 'scan')::boolean, true)
		  AND (p.vuln_scanned_at IS NULL OR p.vuln_scanned_at < $2)
		ORDER BY p.vuln_scanned_at NULLS FIRST, p.id DESC
		LIMIT $3`, CoveredFormats(), dueBefore(time.Now()), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.id, &d.format, &d.ns, &d.name, &d.ver, &d.attrs); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// recordSet is the vulnerability records a run works with, by id.
type recordSet struct {
	mu      sync.Mutex
	byID    map[string]stored
	fetched int
}

type stored struct {
	aliases  []string
	severity string
	fixed    map[string][]string
}

// fixed is the union of the versions the group's records say fix the
// package — they describe the same bug, and not every database lists every
// fix.
func (r *recordSet) fixed(members []string, purlNoVersion string) []string {
	out := []string{}
	for _, id := range members {
		for _, v := range r.byID[id].fixed[purlNoVersion] {
			if !contains(out, v) {
				out = append(out, v)
			}
		}
	}
	return out
}

// records loads the wanted records from the database, fetching from OSV
// only those not stored yet or modified since they were. A rescan of
// unchanged packages therefore costs one querybatch call and no fetches.
func (s *Scanner) records(ctx context.Context, want map[string]time.Time) (*recordSet, error) {
	rs := &recordSet{byID: map[string]stored{}}
	ids := make([]string, 0, len(want))
	for id := range want {
		ids = append(ids, id)
	}
	rows, err := s.Content.DB.Pool.Query(ctx, `SELECT id, aliases, severity, fixed, coalesce(modified, 'epoch') FROM vulnerabilities WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	var fetch []string
	have := map[string]bool{}
	for rows.Next() {
		var st stored
		var id string
		var mod time.Time
		if err := rows.Scan(&id, &st.aliases, &st.severity, &st.fixed, &mod); err != nil {
			rows.Close()
			return nil, err
		}
		have[id] = true
		if want[id].After(mod) {
			fetch = append(fetch, id)
			continue
		}
		rs.byID[id] = st
	}
	rows.Close()
	for _, id := range ids {
		if !have[id] {
			fetch = append(fetch, id)
		}
	}
	return rs, s.fetch(ctx, rs, fetch)
}

// fetch reads records from OSV with a few requests in flight, stores them
// and adds them to rs. Withdrawn records are skipped: they were published in
// error.
func (s *Scanner) fetch(ctx context.Context, rs *recordSet, ids []string) error {
	const workers = 4
	jobs := make(chan string)
	var firstErr error
	var errOnce sync.Once
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				rec, err := s.OSV.Get(ctx, id)
				if err != nil {
					errOnce.Do(func() { firstErr = err })
					continue
				}
				if rec.Withdrawn != nil {
					if s.Content != nil {
						s.Content.DB.Pool.Exec(ctx, `DELETE FROM vulnerabilities WHERE id = $1`, rec.ID)
					}
					continue
				}
				sev, score := rec.rating()
				fixed := rec.fixedVersions()
				save := s.store
				if save == nil {
					save = s.save
				}
				if err := save(ctx, rec, sev, score, fixed); err != nil {
					errOnce.Do(func() { firstErr = err })
					continue
				}
				rs.mu.Lock()
				rs.byID[rec.ID] = stored{nonNil(rec.Aliases), sev, fixed}
				rs.fetched++
				rs.mu.Unlock()
			}
		}()
	}
	for _, id := range ids {
		select {
		case jobs <- id:
		case <-ctx.Done():
		}
	}
	close(jobs)
	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return firstErr
}

func (s *Scanner) save(ctx context.Context, rec *Record, sev string, score *float64, fixed map[string][]string) error {
	var pub *time.Time
	if !rec.Published.IsZero() {
		pub = &rec.Published
	}
	_, err := s.Content.DB.Pool.Exec(ctx, `
		INSERT INTO vulnerabilities (id, aliases, summary, severity, score, published, modified, fixed, fetched_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now())
		ON CONFLICT (id) DO UPDATE SET aliases = EXCLUDED.aliases, summary = EXCLUDED.summary,
			severity = EXCLUDED.severity, score = EXCLUDED.score, published = EXCLUDED.published,
			modified = EXCLUDED.modified, fixed = EXCLUDED.fixed, fetched_at = now()`,
		rec.ID, nonNil(rec.Aliases), summaryOf(rec), sev, score, pub, rec.Modified, fixed)
	return err
}

// group is one actual vulnerability: the records that describe it, and the
// one that represents them.
type group struct {
	rep     string
	members []string
}

// collapse turns the ids OSV matched for one package into one group per
// actual vulnerability.
//
// The same issue is usually published by several databases — GO-2021-0113,
// GHSA-ppp9-7jff-5vj2 and CVE-2021-38561 are one bug — and OSV returns each.
// Counted separately, every number shown would be inflated two- or
// three-fold. Records that name each other as aliases are one vulnerability,
// represented by one with a severity rating, a GitHub advisory when there is
// a choice, since those carry the most complete data.
//
// When none of a group's records has a rating, its GitHub alias is fetched
// to borrow one: Go's and PyPI's own databases often omit it.
func (s *Scanner) collapse(ctx context.Context, ms []match, rs *recordSet) []group {
	parent := map[string]string{}
	var find func(string) string
	find = func(x string) string {
		p, ok := parent[x]
		if !ok || p == x {
			parent[x] = x
			return x
		}
		root := find(p)
		parent[x] = root
		return root
	}
	var matched []string
	for _, m := range ms {
		if _, ok := rs.byID[m.ID]; ok { // not withdrawn, fetched
			matched = append(matched, m.ID)
			find(m.ID)
		}
	}
	for _, id := range matched {
		for _, a := range rs.byID[id].aliases {
			parent[find(a)] = find(id)
		}
	}
	byRoot := map[string][]string{}
	for _, id := range matched {
		r := find(id)
		byRoot[r] = append(byRoot[r], id)
	}

	var out []group
	for _, members := range byRoot {
		if SeverityRank(rs.byID[pick(members, rs)].severity) == 0 {
			var borrow []string
			for _, id := range members {
				for _, a := range rs.byID[id].aliases {
					if _, ok := rs.byID[a]; !ok && strings.HasPrefix(a, "GHSA-") && !contains(borrow, a) {
						borrow = append(borrow, a)
					}
				}
			}
			if len(borrow) > 0 && s.fetch(ctx, rs, borrow) == nil {
				for _, a := range borrow {
					if _, ok := rs.byID[a]; ok {
						members = append(members, a)
					}
				}
			}
		}
		out = append(out, group{pick(members, rs), members})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].rep < out[j].rep })
	return out
}

// pick chooses the id that represents a group of aliases.
func pick(ids []string, rs *recordSet) string {
	weight := func(id string) int {
		n := 0
		if SeverityRank(rs.byID[id].severity) > 0 {
			n += 2
		}
		if strings.HasPrefix(id, "GHSA-") {
			n++
		}
		return n
	}
	best := ""
	for _, id := range ids {
		if best == "" || weight(id) > weight(best) || (weight(id) == weight(best) && id < best) {
			best = id
		}
	}
	return best
}

func summaryOf(r *Record) string {
	if r.Summary != "" {
		return r.Summary
	}
	// Some databases have no summary; the first line of the details is the
	// closest thing to one.
	d := strings.TrimSpace(r.Details)
	if i := strings.IndexByte(d, '\n'); i > 0 {
		d = d[:i]
	}
	if len(d) > 200 {
		d = d[:200] + "…"
	}
	return d
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// nugetID reads a proxied NuGet package's id, with its published casing,
// from the .nuspec inside the stored .nupkg. Only the zip directory and the
// .nuspec are read, not the whole package, which can run to hundreds of MB.
func (s *Scanner) nugetID(ctx context.Context, packageID int64) string {
	var digest string
	var size int64
	err := s.Content.DB.Pool.QueryRow(ctx, `
		SELECT blob_digest, size FROM assets
		WHERE package_id = $1 AND path LIKE '%.nupkg' AND blob_digest IS NOT NULL AND NOT negative
		LIMIT 1`, packageID).Scan(&digest, &size)
	if err != nil || size <= 0 {
		return ""
	}
	zr, err := zip.NewReader(&blobReaderAt{ctx: ctx, c: s.Content, d: storage.Digest(digest)}, size)
	if err != nil {
		return ""
	}
	for _, f := range zr.File {
		if path.Dir(f.Name) != "." || !strings.HasSuffix(strings.ToLower(f.Name), ".nuspec") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return ""
		}
		b, err := io.ReadAll(io.LimitReader(rc, 1<<20))
		rc.Close()
		if err != nil {
			return ""
		}
		ns, err := nuget.ParseNuspec(b)
		if err != nil {
			return ""
		}
		return ns.ID
	}
	return ""
}

// blobReaderAt reads a stored blob by range, for archive/zip.
type blobReaderAt struct {
	ctx context.Context
	c   *content.Service
	d   storage.Digest
}

func (b *blobReaderAt) ReadAt(p []byte, off int64) (int, error) {
	rc, err := b.c.OpenBlobRange(b.ctx, b.d, off, int64(len(p)))
	if err != nil {
		return 0, err
	}
	defer rc.Close()
	n, err := io.ReadFull(rc, p)
	if errors.Is(err, io.ErrUnexpectedEOF) {
		err = io.EOF
	}
	return n, err
}

// announce passes findings not announced yet, at or above MinNotify, to
// Announce, and marks them announced. Below the threshold they are left
// unmarked, so lowering it later announces them then.
func (s *Scanner) announce(ctx context.Context, logf func(string, ...any)) error {
	if s.Announce == nil {
		return nil
	}
	min := SeverityRank(s.MinNotify)
	if min == 0 {
		min = SeverityRank(SeverityHigh)
	}
	var all []uuid.UUID
	if err := s.Content.DB.Pool.QueryRow(ctx, `SELECT coalesce(array_agg(id), '{}') FROM repositories`).Scan(&all); err != nil {
		return err
	}
	found, err := Findings(ctx, s.Content, Filter{RepoIDs: all, unnotified: true})
	if err != nil {
		return err
	}
	var out []Finding
	for _, f := range found {
		if SeverityRank(f.Severity) >= min {
			out = append(out, f)
		}
	}
	if len(out) == 0 {
		return nil
	}
	if err := s.Announce(ctx, out); err != nil {
		return fmt.Errorf("announce: %w", err)
	}
	b := &pgx.Batch{}
	for _, f := range out {
		b.Queue(`UPDATE package_vulnerabilities SET notified_at = now() WHERE package_id = $1 AND vuln_id = $2`, f.PackageID, f.VulnID)
	}
	if err := s.Content.DB.Pool.SendBatch(ctx, b).Close(); err != nil {
		return err
	}
	logf("announced %d new findings", len(out))
	return nil
}
