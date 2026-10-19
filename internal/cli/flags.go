package cli

import (
	"context"
	"net/url"

	"github.com/spf13/cobra"
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
	registerFlagProjectCommands(flags, globals)
	root.AddCommand(flags)
}

// fetchFlagRecord reads a flag by ID or key for 'flags get'.
func fetchFlagRecord(ctx context.Context, resolved *resolvedContext, ref string) (any, error) {
	id, err := resolveFlagID(ctx, resolved, ref)
	if err != nil {
		return nil, err
	}
	raw, err := resolved.Client.Get(ctx, flagPath(resolved.ProjectID, id), nil)
	if err != nil {
		return nil, err
	}
	return decodeRaw(raw), nil
}

func flagSubresourceCommand(globals *GlobalFlags, use, short, suffix string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				if err := requireProject(resolved); err != nil {
					return err
				}
				id, err := resolveFlagID(ctx, resolved, args[0])
				if err != nil {
					return err
				}
				raw, err := resolved.Client.Get(ctx, flagPath(resolved.ProjectID, id, suffix), nil)
				if err != nil {
					return err
				}
				return writeRawResource(raw, globals.Format, globals.Full)
			})
		},
	}
}
