package content

import (
	"context"
	"strings"
	"sync"
)

// Sort is a column and direction a client asked for. It is only ever used
// through OrderBy, which resolves the key against a fixed list, so nothing
// a client sends reaches SQL.
type Sort struct {
	Key  string
	Desc bool
}

// ParseSort reads "key" and "order" ("asc" / "desc") request values.
func ParseSort(key, order string) Sort {
	return Sort{Key: key, Desc: strings.EqualFold(order, "desc") || strings.EqualFold(order, "descend")}
}

// SortColumn is how one sortable key orders: the SQL expressions, in order,
// and whether they hold version strings.
type SortColumn struct {
	Exprs   []string
	Version bool
}

var numericCollation struct {
	once sync.Once
	ok   bool
}

// versionCollation is " COLLATE numeric_text" when migration 0006 managed to
// create it, so 2.10 sorts after 2.9, and "" on a PostgreSQL without ICU.
func (s *Service) versionCollation(ctx context.Context) string {
	numericCollation.once.Do(func() {
		var n int
		if err := s.DB.Pool.QueryRow(ctx, `SELECT count(*) FROM pg_collation WHERE collname = 'numeric_text'`).Scan(&n); err == nil {
			numericCollation.ok = n > 0
		}
	})
	if numericCollation.ok {
		return ` COLLATE numeric_text`
	}
	return ""
}

// OrderBy builds an ORDER BY clause for sort from cols. An unknown key gives
// def. tie is appended last so that rows equal on the sorted column keep a
// fixed order — without it the same row can turn up on two pages.
func (s *Service) OrderBy(ctx context.Context, sort Sort, cols map[string]SortColumn, def, tie string) string {
	c, ok := cols[sort.Key]
	if !ok {
		return " ORDER BY " + def + ", " + tie
	}
	dir := " ASC NULLS LAST"
	if sort.Desc {
		dir = " DESC NULLS LAST"
	}
	coll := ""
	if c.Version {
		coll = s.versionCollation(ctx)
	}
	parts := make([]string, len(c.Exprs))
	for i, e := range c.Exprs {
		parts[i] = e + coll + dir
	}
	return " ORDER BY " + strings.Join(parts, ", ") + ", " + tie
}
