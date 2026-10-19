package cli

import (
	"strconv"
	"strings"
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
		return apiPath(append([]string{"organizations", r.OrgID}, segments...)...), nil
	case scopeProject:
		return projectAPIPath(r.ProjectID, segments...), nil
	case scopeEnvironment:
		return apiPath(append([]string{"environments", strconv.Itoa(r.EnvironmentID)}, segments...)...), nil
	default:
		return apiPath(segments...), nil
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

// projectAPIPath builds a path in a given project, for commands that reach
// beyond the current one (diff, copy).
func projectAPIPath(projectID int, segments ...string) string {
	return apiPath(append([]string{"projects", strconv.Itoa(projectID)}, segments...)...)
}

// apiPath joins segments under /api/ with PostHog's trailing slash.
func apiPath(segments ...string) string {
	return "/api/" + strings.Join(segments, "/") + "/"
}

func flagsPath(projectID int, segments ...string) string {
	return projectAPIPath(projectID, append([]string{"feature_flags"}, segments...)...)
}

func flagPath(projectID int, id string, segments ...string) string {
	return flagsPath(projectID, append([]string{id}, segments...)...)
}
