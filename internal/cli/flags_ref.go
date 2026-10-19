package cli

import (
	"context"

	"github.com/shhac/agent-posthog/internal/featureflag"
)

// flagProject is the current project's feature flags; a project must be
// configured.
func (r *resolvedContext) flagProject() (featureflag.Project, error) {
	if err := requireProject(r); err != nil {
		return featureflag.Project{}, err
	}
	return featureflag.Project{Client: r.Client, ID: r.ProjectID, OrgID: r.OrgID, Host: r.Host, Alias: r.ProjectAlias}, nil
}

// currentFlag reads the flag a command targets in the current project.
func currentFlag(ctx context.Context, resolved *resolvedContext, ref string) (map[string]any, error) {
	p, err := resolved.flagProject()
	if err != nil {
		return nil, err
	}
	return p.Fetch(ctx, ref)
}

func resolveFlagID(ctx context.Context, resolved *resolvedContext, ref string) (string, error) {
	p, err := resolved.flagProject()
	if err != nil {
		return "", err
	}
	return p.ResolveID(ctx, ref)
}

func fetchFlag(ctx context.Context, resolved *resolvedContext, projectID int, ref string) (map[string]any, error) {
	return projectFlags(resolved, projectID).Fetch(ctx, ref)
}

func flagInProject(ctx context.Context, resolved *resolvedContext, projectID int, key string) (map[string]any, error) {
	return projectFlags(resolved, projectID).WithKey(ctx, key)
}

func flagKeyByID(ctx context.Context, resolved *resolvedContext, projectID int, id string) (string, error) {
	return projectFlags(resolved, projectID).KeyByID(ctx, id)
}

func projectFlags(resolved *resolvedContext, projectID int) featureflag.Project {
	return featureflag.Project{Client: resolved.Client, ID: projectID, OrgID: resolved.OrgID, Host: resolved.Host}
}
