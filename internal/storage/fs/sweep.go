package fs

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/holiaokho/holiaokho/internal/storage"
)

// Sweep removes leftovers from <root>/uploads. Both kinds live there and are
// told apart by name: Put's scratch files are "put-*", while an upload a
// client started carries the id the client was given.
func (s *Store) Sweep(ctx context.Context, kind storage.Leftover, cutoff time.Time) (storage.Swept, error) {
	return storage.SweepDir(ctx, filepath.Join(s.root, "uploads"), cutoff, func(name string) bool {
		scratch := strings.HasPrefix(name, "put-")
		if kind == storage.Scratch {
			return scratch
		}
		return !scratch && storage.ValidUploadID(name)
	})
}

var _ storage.Sweeper = (*Store)(nil)
