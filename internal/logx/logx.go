// Package logx holds small logging helpers shared by the proxy engine and the
// format plugins.
package logx

import (
	"context"
	"errors"
	"io"
	"net"
	"syscall"
)

// Disconnected reports whether err is just the client (or our own context)
// going away rather than a real failure. A CI run cancels requests constantly
// — npm and Maven abandon group members as soon as one answers — and logging
// those at warn level buries the failures that matter.
func Disconnected(err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, context.Canceled),
		errors.Is(err, io.ErrClosedPipe),
		errors.Is(err, syscall.EPIPE),
		errors.Is(err, syscall.ECONNRESET),
		errors.Is(err, net.ErrClosed):
		return true
	}
	return false
}
