package s3

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/holiaokho/holiaokho/internal/storage"
)

// Runs against a real S3-compatible service when one is configured:
//
//	HOLIAOKHO_TEST_S3_ENDPOINT=http://localhost:19000 \
//	HOLIAOKHO_TEST_S3_ACCESS_KEY=... HOLIAOKHO_TEST_S3_SECRET_KEY=... go test ./internal/storage/s3/
func testStore(t *testing.T) *Store {
	t.Helper()
	ep := os.Getenv("HOLIAOKHO_TEST_S3_ENDPOINT")
	if ep == "" {
		t.Skip("HOLIAOKHO_TEST_S3_ENDPOINT not set")
	}
	ctx := context.Background()
	bucket := "sweep-" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))
	cfg := Config{Endpoint: ep, Bucket: bucket, Prefix: "hlk", PathStyle: true, TempDir: t.TempDir(),
		AccessKey: os.Getenv("HOLIAOKHO_TEST_S3_ACCESS_KEY"), SecretKey: os.Getenv("HOLIAOKHO_TEST_S3_SECRET_KEY")}
	// New fails fast on a missing bucket, so create it first.
	client := awss3.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
		return aws.Credentials{AccessKeyID: cfg.AccessKey, SecretAccessKey: cfg.SecretKey}, nil
	})}, func(o *awss3.Options) { o.BaseEndpoint = aws.String(ep); o.UsePathStyle = true })
	client.CreateBucket(ctx, &awss3.CreateBucketInput{Bucket: &bucket})
	s, err := New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (s *Store) put(t *testing.T, key string) {
	t.Helper()
	if _, err := s.client.PutObject(context.Background(), &awss3.PutObjectInput{Bucket: &s.cfg.Bucket, Key: &key, Body: strings.NewReader("12345")}); err != nil {
		t.Fatal(err)
	}
}

func (s *Store) exists(key string) bool {
	_, err := s.client.HeadObject(context.Background(), &awss3.HeadObjectInput{Bucket: &s.cfg.Bucket, Key: &key})
	return err == nil
}

func (s *Store) startMultipart(t *testing.T, key string) {
	t.Helper()
	if _, err := s.client.CreateMultipartUpload(context.Background(), &awss3.CreateMultipartUploadInput{Bucket: &s.cfg.Bucket, Key: &key}); err != nil {
		t.Fatal(err)
	}
}

func (s *Store) multiparts(t *testing.T) map[string]bool {
	t.Helper()
	out, err := s.client.ListMultipartUploads(context.Background(), &awss3.ListMultipartUploadsInput{Bucket: &s.cfg.Bucket})
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]bool{}
	for _, u := range out.Uploads {
		m[*u.Key] = true
	}
	return m
}

func TestSweepScratchTouchesOnlyWhatIsOurs(t *testing.T) {
	s := testStore(t)
	s.put(t, "hlk/uploads/left-by-a-crash")
	s.put(t, "hlk/sha256/ab/abcdef") // a real blob
	s.put(t, "other-app/uploads/theirs")
	s.startMultipart(t, "hlk/sha256/cd/in-progress")
	s.startMultipart(t, "other-app/big-upload")

	// A cutoff in the future makes everything "old", so only the rules about
	// what is ours stand between these objects and deletion.
	got, err := s.Sweep(context.Background(), storage.Scratch, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if got.Items != 1 || s.exists("hlk/uploads/left-by-a-crash") {
		t.Fatalf("scratch object not removed: %+v", got)
	}
	if !s.exists("hlk/sha256/ab/abcdef") {
		t.Fatal("removed a blob")
	}
	if !s.exists("other-app/uploads/theirs") {
		t.Fatal("removed another application's object")
	}
	mp := s.multiparts(t)
	if !mp["hlk/sha256/cd/in-progress"] {
		t.Fatal("aborted a multipart upload that started moments ago")
	}
	if !mp["other-app/big-upload"] {
		t.Fatal("aborted another application's multipart upload")
	}

	// Now let the in-progress one be old enough to count as abandoned.
	defer func(d time.Duration) { staleMultipart = d }(staleMultipart)
	staleMultipart = 0
	time.Sleep(1100 * time.Millisecond) // Initiated has second resolution
	if _, err := s.Sweep(context.Background(), storage.Scratch, time.Now()); err != nil {
		t.Fatal(err)
	}
	mp = s.multiparts(t)
	if mp["hlk/sha256/cd/in-progress"] {
		t.Fatal("stale multipart upload not aborted")
	}
	if !mp["other-app/big-upload"] {
		t.Fatal("aborted another application's multipart upload")
	}
}

func TestSweepAbandonedIsLocal(t *testing.T) {
	s := testStore(t)
	p := filepath.Join(s.tmp, "0f1e2d3c-aaaa-bbbb-cccc-000000000001")
	os.WriteFile(p, []byte("12345"), 0o644)
	old := time.Now().Add(-48 * time.Hour)
	os.Chtimes(p, old, old)
	got, err := s.Sweep(context.Background(), storage.Abandoned, time.Now().Add(-24*time.Hour))
	if err != nil || got.Items != 1 {
		t.Fatalf("%+v, %v", got, err)
	}
}
