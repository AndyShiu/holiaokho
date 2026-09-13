package auth

import (
	"strings"

	"github.com/holiaokho/holiaokho/internal/model"
)

// Actions.
const (
	Read   = "read"
	Write  = "write"
	Delete = "delete"
	Admin  = "admin"
)

// Principal is the resolved identity of a request.
type Principal struct {
	Username   string
	Roles      []string
	Privileges []model.Privilege
	Anonymous  bool
	// Via records how the principal authenticated: basic|token|session|anonymous.
	Via string
	// MustChangePassword is carried from the user record so the API can lock
	// the account down to the password-change endpoint.
	MustChangePassword bool
}

// Can reports whether the principal may perform action on target.
// target forms: "repo:<name>", "format:<format>", "app:<area>".
// Repo privileges also match by format when the caller passes both via
// CanRepo.
func (p *Principal) Can(target, action string) bool {
	if p == nil {
		return false
	}
	for _, pr := range p.Privileges {
		if matchTarget(pr.Target, target) && matchAction(pr.Actions, action) {
			return true
		}
	}
	return false
}

// CanRepo checks repo-level permission, honouring "repo:*", "format:<f>" and "*".
func (p *Principal) CanRepo(repoName, format, action string) bool {
	return p.Can("repo:"+repoName, action) || p.Can("format:"+format, action)
}

// SelectorLookup resolves content selectors by name; set by the server.
var SelectorLookup func(name string) *ContentSelector

// CanContent checks permission for a specific path inside a repository,
// including content-selector privileges ("selector:<name>@<repo|*>").
func (p *Principal) CanContent(repoName, format, path, action string) bool {
	if p.CanRepo(repoName, format, action) {
		return true
	}
	if p == nil || SelectorLookup == nil {
		return false
	}
	for _, pr := range p.Privileges {
		if !strings.HasPrefix(pr.Target, "selector:") || !matchAction(pr.Actions, action) {
			continue
		}
		spec := strings.TrimPrefix(pr.Target, "selector:")
		name, repoPat := spec, "*"
		if i := strings.IndexByte(spec, '@'); i >= 0 {
			name, repoPat = spec[:i], spec[i+1:]
		}
		if repoPat != "*" && repoPat != repoName {
			continue
		}
		if sel := SelectorLookup(name); sel != nil && sel.Match(SelectorInput{Format: format, Path: path}) {
			return true
		}
	}
	return false
}

func matchTarget(granted, want string) bool {
	if granted == "*" {
		return true
	}
	if granted == want {
		return true
	}
	// "repo:*" / "app:*" / "format:*"
	if strings.HasSuffix(granted, ":*") {
		return strings.HasPrefix(want, strings.TrimSuffix(granted, "*"))
	}
	return false
}

func matchAction(granted []string, want string) bool {
	for _, a := range granted {
		if a == "*" || a == want {
			return true
		}
		// admin implies everything on that target.
		if a == Admin {
			return true
		}
		// write implies read.
		if a == Write && want == Read {
			return true
		}
	}
	return false
}
