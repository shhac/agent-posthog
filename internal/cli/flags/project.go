package flags

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/cli/shared"
	"github.com/shhac/agent-posthog/internal/featureflag"
)

// WithProject runs fn against the current project's feature flags.
func WithProject(cmd *cobra.Command, globals *shared.GlobalFlags, fn func(ctx context.Context, p featureflag.Project) error) error {
	return shared.WithClient(cmd.Context(), globals, func(ctx context.Context, resolved *shared.Resolved) error {
		p, err := project(resolved)
		if err != nil {
			return err
		}
		return fn(ctx, p)
	})
}

// project is the current project's feature flags; a project must be
// configured.
func project(r *shared.Resolved) (featureflag.Project, error) {
	if err := shared.RequireProject(r); err != nil {
		return featureflag.Project{}, err
	}
	return featureflag.Project{Client: r.Client, ID: r.ProjectID, OrgID: r.OrgID, Host: r.Host, Alias: r.ProjectAlias}, nil
}

// target is another project of the profile, named by ID or alias.
func target(resolved *shared.Resolved, ref string) (featureflag.Target, error) {
	id, err := resolved.ProjectRef(ref)
	if err != nil {
		return featureflag.Target{}, err
	}
	return featureflag.Target{ID: id, Alias: shared.AliasName(ref)}, nil
}
