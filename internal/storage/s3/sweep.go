package s3

import (
	"context"
	"strings"
	"time"

	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/holiaokho/holiaokho/internal/storage"
)

// staleMultipart is how old an unfinished multipart upload has to be before
// it is aborted. Much longer than any other cutoff: S3 cannot say when a part
// was last uploaded, only when the upload began, and a streamed proxy
// download over a slow link can legitimately keep one open for hours. None
// runs for a week.
var staleMultipart = 7 * 24 * time.Hour

// Sweep removes leftovers.
//
// Abandoned uploads are staged on local disk until they are committed, so
// they are swept there. Scratch lives in the bucket: objects under uploads/
// that a crash left between upload and delete, and multipart uploads a crash
// left unfinished — which S3 keeps, invisibly, and bills for, until someone
// aborts them.
//
// Only keys under this store's own prefix and its own two directories are
// touched. The bucket may be shared, and another application's uploads are
// not ours to judge.
func (s *Store) Sweep(ctx context.Context, kind storage.Leftover, cutoff time.Time) (storage.Swept, error) {
	if kind == storage.Abandoned {
		return storage.SweepDir(ctx, s.tmp, cutoff, storage.ValidUploadID)
	}
	var out storage.Swept

	scratch := s.cfg.Prefix + "uploads/"
	pages := awss3.NewListObjectsV2Paginator(s.client, &awss3.ListObjectsV2Input{Bucket: &s.cfg.Bucket, Prefix: &scratch})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return out, err
		}
		for _, o := range page.Contents {
			if o.Key == nil || o.LastModified == nil || !o.LastModified.Before(cutoff) {
				continue
			}
			if _, err := s.client.DeleteObject(ctx, &awss3.DeleteObjectInput{Bucket: &s.cfg.Bucket, Key: o.Key}); err == nil {
				out.Items++
				if o.Size != nil {
					out.Bytes += *o.Size
				}
			}
		}
	}

	ours := func(key string) bool {
		return strings.HasPrefix(key, s.cfg.Prefix+"uploads/") || strings.HasPrefix(key, s.cfg.Prefix+"sha256/")
	}
	stale := time.Now().Add(-staleMultipart)
	// No Prefix: MinIO matches it only against a complete key, so asking
	// for "hlk/" there returns nothing at all and nothing would ever be
	// aborted. Listing everything and filtering here works on both; it only
	// reads, so a shared bucket is safe.
	in := &awss3.ListMultipartUploadsInput{Bucket: &s.cfg.Bucket}
	for {
		page, err := s.client.ListMultipartUploads(ctx, in)
		if err != nil {
			return out, err
		}
		for _, u := range page.Uploads {
			if u.Key == nil || u.UploadId == nil || u.Initiated == nil || !ours(*u.Key) || !u.Initiated.Before(stale) {
				continue
			}
			if _, err := s.client.AbortMultipartUpload(ctx, &awss3.AbortMultipartUploadInput{Bucket: &s.cfg.Bucket, Key: u.Key, UploadId: u.UploadId}); err == nil {
				out.Items++
			}
		}
		if page.IsTruncated == nil || !*page.IsTruncated {
			return out, nil
		}
		in.KeyMarker, in.UploadIdMarker = page.NextKeyMarker, page.NextUploadIdMarker
	}
}

var _ storage.Sweeper = (*Store)(nil)
