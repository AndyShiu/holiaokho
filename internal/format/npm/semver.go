package npm

import (
	"strconv"
	"strings"
)

// semverLess orders versions per semver 2.0 precedence; non-semver strings
// fall back to lexical order after all valid versions.
func semverLess(a, b string) bool {
	pa, oka := parseSemver(a)
	pb, okb := parseSemver(b)
	if oka != okb {
		return oka // valid versions sort before non-semver strings
	}
	if !oka {
		return a < b
	}
	for i := 0; i < 3; i++ {
		if pa.nums[i] != pb.nums[i] {
			return pa.nums[i] < pb.nums[i]
		}
	}
	// A version without prerelease is greater.
	if len(pa.pre) == 0 || len(pb.pre) == 0 {
		return len(pa.pre) > 0 && len(pb.pre) == 0
	}
	for i := 0; i < len(pa.pre) && i < len(pb.pre); i++ {
		x, y := pa.pre[i], pb.pre[i]
		xi, xe := strconv.Atoi(x)
		yi, ye := strconv.Atoi(y)
		switch {
		case xe == nil && ye == nil:
			if xi != yi {
				return xi < yi
			}
		case xe == nil:
			return true
		case ye == nil:
			return false
		default:
			if x != y {
				return x < y
			}
		}
	}
	return len(pa.pre) < len(pb.pre)
}

type semver struct {
	nums [3]int
	pre  []string
}

func parseSemver(v string) (semver, bool) {
	var s semver
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	if i := strings.IndexByte(v, '-'); i >= 0 {
		s.pre = strings.Split(v[i+1:], ".")
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return s, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return s, false
		}
		s.nums[i] = n
	}
	return s, true
}
