package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/featureflag"
)

// WithProject runs fn against the current project's feature flags.
func WithProject(cmd *cobra.Command, globals *GlobalFlags, fn func(ctx context.Context, p featureflag.Project) error) error {
	return WithClient(cmd.Context(), globals, func(ctx context.Context, resolved *Resolved) error {
		p, err := resolved.flagProject()
		if err != nil {
			return err
		}
		return fn(ctx, p)
	})
}

// flagProject is the current project's feature flags; a project must be
// configured.
func (r *Resolved) flagProject() (featureflag.Project, error) {
	if err := RequireProject(r); err != nil {
		return featureflag.Project{}, err
	}
	return featureflag.Project{Client: r.Client, ID: r.ProjectID, OrgID: r.OrgID, Host: r.Host, Alias: r.ProjectAlias}, nil
}
