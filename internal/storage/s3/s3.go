// Package s3 is the S3-compatible Storage backend. Only the standard S3 API
// is used (Put/Get/Head/Delete/Copy + multipart upload), so any compatible
// service works: AWS S3, MinIO, Garage, RustFS, SeaweedFS, Ceph RGW, R2.
//
// Blobs are content-addressed at <prefix>sha256/<aa>/<hex>. Because the
// digest is only known after streaming, uploads without a known digest go to
// a temporary key and are then copied server-side to their final key.
// Resumable uploads (Docker chunked pushes) are staged on local disk.
package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/google/uuid"

	"github.com/holiaokho/holiaokho/internal/storage"
)

type Config struct {
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	Bucket    string `json:"bucket"`
	Prefix    string `json:"prefix"`
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"`
	PathStyle bool   `json:"pathStyle"`
	// TempDir stages resumable uploads; defaults to the OS temp dir.
	TempDir string `json:"tempDir"`
}

type Store struct {
	cfg    Config
	client *awss3.Client
	up     *manager.Uploader
	tmp    string
}

func New(ctx context.Context, cfg Config) (*Store, error) {
	if cfg.Bucket == "" {
		return nil, errors.New("s3: bucket required")
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	opts := []func(*awscfg.LoadOptions) error{awscfg.WithRegion(cfg.Region)}
	if cfg.AccessKey != "" {
		opts = append(opts, awscfg.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")))
	}
	ac, err := awscfg.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, err
	}
	client := awss3.NewFromConfig(ac, func(o *awss3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		o.UsePathStyle = cfg.PathStyle
	})
	if cfg.Prefix != "" && !strings.HasSuffix(cfg.Prefix, "/") {
		cfg.Prefix += "/"
	}
	tmp := cfg.TempDir
	if tmp == "" {
		tmp = filepath.Join(os.TempDir(), "holiaokho-uploads")
	}
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return nil, err
	}
	s := &Store{cfg: cfg, client: client, tmp: tmp}
	s.up = manager.NewUploader(client, func(u *manager.Uploader) { u.PartSize = 16 << 20; u.Concurrency = 4 })
	// Fail fast on bad credentials / bucket.
	if _, err := client.HeadBucket(ctx, &awss3.HeadBucketInput{Bucket: &cfg.Bucket}); err != nil {
		return nil, fmt.Errorf("s3: head bucket %s: %w", cfg.Bucket, err)
	}
	return s, nil
}

func (s *Store) Type() string { return "s3" }

func (s *Store) key(d storage.Digest) string {
	h := d.Hex()
	return s.cfg.Prefix + "sha256/" + h[:2] + "/" + h
}

func (s *Store) Put(ctx context.Context, r io.Reader, expected storage.Digest) (storage.Info, error) {
	h := storage.NewHasher()
	tee := io.TeeReader(r, h)
	if expected != "" {
		// Already present? Skip the upload entirely.
		if info, err := s.Stat(ctx, expected); err == nil {
			io.Copy(io.Discard, r)
			return info, nil
		}
		key := s.key(expected)
		if _, err := s.up.Upload(ctx, &awss3.PutObjectInput{Bucket: &s.cfg.Bucket, Key: &key, Body: tee}); err != nil {
			return storage.Info{}, err
		}
		if h.Digest() != expected {
			s.client.DeleteObject(ctx, &awss3.DeleteObjectInput{Bucket: &s.cfg.Bucket, Key: &key})
			return storage.Info{}, fmt.Errorf("digest mismatch: expected %s got %s", expected, h.Digest())
		}
		return storage.Info{Digest: expected, Size: h.Size()}, nil
	}
	tmpKey := s.cfg.Prefix + "uploads/" + uuid.NewString()
	if _, err := s.up.Upload(ctx, &awss3.PutObjectInput{Bucket: &s.cfg.Bucket, Key: &tmpKey, Body: tee}); err != nil {
		return storage.Info{}, err
	}
	defer s.client.DeleteObject(ctx, &awss3.DeleteObjectInput{Bucket: &s.cfg.Bucket, Key: &tmpKey})
	d := h.Digest()
	if _, err := s.Stat(ctx, d); err == nil {
		return storage.Info{Digest: d, Size: h.Size()}, nil // dedup
	}
	key := s.key(d)
	src := s.cfg.Bucket + "/" + tmpKey
	if _, err := s.client.CopyObject(ctx, &awss3.CopyObjectInput{Bucket: &s.cfg.Bucket, Key: &key, CopySource: &src}); err != nil {
		return storage.Info{}, fmt.Errorf("s3 copy to final key: %w", err)
	}
	return storage.Info{Digest: d, Size: h.Size()}, nil
}

func (s *Store) Get(ctx context.Context, d storage.Digest) (io.ReadCloser, error) {
	return s.GetRange(ctx, d, 0, -1)
}

func (s *Store) GetRange(ctx context.Context, d storage.Digest, off, length int64) (io.ReadCloser, error) {
	key := s.key(d)
	in := &awss3.GetObjectInput{Bucket: &s.cfg.Bucket, Key: &key}
	if off > 0 || length >= 0 {
		if length < 0 {
			in.Range = aws.String(fmt.Sprintf("bytes=%d-", off))
		} else {
			in.Range = aws.String(fmt.Sprintf("bytes=%d-%d", off, off+length-1))
		}
	}
	out, err := s.client.GetObject(ctx, in)
	if err != nil {
		if isNotFound(err) {
			return nil, storage.ErrNotFound
		}
		return nil, err
	}
	return out.Body, nil
}

func (s *Store) Stat(ctx context.Context, d storage.Digest) (storage.Info, error) {
	key := s.key(d)
	out, err := s.client.HeadObject(ctx, &awss3.HeadObjectInput{Bucket: &s.cfg.Bucket, Key: &key})
	if err != nil {
		if isNotFound(err) {
			return storage.Info{}, storage.ErrNotFound
		}
		return storage.Info{}, err
	}
	return storage.Info{Digest: d, Size: aws.ToInt64(out.ContentLength)}, nil
}

func (s *Store) Delete(ctx context.Context, d storage.Digest) error {
	key := s.key(d)
	_, err := s.client.DeleteObject(ctx, &awss3.DeleteObjectInput{Bucket: &s.cfg.Bucket, Key: &key})
	return err
}

func (s *Store) Link(ctx context.Context, src string, d storage.Digest) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = s.Put(ctx, f, d)
	return err
}

func isNotFound(err error) bool {
	var nf *types.NotFound
	var nk *types.NoSuchKey
	if errors.As(err, &nf) || errors.As(err, &nk) {
		return true
	}
	return strings.Contains(err.Error(), "StatusCode: 404")
}

// ---------------------------------------------------- resumable uploads

// upload stages bytes in a local temp file; Commit streams it to S3.
type upload struct {
	s    *Store
	id   string
	f    *os.File
	size int64
}

func (s *Store) Begin(ctx context.Context, id string) (storage.Upload, error) {
	if id == "" {
		id = uuid.NewString()
	}
	f, err := os.OpenFile(filepath.Join(s.tmp, id), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &upload{s: s, id: id, f: f}, nil
}

func (s *Store) Resume(ctx context.Context, id string) (storage.Upload, error) {
	f, err := os.OpenFile(filepath.Join(s.tmp, id), os.O_RDWR|os.O_APPEND, 0o644)
	if errors.Is(err, os.ErrNotExist) {
		return nil, storage.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	st, _ := f.Stat()
	return &upload{s: s, id: id, f: f, size: st.Size()}, nil
}

func (u *upload) Write(p []byte) (int, error) {
	n, err := u.f.Write(p)
	u.size += int64(n)
	return n, err
}
func (u *upload) Size() int64  { return u.size }
func (u *upload) Close() error { return u.f.Close() }

func (u *upload) Commit(ctx context.Context, expected storage.Digest) (storage.Info, error) {
	name := u.f.Name()
	u.f.Close()
	defer os.Remove(name)
	f, err := os.Open(name)
	if err != nil {
		return storage.Info{}, err
	}
	defer f.Close()
	return u.s.Put(ctx, f, expected)
}

func (u *upload) Abort(ctx context.Context) error {
	u.f.Close()
	return os.Remove(u.f.Name())
}
