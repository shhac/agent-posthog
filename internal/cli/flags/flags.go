package flags

import (
	"context"
	"net/url"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/cli/shared"
	"github.com/shhac/agent-posthog/internal/featureflag"
)

func Register(root *cobra.Command, globals *shared.GlobalFlags) {
	flags := &cobra.Command{Use: "flags", Short: "Read, create, edit, and evaluate PostHog feature flags"}
	var active, search string
	list := shared.ListCommand(globals, shared.ListSpec{
		Use: "list", Short: "List feature flags", Scope: shared.ScopeProject, Collection: []string{"feature_flags"},
		Query: func(q url.Values) {
			shared.SetIf(q, "active", active)
			shared.SetIf(q, "search", search)
		},
	})
	list.Flags().StringVar(&active, "active", "", "Active filter: true or false")
	list.Flags().StringVar(&search, "search", "", "Search feature flags")
	flags.AddCommand(list)
	flags.AddCommand(shared.EntityGetCommand("get <id-or-key>...", "Get one or more feature flags by ID or key", globals, shared.ScopeProject, fetchFlagRecord))
	flags.AddCommand(flagSubresourceCommand(globals, "dependent <id-or-key>", "List dependent feature flags", "dependent_flags"))
	flags.AddCommand(flagSubresourceCommand(globals, "activity <id-or-key>", "Get feature flag activity", "activity"))
	registerFlagWrites(flags, globals)
	registerFlagSpecCommands(flags, globals)
	registerFlagConditionCommands(flags, globals)
	registerFlagVariantCommands(flags, globals)
	registerFlagPayloadCommands(flags, globals)
	flags.AddCommand(evaluateCommand(globals))
	flags.AddCommand(testCommand(globals))
	flags.AddCommand(reachCommand(globals))
	flags.AddCommand(diffCommand(globals))
	flags.AddCommand(copyCommand(globals))
	root.AddCommand(flags)
}

// fetchFlagRecord reads a flag by ID or key for 'flags get'.
func fetchFlagRecord(ctx context.Context, resolved *shared.Resolved, ref string) (any, error) {
	p, err := project(resolved)
	if err != nil {
		return nil, err
	}
	_, raw, err := p.Read(ctx, ref)
	if err != nil {
		return nil, err
	}
	return shared.DecodeRaw(raw), nil
}

func flagSubresourceCommand(globals *shared.GlobalFlags, use, short, subresource string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return WithProject(cmd, globals, func(ctx context.Context, p featureflag.Project) error {
				_, raw, err := p.Read(ctx, args[0], subresource)
				if err != nil {
					return err
				}
				return shared.WriteRaw(raw, globals.Format, shared.ShapeOf(globals))
			})
		},
	}
}
