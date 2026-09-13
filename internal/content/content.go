// Package content is the persistence layer for repositories, packages,
// assets and blobs. Format plugins and the repo engine only talk to this
// package and to storage.Storage; they never issue SQL themselves.
package content

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/holiaokho/holiaokho/internal/db"
	"github.com/holiaokho/holiaokho/internal/model"
	"github.com/holiaokho/holiaokho/internal/secrets"
	"github.com/holiaokho/holiaokho/internal/storage"
	fsstore "github.com/holiaokho/holiaokho/internal/storage/fs"
	s3store "github.com/holiaokho/holiaokho/internal/storage/s3"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("already exists")
)

type Service struct {
	DB  *db.DB
	Log *slog.Logger

	mu     sync.RWMutex
	stores map[uuid.UUID]storage.Storage
	repos  map[string]*model.Repository // cache by name
}

func New(d *db.DB, log *slog.Logger) *Service {
	return &Service{DB: d, Log: log, stores: map[uuid.UUID]storage.Storage{}, repos: map[string]*model.Repository{}}
}

// ---------------------------------------------------------------- storages

// LoadStorages opens every storage row; unknown types are logged and skipped.
func (s *Service) LoadStorages(ctx context.Context) error {
	rows, err := s.DB.Pool.Query(ctx, `SELECT id, name, type, config FROM storages`)
	if err != nil {
		return err
	}
	defer rows.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	for rows.Next() {
		var st model.Storage
		if err := rows.Scan(&st.ID, &st.Name, &st.Type, &st.Config); err != nil {
			return err
		}
		if _, ok := s.stores[st.ID]; ok {
			continue
		}
		impl, err := openStorage(st)
		if err != nil {
			s.Log.Error("open storage", "name", st.Name, "err", err)
			continue
		}
		s.stores[st.ID] = impl
	}
	return nil
}

func openStorage(st model.Storage) (storage.Storage, error) {
	if dec, err := secrets.DecryptPaths(st.Config, secrets.StorageConfigPaths); err == nil {
		st.Config = dec
	} else {
		return nil, err
	}
	switch st.Type {
	case "fs":
		var cfg struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(st.Config, &cfg); err != nil {
			return nil, err
		}
		return fsstore.New(cfg.Path)
	case "s3":
		var cfg s3store.Config
		if err := json.Unmarshal(st.Config, &cfg); err != nil {
			return nil, err
		}
		return s3store.New(context.Background(), cfg)
	default:
		return nil, fmt.Errorf("unknown storage type %q", st.Type)
	}
}

// TestStorage opens a storage from its (unsaved) definition and performs a
// write/read/delete round trip with a probe blob. Secrets given as "***"
// are taken from the stored definition of the same name, if any.
func (s *Service) TestStorage(ctx context.Context, st model.Storage) (map[string]any, error) {
	if st.Name != "" {
		if cur, err := s.StorageByName(ctx, st.Name); err == nil {
			var in, old map[string]any
			if json.Unmarshal(st.Config, &in) == nil && json.Unmarshal(cur.Config, &old) == nil {
				for _, k := range secrets.StorageConfigPaths {
					if v, _ := in[k].(string); v == secrets.Redacted {
						in[k] = old[k] // still encrypted; openStorage decrypts
					}
				}
				st.Config, _ = json.Marshal(in)
			}
		}
	}
	start := time.Now()
	store, err := openStorage(st)
	if err != nil {
		return nil, err
	}
	probe := []byte("holiaokho storage probe " + uuid.NewString())
	sum := sha256.Sum256(probe)
	d := storage.DigestFromHex(hex.EncodeToString(sum[:]))
	if _, err := store.Put(ctx, bytes.NewReader(probe), d); err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}
	defer store.Delete(ctx, d)
	rc, err := store.Get(ctx, d)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	got, err := io.ReadAll(rc)
	rc.Close()
	if err != nil || !bytes.Equal(got, probe) {
		return nil, errors.New("read: probe content mismatch")
	}
	if err := store.Delete(ctx, d); err != nil {
		return nil, fmt.Errorf("delete: %w", err)
	}
	return map[string]any{"ok": true, "type": store.Type(), "latencyMs": time.Since(start).Milliseconds()}, nil
}

// EnsureDefaultStorage creates the "default" store if none exists.
// cfgJSON is the backend configuration object.
func (s *Service) EnsureDefaultStorage(ctx context.Context, typ, cfgJSON string) error {
	var n int
	if err := s.DB.Pool.QueryRow(ctx, `SELECT count(*) FROM storages`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err := s.DB.Pool.Exec(ctx, `INSERT INTO storages(id, name, type, config) VALUES ($1,'default',$2,$3)`,
		uuid.New(), typ, []byte(cfgJSON))
	return err
}

func (s *Service) ListStorages(ctx context.Context) ([]model.Storage, error) {
	rows, err := s.DB.Pool.Query(ctx, `SELECT id, name, type, config, quota_bytes, created_at FROM storages ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Storage
	for rows.Next() {
		var st model.Storage
		if err := rows.Scan(&st.ID, &st.Name, &st.Type, &st.Config, &st.QuotaBytes, &st.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, nil
}

func (s *Service) CreateStorage(ctx context.Context, st *model.Storage) error {
	impl, err := openStorage(*st)
	if err != nil {
		return err
	}
	st.ID = uuid.New()
	enc, err := secrets.EncryptPaths(st.Config, secrets.StorageConfigPaths)
	if err != nil {
		return err
	}
	_, err = s.DB.Pool.Exec(ctx, `INSERT INTO storages(id, name, type, config, quota_bytes) VALUES ($1,$2,$3,$4,$5)`,
		st.ID, st.Name, st.Type, enc, st.QuotaBytes)
	if err != nil {
		if isUnique(err) {
			return ErrConflict
		}
		return err
	}
	s.mu.Lock()
	s.stores[st.ID] = impl
	s.mu.Unlock()
	return nil
}

func (s *Service) StorageByName(ctx context.Context, name string) (model.Storage, error) {
	var st model.Storage
	err := s.DB.Pool.QueryRow(ctx, `SELECT id, name, type, config, quota_bytes, created_at FROM storages WHERE name=$1`, name).
		Scan(&st.ID, &st.Name, &st.Type, &st.Config, &st.QuotaBytes, &st.CreatedAt)
	if db.IsNoRows(err) {
		return st, ErrNotFound
	}
	return st, err
}

// Store returns the storage implementation for an ID.
func (s *Service) Store(id uuid.UUID) (storage.Storage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.stores[id]
	if !ok {
		return nil, fmt.Errorf("storage %s not loaded", id)
	}
	return st, nil
}

// ------------------------------------------------------------ repositories

func decodeRepo(r *model.Repository) error {
	var a struct {
		Proxy  *model.ProxyAttrs  `json:"proxy"`
		Hosted *model.HostedAttrs `json:"hosted"`
		Group  *model.GroupAttrs  `json:"group"`
	}
	if len(r.Attributes) > 0 {
		if err := json.Unmarshal(r.Attributes, &a); err != nil {
			return fmt.Errorf("repository %s attributes: %w", r.Name, err)
		}
	}
	r.Proxy, r.Hosted = a.Proxy, a.Hosted
	if a.Group != nil {
		r.Members = a.Group.Members
	}
	switch r.Type {
	case model.Proxy:
		if r.Proxy == nil {
			return fmt.Errorf("repository %s: proxy attributes required", r.Name)
		}
		if r.Proxy.MetadataMaxAge == 0 {
			r.Proxy.MetadataMaxAge = 1440
		}
		if r.Proxy.ContentMaxAge == 0 {
			r.Proxy.ContentMaxAge = 1440
		}
	case model.Hosted:
		if r.Hosted == nil {
			r.Hosted = &model.HostedAttrs{WritePolicy: model.WriteAllow}
		}
		if r.Hosted.WritePolicy == "" {
			r.Hosted.WritePolicy = model.WriteAllow
		}
	}
	return nil
}

const repoCols = `id, name, format, type, storage_id, online, attributes, routing_rule_id, created_at, updated_at`

func scanRepo(row pgx.Row) (*model.Repository, error) {
	var r model.Repository
	if err := row.Scan(&r.ID, &r.Name, &r.Format, &r.Type, &r.StorageID, &r.Online, &r.Attributes, &r.RoutingRuleID, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, err
	}
	if dec, err := secrets.DecryptPaths(r.Attributes, secrets.RepositoryPaths); err == nil {
		r.Attributes = dec
	} else {
		// Keep the repository usable (its secret fields stay opaque) and
		// make the problem visible instead of hiding every repository.
		slog.Error("repository secrets cannot be decrypted; check secrets.key / previous_keys", "repository", r.Name, "err", err)
	}
	if err := decodeRepo(&r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *Service) ListRepos(ctx context.Context) ([]*model.Repository, error) {
	rows, err := s.DB.Pool.Query(ctx, `SELECT `+repoCols+` FROM repositories ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Repository
	for rows.Next() {
		r, err := scanRepo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// Repo fetches a repository by name (cached; invalidated on write).
func (s *Service) Repo(ctx context.Context, name string) (*model.Repository, error) {
	s.mu.RLock()
	r, ok := s.repos[name]
	s.mu.RUnlock()
	if ok {
		return r, nil
	}
	r, err := scanRepo(s.DB.Pool.QueryRow(ctx, `SELECT `+repoCols+` FROM repositories WHERE name=$1`, name))
	if db.IsNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.repos[name] = r
	s.mu.Unlock()
	return r, nil
}

// Invalidate drops the repository cache (after changes made outside the service).
func (s *Service) Invalidate() { s.invalidate() }

func (s *Service) invalidate() {
	s.mu.Lock()
	s.repos = map[string]*model.Repository{}
	s.mu.Unlock()
}

func (s *Service) CreateRepo(ctx context.Context, r *model.Repository) error {
	if err := validateRepoName(r.Name); err != nil {
		return err
	}
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	if len(r.Attributes) == 0 {
		r.Attributes = json.RawMessage(`{}`)
	}
	if err := decodeRepo(r); err != nil {
		return err
	}
	if r.Type == model.Group {
		if err := s.checkMembers(ctx, r); err != nil {
			return err
		}
	}
	if r.StorageID == uuid.Nil {
		st, err := s.StorageByName(ctx, "default")
		if err != nil {
			return err
		}
		r.StorageID = st.ID
	}
	enc, err := secrets.EncryptPaths(r.Attributes, secrets.RepositoryPaths)
	if err != nil {
		return err
	}
	_, err = s.DB.Pool.Exec(ctx, `INSERT INTO repositories(id,name,format,type,storage_id,online,attributes,routing_rule_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, r.ID, r.Name, r.Format, r.Type, r.StorageID, r.Online, enc, r.RoutingRuleID)
	if err != nil {
		if isUnique(err) {
			return ErrConflict
		}
		return err
	}
	s.invalidate()
	return nil
}

func (s *Service) UpdateRepo(ctx context.Context, r *model.Repository) error {
	if err := decodeRepo(r); err != nil {
		return err
	}
	if r.Type == model.Group {
		if err := s.checkMembers(ctx, r); err != nil {
			return err
		}
	}
	enc, err := secrets.EncryptPaths(r.Attributes, secrets.RepositoryPaths)
	if err != nil {
		return err
	}
	tag, err := s.DB.Pool.Exec(ctx, `UPDATE repositories SET online=$2, attributes=$3, routing_rule_id=$4, updated_at=now() WHERE name=$1`,
		r.Name, r.Online, enc, r.RoutingRuleID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	s.invalidate()
	return nil
}

func (s *Service) DeleteRepo(ctx context.Context, name string) error {
	return s.DB.Tx(ctx, func(tx pgx.Tx) error {
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM repositories WHERE name=$1`, name).Scan(&id); err != nil {
			if db.IsNoRows(err) {
				return ErrNotFound
			}
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE blobs b SET ref_count = ref_count - x.n FROM
			(SELECT blob_digest, count(*) n FROM assets WHERE repo_id=$1 AND blob_digest IS NOT NULL GROUP BY blob_digest) x
			WHERE b.digest = x.blob_digest`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM repositories WHERE id=$1`, id); err != nil {
			return err
		}
		s.invalidate()
		return nil
	})
}

func (s *Service) checkMembers(ctx context.Context, r *model.Repository) error {
	for _, m := range r.Members {
		if m == r.Name {
			return fmt.Errorf("group %s cannot contain itself", r.Name)
		}
		mr, err := s.Repo(ctx, m)
		if err != nil {
			return fmt.Errorf("group member %q: %w", m, err)
		}
		if mr.Format != r.Format {
			return fmt.Errorf("group member %q has format %s, expected %s", m, mr.Format, r.Format)
		}
	}
	return nil
}

func validateRepoName(n string) error {
	if n == "" || len(n) > 200 {
		return errors.New("invalid repository name")
	}
	for _, c := range n {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return fmt.Errorf("invalid repository name %q: only letters, digits, '-', '_' and '.' allowed", n)
		}
	}
	return nil
}

// ------------------------------------------------------------------ blobs

// PutBlob streams r into the repo's storage and records the blob row.
func (s *Service) PutBlob(ctx context.Context, storageID uuid.UUID, r io.Reader, expected storage.Digest) (storage.Info, error) {
	st, err := s.Store(storageID)
	if err != nil {
		return storage.Info{}, err
	}
	if err := s.CheckQuota(ctx, storageID); err != nil {
		return storage.Info{}, err
	}
	info, err := st.Put(ctx, r, expected)
	if err != nil {
		return storage.Info{}, err
	}
	return info, s.RecordBlob(ctx, storageID, info)
}

// RecordBlob upserts the blobs row (ref_count untouched) and revives a
// soft-deleted row.
func (s *Service) RecordBlob(ctx context.Context, storageID uuid.UUID, info storage.Info) error {
	_, err := s.DB.Pool.Exec(ctx, `INSERT INTO blobs(digest,size,storage_id) VALUES ($1,$2,$3) ON CONFLICT (digest) DO UPDATE SET deleted_at = NULL`,
		string(info.Digest), info.Size, storageID)
	return err
}

var ErrQuota = errors.New("storage quota exceeded")

// CheckQuota returns ErrQuota when the storage's soft quota is already exceeded.
func (s *Service) CheckQuota(ctx context.Context, storageID uuid.UUID) error {
	var quota, used int64
	err := s.DB.Pool.QueryRow(ctx, `SELECT s.quota_bytes, coalesce((SELECT sum(size) FROM blobs b WHERE b.storage_id = s.id AND b.deleted_at IS NULL),0) FROM storages s WHERE s.id=$1`, storageID).Scan(&quota, &used)
	if err != nil {
		return err
	}
	if quota > 0 && used >= quota {
		return fmt.Errorf("%w: %d of %d bytes used", ErrQuota, used, quota)
	}
	return nil
}

// StorageUsage returns bytes used and blob count for a storage.
func (s *Service) StorageUsage(ctx context.Context, storageID uuid.UUID) (used int64, count int64, err error) {
	err = s.DB.Pool.QueryRow(ctx, `SELECT coalesce(sum(size),0), count(*) FROM blobs WHERE storage_id=$1 AND deleted_at IS NULL`, storageID).Scan(&used, &count)
	return
}

func (s *Service) OpenBlob(ctx context.Context, d storage.Digest) (io.ReadCloser, int64, error) {
	var sid uuid.UUID
	var size int64
	err := s.DB.Pool.QueryRow(ctx, `SELECT storage_id, size FROM blobs WHERE digest=$1`, string(d)).Scan(&sid, &size)
	if db.IsNoRows(err) {
		return nil, 0, ErrNotFound
	}
	if err != nil {
		return nil, 0, err
	}
	st, err := s.Store(sid)
	if err != nil {
		return nil, 0, err
	}
	rc, err := st.Get(ctx, d)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, 0, ErrNotFound
	}
	return rc, size, err
}

func (s *Service) OpenBlobRange(ctx context.Context, d storage.Digest, off, length int64) (io.ReadCloser, error) {
	var sid uuid.UUID
	err := s.DB.Pool.QueryRow(ctx, `SELECT storage_id FROM blobs WHERE digest=$1`, string(d)).Scan(&sid)
	if db.IsNoRows(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	st, err := s.Store(sid)
	if err != nil {
		return nil, err
	}
	return st.GetRange(ctx, d, off, length)
}

func (s *Service) BlobExists(ctx context.Context, d storage.Digest) (int64, bool, error) {
	var size int64
	err := s.DB.Pool.QueryRow(ctx, `SELECT size FROM blobs WHERE digest=$1`, string(d)).Scan(&size)
	if db.IsNoRows(err) {
		return 0, false, nil
	}
	return size, err == nil, err
}

// ----------------------------------------------------------------- assets

const assetCols = `id, repo_id, package_id, path, blob_digest, size, content_type, attrs, created_at, updated_at, cache_expires_at, last_downloaded_at, negative`

func scanAsset(row pgx.Row) (*model.Asset, error) {
	var a model.Asset
	err := row.Scan(&a.ID, &a.RepoID, &a.PackageID, &a.Path, &a.BlobDigest, &a.Size, &a.ContentType, &a.Attrs,
		&a.CreatedAt, &a.UpdatedAt, &a.CacheExpiresAt, &a.LastDownloadedAt, &a.Negative)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *Service) Asset(ctx context.Context, repoID uuid.UUID, path string) (*model.Asset, error) {
	a, err := scanAsset(s.DB.Pool.QueryRow(ctx, `SELECT `+assetCols+` FROM assets WHERE repo_id=$1 AND path=$2`, repoID, path))
	if db.IsNoRows(err) {
		return nil, ErrNotFound
	}
	return a, err
}

func (s *Service) AssetByID(ctx context.Context, id int64) (*model.Asset, error) {
	a, err := scanAsset(s.DB.Pool.QueryRow(ctx, `SELECT `+assetCols+` FROM assets WHERE id=$1`, id))
	if db.IsNoRows(err) {
		return nil, ErrNotFound
	}
	return a, err
}

// UpsertAsset inserts or replaces the asset at (repo, path), maintaining
// blob ref_counts. Returns the stored asset (with ID).
func (s *Service) UpsertAsset(ctx context.Context, a *model.Asset) error {
	if len(a.Attrs) == 0 {
		a.Attrs = json.RawMessage(`{}`)
	}
	err := s.upsertAsset(ctx, a)
	if isUnique(err) {
		// Lost a race with a concurrent insert of the same path; the row now
		// exists, so the update branch will be taken.
		err = s.upsertAsset(ctx, a)
	}
	return err
}

func (s *Service) upsertAsset(ctx context.Context, a *model.Asset) error {
	return s.DB.Tx(ctx, func(tx pgx.Tx) error {
		var oldDigest *string
		var id int64
		err := tx.QueryRow(ctx, `SELECT id, blob_digest FROM assets WHERE repo_id=$1 AND path=$2 FOR UPDATE`, a.RepoID, a.Path).Scan(&id, &oldDigest)
		switch {
		case db.IsNoRows(err):
			err = tx.QueryRow(ctx, `INSERT INTO assets(repo_id,package_id,path,blob_digest,size,content_type,attrs,cache_expires_at,negative)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id, created_at, updated_at`,
				a.RepoID, a.PackageID, a.Path, a.BlobDigest, a.Size, a.ContentType, a.Attrs, a.CacheExpiresAt, a.Negative).
				Scan(&a.ID, &a.CreatedAt, &a.UpdatedAt)
			if err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			a.ID = id
			_, err = tx.Exec(ctx, `UPDATE assets SET package_id=$2, blob_digest=$3, size=$4, content_type=$5, attrs=$6,
				cache_expires_at=$7, negative=$8, updated_at=now() WHERE id=$1`,
				id, a.PackageID, a.BlobDigest, a.Size, a.ContentType, a.Attrs, a.CacheExpiresAt, a.Negative)
			if err != nil {
				return err
			}
			if oldDigest != nil && (a.BlobDigest == nil || *oldDigest != *a.BlobDigest) {
				if _, err := tx.Exec(ctx, `UPDATE blobs SET ref_count=ref_count-1 WHERE digest=$1`, *oldDigest); err != nil {
					return err
				}
				oldDigest = nil
			}
		}
		if a.BlobDigest != nil && (oldDigest == nil || *oldDigest != *a.BlobDigest) {
			if _, err := tx.Exec(ctx, `UPDATE blobs SET ref_count=ref_count+1 WHERE digest=$1`, *a.BlobDigest); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Service) DeleteAsset(ctx context.Context, repoID uuid.UUID, path string) error {
	return s.DB.Tx(ctx, func(tx pgx.Tx) error {
		return deleteAssetTx(ctx, tx, repoID, path)
	})
}

func deleteAssetTx(ctx context.Context, tx pgx.Tx, repoID uuid.UUID, path string) error {
	var digest *string
	err := tx.QueryRow(ctx, `DELETE FROM assets WHERE repo_id=$1 AND path=$2 RETURNING blob_digest`, repoID, path).Scan(&digest)
	if db.IsNoRows(err) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if digest != nil {
		_, err = tx.Exec(ctx, `UPDATE blobs SET ref_count=ref_count-1 WHERE digest=$1`, *digest)
	}
	return err
}

// PurgeRepoContent removes every package and asset of a repository in one
// statement, decrementing the blob reference counts as it goes. The blobs
// themselves are freed by blob-gc and the disk space returned by
// compact-blobs, exactly as for a single delete.
//
// For a proxy this empties the cache; for a hosted repository it deletes the
// artifacts, so callers must confirm first.
func (s *Service) PurgeRepoContent(ctx context.Context, repoID uuid.UUID) (assets int64, packages int64, err error) {
	err = s.DB.Tx(ctx, func(tx pgx.Tx) error {
		// One UPDATE per distinct digest rather than per asset row.
		if _, err := tx.Exec(ctx, `
			UPDATE blobs b SET ref_count = GREATEST(b.ref_count - d.n, 0)
			FROM (SELECT blob_digest AS digest, count(*) AS n FROM assets
			      WHERE repo_id=$1 AND blob_digest IS NOT NULL GROUP BY blob_digest) d
			WHERE b.digest = d.digest`, repoID); err != nil {
			return err
		}
		ta, err := tx.Exec(ctx, `DELETE FROM assets WHERE repo_id=$1`, repoID)
		if err != nil {
			return err
		}
		tp, err := tx.Exec(ctx, `DELETE FROM packages WHERE repo_id=$1`, repoID)
		if err != nil {
			return err
		}
		assets, packages = ta.RowsAffected(), tp.RowsAffected()
		return nil
	})
	return assets, packages, err
}

// ListAssets lists assets under a path prefix (for browse / group merge).
func (s *Service) ListAssets(ctx context.Context, repoID uuid.UUID, prefix string, limit int) ([]*model.Asset, error) {
	if limit <= 0 {
		limit = 1000
	}
	rows, err := s.DB.Pool.Query(ctx, `SELECT `+assetCols+` FROM assets WHERE repo_id=$1 AND path LIKE $2 AND NOT negative ORDER BY path LIMIT $3`,
		repoID, escapeLike(prefix)+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Asset
	for rows.Next() {
		a, err := scanAsset(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

// ListChildren returns the immediate children names (dirs and files) under a
// directory prefix — used for directory listings and browse.
func (s *Service) ListChildren(ctx context.Context, repoID uuid.UUID, dir string) (dirs []string, files []*model.Asset, err error) {
	if dir != "" && !strings.HasSuffix(dir, "/") {
		dir += "/"
	}
	rows, err := s.DB.Pool.Query(ctx, `SELECT `+assetCols+` FROM assets WHERE repo_id=$1 AND path LIKE $2 AND NOT negative ORDER BY path`,
		repoID, escapeLike(dir)+"%")
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		a, err := scanAsset(rows)
		if err != nil {
			return nil, nil, err
		}
		rest := strings.TrimPrefix(a.Path, dir)
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			d := rest[:i]
			if !seen[d] {
				seen[d] = true
				dirs = append(dirs, d)
			}
		} else {
			files = append(files, a)
		}
	}
	return dirs, files, nil
}

func (s *Service) TouchDownloaded(ctx context.Context, assetID int64) {
	// Best-effort; throttle to once per minute per asset to avoid write storms.
	s.DB.Pool.Exec(ctx, `UPDATE assets SET last_downloaded_at=now() WHERE id=$1 AND (last_downloaded_at IS NULL OR last_downloaded_at < now() - interval '1 minute')`, assetID)
	s.DB.Pool.Exec(ctx, `UPDATE packages p SET last_downloaded_at=now() FROM assets a WHERE a.id=$1 AND p.id=a.package_id AND (p.last_downloaded_at IS NULL OR p.last_downloaded_at < now() - interval '1 minute')`, assetID)
}

// --------------------------------------------------------------- packages

const pkgCols = `id, repo_id, namespace, name, version, attrs, created_at, updated_at, last_downloaded_at`

func scanPkg(row pgx.Row) (*model.Package, error) {
	var p model.Package
	if err := row.Scan(&p.ID, &p.RepoID, &p.Namespace, &p.Name, &p.Version, &p.Attrs, &p.CreatedAt, &p.UpdatedAt, &p.LastDownloadedAt); err != nil {
		return nil, err
	}
	return &p, nil
}

// UpsertPackage returns the package ID for (repo, ns, name, version),
// creating it if needed and merging attrs.
func (s *Service) UpsertPackage(ctx context.Context, p *model.Package) error {
	if len(p.Attrs) == 0 {
		p.Attrs = json.RawMessage(`{}`)
	}
	return s.DB.Pool.QueryRow(ctx, `INSERT INTO packages(repo_id,namespace,name,version,attrs) VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (repo_id,namespace,name,version) DO UPDATE SET attrs = packages.attrs || EXCLUDED.attrs, updated_at=now()
		RETURNING id, created_at, updated_at`, p.RepoID, p.Namespace, p.Name, p.Version, p.Attrs).
		Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
}

func (s *Service) Package(ctx context.Context, repoID uuid.UUID, ns, name, version string) (*model.Package, error) {
	p, err := scanPkg(s.DB.Pool.QueryRow(ctx, `SELECT `+pkgCols+` FROM packages WHERE repo_id=$1 AND namespace=$2 AND name=$3 AND version=$4`,
		repoID, ns, name, version))
	if db.IsNoRows(err) {
		return nil, ErrNotFound
	}
	return p, err
}

func (s *Service) PackageByID(ctx context.Context, id int64) (*model.Package, error) {
	p, err := scanPkg(s.DB.Pool.QueryRow(ctx, `SELECT `+pkgCols+` FROM packages WHERE id=$1`, id))
	if db.IsNoRows(err) {
		return nil, ErrNotFound
	}
	return p, err
}

// PackageVersions lists all versions of a package name in a repo.
func (s *Service) PackageVersions(ctx context.Context, repoID uuid.UUID, ns, name string) ([]*model.Package, error) {
	rows, err := s.DB.Pool.Query(ctx, `SELECT `+pkgCols+` FROM packages WHERE repo_id=$1 AND namespace=$2 AND name=$3 ORDER BY created_at`, repoID, ns, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Package
	for rows.Next() {
		p, err := scanPkg(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func (s *Service) PackageAssets(ctx context.Context, pkgID int64) ([]*model.Asset, error) {
	rows, err := s.DB.Pool.Query(ctx, `SELECT `+assetCols+` FROM assets WHERE package_id=$1 ORDER BY path`, pkgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Asset
	for rows.Next() {
		a, err := scanAsset(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

// DeletePackage removes the package and all its assets.
func (s *Service) DeletePackage(ctx context.Context, id int64) error {
	return s.DB.Tx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT repo_id, path FROM assets WHERE package_id=$1`, id)
		if err != nil {
			return err
		}
		type rp struct {
			r uuid.UUID
			p string
		}
		var list []rp
		for rows.Next() {
			var x rp
			if err := rows.Scan(&x.r, &x.p); err != nil {
				rows.Close()
				return err
			}
			list = append(list, x)
		}
		rows.Close()
		for _, x := range list {
			if err := deleteAssetTx(ctx, tx, x.r, x.p); err != nil && !errors.Is(err, ErrNotFound) {
				return err
			}
		}
		tag, err := tx.Exec(ctx, `DELETE FROM packages WHERE id=$1`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

type SearchQuery struct {
	Q         string
	Format    string
	Repo      string
	Namespace string
	Name      string
	Version   string
	Limit     int
	Offset    int
}

type SearchHit struct {
	model.Package
	RepoName string `json:"repository"`
	Format   string `json:"format"`
}

func (s *Service) Search(ctx context.Context, q SearchQuery) ([]SearchHit, error) {
	if q.Limit <= 0 || q.Limit > 500 {
		q.Limit = 50
	}
	var where []string
	var args []any
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if q.Q != "" {
		add(`(p.name ILIKE $%[1]d OR p.namespace ILIKE $%[1]d OR p.namespace || ':' || p.name ILIKE $%[1]d)`, "%"+escapeLike(q.Q)+"%")
	}
	if q.Format != "" {
		add(`r.format = $%d`, q.Format)
	}
	if q.Repo != "" {
		add(`r.name = $%d`, q.Repo)
	}
	if q.Namespace != "" {
		add(`p.namespace = $%d`, q.Namespace)
	}
	if q.Name != "" {
		add(`p.name = $%d`, q.Name)
	}
	if q.Version != "" {
		add(`p.version = $%d`, q.Version)
	}
	sql := `SELECT p.id, p.repo_id, p.namespace, p.name, p.version, p.attrs, p.created_at, p.updated_at, p.last_downloaded_at, r.name, r.format
		FROM packages p JOIN repositories r ON r.id = p.repo_id`
	if len(where) > 0 {
		sql += " WHERE " + strings.Join(where, " AND ")
	}
	args = append(args, q.Limit, q.Offset)
	sql += fmt.Sprintf(" ORDER BY p.namespace, p.name, p.created_at DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := s.DB.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SearchHit
	for rows.Next() {
		var h SearchHit
		if err := rows.Scan(&h.ID, &h.RepoID, &h.Namespace, &h.Name, &h.Version, &h.Attrs, &h.CreatedAt, &h.UpdatedAt, &h.LastDownloadedAt, &h.RepoName, &h.Format); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, nil
}

// RepoStats returns size and counts for the repositories list.
type RepoStats struct {
	Packages int64 `json:"packages"`
	Assets   int64 `json:"assets"`
	Size     int64 `json:"size"`
}

func (s *Service) RepoStats(ctx context.Context, repoID uuid.UUID) (RepoStats, error) {
	var st RepoStats
	err := s.DB.Pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM packages WHERE repo_id=$1),
		(SELECT count(*) FROM assets WHERE repo_id=$1 AND NOT negative),
		(SELECT coalesce(sum(size),0) FROM assets WHERE repo_id=$1 AND NOT negative)`, repoID).
		Scan(&st.Packages, &st.Assets, &st.Size)
	return st, err
}

// ---------------------------------------------------------------- helpers

func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "23505")
}

// Now is overridable in tests.
var Now = time.Now
