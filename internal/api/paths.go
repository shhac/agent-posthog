package api

import (
	"strconv"
	"strings"
)

// Path joins segments under /api/ with PostHog's trailing slash.
func Path(segments ...string) string {
	return "/api/" + strings.Join(segments, "/") + "/"
}

// ProjectPath builds a path in a project: ProjectPath(1, "feature_flags") is
// /api/projects/1/feature_flags/.
func ProjectPath(projectID int, segments ...string) string {
	return Path(append([]string{"projects", strconv.Itoa(projectID)}, segments...)...)
}
