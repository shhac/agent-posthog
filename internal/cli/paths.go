package cli

import (
	"strconv"

	"github.com/shhac/agent-posthog/internal/api"
)

// scope is the part of PostHog a command works in.
type scope int

const (
	scopeAPI scope = iota
	scopeOrg
	scopeProject
	scopeEnvironment
)

// path checks the scope is configured, then builds a PostHog API path under
// it: path(scopeProject, "feature_flags", id) is /api/projects/<id>/feature_flags/<id>/.
func (r *resolvedContext) path(s scope, segments ...string) (string, error) {
	if err := r.require(s); err != nil {
		return "", err
	}
	switch s {
	case scopeOrg:
		return api.Path(append([]string{"organizations", r.OrgID}, segments...)...), nil
	case scopeProject:
		return api.ProjectPath(r.ProjectID, segments...), nil
	case scopeEnvironment:
		return api.Path(append([]string{"environments", strconv.Itoa(r.EnvironmentID)}, segments...)...), nil
	default:
		return api.Path(segments...), nil
	}
}

func (r *resolvedContext) require(s scope) error {
	switch s {
	case scopeOrg:
		return requireOrg(r)
	case scopeProject:
		return requireProject(r)
	case scopeEnvironment:
		return requireEnvironment(r)
	default:
		return nil
	}
}
