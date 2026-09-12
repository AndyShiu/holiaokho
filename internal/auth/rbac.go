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
