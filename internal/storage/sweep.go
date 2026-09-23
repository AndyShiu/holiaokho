package storage

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

// SweepDir removes the regular files in dir that match and were last
// modified before cutoff, for backends that stage data on local disk. Files
// that vanish or fail to delete are skipped; the next run sees them again.
func SweepDir(ctx context.Context, dir string, cutoff time.Time, match func(name string) bool) (Swept, error) {
	var out Swept
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return out, err
	}
	for _, e := range entries {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		if !e.Type().IsRegular() || !match(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		if os.Remove(filepath.Join(dir, e.Name())) == nil {
			out.Items++
			out.Bytes += info.Size()
		}
	}
	return out, nil
}
