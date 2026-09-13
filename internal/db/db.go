// Package db owns the PostgreSQL connection pool and schema migrations.
// Queries are hand-written SQL kept next to the service that uses them.
package db

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type DB struct {
	Pool *pgxpool.Pool
}

func Open(ctx context.Context, url string, maxConns int32) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	if maxConns > 0 {
		cfg.MaxConns = maxConns
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pingWithRetry(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	return &DB{Pool: pool}, nil
}

// startupPingTimeout bounds how long Open waits for the database to answer.
// Kubernetes restarts a container the instant its sandbox is recreated, which
// can happen before CoreDNS is ready again: a real deployment exited twice
// because "nexus-postgres" briefly did not resolve. Ordering between a pod and
// its dependencies is never guaranteed, so wait rather than crash-loop.
const startupPingTimeout = 90 * time.Second

func pingWithRetry(ctx context.Context, pool *pgxpool.Pool) error {
	deadline := time.Now().Add(startupPingTimeout)
	delay := 250 * time.Millisecond
	for attempt := 1; ; attempt++ {
		err := pool.Ping(ctx)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil || time.Now().After(deadline) {
			return fmt.Errorf("ping database after %d attempts: %w", attempt, err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("ping database: %w", ctx.Err())
		case <-time.After(delay):
		}
		if delay < 5*time.Second {
			delay *= 2
		}
	}
}

func (d *DB) Close() { d.Pool.Close() }

// Migrate applies every embedded migration not yet recorded in
// schema_migrations, in filename order, each inside its own transaction.
func (d *DB) Migrate(ctx context.Context, log *slog.Logger) error {
	_, err := d.Pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`)
	if err != nil {
		return err
	}
	// Serialise concurrent starters (e.g. several replicas booting at once).
	conn, err := d.Pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(7419001)`); err != nil {
		return err
	}
	defer conn.Exec(ctx, `SELECT pg_advisory_unlock(7419001)`)

	applied := map[string]bool{}
	rows, err := conn.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return err
		}
		applied[v] = true
	}
	rows.Close()

	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		version := strings.TrimSuffix(name, ".sql")
		if applied[version] {
			continue
		}
		sqlBytes, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		log.Info("applying migration", "version", version)
		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("migration %s: %w", version, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES ($1)`, version); err != nil {
			tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Tx runs fn inside a transaction.
func (d *DB) Tx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

// IsNoRows reports whether err is pgx.ErrNoRows.
func IsNoRows(err error) bool { return err == pgx.ErrNoRows }
