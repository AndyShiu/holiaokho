package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"
)

// Content selectors implement the subset of Nexus CSEL that matters in
// practice:
//
//	format == "maven"
//	path =^ "/com/acme/"            (starts with)
//	path =~ "^/com/acme/.*-SNAPSHOT" (regex)
//	coordinate.groupId == "com.acme"
//	a and b, a or b, not a, ( ... ), != for every operator
//
// A selector is referenced from privileges as target "selector:<name>@<repo>"
// (repo may be "*"), granting the actions only on matching content.

type ContentSelector struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Expression  string    `json:"expression"`
	CreatedAt   time.Time `json:"createdAt"`
	node        node
}

// SelectorInput is what an expression is evaluated against.
type SelectorInput struct {
	Format string
	Path   string            // leading slash
	Coord  map[string]string // coordinate.* fields (groupId, artifactId, version, name, ...)
}

func (c *ContentSelector) Compile() error {
	n, err := parseExpr(c.Expression)
	if err != nil {
		return err
	}
	c.node = n
	return nil
}

func (c *ContentSelector) Match(in SelectorInput) bool {
	if c.node == nil {
		if err := c.Compile(); err != nil {
			return false
		}
	}
	if !strings.HasPrefix(in.Path, "/") {
		in.Path = "/" + in.Path
	}
	return c.node.eval(in)
}

// ------------------------------------------------------------- parsing

type node interface{ eval(SelectorInput) bool }

type andNode struct{ l, r node }
type orNode struct{ l, r node }
type notNode struct{ n node }
type cmpNode struct {
	field, op, value string
	re               *regexp.Regexp
}

func (n andNode) eval(in SelectorInput) bool { return n.l.eval(in) && n.r.eval(in) }
func (n orNode) eval(in SelectorInput) bool  { return n.l.eval(in) || n.r.eval(in) }
func (n notNode) eval(in SelectorInput) bool { return !n.n.eval(in) }
func (n cmpNode) eval(in SelectorInput) bool {
	var v string
	switch {
	case n.field == "format":
		v = in.Format
	case n.field == "path":
		v = in.Path
	case strings.HasPrefix(n.field, "coordinate."):
		v = in.Coord[strings.TrimPrefix(n.field, "coordinate.")]
	default:
		return false
	}
	switch n.op {
	case "==":
		return v == n.value
	case "!=":
		return v != n.value
	case "=^":
		return strings.HasPrefix(v, n.value)
	case "=~":
		return n.re.MatchString(v)
	}
	return false
}

type parser struct {
	toks []string
	pos  int
}

func tokenize(s string) ([]string, error) {
	var toks []string
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case unicode.IsSpace(rune(c)):
			i++
		case c == '(' || c == ')':
			toks = append(toks, string(c))
			i++
		case c == '"' || c == '\'':
			j := i + 1
			for j < len(s) && s[j] != c {
				j++
			}
			if j >= len(s) {
				return nil, errors.New("unterminated string")
			}
			toks = append(toks, "\x00"+s[i+1:j])
			i = j + 1
		case c == '=' || c == '!':
			if i+1 < len(s) && (s[i+1] == '=' || s[i+1] == '^' || s[i+1] == '~') {
				toks = append(toks, s[i:i+2])
				i += 2
			} else {
				return nil, fmt.Errorf("bad operator at %d", i)
			}
		default:
			j := i
			for j < len(s) && (unicode.IsLetter(rune(s[j])) || unicode.IsDigit(rune(s[j])) || s[j] == '.' || s[j] == '_') {
				j++
			}
			if j == i {
				return nil, fmt.Errorf("unexpected character %q", c)
			}
			toks = append(toks, s[i:j])
			i = j
		}
	}
	return toks, nil
}

func parseExpr(s string) (node, error) {
	toks, err := tokenize(s)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	n, err := p.or()
	if err != nil {
		return nil, err
	}
	if p.pos != len(p.toks) {
		return nil, fmt.Errorf("unexpected token %q", p.toks[p.pos])
	}
	return n, nil
}

func (p *parser) peek() string {
	if p.pos < len(p.toks) {
		return p.toks[p.pos]
	}
	return ""
}
func (p *parser) next() string { t := p.peek(); p.pos++; return t }

func (p *parser) or() (node, error) {
	l, err := p.and()
	if err != nil {
		return nil, err
	}
	for strings.EqualFold(p.peek(), "or") {
		p.next()
		r, err := p.and()
		if err != nil {
			return nil, err
		}
		l = orNode{l, r}
	}
	return l, nil
}

func (p *parser) and() (node, error) {
	l, err := p.unary()
	if err != nil {
		return nil, err
	}
	for strings.EqualFold(p.peek(), "and") {
		p.next()
		r, err := p.unary()
		if err != nil {
			return nil, err
		}
		l = andNode{l, r}
	}
	return l, nil
}

func (p *parser) unary() (node, error) {
	switch {
	case strings.EqualFold(p.peek(), "not"):
		p.next()
		n, err := p.unary()
		if err != nil {
			return nil, err
		}
		return notNode{n}, nil
	case p.peek() == "(":
		p.next()
		n, err := p.or()
		if err != nil {
			return nil, err
		}
		if p.next() != ")" {
			return nil, errors.New("missing )")
		}
		return n, nil
	}
	field := p.next()
	if field == "" || strings.HasPrefix(field, "\x00") {
		return nil, errors.New("expected field name")
	}
	op := p.next()
	switch op {
	case "==", "!=", "=^", "=~":
	default:
		return nil, fmt.Errorf("expected operator after %s", field)
	}
	val := p.next()
	if !strings.HasPrefix(val, "\x00") {
		return nil, fmt.Errorf("expected quoted string after %s %s", field, op)
	}
	n := cmpNode{field: field, op: op, value: val[1:]}
	if op == "=~" {
		re, err := regexp.Compile(n.value)
		if err != nil {
			return nil, fmt.Errorf("regex %q: %w", n.value, err)
		}
		n.re = re
	}
	return n, nil
}

// ------------------------------------------------------------ storage

func (s *Service) ListSelectors(ctx context.Context) ([]ContentSelector, error) {
	rows, err := s.DB.Pool.Query(ctx, `SELECT id, name, description, expression, created_at FROM content_selectors ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ContentSelector
	for rows.Next() {
		var c ContentSelector
		if err := rows.Scan(&c.ID, &c.Name, &c.Description, &c.Expression, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func (s *Service) SaveSelector(ctx context.Context, c *ContentSelector, create bool) error {
	if c.Name == "" {
		return errors.New("name required")
	}
	if err := c.Compile(); err != nil {
		return fmt.Errorf("expression: %w", err)
	}
	defer s.invalidateSelectors()
	if create {
		c.ID = uuid.New()
		_, err := s.DB.Pool.Exec(ctx, `INSERT INTO content_selectors(id,name,description,expression) VALUES ($1,$2,$3,$4)`, c.ID, c.Name, c.Description, c.Expression)
		if err != nil && strings.Contains(err.Error(), "23505") {
			return ErrConflict
		}
		return err
	}
	tag, err := s.DB.Pool.Exec(ctx, `UPDATE content_selectors SET name=$2, description=$3, expression=$4 WHERE id=$1`, c.ID, c.Name, c.Description, c.Expression)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) DeleteSelector(ctx context.Context, id uuid.UUID) error {
	defer s.invalidateSelectors()
	tag, err := s.DB.Pool.Exec(ctx, `DELETE FROM content_selectors WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

var selectorCache struct {
	mu   sync.RWMutex
	byNm map[string]*ContentSelector
	at   time.Time
}

func (s *Service) invalidateSelectors() {
	selectorCache.mu.Lock()
	selectorCache.at = time.Time{}
	selectorCache.mu.Unlock()
}

// Selector returns a compiled selector by name (cached 30s).
func (s *Service) Selector(name string) *ContentSelector {
	selectorCache.mu.RLock()
	c, ok := selectorCache.byNm[name]
	fresh := time.Since(selectorCache.at) < 30*time.Second
	selectorCache.mu.RUnlock()
	if ok && fresh {
		return c
	}
	list, err := s.ListSelectors(context.Background())
	if err != nil {
		return c
	}
	m := map[string]*ContentSelector{}
	for i := range list {
		if err := list[i].Compile(); err == nil {
			m[list[i].Name] = &list[i]
		}
	}
	selectorCache.mu.Lock()
	selectorCache.byNm, selectorCache.at = m, time.Now()
	selectorCache.mu.Unlock()
	return m[name]
}

var _ = json.Marshal
