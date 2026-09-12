package pypi

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/holiaokho/holiaokho/internal/storage"
)

func digest(s string) storage.Digest { return storage.Digest(s) }

var pep440Re = regexp.MustCompile(`^v?(?:(\d+)!)?(\d+(?:\.\d+)*)(?:[-_.]?(a|b|rc|alpha|beta|c|pre|preview)[-_.]?(\d*))?(?:[-_.]?(post|rev|r)[-_.]?(\d*)|-(\d+))?(?:[-_.]?dev[-_.]?(\d*))?(?:\+.*)?$`)

type pep440 struct {
	epoch   int
	release []int
	pre     int // -1 none; else rank*1000+num
	post    int // -1 none
	dev     int // -1 none
}

func parsePEP440(v string) (pep440, bool) {
	m := pep440Re.FindStringSubmatch(strings.ToLower(strings.TrimSpace(v)))
	if m == nil {
		return pep440{}, false
	}
	p := pep440{pre: -1, post: -1, dev: -1}
	if m[1] != "" {
		p.epoch, _ = strconv.Atoi(m[1])
	}
	for _, x := range strings.Split(m[2], ".") {
		n, _ := strconv.Atoi(x)
		p.release = append(p.release, n)
	}
	for len(p.release) > 1 && p.release[len(p.release)-1] == 0 {
		p.release = p.release[:len(p.release)-1]
	}
	if m[3] != "" {
		rank := map[string]int{"a": 0, "alpha": 0, "b": 1, "beta": 1, "c": 2, "pre": 2, "preview": 2, "rc": 2}[m[3]]
		n, _ := strconv.Atoi(m[4])
		p.pre = rank*1000 + n
	}
	if m[5] != "" {
		n, _ := strconv.Atoi(m[6])
		p.post = n
	} else if m[7] != "" {
		p.post, _ = strconv.Atoi(m[7])
	}
	if strings.Contains(m[0], "dev") {
		n, _ := strconv.Atoi(m[8])
		p.dev = n
	}
	return p, true
}

func pep440Less(a, b string) bool {
	pa, oka := parsePEP440(a)
	pb, okb := parsePEP440(b)
	if oka != okb {
		return oka
	}
	if !oka {
		return a < b
	}
	if pa.epoch != pb.epoch {
		return pa.epoch < pb.epoch
	}
	n := max(len(pa.release), len(pb.release))
	for i := 0; i < n; i++ {
		var x, y int
		if i < len(pa.release) {
			x = pa.release[i]
		}
		if i < len(pb.release) {
			y = pb.release[i]
		}
		if x != y {
			return x < y
		}
	}
	// PEP 440 ordering keys (see packaging's _cmpkey):
	//   pre:  dev-only release → -inf; no pre → +inf; else (rank, n)
	//   post: none → -inf
	//   dev:  none → +inf
	const inf = 1 << 30
	key := func(p pep440) [3]int {
		pre := p.pre
		if p.pre < 0 && p.post < 0 && p.dev >= 0 {
			pre = -inf
		} else if p.pre < 0 {
			pre = inf
		}
		post := p.post
		if post < 0 {
			post = -inf
		}
		dev := p.dev
		if dev < 0 {
			dev = inf
		}
		return [3]int{pre, post, dev}
	}
	ka, kb := key(pa), key(pb)
	for i := 0; i < 3; i++ {
		if ka[i] != kb[i] {
			return ka[i] < kb[i]
		}
	}
	return false
}
