// Package task runs periodic maintenance: blob garbage collection, cleanup
// policies, expired session/negative-cache pruning. Tasks are registered by
// name with an interval and can be triggered on demand via the API.
package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/holiaokho/holiaokho/internal/content"
	"github.com/holiaokho/holiaokho/internal/db"
	"github.com/holiaokho/holiaokho/internal/model"
	"github.com/holiaokho/holiaokho/internal/repo"
	"github.com/holiaokho/holiaokho/internal/storage"
)

type Func func(ctx context.Context, log func(string, ...any)) error

type Task struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Interval    time.Duration `json:"-"`
	IntervalStr string        `json:"interval"`
	// Cron, when set, replaces the interval (persisted in task_schedules).
	Cron       string     `json:"cron,omitempty"`
	Enabled    bool       `json:"enabled"`
	LastRun    *time.Time `json:"lastRun"`
	NextRun    *time.Time `json:"nextRun"`
	Running    bool       `json:"running"`
	LastStatus string     `json:"lastStatus,omitempty"`
	fn         Func
	sched      *Schedule
}

// Notifier is called when a task run fails (email/webhook); set by the server.
type Notifier func(taskName string, err error, log string)

type Scheduler struct {
	DB     *db.DB
	Log    *slog.Logger
	Notify Notifier

	mu     sync.Mutex
	tasks  map[string]*Task
	wake   chan string
	ctx    context.Context
	cancel context.CancelFunc
}

func NewScheduler(d *db.DB, log *slog.Logger) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{DB: d, Log: log, tasks: map[string]*Task{}, wake: make(chan string, 16), ctx: ctx, cancel: cancel}
}

func (s *Scheduler) Register(name, desc string, every time.Duration, fn Func) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := time.Now().Add(every)
	t := &Task{Name: name, Description: desc, Interval: every, IntervalStr: every.String(), Enabled: true, NextRun: &next, fn: fn}
	s.tasks[name] = t
	// Restore a persisted cron schedule, if any.
	var cron string
	var enabled bool
	if err := s.DB.Pool.QueryRow(context.Background(), `SELECT cron, enabled FROM task_schedules WHERE task_name=$1`, name).Scan(&cron, &enabled); err == nil {
		s.applySchedule(t, cron, enabled)
	}
}

func (s *Scheduler) applySchedule(t *Task, cron string, enabled bool) error {
	t.Enabled = enabled
	if cron == "" {
		t.Cron, t.sched = "", nil
		next := time.Now().Add(t.Interval)
		t.NextRun = &next
		return nil
	}
	sc, err := ParseCron(cron)
	if err != nil {
		return err
	}
	t.Cron, t.sched = cron, sc
	next := sc.Next(time.Now())
	t.NextRun = &next
	return nil
}

// SetSchedule sets (cron != "") or clears a task's cron schedule and persists it.
func (s *Scheduler) SetSchedule(ctx context.Context, name, cron string, enabled bool) error {
	s.mu.Lock()
	t := s.tasks[name]
	if t == nil {
		s.mu.Unlock()
		return fmt.Errorf("unknown task %q", name)
	}
	err := s.applySchedule(t, cron, enabled)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	_, err = s.DB.Pool.Exec(ctx, `INSERT INTO task_schedules(task_name, cron, enabled) VALUES ($1,$2,$3)
		ON CONFLICT (task_name) DO UPDATE SET cron=EXCLUDED.cron, enabled=EXCLUDED.enabled`, name, cron, enabled)
	return err
}

func (s *Scheduler) List() []Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *Scheduler) RunNow(name string) error {
	s.mu.Lock()
	_, ok := s.tasks[name]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("unknown task %q", name)
	}
	select {
	case s.wake <- name:
	default:
		return errors.New("scheduler busy")
	}
	return nil
}

func (s *Scheduler) Start() {
	go s.loop()
}

func (s *Scheduler) Stop() { s.cancel() }

func (s *Scheduler) loop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case name := <-s.wake:
			s.run(name)
		case <-ticker.C:
			now := time.Now()
			var due []string
			s.mu.Lock()
			for _, t := range s.tasks {
				if t.Enabled && t.NextRun != nil && !t.NextRun.After(now) && !t.Running {
					due = append(due, t.Name)
				}
			}
			s.mu.Unlock()
			for _, n := range due {
				s.run(n)
			}
		}
	}
}

func (s *Scheduler) run(name string) {
	s.mu.Lock()
	t := s.tasks[name]
	if t == nil || t.Running {
		s.mu.Unlock()
		return
	}
	t.Running = true
	s.mu.Unlock()

	go func() {
		ctx := s.ctx
		var runID int64
		var logBuf strings.Builder
		logf := func(f string, args ...any) {
			line := fmt.Sprintf(f, args...)
			s.Log.Info("task", "name", name, "msg", line)
			if logBuf.Len() < 64<<10 {
				logBuf.WriteString(time.Now().UTC().Format(time.RFC3339) + " " + line + "\n")
			}
		}
		s.DB.Pool.QueryRow(ctx, `INSERT INTO task_runs(task_name) VALUES ($1) RETURNING id`, name).Scan(&runID)
		err := t.fn(ctx, logf)
		status := "ok"
		if err != nil {
			status = "failed"
			logf("error: %v", err)
			if s.Notify != nil {
				s.Notify(name, err, logBuf.String())
			}
		}
		s.DB.Pool.Exec(ctx, `UPDATE task_runs SET finished_at=now(), status=$2, log=$3 WHERE id=$1`, runID, status, logBuf.String())
		now := time.Now()
		next := now.Add(t.Interval)
		s.mu.Lock()
		if t.sched != nil {
			next = t.sched.Next(now)
		}
		t.Running = false
		t.LastRun = &now
		t.NextRun = &next
		t.LastStatus = status
		s.mu.Unlock()
	}()
}

func (s *Scheduler) Runs(ctx context.Context, name string, limit int) ([]model.TaskRun, error) {
	q := `SELECT id, task_name, started_at, finished_at, status, log FROM task_runs`
	var args []any
	if name != "" {
		q += ` WHERE task_name=$1`
		args = append(args, name)
	}
	q += fmt.Sprintf(` ORDER BY started_at DESC LIMIT %d`, limit)
	rows, err := s.DB.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.TaskRun
	for rows.Next() {
		var r model.TaskRun
		if err := rows.Scan(&r.ID, &r.TaskName, &r.StartedAt, &r.FinishedAt, &r.Status, &r.Log); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// ------------------------------------------------------------ built-ins

// RegisterBuiltins wires the standard maintenance tasks.
func RegisterBuiltins(s *Scheduler, c *content.Service) {
	s.Register("blob-gc", "Soft-delete blobs no longer referenced by any asset (after a 1h grace period)", time.Hour, func(ctx context.Context, logf func(string, ...any)) error {
		return BlobGC(ctx, c, time.Hour, logf)
	})
	s.Register("compact-blobs", "Permanently remove soft-deleted blobs from storage (Nexus 'Compact blob store')", 24*time.Hour, func(ctx context.Context, logf func(string, ...any)) error {
		return CompactBlobs(ctx, c, 24*time.Hour, logf)
	})
	s.Register("cleanup-policies", "Apply cleanup policies to their repositories", 24*time.Hour, func(ctx context.Context, logf func(string, ...any)) error {
		return RunCleanupPolicies(ctx, c, logf)
	})
	s.Register("prune-expired", "Remove expired sessions, negative cache entries and stale task runs", time.Hour, func(ctx context.Context, logf func(string, ...any)) error {
		t1, _ := c.DB.Pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at < now()`)
		t2, _ := c.DB.Pool.Exec(ctx, `DELETE FROM assets WHERE negative AND cache_expires_at < now()`)
		t3, _ := c.DB.Pool.Exec(ctx, `DELETE FROM task_runs WHERE started_at < now() - interval '30 days'`)
		logf("sessions=%d negative=%d task_runs=%d", t1.RowsAffected(), t2.RowsAffected(), t3.RowsAffected())
		return nil
	})
	// A day, as Nexus does: long enough that a client pausing between chunks
	// or retrying after a network blip finds its upload still there.
	s.Register("delete-incomplete-uploads", "Delete uploads a client started and never finished, such as an interrupted docker push (untouched for 24h)", 24*time.Hour, func(ctx context.Context, logf func(string, ...any)) error {
		return SweepStorages(ctx, c, storage.Abandoned, 24*time.Hour, logf)
	})
	// An hour is safe because age runs from the last write: a transfer in
	// progress, however slow, keeps touching its file.
	s.Register("delete-temp-files", "Delete temporary files left behind when the server stopped in the middle of a write (untouched for 1h)", 24*time.Hour, func(ctx context.Context, logf func(string, ...any)) error {
		err := SweepStorages(ctx, c, storage.Scratch, time.Hour, logf)
		sw, serr := repo.SweepSpools(ctx, time.Now().Add(-time.Hour))
		logf("download spools: removed %d, %d bytes", sw.Items, sw.Bytes)
		return errors.Join(err, serr)
	})
}

// SweepStorages asks every storage that stages data outside its blob layout
// to remove leftovers of kind older than age. One storage failing does not
// stop the others.
func SweepStorages(ctx context.Context, c *content.Service, kind storage.Leftover, age time.Duration, logf func(string, ...any)) error {
	list, err := c.ListStorages(ctx)
	if err != nil {
		return err
	}
	cutoff := time.Now().Add(-age)
	var errs []error
	for _, m := range list {
		st, err := c.Store(m.ID)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", m.Name, err))
			continue
		}
		sw, ok := st.(storage.Sweeper)
		if !ok {
			continue
		}
		r, err := sw.Sweep(ctx, kind, cutoff)
		logf("%s: removed %d, %d bytes", m.Name, r.Items, r.Bytes)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", m.Name, err))
		}
	}
	return errors.Join(errs...)
}

// BlobGC soft-deletes unreferenced blobs older than grace. Bytes stay on
// disk until CompactBlobs runs, so an accidental deletion can be undone by
// re-referencing the blob (the row is revived automatically).
func BlobGC(ctx context.Context, c *content.Service, grace time.Duration, logf func(string, ...any)) error {
	tag, err := c.DB.Pool.Exec(ctx, `UPDATE blobs SET deleted_at = now() WHERE deleted_at IS NULL AND ref_count <= 0
		AND created_at < now() - $1::interval AND NOT EXISTS (SELECT 1 FROM assets a WHERE a.blob_digest = blobs.digest)`, grace.String())
	if err != nil {
		return err
	}
	// Revive blobs that were referenced again after being soft-deleted.
	rev, _ := c.DB.Pool.Exec(ctx, `UPDATE blobs SET deleted_at = NULL WHERE deleted_at IS NOT NULL AND ref_count > 0`)
	logf("soft-deleted %d blobs, revived %d", tag.RowsAffected(), rev.RowsAffected())
	return nil
}

// CompactBlobs permanently deletes blobs soft-deleted longer than retention ago.
func CompactBlobs(ctx context.Context, c *content.Service, retention time.Duration, logf func(string, ...any)) error {
	rows, err := c.DB.Pool.Query(ctx, `SELECT digest, storage_id, size FROM blobs WHERE deleted_at IS NOT NULL AND deleted_at < now() - $1::interval AND ref_count <= 0 LIMIT 20000`, retention.String())
	if err != nil {
		return err
	}
	type cand struct {
		d    string
		sid  uuid.UUID
		size int64
	}
	var cands []cand
	for rows.Next() {
		var x cand
		if err := rows.Scan(&x.d, &x.sid, &x.size); err != nil {
			rows.Close()
			return err
		}
		cands = append(cands, x)
	}
	rows.Close()
	var n int
	var bytes int64
	for _, x := range cands {
		tag, err := c.DB.Pool.Exec(ctx, `DELETE FROM blobs WHERE digest=$1 AND ref_count <= 0 AND deleted_at IS NOT NULL AND NOT EXISTS (SELECT 1 FROM assets WHERE blob_digest=$1)`, x.d)
		if err != nil || tag.RowsAffected() == 0 {
			continue
		}
		st, err := c.Store(x.sid)
		if err != nil {
			continue
		}
		if err := st.Delete(ctx, storage.Digest(x.d)); err != nil {
			logf("delete %s: %v", x.d, err)
			continue
		}
		n++
		bytes += x.size
	}
	logf("removed %d blobs, %d bytes reclaimed", n, bytes)
	return nil
}

// ------------------------------------------------------- cleanup policies

type CleanupPolicyView struct {
	model.CleanupPolicy
	Repositories []string `json:"repositories"`
}

func ListCleanupPolicies(ctx context.Context, d *db.DB) ([]CleanupPolicyView, error) {
	rows, err := d.Pool.Query(ctx, `SELECT p.id, p.name, p.format, p.criteria,
		coalesce((SELECT array_agg(r.name ORDER BY r.name) FROM repo_cleanup_policies rc JOIN repositories r ON r.id=rc.repo_id WHERE rc.policy_id=p.id), '{}')
		FROM cleanup_policies p ORDER BY p.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CleanupPolicyView
	for rows.Next() {
		var v CleanupPolicyView
		var crit []byte
		if err := rows.Scan(&v.ID, &v.Name, &v.Format, &crit, &v.Repositories); err != nil {
			return nil, err
		}
		json.Unmarshal(crit, &v.Criteria)
		out = append(out, v)
	}
	return out, nil
}

func SaveCleanupPolicy(ctx context.Context, d *db.DB, p *model.CleanupPolicy, create bool) error {
	if p.Criteria.NameRegex != "" {
		if _, err := regexp.Compile(p.Criteria.NameRegex); err != nil {
			return fmt.Errorf("nameRegex: %w", err)
		}
	}
	if p.Criteria.VersionRegex != "" {
		if _, err := regexp.Compile(p.Criteria.VersionRegex); err != nil {
			return fmt.Errorf("versionRegex: %w", err)
		}
	}
	crit, _ := json.Marshal(p.Criteria)
	if create {
		_, err := d.Pool.Exec(ctx, `INSERT INTO cleanup_policies(id,name,format,criteria) VALUES ($1,$2,$3,$4)`, p.ID, p.Name, p.Format, crit)
		if err != nil && strings.Contains(err.Error(), "23505") {
			return content.ErrConflict
		}
		return err
	}
	tag, err := d.Pool.Exec(ctx, `UPDATE cleanup_policies SET name=$2, format=$3, criteria=$4 WHERE id=$1`, p.ID, p.Name, p.Format, crit)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return content.ErrNotFound
	}
	return nil
}

// VersionLess is injected by the server so cleanup can order versions per
// format without importing the format packages here.
var VersionLess = func(format, a, b string) bool { return a < b }

// PreviewCleanupPolicy lists the packages a policy would delete on a repo
// without deleting anything (Nexus "cleanup preview").
func PreviewCleanupPolicy(ctx context.Context, c *content.Service, rp *model.Repository, p *model.CleanupPolicy, limit int) ([]model.Package, error) {
	ids, err := matchPolicy(ctx, c, rp, p)
	if err != nil {
		return nil, err
	}
	var out []model.Package
	for i, id := range ids {
		if limit > 0 && i >= limit {
			break
		}
		if pk, err := c.PackageByID(ctx, id); err == nil {
			out = append(out, *pk)
		}
	}
	return out, nil
}

// RunCleanupPolicies deletes packages matching each policy assigned to a repo.
func RunCleanupPolicies(ctx context.Context, c *content.Service, logf func(string, ...any)) error {
	policies, err := ListCleanupPolicies(ctx, c.DB)
	if err != nil {
		return err
	}
	total := 0
	for _, p := range policies {
		for _, repoName := range p.Repositories {
			rp, err := c.Repo(ctx, repoName)
			if err != nil {
				continue
			}
			if p.Format != "" && p.Format != rp.Format {
				continue
			}
			n, err := applyPolicy(ctx, c, rp, &p.CleanupPolicy)
			if err != nil {
				logf("policy %s on %s: %v", p.Name, repoName, err)
				continue
			}
			if n > 0 {
				logf("policy %s on %s: removed %d packages", p.Name, repoName, n)
			}
			total += n
		}
	}
	logf("removed %d packages in total", total)
	return nil
}

func applyPolicy(ctx context.Context, c *content.Service, rp *model.Repository, p *model.CleanupPolicy) (int, error) {
	ids, err := matchPolicy(ctx, c, rp, p)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		if err := c.DeletePackage(ctx, id); err == nil {
			n++
		}
	}
	return n, nil
}

// matchPolicy returns the package IDs a policy selects in a repository.
func matchPolicy(ctx context.Context, c *content.Service, rp *model.Repository, p *model.CleanupPolicy) ([]int64, error) {
	cr := p.Criteria
	var where []string
	var args []any
	args = append(args, rp.ID)
	where = append(where, "repo_id=$1")
	if cr.LastDownloadedDays > 0 {
		args = append(args, fmt.Sprintf("%d days", cr.LastDownloadedDays))
		where = append(where, fmt.Sprintf("coalesce(last_downloaded_at, created_at) < now() - $%d::interval", len(args)))
	}
	if cr.LastUpdatedDays > 0 {
		args = append(args, fmt.Sprintf("%d days", cr.LastUpdatedDays))
		where = append(where, fmt.Sprintf("updated_at < now() - $%d::interval", len(args)))
	}
	if cr.NameRegex != "" {
		args = append(args, cr.NameRegex)
		where = append(where, fmt.Sprintf("(namespace || '/' || name) ~ $%d", len(args)))
	}
	if cr.VersionRegex != "" {
		args = append(args, cr.VersionRegex)
		where = append(where, fmt.Sprintf("version ~ $%d", len(args)))
	}
	if cr.Prerelease == "true" {
		where = append(where, "(version ~ '-' OR version ~* 'snapshot')")
	} else if cr.Prerelease == "false" {
		where = append(where, "NOT (version ~ '-' OR version ~* 'snapshot')")
	}
	rows, err := c.DB.Pool.Query(ctx, `SELECT id, namespace, name, version FROM packages WHERE `+strings.Join(where, " AND "), args...)
	if err != nil {
		return nil, err
	}
	type pk struct {
		id      int64
		ns, nm  string
		version string
	}
	var cands []pk
	for rows.Next() {
		var x pk
		if err := rows.Scan(&x.id, &x.ns, &x.nm, &x.version); err != nil {
			rows.Close()
			return nil, err
		}
		cands = append(cands, x)
	}
	rows.Close()
	if cr.KeepLatest > 0 {
		// Group by name, keep the newest N versions of each.
		byName := map[string][]pk{}
		for _, x := range cands {
			byName[x.ns+"/"+x.nm] = append(byName[x.ns+"/"+x.nm], x)
		}
		cands = cands[:0]
		for _, list := range byName {
			sort.Slice(list, func(i, j int) bool { return VersionLess(rp.Format, list[j].version, list[i].version) }) // newest first
			if len(list) > cr.KeepLatest {
				cands = append(cands, list[cr.KeepLatest:]...)
			}
		}
	}
	ids := make([]int64, 0, len(cands))
	for _, x := range cands {
		ids = append(ids, x.id)
	}
	return ids, nil
}
