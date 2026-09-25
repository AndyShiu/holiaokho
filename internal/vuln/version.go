package vuln

import (
	"strconv"
	"strings"
	"unicode"
)

// compareVersions orders two version strings: negative, zero or positive.
//
// It is not any one ecosystem's rules but what they share, which is enough
// for the one job it has — choosing among the fixed versions OSV lists for a
// package: numbers compare as numbers (2.10 after 2.9), a leading "v" is
// ignored (Go), and a pre-release (alpha, beta, rc, snapshot, a trailing
// "-something" on npm) comes before the release it precedes.
func compareVersions(a, b string) int {
	ta, tb := tokens(a), tokens(b)
	for i := 0; i < len(ta) || i < len(tb); i++ {
		var x, y token
		if i < len(ta) {
			x = ta[i]
		}
		if i < len(tb) {
			y = tb[i]
		}
		if c := x.compare(y); c != 0 {
			return c
		}
	}
	return 0
}

type token struct {
	num   int
	text  string
	isNum bool
	set   bool
}

// weight places a token that is absent (the end of a shorter version), a
// number, and a word relative to each other. 1.0 == 1.0.0; 1.0-rc1 < 1.0;
// 1.0.1 > 1.0.
func (t token) weight() int {
	switch {
	case !t.set:
		return 1
	case t.isNum:
		return 2
	case isPreRelease(t.text):
		return 0
	default:
		return 3 // a qualifier after a release, like Maven's "RELEASE" or "jre"
	}
}

func (t token) compare(o token) int {
	if t.set && o.set && t.isNum && o.isNum {
		return t.num - o.num
	}
	if !t.set && o.set && o.isNum && o.num == 0 || !o.set && t.set && t.isNum && t.num == 0 {
		return 0 // 1.0 equals 1.0.0
	}
	if w, v := t.weight(), o.weight(); w != v {
		return w - v
	}
	return strings.Compare(t.text, o.text)
}

func isPreRelease(s string) bool {
	for _, p := range []string{"alpha", "beta", "pre", "rc", "cr", "m", "dev", "snapshot", "a", "b", "preview", "ea"} {
		if s == p || strings.HasPrefix(s, p) && len(s) > len(p) && unicode.IsDigit(rune(s[len(p)])) {
			return true
		}
	}
	return false
}

func tokens(v string) []token {
	v = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(v), "v"))
	// Build metadata (semver "+…") does not order versions.
	if i := strings.IndexByte(v, '+'); i >= 0 && !strings.Contains(v[i:], "incompatible") {
		v = v[:i]
	}
	var out []token
	var cur strings.Builder
	var digits bool
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		s := cur.String()
		if digits {
			n, _ := strconv.Atoi(s)
			out = append(out, token{num: n, isNum: true, set: true})
		} else {
			out = append(out, token{text: s, set: true})
		}
		cur.Reset()
	}
	for _, r := range v {
		switch {
		case unicode.IsDigit(r):
			if !digits {
				flush()
			}
			digits = true
			cur.WriteRune(r)
		case unicode.IsLetter(r):
			if digits {
				flush()
			}
			digits = false
			cur.WriteRune(r)
		default: // . - _ ~ separate tokens
			flush()
			digits = false
		}
	}
	flush()
	return out
}

// fixFor picks, from the versions a vulnerability is fixed in, the one to
// upgrade to from current: the lowest that is higher than current. OSV lists
// fixes on every maintained branch — Log4Shell is fixed in 2.3.1, 2.12.2 and
// 2.15.0 — and for 2.14.1 only 2.15.0 is an upgrade. Empty when none is.
//
// A version can also carry a flavour: Guava ships every release as -jre and
// -android, and advisories list only the -android one. Telling someone on
// 31.1-jre to move to 32.0.0-android would switch them to the wrong
// artifact, so a fix in another flavour is given in the current one.
func fixFor(current string, fixed []string) string {
	best := ""
	cf := flavour(current)
	for _, f := range fixed {
		if ff := flavour(f); cf != "" && ff != "" && ff != cf {
			f = strings.TrimSuffix(f, ff) + cf
		}
		if compareVersions(f, current) > 0 && (best == "" || compareVersions(f, best) < 0) {
			best = f
		}
	}
	return best
}

// flavour is a trailing word naming a variant rather than a pre-release —
// "jre" in 31.1-jre — or "" when there is none.
func flavour(v string) string {
	i := strings.LastIndexAny(v, "-.")
	if i < 0 || i == len(v)-1 {
		return ""
	}
	w := strings.ToLower(v[i+1:])
	for _, r := range w {
		if !unicode.IsLetter(r) {
			return ""
		}
	}
	if isPreRelease(w) || w == "release" || w == "final" || w == "ga" {
		return ""
	}
	return v[i+1:]
}
