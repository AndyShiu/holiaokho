package repo

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// brokenAfter delivers s and then fails the way a dropped connection does.
type brokenAfter struct{ r io.Reader }

func (b *brokenAfter) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if err == io.EOF {
		return n, io.ErrUnexpectedEOF
	}
	return n, err
}

// silentUntilCancelled is a connection that stays open and sends nothing.
type silentUntilCancelled struct{ ctx context.Context }

func (s silentUntilCancelled) Read([]byte) (int, error) {
	<-s.ctx.Done()
	return 0, s.ctx.Err()
}

func TestResumeContinuesFromWhereTheConnectionBroke(t *testing.T) {
	rb := newResumingBody(context.Background())
	defer rb.Close()
	rb.attempt()
	rb.body = io.NopCloser(&brokenAfter{strings.NewReader("hello ")})
	var asked []int64
	rb.open = func(_ context.Context, off int64) (io.ReadCloser, error) {
		asked = append(asked, off)
		return io.NopCloser(strings.NewReader("world")), nil
	}
	b, err := io.ReadAll(rb)
	if err != nil || string(b) != "hello world" {
		t.Fatalf("got %q, %v", b, err)
	}
	if len(asked) != 1 || asked[0] != 6 {
		t.Fatalf("resumed at %v, want [6]", asked)
	}
}

func TestResumeAfterAStall(t *testing.T) {
	defer func(d time.Duration) { stallTimeout = d }(stallTimeout)
	stallTimeout = 50 * time.Millisecond

	rb := newResumingBody(context.Background())
	defer rb.Close()
	rb.body = io.NopCloser(silentUntilCancelled{rb.attempt()})
	rb.open = func(context.Context, int64) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("data")), nil
	}
	done := make(chan []byte, 1)
	go func() { b, _ := io.ReadAll(rb); done <- b }()
	select {
	case b := <-done:
		if string(b) != "data" {
			t.Fatalf("got %q", b)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a silent connection was never abandoned")
	}
}

func TestResumeGivesUpWhenNothingArrives(t *testing.T) {
	rb := newResumingBody(context.Background())
	defer rb.Close()
	rb.attempt()
	rb.body = io.NopCloser(&brokenAfter{strings.NewReader("abc")})
	calls := 0
	rb.open = func(context.Context, int64) (io.ReadCloser, error) {
		calls++
		return nil, errors.New("upstream cannot resume")
	}
	b, err := io.ReadAll(rb)
	if err == nil || rb.err == nil {
		t.Fatal("want an error once resuming stops working")
	}
	if string(b) != "abc" {
		t.Fatalf("got %q", b)
	}
	if calls != maxResumeFailures {
		t.Fatalf("tried %d times, want %d", calls, maxResumeFailures)
	}
}

func TestResumeCountsOnlyFailuresInARow(t *testing.T) {
	rb := newResumingBody(context.Background())
	defer rb.Close()
	rb.attempt()
	rb.body = io.NopCloser(&brokenAfter{strings.NewReader("a")})
	// Every resume delivers one byte and then breaks again: always making
	// progress, so it must never give up, however many resumes it takes.
	parts := strings.Split("bcdefghij", "")
	rb.open = func(context.Context, int64) (io.ReadCloser, error) {
		if len(parts) == 0 {
			return io.NopCloser(strings.NewReader("")), nil
		}
		p := parts[0]
		parts = parts[1:]
		return io.NopCloser(&brokenAfter{strings.NewReader(p)}), nil
	}
	b, err := io.ReadAll(rb)
	if err != nil || string(b) != "abcdefghij" {
		t.Fatalf("got %q, %v", b, err)
	}
}
