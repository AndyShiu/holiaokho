// Package storage defines the content-addressed blob store abstraction.
// Keys are sha256 digests; backends know nothing about formats or repos.
package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"strings"
)

var ErrNotFound = errors.New("blob not found")

// Digest is "sha256:<64 hex>".
type Digest string

func (d Digest) Hex() string    { return strings.TrimPrefix(string(d), "sha256:") }
func (d Digest) String() string { return string(d) }

func ParseDigest(s string) (Digest, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "sha256:") || len(s) != 7+64 {
		return "", fmt.Errorf("invalid digest %q", s)
	}
	if _, err := hex.DecodeString(s[7:]); err != nil {
		return "", fmt.Errorf("invalid digest %q", s)
	}
	return Digest(strings.ToLower(s)), nil
}

func DigestFromHex(h string) Digest { return Digest("sha256:" + strings.ToLower(h)) }

type Info struct {
	Digest Digest
	Size   int64
}

// Upload is an in-progress write whose digest is not yet known.
// Commit verifies (or computes) the digest and makes the blob durable.
type Upload interface {
	io.Writer
	// Size written so far.
	Size() int64
	// Commit finalises the upload. If expected != "" it must match.
	Commit(ctx context.Context, expected Digest) (Info, error)
	// Abort discards the upload.
	Abort(ctx context.Context) error
}

type Storage interface {
	// Put stores r under its digest. If expected != "" the content must match.
	Put(ctx context.Context, r io.Reader, expected Digest) (Info, error)
	Get(ctx context.Context, d Digest) (io.ReadCloser, error)
	// GetRange returns bytes [offset, offset+length); length<0 = to end.
	GetRange(ctx context.Context, d Digest, offset, length int64) (io.ReadCloser, error)
	Stat(ctx context.Context, d Digest) (Info, error)
	Delete(ctx context.Context, d Digest) error
	// Begin starts a resumable upload (Docker chunked uploads).
	Begin(ctx context.Context, id string) (Upload, error)
	// Resume reopens an upload started with Begin.
	Resume(ctx context.Context, id string) (Upload, error)
	// Link creates the blob from an existing file without copying (fs only,
	// used by the Nexus importer). Backends may return ErrUnsupported.
	Link(ctx context.Context, srcPath string, d Digest) error
	Type() string
}

var ErrUnsupported = errors.New("operation not supported by this storage")

// Hasher wraps a writer computing sha256 on the fly.
type Hasher struct {
	h hash.Hash
	n int64
}

func NewHasher() *Hasher { return &Hasher{h: sha256.New()} }
func (h *Hasher) Write(p []byte) (int, error) {
	n, err := h.h.Write(p)
	h.n += int64(n)
	return n, err
}
func (h *Hasher) Digest() Digest { return DigestFromHex(hex.EncodeToString(h.h.Sum(nil))) }
func (h *Hasher) Size() int64    { return h.n }
