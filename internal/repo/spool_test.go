package repo

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/holiaokho/holiaokho/internal/model"
)

// readWithin reads len(want) bytes or fails the test after d.
func readWithin(t *testing.T, r io.Reader, n int, d time.Duration) []byte {
	t.Helper()
	got := make(chan []byte, 1)
	go func() {
		b := make([]byte, n)
		io.ReadFull(r, b)
		got <- b
	}()
	select {
	case b := <-got:
		return b
	case <-time.After(d):
		t.Fatalf("read of %d bytes did not complete within %s", n, d)
		return nil
	}
}

func TestSpoolServesBytesBeforeTheDownloadEnds(t *testing.T) {
	sp, err := newSpool()
	if err != nil {
		t.Fatal(err)
	}
	sp.begin(10, "application/octet-stream")
	sp.acquire()
	r := sp.reader()

	sp.Write([]byte("hello"))
	// Four, not five: the last byte written is held until the download is
	// known to be good.
	if got := readWithin(t, r, 4, time.Second); string(got) != "hell" {
		t.Fatalf("got %q", got)
	}
	sp.Write([]byte("world"))
	if got := readWithin(t, r, 5, time.Second); string(got) != "oworl" {
		t.Fatalf("got %q", got)
	}

	sp.finish(&model.Asset{}, nil)
	rest, err := io.ReadAll(r)
	if err != nil || string(rest) != "d" {
		t.Fatalf("after finish got %q, %v", rest, err)
	}
	r.Close()
	sp.release()
}

func TestSpoolWithholdsTheLastByteWhenTheDownloadFails(t *testing.T) {
	sp, _ := newSpool()
	sp.begin(5, "")
	sp.acquire()
	r := sp.reader()
	sp.Write([]byte("hello"))
	sp.finish(nil, errors.New("digest mismatch"))

	b, err := io.ReadAll(r)
	if !errors.Is(err, errSpoolFailed) {
		t.Fatalf("want errSpoolFailed, got %v", err)
	}
	// A client given all five bytes and a clean EOF would have no way to
	// know it received the wrong content.
	if string(b) != "hell" {
		t.Fatalf("got %q", b)
	}
	r.Close()
	sp.release()
}

func TestSpoolReadersJoiningLateStartFromTheBeginning(t *testing.T) {
	sp, _ := newSpool()
	sp.begin(-1, "")
	sp.acquire()
	early := sp.reader()
	sp.Write([]byte("abcdef"))
	readWithin(t, early, 3, time.Second)

	sp.acquire()
	late := sp.reader()
	sp.finish(&model.Asset{}, nil)
	all, _ := io.ReadAll(late)
	if string(all) != "abcdef" {
		t.Fatalf("late reader got %q", all)
	}
	early.Close()
	late.Close()
	sp.release()
}

func TestSpoolFileIsRemovedByTheLastRelease(t *testing.T) {
	sp, _ := newSpool()
	name := sp.f.Name()
	sp.acquire()
	r := sp.reader()
	sp.finish(&model.Asset{}, nil)

	sp.release() // the download's reference
	if _, err := os.Stat(name); err != nil {
		t.Fatalf("file removed while a reader still held it: %v", err)
	}
	r.Close()
	r.Close() // closing twice must not release twice
	if _, err := os.Stat(name); !os.IsNotExist(err) {
		t.Fatalf("file still present after the last release: %v", err)
	}
}

func TestSpoolWaitReturnsWhenTheFetchEndsWithoutStreaming(t *testing.T) {
	sp, _ := newSpool()
	go sp.finish(&model.Asset{Negative: true}, nil) // a 404
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := sp.wait(ctx); err != nil {
		t.Fatal(err)
	}
	if sp.streaming {
		t.Fatal("a fetch that never began must not be served as a stream")
	}
	sp.release()
}
