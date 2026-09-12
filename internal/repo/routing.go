package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/holiaokho/holiaokho/internal/model"
)

// RoutingRule decides whether a request path may be served by a repository.
// mode "block": any matcher hit → denied. mode "allow": only matcher hits are served.
type RoutingRule struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Mode        string    `json:"mode"`
	Matchers    []string  `json:"matchers"`
	CreatedAt   time.Time `json:"createdAt"`
	compiled    []*regexp.Regexp
}

func (r *RoutingRule) Compile() error {
	r.compiled = r.compiled[:0]
	for _, m := range r.Matchers {
		re, err := regexp.Compile(m)
		if err != nil {
			return fmt.Errorf("matcher %q: %w", m, err)
		}
		r.compiled = append(r.compiled, re)
	}
	if r.Mode != "allow" && r.Mode != "block" {
		return fmt.Errorf("mode must be allow or block")
	}
	return nil
}

// Allows reports whether path (with a leading slash) passes the rule.
func (r *RoutingRule) Allows(path string) bool {
	if len(path) == 0 || path[0] != '/' {
		path = "/" + path
	}
	hit := false
	for _, re := range r.compiled {
		if re.MatchString(path) {
			hit = true
			break
		}
	}
	if r.Mode == "allow" {
		return hit
	}
	return !hit
}

// routing caches compiled rules by repository name.
type routing struct {
	mu    sync.RWMutex
	rules map[uuid.UUID]*RoutingRule
	at    time.Time
}

// RuleFor returns the compiled routing rule assigned to a repository, or nil.
func (e *Engine) RuleFor(ctx context.Context, rp *model.Repository) *RoutingRule {
	if rp.RoutingRuleID == nil {
		return nil
	}
	e.routing.mu.RLock()
	fresh := time.Since(e.routing.at) < 30*time.Second
	rule, ok := e.routing.rules[*rp.RoutingRuleID]
	e.routing.mu.RUnlock()
	if ok && fresh {
		return rule
	}
	rules, err := ListRoutingRules(ctx, e.Content.DB.Pool)
	if err != nil {
		e.Log.Warn("load routing rules", "err", err)
		return rule
	}
	m := map[uuid.UUID]*RoutingRule{}
	for i := range rules {
		if err := rules[i].Compile(); err == nil {
			m[rules[i].ID] = &rules[i]
		}
	}
	e.routing.mu.Lock()
	e.routing.rules, e.routing.at = m, time.Now()
	e.routing.mu.Unlock()
	return m[*rp.RoutingRuleID]
}

// InvalidateRouting drops the cache (called after rule changes).
func (e *Engine) InvalidateRouting() {
	e.routing.mu.Lock()
	e.routing.at = time.Time{}
	e.routing.mu.Unlock()
}

// ListRoutingRules reads all rules from the database.
func ListRoutingRules(ctx context.Context, pool *pgxpool.Pool) ([]RoutingRule, error) {
	rs, err := pool.Query(ctx, `SELECT id, name, description, mode, matchers, created_at FROM routing_rules ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	var out []RoutingRule
	for rs.Next() {
		var r RoutingRule
		var m []byte
		if err := rs.Scan(&r.ID, &r.Name, &r.Description, &r.Mode, &m, &r.CreatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal(m, &r.Matchers)
		out = append(out, r)
	}
	return out, nil
}
