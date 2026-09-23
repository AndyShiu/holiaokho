package fs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/holiaokho/holiaokho/internal/storage"
)

func TestSweepTellsTheKindsApartAndSparesRecentFiles(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	up := filepath.Join(s.root, "uploads")
	old := time.Now().Add(-48 * time.Hour)
	write := func(name string, age time.Time) string {
		p := filepath.Join(up, name)
		if err := os.WriteFile(p, []byte("12345"), 0o644); err != nil {
			t.Fatal(err)
		}
		os.Chtimes(p, age, age)
		return p
	}
	oldPush := write("0f1e2d3c-aaaa-bbbb-cccc-000000000001", old)
	newPush := write("0f1e2d3c-aaaa-bbbb-cccc-000000000002", time.Now())
	oldPut := write("put-111", old)
	newPut := write("put-222", time.Now())
	stranger := write("not.an.upload", old) // not a name we create

	cutoff := time.Now().Add(-24 * time.Hour)
	got, err := s.Sweep(context.Background(), storage.Abandoned, cutoff)
	if err != nil || got.Items != 1 || got.Bytes != 5 {
		t.Fatalf("abandoned sweep: %+v, %v", got, err)
	}
	got, err = s.Sweep(context.Background(), storage.Scratch, cutoff)
	if err != nil || got.Items != 1 {
		t.Fatalf("scratch sweep: %+v, %v", got, err)
	}

	for p, want := range map[string]bool{oldPush: false, oldPut: false, newPush: true, newPut: true, stranger: true} {
		_, err := os.Stat(p)
		if exists := err == nil; exists != want {
			t.Errorf("%s: exists=%v, want %v", filepath.Base(p), exists, want)
		}
	}
}

// A write in progress keeps touching its file, which is what makes a short
// cutoff safe for scratch files: age is from the last write, not the first.
func TestSweepMeasuresAgeFromTheLastWrite(t *testing.T) {
	s, _ := New(t.TempDir())
	up, err := s.Begin(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer up.Abort(context.Background())
	name := up.(*upload).f.Name()
	long := time.Now().Add(-5 * time.Hour)
	os.Chtimes(name, long, long)    // started long ago…
	up.Write([]byte("still going")) // …but written to just now

	got, _ := s.Sweep(context.Background(), storage.Scratch, time.Now().Add(-time.Hour))
	if got.Items != 0 {
		t.Fatal("swept a file that is still being written")
	}
}
