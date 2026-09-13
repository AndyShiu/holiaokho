package server

import (
	"log/slog"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
)

// SetMemoryLimitFromCgroup teaches the Go runtime about the container's memory
// limit. Without it the runtime sizes the heap against the host's memory, keeps
// freed spans instead of collecting, and the kernel OOM-kills the process: a
// real cut-over was killed at a 1Gi limit while the live heap was only ~67MB
// and RSS had reached ~720MB.
//
// An explicit GOMEMLIMIT always wins. Otherwise the limit is read from cgroup
// v2 (then v1) and 90% of it is handed to the runtime, leaving room for
// stacks, the binary itself and non-heap allocations.
func SetMemoryLimitFromCgroup(log *slog.Logger) {
	if _, ok := os.LookupEnv("GOMEMLIMIT"); ok {
		return // operator knows better
	}
	limit, source, ok := cgroupMemoryLimit()
	if !ok {
		return // not containerised, or no limit set: leave the default
	}
	target := limit / 100 * 90
	debug.SetMemoryLimit(target)
	log.Info("memory limit from cgroup", "limit", limit, "gomemlimit", target, "source", source)
}

func cgroupMemoryLimit() (int64, string, bool) {
	// cgroup v2: "max" means unlimited.
	for _, p := range []string{"/sys/fs/cgroup/memory.max", "/sys/fs/cgroup/memory/memory.limit_in_bytes"} {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		s := strings.TrimSpace(string(b))
		if s == "" || s == "max" {
			continue
		}
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil || v <= 0 {
			continue
		}
		// cgroup v1 reports a sentinel close to max int64 when unlimited.
		if v > 1<<50 {
			continue
		}
		return v, p, true
	}
	return 0, "", false
}
