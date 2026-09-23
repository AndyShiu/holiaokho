package repo

import (
	"context"
	"io"
	"sync"
	"time"
)

// maxResumeFailures is how many attempts in a row may deliver nothing before
// a streamed transfer is given up. Any progress resets the count, so a long
// download that stalls now and then keeps going for as long as it keeps
// moving.
const maxResumeFailures = 3

// resumingBody reads an upstream body, and when the transfer stalls or
// breaks off, asks the upstream again from where it stopped.
//
// Without it a stall on a slow, unsteady upstream loses everything received
// so far: the client retries, a new download starts from byte zero, and it
// can stall at the same place again. Registries and their CDNs honour Range
// on blobs, and storage verifies the digest of the whole stream regardless of
// how many requests it took to receive it.
type resumingBody struct {
	parent context.Context
	open   func(ctx context.Context, off int64) (io.ReadCloser, error)

	mu     sync.Mutex
	cancel context.CancelFunc // the current attempt
	timer  *time.Timer

	body  io.ReadCloser
	off   int64
	fails int
	err   error // why it gave up, once it has
}

func newResumingBody(parent context.Context) *resumingBody {
	b := &resumingBody{parent: parent}
	b.timer = time.AfterFunc(stallTimeout, func() {
		b.mu.Lock()
		cancel := b.cancel
		b.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	})
	return b
}

// attempt returns the context for one upstream request, cancelled if that
// request goes stallTimeout without delivering anything.
func (b *resumingBody) attempt() context.Context {
	ctx, cancel := context.WithCancel(b.parent)
	b.mu.Lock()
	prev := b.cancel
	b.cancel = cancel
	b.mu.Unlock()
	if prev != nil {
		prev()
	}
	b.timer.Reset(stallTimeout)
	return ctx
}

func (b *resumingBody) Read(p []byte) (int, error) {
	for {
		n, err := b.body.Read(p)
		if n > 0 {
			b.off += int64(n)
			b.fails = 0
			b.timer.Reset(stallTimeout)
			return n, nil
		}
		if err == nil {
			continue
		}
		if err == io.EOF {
			return 0, io.EOF
		}
		if b.parent.Err() != nil || b.fails >= maxResumeFailures {
			b.err = err
			return 0, err
		}
		b.fails++
		b.body.Close()
		body, oerr := b.open(b.attempt(), b.off)
		if oerr != nil {
			// Counted like any other attempt that delivered nothing.
			body = io.NopCloser(errReader{oerr})
		}
		b.body = body
	}
}

func (b *resumingBody) Close() error {
	b.timer.Stop()
	b.mu.Lock()
	if b.cancel != nil {
		b.cancel()
	}
	b.mu.Unlock()
	return b.body.Close()
}

type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }
