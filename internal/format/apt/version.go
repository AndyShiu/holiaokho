package apt

import (
	"strconv"
	"unicode"

	"github.com/holiaokho/holiaokho/internal/storage"
)

type storageDigest = storage.Digest

// debVersionLess implements Debian version comparison (epoch:upstream-revision).
func debVersionLess(a, b string) bool { return debCompare(a, b) < 0 }

func debCompare(a, b string) int {
	ea, ua, ra := splitDeb(a)
	eb, ub, rb := splitDeb(b)
	if ea != eb {
		return ea - eb
	}
	if c := verFragCompare(ua, ub); c != 0 {
		return c
	}
	return verFragCompare(ra, rb)
}

func splitDeb(v string) (epoch int, upstream, revision string) {
	for i := 0; i < len(v); i++ {
		if v[i] == ':' {
			epoch, _ = strconv.Atoi(v[:i])
			v = v[i+1:]
			break
		}
		if !unicode.IsDigit(rune(v[i])) {
			break
		}
	}
	for i := len(v) - 1; i >= 0; i-- {
		if v[i] == '-' {
			return epoch, v[:i], v[i+1:]
		}
	}
	return epoch, v, ""
}

func order(c byte) int {
	switch {
	case c == '~':
		return -1
	case c == 0:
		return 0
	case unicode.IsDigit(rune(c)):
		return 0
	case unicode.IsLetter(rune(c)):
		return int(c)
	default:
		return int(c) + 256
	}
}

func verFragCompare(a, b string) int {
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		// Non-digit run.
		for (i < len(a) && !unicode.IsDigit(rune(a[i]))) || (j < len(b) && !unicode.IsDigit(rune(b[j]))) {
			var ca, cb byte
			if i < len(a) && !unicode.IsDigit(rune(a[i])) {
				ca = a[i]
			}
			if j < len(b) && !unicode.IsDigit(rune(b[j])) {
				cb = b[j]
			}
			if ca == 0 && cb == 0 {
				break
			}
			if oa, ob := order(ca), order(cb); oa != ob {
				return oa - ob
			}
			if ca != 0 {
				i++
			}
			if cb != 0 {
				j++
			}
		}
		// Digit run.
		var na, nb int
		for i < len(a) && unicode.IsDigit(rune(a[i])) {
			na = na*10 + int(a[i]-'0')
			i++
		}
		for j < len(b) && unicode.IsDigit(rune(b[j])) {
			nb = nb*10 + int(b[j]-'0')
			j++
		}
		if na != nb {
			return na - nb
		}
	}
	return 0
}
