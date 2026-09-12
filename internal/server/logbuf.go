package server

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync"
)

// LogBuffer keeps the last N log lines in memory so the API can show them
// (Nexus "Logs" page) without depending on where stdout goes.
type LogBuffer struct {
	mu    sync.Mutex
	lines []string
	next  int
	full  bool
}

func NewLogBuffer(n int) *LogBuffer { return &LogBuffer{lines: make([]string, n)} }

func (b *LogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	for _, l := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		b.lines[b.next] = l
		b.next = (b.next + 1) % len(b.lines)
		if b.next == 0 {
			b.full = true
		}
	}
	b.mu.Unlock()
	return len(p), nil
}

// Tail returns the last n lines (oldest first).
func (b *LogBuffer) Tail(n int) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var all []string
	if b.full {
		all = append(all, b.lines[b.next:]...)
	}
	all = append(all, b.lines[:b.next]...)
	if n > 0 && len(all) > n {
		all = all[len(all)-n:]
	}
	return all
}

// teeHandler writes records to two slog handlers.
type teeHandler struct{ a, b slog.Handler }

func (t teeHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return t.a.Enabled(ctx, l) || t.b.Enabled(ctx, l)
}
func (t teeHandler) Handle(ctx context.Context, r slog.Record) error {
	if t.a.Enabled(ctx, r.Level) {
		t.a.Handle(ctx, r)
	}
	if t.b.Enabled(ctx, r.Level) {
		t.b.Handle(ctx, r.Clone())
	}
	return nil
}
func (t teeHandler) WithAttrs(as []slog.Attr) slog.Handler {
	return teeHandler{t.a.WithAttrs(as), t.b.WithAttrs(as)}
}
func (t teeHandler) WithGroup(n string) slog.Handler {
	return teeHandler{t.a.WithGroup(n), t.b.WithGroup(n)}
}

// System groups runtime knobs exposed through the API.
type System struct {
	Level  *slog.LevelVar
	Buffer *LogBuffer
	Format string
}

// NewSystemLogger builds the process logger from config: chosen output
// handler + in-memory buffer, with a runtime-adjustable level.
func NewSystemLogger(levelStr, format string) (*slog.Logger, *System) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(levelStr)); err != nil {
		level = slog.LevelInfo
	}
	lv := &slog.LevelVar{}
	lv.Set(level)
	var out slog.Handler
	if format == "json" {
		out = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lv})
	} else {
		out = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: lv})
	}
	sys := &System{Level: lv, Buffer: NewLogBuffer(2000), Format: format}
	return NewLogger(out, lv, sys.Buffer), sys
}

// NewLogger builds the process logger: output handler + in-memory buffer,
// with a LevelVar that the API can change at runtime.
func NewLogger(out slog.Handler, level *slog.LevelVar, buf *LogBuffer) *slog.Logger {
	bh := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: level})
	return slog.New(teeHandler{out, bh})
}
