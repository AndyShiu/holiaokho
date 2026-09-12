package task

import (
	"testing"
	"time"
)

func TestCron(t *testing.T) {
	base := time.Date(2026, 9, 13, 10, 30, 0, 0, time.UTC) // Sunday
	cases := map[string]time.Time{
		"0 2 * * *":      time.Date(2026, 9, 14, 2, 0, 0, 0, time.UTC),
		"*/15 * * * *":   time.Date(2026, 9, 13, 10, 45, 0, 0, time.UTC),
		"0 0 1 * *":      time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		"30 10 * * 1":    time.Date(2026, 9, 14, 10, 30, 0, 0, time.UTC),
		"@hourly":        time.Date(2026, 9, 13, 11, 0, 0, 0, time.UTC),
		"0 9-17 * * 1-5": time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC),
	}
	for expr, want := range cases {
		s, err := ParseCron(expr)
		if err != nil {
			t.Fatalf("%s: %v", expr, err)
		}
		if got := s.Next(base); !got.Equal(want) {
			t.Errorf("%s: got %v want %v", expr, got, want)
		}
	}
	for _, bad := range []string{"* * *", "60 * * * *", "a b c d e", "*/0 * * * *"} {
		if _, err := ParseCron(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}
