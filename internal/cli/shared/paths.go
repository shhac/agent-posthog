package shared

import (
	"strconv"

	"github.com/shhac/agent-posthog/internal/api"
)

// Scope is the part of PostHog a command works in.
type Scope int

const (
	ScopeAPI Scope = iota
	ScopeOrg
	ScopeProject
	ScopeEnvironment
)

// Path checks the scope is configured, then builds a PostHog API Path under
// it: Path(ScopeProject, "feature_flags", id) is /api/projects/<id>/feature_flags/<id>/.
func (r *Resolved) Path(s Scope, segments ...string) (string, error) {
	if err := r.require(s); err != nil {
		return "", err
	}
	switch s {
	case ScopeOrg:
		return api.Path(append([]string{"organizations", r.OrgID}, segments...)...), nil
	case ScopeProject:
		return api.ProjectPath(r.ProjectID, segments...), nil
	case ScopeEnvironment:
		return api.Path(append([]string{"environments", strconv.Itoa(r.EnvironmentID)}, segments...)...), nil
	default:
		return api.Path(segments...), nil
	}
}

func (r *Resolved) require(s Scope) error {
	switch s {
	case ScopeOrg:
		return RequireOrg(r)
	case ScopeProject:
		return RequireProject(r)
	case ScopeEnvironment:
		return requireEnvironment(r)
	default:
		return nil
	}
}
