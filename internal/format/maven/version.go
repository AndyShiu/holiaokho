package maven

import (
	"strconv"
	"strings"
	"unicode"
)

// Compare implements Maven's ComparableVersion ordering closely enough for
// "latest"/"release" selection and cleanup: numeric tokens compare
// numerically, qualifiers have the canonical order
// alpha < beta < milestone < rc < snapshot < (release) < sp, and unknown
// qualifiers sort lexically after "sp".
func Compare(a, b string) int {
	ta, tb := tokenize(a), tokenize(b)
	n := len(ta)
	if len(tb) > n {
		n = len(tb)
	}
	for i := 0; i < n; i++ {
		var x, y token
		if i < len(ta) {
			x = ta[i]
		} else {
			x = token{kind: kindNull}
		}
		if i < len(tb) {
			y = tb[i]
		} else {
			y = token{kind: kindNull}
		}
		if c := compareToken(x, y); c != 0 {
			return c
		}
	}
	return 0
}

// Less is Compare(a,b) < 0.
func Less(a, b string) bool { return Compare(a, b) < 0 }

type kind int

const (
	kindNull kind = iota
	kindInt
	kindStr
)

type token struct {
	kind kind
	num  int64
	str  string
}

func tokenize(v string) []token {
	v = strings.ToLower(v)
	var out []token
	var cur strings.Builder
	flush := func(isDigit bool) {
		if cur.Len() == 0 {
			return
		}
		s := cur.String()
		cur.Reset()
		if isDigit {
			n, _ := strconv.ParseInt(s, 10, 64)
			out = append(out, token{kind: kindInt, num: n})
		} else {
			out = append(out, token{kind: kindStr, str: s})
		}
	}
	prevDigit := false
	for i, r := range v {
		switch {
		case r == '.' || r == '-' || r == '_':
			flush(prevDigit)
		case unicode.IsDigit(r):
			if i > 0 && !prevDigit && cur.Len() > 0 {
				flush(false)
			}
			cur.WriteRune(r)
			prevDigit = true
			continue
		default:
			if cur.Len() > 0 && prevDigit {
				flush(true)
			}
			cur.WriteRune(r)
			prevDigit = false
			continue
		}
		prevDigit = false
	}
	flush(prevDigit)
	// Trim trailing zero / empty tokens so 1.0 == 1.
	for len(out) > 0 {
		last := out[len(out)-1]
		if (last.kind == kindInt && last.num == 0) || (last.kind == kindStr && qualifierRank(last.str) == rankRelease) {
			out = out[:len(out)-1]
			continue
		}
		break
	}
	return out
}

const (
	rankAlpha = iota
	rankBeta
	rankMilestone
	rankRC
	rankSnapshot
	rankRelease
	rankSP
	rankUnknown
)

func qualifierRank(s string) int {
	switch s {
	case "alpha", "a":
		return rankAlpha
	case "beta", "b":
		return rankBeta
	case "milestone", "m":
		return rankMilestone
	case "rc", "cr":
		return rankRC
	case "snapshot":
		return rankSnapshot
	case "", "ga", "final", "release":
		return rankRelease
	case "sp":
		return rankSP
	}
	return rankUnknown
}

func compareToken(x, y token) int {
	switch {
	case x.kind == kindNull && y.kind == kindNull:
		return 0
	case x.kind == kindNull:
		return -compareToken(y, x)
	case y.kind == kindNull:
		// int vs null: int > null (1.1 > 1); str vs null: qualifier < release.
		if x.kind == kindInt {
			if x.num == 0 {
				return 0
			}
			return 1
		}
		r := qualifierRank(x.str)
		if r < rankRelease {
			return -1
		}
		if r == rankRelease {
			return 0
		}
		return 1
	case x.kind == kindInt && y.kind == kindInt:
		switch {
		case x.num < y.num:
			return -1
		case x.num > y.num:
			return 1
		}
		return 0
	case x.kind == kindInt:
		return 1 // numbers are newer than qualifiers
	case y.kind == kindInt:
		return -1
	}
	rx, ry := qualifierRank(x.str), qualifierRank(y.str)
	if rx != ry {
		if rx < ry {
			return -1
		}
		return 1
	}
	if rx == rankUnknown {
		return strings.Compare(x.str, y.str)
	}
	return 0
}

// IsSnapshotVersion reports whether v ends with -SNAPSHOT.
func IsSnapshotVersion(v string) bool { return strings.HasSuffix(v, "-SNAPSHOT") }
