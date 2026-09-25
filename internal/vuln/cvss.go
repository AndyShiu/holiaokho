package vuln

import (
	"math"
	"strings"
)

// cvss3Score computes the CVSS v3.x base score of a vector such as
// "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H". ok is false for anything
// that is not a complete v3 vector.
//
// Needed because a good share of OSV records — Go's and PyPI's own among
// them — carry a vector but no rating, and a vulnerability without a severity
// cannot be sorted, filtered or decided on.
//
// The formula is the one in the CVSS v3.1 specification, section 7.
func cvss3Score(vector string) (float64, bool) {
	parts := strings.Split(vector, "/")
	if len(parts) == 0 || !strings.HasPrefix(parts[0], "CVSS:3.") {
		return 0, false
	}
	m := map[string]string{}
	for _, p := range parts[1:] {
		if k, v, ok := strings.Cut(p, ":"); ok {
			m[k] = v
		}
	}
	scope := m["S"]
	changed := scope == "C"
	if scope != "U" && !changed {
		return 0, false
	}
	pick := func(key string, w map[string]float64) (float64, bool) {
		v, ok := w[m[key]]
		return v, ok
	}
	cia := map[string]float64{"H": 0.56, "L": 0.22, "N": 0}
	av, ok1 := pick("AV", map[string]float64{"N": 0.85, "A": 0.62, "L": 0.55, "P": 0.2})
	ac, ok2 := pick("AC", map[string]float64{"L": 0.77, "H": 0.44})
	prW := map[string]float64{"N": 0.85, "L": 0.62, "H": 0.27}
	if changed {
		prW = map[string]float64{"N": 0.85, "L": 0.68, "H": 0.5}
	}
	pr, ok3 := pick("PR", prW)
	ui, ok4 := pick("UI", map[string]float64{"N": 0.85, "R": 0.62})
	c, ok5 := pick("C", cia)
	i, ok6 := pick("I", cia)
	a, ok7 := pick("A", cia)
	if !(ok1 && ok2 && ok3 && ok4 && ok5 && ok6 && ok7) {
		return 0, false
	}

	iss := 1 - (1-c)*(1-i)*(1-a)
	var impact float64
	if changed {
		impact = 7.52*(iss-0.029) - 3.25*math.Pow(iss-0.02, 15)
	} else {
		impact = 6.42 * iss
	}
	if impact <= 0 {
		return 0, true
	}
	exploitability := 8.22 * av * ac * pr * ui
	if changed {
		return roundUp(math.Min(1.08*(impact+exploitability), 10)), true
	}
	return roundUp(math.Min(impact+exploitability, 10)), true
}

// roundUp is the specification's Roundup: the smallest one-decimal number
// not below x, computed on integers so 4.000000001 does not become 4.1.
func roundUp(x float64) float64 {
	n := int64(math.Round(x * 100000))
	if n%10000 == 0 {
		return float64(n) / 100000
	}
	return float64(n/10000+1) / 10
}

// ratingFor maps a CVSS score to the rating scale the rest of the system
// uses. CVSS calls the middle band MEDIUM; GitHub's advisories, where most
// ratings come from, call it MODERATE, and one name is kept.
func ratingFor(score float64) string {
	switch {
	case score >= 9:
		return SeverityCritical
	case score >= 7:
		return SeverityHigh
	case score >= 4:
		return SeverityModerate
	case score > 0:
		return SeverityLow
	}
	return SeverityUnknown
}
