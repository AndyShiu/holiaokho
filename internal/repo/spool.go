package repo

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"time"

	"github.com/holiaokho/holiaokho/internal/model"
)

// stallTimeout is how long a streamed upstream body may go without delivering
// a byte before the transfer is abandoned.
//
// Streamed downloads deliberately have no total time limit. A 200 MB layer
// over a 40 KB/s link takes well over an hour and is still making progress
// the whole time; a total limit would kill it at the same point on every
// attempt, so it would never be cached and every client would start over.
// What actually needs catching is a connection that has stopped moving.
var stallTimeout = 60 * time.Second

// spool is one upstream download in progress, readable by any number of
// clients while it is still being written.
//
// Without it a cache miss is silent until the whole body has been fetched,
// verified and stored: a client pulling a large layer over a slow upstream
// sees no bytes for minutes, and either it or something between it and us
// gives up first. With it the bytes go out as they arrive, and the stored
// copy is still produced by exactly the same path as before — the spool is a
// tee on the body storage reads, not a second download.
type spool struct {
	mu   sync.Mutex
	cond *sync.Cond
	f    *os.File
	n    int64 // bytes written so far
	refs int   // the download itself plus every open reader

	// started is closed once the caller has something to act on: either the
	// upstream answered 200 and bytes are on their way, or the fetch ended
	// without that (404, 304, an error) and the result says what happened.
	started   chan struct{}
	startOnce sync.Once
	streaming bool
	size      int64 // Content-Length, or -1 when upstream did not say
	ct        string

	done  bool
	err   error
	asset *model.Asset
}

func newSpool() (*spool, error) {
	f, err := os.CreateTemp("", "holiaokho-spool-*")
	if err != nil {
		return nil, err
	}
	s := &spool{f: f, started: make(chan struct{}), size: -1, refs: 1}
	s.cond = sync.NewCond(&s.mu)
	return s, nil
}

// begin marks the response as streamable: the upstream said 200 and the body
// is about to be copied through Write.
func (s *spool) begin(size int64, ct string) {
	s.mu.Lock()
	s.streaming, s.size, s.ct = true, size, ct
	s.mu.Unlock()
	s.startOnce.Do(func() { close(s.started) })
}

// Write receives the upstream body as storage reads it.
func (s *spool) Write(p []byte) (int, error) {
	n, err := s.f.WriteAt(p, s.n)
	s.mu.Lock()
	s.n += int64(n)
	s.cond.Broadcast()
	s.mu.Unlock()
	return n, err
}

// finish records the outcome and wakes every reader.
func (s *spool) finish(a *model.Asset, err error) {
	s.mu.Lock()
	s.done, s.asset, s.err = true, a, err
	s.cond.Broadcast()
	s.mu.Unlock()
	s.startOnce.Do(func() { close(s.started) })
}

func (s *spool) acquire() {
	s.mu.Lock()
	s.refs++
	s.mu.Unlock()
}

// release drops one reference; the last one out removes the file.
func (s *spool) release() {
	s.mu.Lock()
	s.refs--
	last := s.refs == 0
	s.mu.Unlock()
	if last {
		s.f.Close()
		os.Remove(s.f.Name())
	}
}

// wait blocks until the spool has started or finished, or ctx ends.
func (s *spool) wait(ctx context.Context) error {
	select {
	case <-s.started:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// reader returns a new reader positioned at the start. The caller already
// holds the reference it will release on Close.
func (s *spool) reader() io.ReadCloser { return &spoolReader{s: s} }

type spoolReader struct {
	s      *spool
	off    int64
	closed bool
}

var errSpoolFailed = errors.New("upstream transfer failed")

func (r *spoolReader) Read(p []byte) (int, error) {
	s := r.s
	s.mu.Lock()
	var limit int64
	for {
		// The final byte is held back until storage has accepted the whole
		// body. If verification fails the client then sees a short read —
		// an unmistakable failure — instead of a complete-looking response
		// with the wrong content, which is what it would get if we sent
		// everything and only then found out.
		limit = s.n
		if !s.done || s.err != nil {
			limit--
		}
		if r.off < limit {
			break
		}
		if s.done {
			err := s.err
			s.mu.Unlock()
			if err != nil {
				return 0, errSpoolFailed
			}
			return 0, io.EOF
		}
		s.cond.Wait()
	}
	s.mu.Unlock()
	if max := limit - r.off; int64(len(p)) > max {
		p = p[:max]
	}
	n, err := s.f.ReadAt(p, r.off)
	r.off += int64(n)
	if err == io.EOF && n > 0 {
		err = nil
	}
	return n, err
}

func (r *spoolReader) Close() error {
	if !r.closed {
		r.closed = true
		r.s.release()
	}
	return nil
}
