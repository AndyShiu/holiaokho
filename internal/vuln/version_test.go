package vuln

import (
	"testing"
	"time"
)

func TestCompareVersions(t *testing.T) {
	less := [][2]string{
		{"2.9", "2.10"},
		{"2.14.1", "2.15.0"},
		{"2.12.2", "2.14.1"},
		{"v0.3.5", "v0.3.7"},
		{"0.3.5", "v0.3.7"},    // with and without Go's v
		{"1.0.0-rc1", "1.0.0"}, // pre-release first
		{"8.0.0-alpha.0", "8.0.0-alpha.4"},
		{"1.0.0-beta", "1.0.0-rc.1"},
		{"2.9.10", "2.9.10.4"}, // Maven's fourth digit
		{"5.3.0.RELEASE", "5.3.1"},
		{"1.0", "1.0.1"},
		{"31.0-jre", "31.1-jre"},
		{"2.2.18", "3.0.12"},
		{"1.2.5", "1.2.6"},
	}
	for _, p := range less {
		if compareVersions(p[0], p[1]) >= 0 || compareVersions(p[1], p[0]) <= 0 {
			t.Errorf("want %s < %s", p[0], p[1])
		}
	}
	equal := [][2]string{{"1.0", "1.0.0"}, {"v1.2.3", "1.2.3"}, {"1.2.3+build5", "1.2.3"}}
	for _, p := range equal {
		if compareVersions(p[0], p[1]) != 0 {
			t.Errorf("want %s == %s", p[0], p[1])
		}
	}
}

func TestFixFor(t *testing.T) {
	cases := []struct {
		current string
		fixed   []string
		want    string
	}{
		// Log4Shell: fixed on three branches; only one is an upgrade.
		{"2.14.1", []string{"2.15.0", "2.3.1", "2.12.2"}, "2.15.0"},
		{"2.11.0", []string{"2.15.0", "2.3.1", "2.12.2"}, "2.12.2"},
		{"4.17.20", []string{"4.18.0", "4.17.21"}, "4.17.21"},
		{"2.9.8", []string{"2.9.10.4", "2.8.11.6", "2.7.9.7"}, "2.9.10.4"},
		{"1.0.0", nil, ""},
		{"3.0.0", []string{"2.0.0"}, ""}, // nothing above the current version
		// Guava: advisories name the -android flavour; a -jre user needs -jre.
		{"31.1-jre", []string{"32.0.0-android"}, "32.0.0-jre"},
		{"31.1-android", []string{"32.0.0-android"}, "32.0.0-android"},
		{"5.3.0.RELEASE", []string{"5.3.1"}, "5.3.1"}, // RELEASE is not a flavour
		{"1.0.0-rc1", []string{"1.0.0"}, "1.0.0"},     // nor is a pre-release
	}
	for _, c := range cases {
		if got := fixFor(c.current, c.fixed); got != c.want {
			t.Errorf("fixFor(%s, %v) = %q, want %q", c.current, c.fixed, got, c.want)
		}
	}
}

// A daily schedule must find yesterday's results due. The run stamps its
// packages when it finishes, a little after it starts, so a threshold of
// exactly 24 hours skips them and every package is checked every other day.
func TestADailyScheduleRechecksEveryDay(t *testing.T) {
	yesterday := time.Date(2026, 9, 27, 2, 0, 0, 0, time.UTC)
	stamped := yesterday.Add(40 * time.Second) // when yesterday's run finished
	today := yesterday.Add(24 * time.Hour)     // today's run, on the dot
	if !stamped.Before(dueBefore(today)) {
		t.Fatal("yesterday's results are not due at today's run")
	}
	// An hourly schedule does not recheck a package it checked an hour ago.
	if stamped.Before(dueBefore(yesterday.Add(time.Hour))) {
		t.Fatal("a package checked an hour ago is due again")
	}
}
