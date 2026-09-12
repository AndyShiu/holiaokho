package task

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Schedule is a five-field cron expression (minute hour day-of-month month
// day-of-week) supporting *, lists, ranges and steps, plus the shorthands
// @hourly, @daily, @weekly, @monthly.
type Schedule struct {
	min, hour, dom, mon, dow [64]bool
	expr                     string
}

func ParseCron(expr string) (*Schedule, error) {
	switch strings.TrimSpace(expr) {
	case "@hourly":
		expr = "0 * * * *"
	case "@daily", "@midnight":
		expr = "0 0 * * *"
	case "@weekly":
		expr = "0 0 * * 0"
	case "@monthly":
		expr = "0 0 1 * *"
	}
	f := strings.Fields(expr)
	if len(f) != 5 {
		return nil, errors.New("cron expression needs 5 fields")
	}
	s := &Schedule{expr: expr}
	var err error
	if s.min, err = parseField(f[0], 0, 59); err != nil {
		return nil, fmt.Errorf("minute: %w", err)
	}
	if s.hour, err = parseField(f[1], 0, 23); err != nil {
		return nil, fmt.Errorf("hour: %w", err)
	}
	if s.dom, err = parseField(f[2], 1, 31); err != nil {
		return nil, fmt.Errorf("day of month: %w", err)
	}
	if s.mon, err = parseField(f[3], 1, 12); err != nil {
		return nil, fmt.Errorf("month: %w", err)
	}
	if s.dow, err = parseField(f[4], 0, 7); err != nil {
		return nil, fmt.Errorf("day of week: %w", err)
	}
	if s.dow[7] {
		s.dow[0] = true
	}
	return s, nil
}

func parseField(f string, lo, hi int) ([64]bool, error) {
	var set [64]bool
	for _, part := range strings.Split(f, ",") {
		step := 1
		if i := strings.IndexByte(part, '/'); i >= 0 {
			n, err := strconv.Atoi(part[i+1:])
			if err != nil || n <= 0 {
				return set, fmt.Errorf("bad step in %q", part)
			}
			step, part = n, part[:i]
		}
		a, b := lo, hi
		if part != "*" {
			if i := strings.IndexByte(part, '-'); i >= 0 {
				var err error
				if a, err = strconv.Atoi(part[:i]); err != nil {
					return set, fmt.Errorf("bad range %q", part)
				}
				if b, err = strconv.Atoi(part[i+1:]); err != nil {
					return set, fmt.Errorf("bad range %q", part)
				}
			} else {
				n, err := strconv.Atoi(part)
				if err != nil {
					return set, fmt.Errorf("bad value %q", part)
				}
				a, b = n, n
				if step > 1 {
					b = hi
				}
			}
		}
		if a < lo || b > hi || a > b {
			return set, fmt.Errorf("out of range %q", part)
		}
		for v := a; v <= b; v += step {
			set[v] = true
		}
	}
	return set, nil
}

// Next returns the first time strictly after t matching the schedule.
func (s *Schedule) Next(t time.Time) time.Time {
	t = t.Truncate(time.Minute).Add(time.Minute)
	for i := 0; i < 366*24*60; i++ {
		if s.mon[int(t.Month())] && s.dom[t.Day()] && s.dow[int(t.Weekday())] && s.hour[t.Hour()] && s.min[t.Minute()] {
			return t
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}
}

func (s *Schedule) String() string { return s.expr }
