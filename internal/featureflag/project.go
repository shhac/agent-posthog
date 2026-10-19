// Package featureflag reads, plans, and writes PostHog feature flags. It
// returns results for the caller to present and never prints; flag specs and
// patches themselves are modelled in flagspec.
package featureflag

import (
	"github.com/shhac/agent-posthog/internal/api"
)

// Project is one PostHog project's feature flags, reached through Client.
type Project struct {
	Client *api.Client
	ID     int
	OrgID  string
	Host   string
	// Alias is the profile's name for the project, when it has one.
	Alias string
}

// In is another project in the same organization and host, for diffs and
// copies that look across projects.
func (p Project) In(id int, alias string) Project {
	p.ID, p.Alias = id, alias
	return p
}

func (p Project) flagsPath(segments ...string) string {
	return api.ProjectPath(p.ID, append([]string{"feature_flags"}, segments...)...)
}

func (p Project) flagPath(id string, segments ...string) string {
	return p.flagsPath(append([]string{id}, segments...)...)
}
