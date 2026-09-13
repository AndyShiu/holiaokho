// Package backup exports and restores the database (every public table as
// CSV via COPY) plus, optionally, the blobs, into a single tar.gz. It needs
// no external tools, so it works inside the minimal container image.
package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/holiaokho/holiaokho/internal/content"
	"github.com/holiaokho/holiaokho/internal/storage"
)

// Manifest describes a backup archive.
type Manifest struct {
	CreatedAt time.Time `json:"createdAt"`
	Version   string    `json:"version"`
	Tables    []string  `json:"tables"`
	Blobs     bool      `json:"blobs"`
	BlobCount int       `json:"blobCount"`
}

// listTables returns public tables in dependency-safe order (parents first).
func listTables(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	rows, err := pool.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var all []string
	for rows.Next() {
		var t string
		rows.Scan(&t)
		all = append(all, t)
	}
	// Put known parents first; the rest keep alphabetical order.
	order := []string{"schema_migrations", "storages", "routing_rules", "repositories", "repo_group_members", "blobs", "packages", "assets", "users", "roles", "user_roles", "tokens", "sessions", "cleanup_policies", "repo_cleanup_policies", "content_selectors", "webhooks", "task_schedules", "task_runs", "settings", "audit_log"}
	seen := map[string]bool{}
	var out []string
	for _, t := range order {
		for _, a := range all {
			if a == t && !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	for _, a := range all {
		if !seen[a] {
			out = append(out, a)
		}
	}
	return out, nil
}

// Write creates a backup archive at w.
func Write(ctx context.Context, c *content.Service, version string, withBlobs bool, w io.Writer, logf func(string, ...any)) error {
	gz := gzip.NewWriter(w)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()
	tables, err := listTables(ctx, c.DB.Pool)
	if err != nil {
		return err
	}
	man := Manifest{CreatedAt: time.Now().UTC(), Version: version, Tables: tables, Blobs: withBlobs}
	conn, err := c.DB.Pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	for _, t := range tables {
		var buf bytes.Buffer
		if _, err := conn.Conn().PgConn().CopyTo(ctx, &buf, fmt.Sprintf(`COPY %q TO STDOUT WITH (FORMAT csv, HEADER)`, t)); err != nil {
			return fmt.Errorf("export %s: %w", t, err)
		}
		if err := addFile(tw, "db/"+t+".csv", buf.Bytes()); err != nil {
			return err
		}
	}
	if withBlobs {
		rows, err := c.DB.Pool.Query(ctx, `SELECT digest, storage_id FROM blobs WHERE deleted_at IS NULL`)
		if err != nil {
			return err
		}
		type b struct {
			d   string
			sid string
		}
		var blobs []b
		for rows.Next() {
			var x b
			rows.Scan(&x.d, &x.sid)
			blobs = append(blobs, x)
		}
		rows.Close()
		for i, x := range blobs {
			st, err := storeByID(c, x.sid)
			if err != nil {
				continue
			}
			rc, err := st.Get(ctx, storage.Digest(x.d))
			if err != nil {
				logf("blob %s missing: %v", x.d, err)
				continue
			}
			info, err := st.Stat(ctx, storage.Digest(x.d))
			if err != nil {
				rc.Close()
				continue
			}
			hex := strings.TrimPrefix(x.d, "sha256:")
			tw.WriteHeader(&tar.Header{Name: "blobs/" + hex[:2] + "/" + hex, Mode: 0o644, Size: info.Size, ModTime: time.Now()})
			if _, err := io.Copy(tw, rc); err != nil {
				rc.Close()
				return err
			}
			rc.Close()
			man.BlobCount++
			if (i+1)%1000 == 0 {
				logf("blobs written: %d", i+1)
			}
		}
	}
	mb, _ := json.MarshalIndent(man, "", "  ")
	return addFile(tw, "manifest.json", mb)
}

func storeByID(c *content.Service, sid string) (storage.Storage, error) {
	u, err := parseUUID(sid)
	if err != nil {
		return nil, err
	}
	return c.Store(u)
}

func addFile(tw *tar.Writer, name string, b []byte) error {
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(b)), ModTime: time.Now()}); err != nil {
		return err
	}
	_, err := tw.Write(b)
	return err
}

// Restore loads an archive: tables are truncated and re-imported; blobs are
// written into the default storage. Run against an empty or same-version
// schema.
func Restore(ctx context.Context, c *content.Service, r io.Reader, logf func(string, ...any)) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	conn, err := c.DB.Pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	tables := map[string][]byte{}
	var man Manifest
	fsRoot := ""
	var blobStore storage.Storage
	if st, err := c.StorageByName(ctx, "default"); err == nil {
		blobStore, _ = c.Store(st.ID)
		var cfg struct {
			Path string `json:"path"`
		}
		if st.Type == "fs" && json.Unmarshal(st.Config, &cfg) == nil {
			fsRoot = cfg.Path
		}
	}
	nblobs := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		switch {
		case hdr.Name == "manifest.json":
			b, _ := io.ReadAll(tr)
			json.Unmarshal(b, &man)
		case strings.HasPrefix(hdr.Name, "db/"):
			b, _ := io.ReadAll(tr)
			tables[strings.TrimSuffix(strings.TrimPrefix(hdr.Name, "db/"), ".csv")] = b
		case strings.HasPrefix(hdr.Name, "blobs/") && blobStore != nil:
			d := storage.DigestFromHex(filepath.Base(hdr.Name))
			if _, err := blobStore.Stat(ctx, d); err == nil {
				io.Copy(io.Discard, tr)
				continue
			}
			if _, err := blobStore.Put(ctx, tr, d); err != nil {
				logf("blob %s: %v", d, err)
			}
			nblobs++
		}
	}
	order := man.Tables
	if len(order) == 0 {
		for t := range tables {
			order = append(order, t)
		}
	}
	// Disable FK checks for the session while loading.
	if _, err := conn.Exec(ctx, `SET session_replication_role = replica`); err != nil {
		return err
	}
	defer conn.Exec(ctx, `SET session_replication_role = DEFAULT`)
	for _, t := range order {
		csv, ok := tables[t]
		if !ok {
			continue
		}
		if _, err := conn.Exec(ctx, fmt.Sprintf(`TRUNCATE %q CASCADE`, t)); err != nil {
			logf("truncate %s: %v (table missing?)", t, err)
			continue
		}
		if _, err := conn.Conn().PgConn().CopyFrom(ctx, bytes.NewReader(csv), fmt.Sprintf(`COPY %q FROM STDIN WITH (FORMAT csv, HEADER)`, t)); err != nil {
			return fmt.Errorf("import %s: %w", t, err)
		}
	}
	// Blobs were written into this instance's default storage; make the
	// restored storages row point there rather than at the old server's path.
	if fsRoot != "" {
		cfg, _ := json.Marshal(map[string]string{"path": fsRoot})
		conn.Exec(ctx, `UPDATE storages SET config=$1 WHERE name='default' AND type='fs'`, cfg)
	}
	logf("restored %d tables, %d blobs (backup from %s, version %s)", len(tables), nblobs, man.CreatedAt.Format(time.RFC3339), man.Version)
	c.Invalidate()
	return nil
}

// WriteFile writes a timestamped backup into dir and prunes older ones
// beyond keep.
func WriteFile(ctx context.Context, c *content.Service, version, dir string, withBlobs bool, keep int, logf func(string, ...any)) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := filepath.Join(dir, "holiaokho-"+time.Now().UTC().Format("20060102-150405")+".tar.gz")
	f, err := os.Create(name)
	if err != nil {
		return "", err
	}
	if err := Write(ctx, c, version, withBlobs, f, logf); err != nil {
		f.Close()
		os.Remove(name)
		return "", err
	}
	f.Close()
	if keep > 0 {
		matches, _ := filepath.Glob(filepath.Join(dir, "holiaokho-*.tar.gz"))
		if len(matches) > keep {
			for _, old := range matches[:len(matches)-keep] {
				os.Remove(old)
			}
		}
	}
	logf("backup written: %s", name)
	return name, nil
}
