package cli

import (
	"context"
	"net/url"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/featureflag"
)

func registerFlags(root *cobra.Command, globals *GlobalFlags) {
	flags := &cobra.Command{Use: "flags", Short: "Read, create, edit, and evaluate PostHog feature flags"}
	var active, search string
	list := listCommand(globals, listSpec{
		use: "list", short: "List feature flags", scope: scopeProject, collection: []string{"feature_flags"},
		query: func(q url.Values) {
			setIf(q, "active", active)
			setIf(q, "search", search)
		},
	})
	list.Flags().StringVar(&active, "active", "", "Active filter: true or false")
	list.Flags().StringVar(&search, "search", "", "Search feature flags")
	flags.AddCommand(list)
	flags.AddCommand(entityGetCommand("get <id-or-key>...", "Get one or more feature flags by ID or key", globals, scopeProject, fetchFlagRecord))
	flags.AddCommand(flagSubresourceCommand(globals, "dependent <id-or-key>", "List dependent feature flags", "dependent_flags"))
	flags.AddCommand(flagSubresourceCommand(globals, "activity <id-or-key>", "Get feature flag activity", "activity"))
	registerFlagWrites(flags, globals)
	registerFlagSpecCommands(flags, globals)
	registerFlagConditionCommands(flags, globals)
	registerFlagVariantCommands(flags, globals)
	registerFlagPayloadCommands(flags, globals)
	registerFlagEvalCommands(flags, globals)
	flags.AddCommand(diffCommand(globals))
	flags.AddCommand(copyCommand(globals))
	root.AddCommand(flags)
}

// flagProject is the current project's feature flags; a project must be
// configured.
func (r *resolvedContext) flagProject() (featureflag.Project, error) {
	if err := requireProject(r); err != nil {
		return featureflag.Project{}, err
	}
	return featureflag.Project{Client: r.Client, ID: r.ProjectID, OrgID: r.OrgID, Host: r.Host, Alias: r.ProjectAlias}, nil
}

// fetchFlagRecord reads a flag by ID or key for 'flags get'.
func fetchFlagRecord(ctx context.Context, resolved *resolvedContext, ref string) (any, error) {
	p, err := resolved.flagProject()
	if err != nil {
		return nil, err
	}
	id, err := p.ResolveID(ctx, ref)
	if err != nil {
		return nil, err
	}
	raw, err := p.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return decodeRaw(raw), nil
}

func flagSubresourceCommand(globals *GlobalFlags, use, short, subresource string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withFlags(cmd, globals, func(ctx context.Context, p featureflag.Project) error {
				id, err := p.ResolveID(ctx, args[0])
				if err != nil {
					return err
				}
				raw, err := p.Get(ctx, id, subresource)
				if err != nil {
					return err
				}
				return writeRawResource(raw, globals.Format, globals.Full)
			})
		},
	}
}
