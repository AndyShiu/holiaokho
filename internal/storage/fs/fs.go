// Package fs is the local-filesystem Storage backend.
// Layout: <root>/sha256/<aa>/<digest-hex>; uploads live under <root>/uploads.
package fs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/holiaokho/holiaokho/internal/storage"
)

type Store struct {
	root string
}

func New(root string) (*Store, error) {
	for _, d := range []string{filepath.Join(root, "sha256"), filepath.Join(root, "uploads")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	return &Store{root: root}, nil
}

func (s *Store) Type() string { return "fs" }
func (s *Store) Root() string { return s.root }

func (s *Store) path(d storage.Digest) string {
	h := d.Hex()
	return filepath.Join(s.root, "sha256", h[:2], h)
}

func (s *Store) Put(ctx context.Context, r io.Reader, expected storage.Digest) (storage.Info, error) {
	up, err := s.Begin(ctx, "")
	if err != nil {
		return storage.Info{}, err
	}
	if _, err := io.Copy(up, r); err != nil {
		up.Abort(ctx)
		return storage.Info{}, err
	}
	return up.Commit(ctx, expected)
}

func (s *Store) Get(ctx context.Context, d storage.Digest) (io.ReadCloser, error) {
	f, err := os.Open(s.path(d))
	if errors.Is(err, os.ErrNotExist) {
		return nil, storage.ErrNotFound
	}
	return f, err
}

func (s *Store) GetRange(ctx context.Context, d storage.Digest, offset, length int64) (io.ReadCloser, error) {
	f, err := os.Open(s.path(d))
	if errors.Is(err, os.ErrNotExist) {
		return nil, storage.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		f.Close()
		return nil, err
	}
	if length < 0 {
		return f, nil
	}
	return struct {
		io.Reader
		io.Closer
	}{io.LimitReader(f, length), f}, nil
}

func (s *Store) Stat(ctx context.Context, d storage.Digest) (storage.Info, error) {
	st, err := os.Stat(s.path(d))
	if errors.Is(err, os.ErrNotExist) {
		return storage.Info{}, storage.ErrNotFound
	}
	if err != nil {
		return storage.Info{}, err
	}
	return storage.Info{Digest: d, Size: st.Size()}, nil
}

func (s *Store) Delete(ctx context.Context, d storage.Digest) error {
	err := os.Remove(s.path(d))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *Store) Link(ctx context.Context, src string, d storage.Digest) error {
	dst := s.path(d)
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Link(src, dst); err != nil {
		// Cross-device: fall back to copy.
		in, err2 := os.Open(src)
		if err2 != nil {
			return err
		}
		defer in.Close()
		_, err2 = s.Put(ctx, in, d)
		return err2
	}
	return nil
}

// upload writes to a temp file while hashing, then renames into place.
type upload struct {
	s    *Store
	id   string
	f    *os.File
	h    *storage.Hasher
	size int64
}

func (s *Store) Begin(ctx context.Context, id string) (storage.Upload, error) {
	var f *os.File
	var err error
	if id == "" {
		f, err = os.CreateTemp(filepath.Join(s.root, "uploads"), "put-*")
	} else {
		f, err = os.OpenFile(filepath.Join(s.root, "uploads", id), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	}
	if err != nil {
		return nil, err
	}
	return &upload{s: s, id: id, f: f, h: storage.NewHasher()}, nil
}

// Resume reopens a chunked upload. The hash state is not persisted, so the
// existing bytes are re-read to rebuild it (uploads are bounded in size and
// rare, so this is acceptable).
func (s *Store) Resume(ctx context.Context, id string) (storage.Upload, error) {
	p := filepath.Join(s.root, "uploads", id)
	f, err := os.OpenFile(p, os.O_RDWR, 0o644)
	if errors.Is(err, os.ErrNotExist) {
		return nil, storage.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	h := storage.NewHasher()
	n, err := io.Copy(h, f)
	if err != nil {
		f.Close()
		return nil, err
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		f.Close()
		return nil, err
	}
	return &upload{s: s, id: id, f: f, h: h, size: n}, nil
}

func (u *upload) Write(p []byte) (int, error) {
	n, err := u.f.Write(p)
	u.h.Write(p[:n])
	u.size += int64(n)
	return n, err
}

func (u *upload) Size() int64 { return u.size }

func (u *upload) Commit(ctx context.Context, expected storage.Digest) (storage.Info, error) {
	name := u.f.Name()
	if err := u.f.Sync(); err != nil {
		u.f.Close()
		os.Remove(name)
		return storage.Info{}, err
	}
	u.f.Close()
	d := u.h.Digest()
	if expected != "" && expected != d {
		os.Remove(name)
		return storage.Info{}, fmt.Errorf("digest mismatch: expected %s got %s", expected, d)
	}
	dst := u.s.path(d)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		os.Remove(name)
		return storage.Info{}, err
	}
	if _, err := os.Stat(dst); err == nil {
		// Already present (dedup) — discard the new copy.
		os.Remove(name)
		return storage.Info{Digest: d, Size: u.size}, nil
	}
	if err := os.Rename(name, dst); err != nil {
		os.Remove(name)
		return storage.Info{}, err
	}
	return storage.Info{Digest: d, Size: u.size}, nil
}

// Close releases the file handle without committing or aborting, so the
// upload can be resumed later.
func (u *upload) Close() error { return u.f.Close() }

func (u *upload) Abort(ctx context.Context) error {
	u.f.Close()
	return os.Remove(u.f.Name())
}
