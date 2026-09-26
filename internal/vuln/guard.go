package vuln

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/holiaokho/holiaokho/internal/auth"
	"github.com/holiaokho/holiaokho/internal/content"
	"github.com/holiaokho/holiaokho/internal/model"
	"github.com/holiaokho/holiaokho/internal/repo"
)

const (
	// guardTTL is how long a verdict is trusted. A package listed as
	// malicious after it was checked is refused within the hour.
	guardTTL = time.Hour
	// guardUnreachableTTL is how long a download goes unchecked after OSV
	// could not be asked, so an instance with no route out does not wait
	// on every request.
	guardUnreachableTTL = time.Minute
	// guardTimeout bounds the lookup a first download waits for.
	guardTimeout = 5 * time.Second
	// guardCacheMax bounds the verdict cache; past it the cache starts over.
	guardCacheMax = 100_000
)

// Block is one refused download, as passed to OnFirstBlock.
type Block struct {
	PURL       string `json:"purl"`
	Repository string `json:"repository"`
	Format     string `json:"format"`
	Name       string `json:"name"`
	Version    string `json:"version"`
	VulnID     string `json:"id"`
	Summary    string `json:"summary"`
	User       string `json:"user"`
}

// Guard refuses downloads of packages known to be malicious. A scan finds
// a malicious package after it has been stored — after a build has already
// run it. The guard is asked before: on every fetch of a package, from the
// cache or from upstream.
//
// A package the daily scan has checked is judged from that result. One not
// seen before is looked up in OSV on its first request, once however many
// requests arrive together, and the verdict is kept for an hour. When OSV
// cannot be reached the download goes ahead: a repository manager that
// fails closed stops every build whenever its route out does.
type Guard struct {
	Content *content.Service
	OSV     *OSV
	Log     *slog.Logger
	// OnFirstBlock hears of each package version the first time it is
	// refused in a repository.
	OnFirstBlock func(ctx context.Context, b Block)

	mu    sync.Mutex
	cache map[string]verdict
	sf    singleflight.Group
	// query replaces OSV lookups in tests.
	query func(ctx context.Context, purl string) ([]Record, error)
}

type verdict struct {
	purl    string
	id      string // the malicious record; "" when clean
	summary string
	allowed bool
	until   time.Time
}

// Check is the engine's Gate: nil to serve, a *repo.BlockedError to refuse.
func (g *Guard) Check(ctx context.Context, rp *model.Repository, pkg *model.Package) error {
	if pkg == nil || pkg.Version == "" || !ScanEnabled(rp.Attributes) {
		return nil
	}
	v := g.verdict(ctx, rp.Format, pkg)
	if v.id == "" || v.allowed {
		return nil
	}
	name := pkg.Name
	if pkg.Namespace != "" {
		sep := "/"
		if rp.Format == "maven" {
			sep = ":"
		}
		name = pkg.Namespace + sep + pkg.Name
	}
	g.record(ctx, rp, name, pkg.Version, v)
	return &repo.BlockedError{Package: name + "@" + pkg.Version, ID: v.id, Summary: v.summary}
}

// Forget drops the cached verdicts for purl, after it was allowed or the
// allowance withdrawn.
func (g *Guard) Forget(purl string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for k, v := range g.cache {
		if v.purl == purl {
			delete(g.cache, k)
		}
	}
}

func (g *Guard) verdict(ctx context.Context, format string, pkg *model.Package) verdict {
	key := strings.Join([]string{format, pkg.Namespace, pkg.Name, pkg.Version}, "\x00")
	g.mu.Lock()
	if v, ok := g.cache[key]; ok && time.Now().Before(v.until) {
		g.mu.Unlock()
		return v
	}
	g.mu.Unlock()
	res, _, _ := g.sf.Do(key, func() (any, error) {
		v := g.judge(ctx, format, pkg)
		g.mu.Lock()
		if g.cache == nil || len(g.cache) >= guardCacheMax {
			g.cache = map[string]verdict{}
		}
		g.cache[key] = v
		g.mu.Unlock()
		return v, nil
	})
	return res.(verdict)
}

// judge decides, from the last scan when there is a recent one, otherwise
// from OSV.
func (g *Guard) judge(ctx context.Context, format string, pkg *model.Package) verdict {
	now := time.Now()
	purl, covered := PURL(format, pkg.Namespace, pkg.Name, pkg.Version, pkg.Attrs)
	v := verdict{purl: purl, until: now.Add(guardTTL)}
	db := g.Content.DB.Pool

	// What the scan found for this version, in any repository.
	var scanned *time.Time
	var id, summary *string
	err := db.QueryRow(ctx, `
		WITH ps AS (
			SELECT p.id, p.vuln_scanned_at FROM packages p JOIN repositories r ON r.id = p.repo_id
			 WHERE r.format = $1 AND p.namespace = $2 AND p.name = $3 AND p.version = $4),
		m AS (
			SELECT v.id, v.summary FROM ps
			  JOIN package_vulnerabilities pv ON pv.package_id = ps.id
			  JOIN vulnerabilities v ON v.id = pv.vuln_id
			 WHERE v.malicious ORDER BY v.id LIMIT 1)
		SELECT (SELECT max(vuln_scanned_at) FROM ps), (SELECT id FROM m), (SELECT summary FROM m)`,
		format, pkg.Namespace, pkg.Name, pkg.Version).Scan(&scanned, &id, &summary)
	if err != nil {
		g.Log.Warn("malicious-package check: database", "err", err)
	}
	if id != nil {
		v.id = *id
	}
	if summary != nil {
		v.summary = *summary
	}
	switch {
	case v.id != "":
		// Listed as malicious by the scan.
	case scanned != nil && scanned.After(dueBefore(now)):
		return v // checked recently and clean
	case !covered:
		return v // OSV cannot be asked about this package
	default:
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), guardTimeout)
		defer cancel()
		q := g.query
		if q == nil {
			q = g.OSV.Query
		}
		recs, err := q(ctx, purl)
		if err != nil {
			g.Log.Warn("malicious-package check skipped: OSV unreachable", "purl", purl, "err", err)
			v.until = now.Add(guardUnreachableTTL)
			return v
		}
		for _, r := range recs {
			if r.Withdrawn == nil && r.Malicious() {
				v.id, v.summary = r.ID, summaryOf(&r)
				break
			}
		}
	}
	if v.id != "" && purl != "" {
		var n int
		db.QueryRow(ctx, `SELECT count(*) FROM malware_allowed WHERE purl = $1`, purl).Scan(&n)
		v.allowed = n > 0
	}
	return v
}

// record counts the refusal and, the first time, passes it on.
func (g *Guard) record(ctx context.Context, rp *model.Repository, name, version string, v verdict) {
	user := ""
	if p := auth.PrincipalFrom(ctx); p != nil {
		user = p.Username
	}
	purl := v.purl
	if purl == "" {
		purl = rp.Format + ":" + name + "@" + version
	}
	var attempts int
	err := g.Content.DB.Pool.QueryRow(context.WithoutCancel(ctx), `
		INSERT INTO malware_blocks (purl, repository, format, name, version, vuln_id, summary, last_user)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (purl, repository) DO UPDATE SET attempts = malware_blocks.attempts + 1, last_at = now(),
			last_user = EXCLUDED.last_user, vuln_id = EXCLUDED.vuln_id, summary = EXCLUDED.summary
		RETURNING attempts`, purl, rp.Name, rp.Format, name, version, v.id, v.summary, user).Scan(&attempts)
	if err != nil {
		g.Log.Warn("malicious-package block not recorded", "purl", purl, "err", err)
		return
	}
	g.Log.Warn("download blocked: known malicious package", "purl", purl, "repo", rp.Name, "id", v.id, "user", user)
	if attempts == 1 && g.OnFirstBlock != nil {
		b := Block{PURL: purl, Repository: rp.Name, Format: rp.Format, Name: name, Version: version, VulnID: v.id, Summary: v.summary, User: user}
		go g.OnFirstBlock(context.WithoutCancel(ctx), b)
	}
}
