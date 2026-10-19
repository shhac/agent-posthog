package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	agenterrors "github.com/shhac/agent-posthog/internal/errors"
	"github.com/shhac/agent-posthog/internal/flagspec"
)

func registerFlagWrites(flags *cobra.Command, globals *GlobalFlags) {
	flags.AddCommand(lifecycleCommand(globals, "enable <id-or-key>", "Enable a feature flag", "enable", func(map[string]any) *flagspec.Patch {
		return fieldPatch("active", true)
	}))
	flags.AddCommand(lifecycleCommand(globals, "disable <id-or-key>", "Disable a feature flag", "disable", func(map[string]any) *flagspec.Patch {
		return fieldPatch("active", false)
	}))
	flags.AddCommand(lifecycleCommand(globals, "archive <id-or-key>", "Disable and archive a feature flag", "archive", func(map[string]any) *flagspec.Patch {
		p := fieldPatch("archived", true)
		p.Fields["active"] = false
		return p
	}))
	flags.AddCommand(lifecycleCommand(globals, "unarchive <id-or-key>", "Unarchive a feature flag (it stays disabled)", "unarchive", func(map[string]any) *flagspec.Patch {
		return fieldPatch("archived", false)
	}))
	flags.AddCommand(lifecycleCommand(globals, "delete <id-or-key>", "Soft-delete a feature flag (restore it by ID)", "delete", func(map[string]any) *flagspec.Patch {
		return fieldPatch("deleted", true)
	}))
	flags.AddCommand(restoreCommand(globals))
	flags.AddCommand(earlyExitCommand(globals))
}

func fieldPatch(field string, value any) *flagspec.Patch {
	p := flagspec.NewPatch()
	p.Fields[field] = value
	return p
}

func lifecycleCommand(globals *GlobalFlags, use, short, operation string, patch func(map[string]any) *flagspec.Patch) *cobra.Command {
	var opts writeOptions
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				return updateFlag(ctx, resolved, globals, args[0], operation, opts, func(flag map[string]any) (*flagspec.Patch, []string, error) {
					return patch(flag), lifecycleWarnings(operation, flag), nil
				})
			})
		},
	}
	addWriteFlags(cmd, &opts)
	return cmd
}

func lifecycleWarnings(operation string, flag map[string]any) []string {
	if operation != "delete" {
		return nil
	}
	warnings := []string{fmt.Sprintf("Deleted flags drop out of lists; restore with 'agent-posthog flags restore %v'.", flag["id"])}
	if experiments, _ := flag["experiment_set"].([]any); len(experiments) > 0 {
		warnings = append(warnings, "This flag backs an experiment; PostHog may refuse the delete or rename the key.")
	}
	return warnings
}

// restoreCommand undeletes by ID. Deleted flags cannot be listed or read, so
// the plan is the single deleted → false change.
func restoreCommand(globals *GlobalFlags) *cobra.Command {
	var opts writeOptions
	cmd := &cobra.Command{
		Use:   "restore <id>",
		Short: "Restore a soft-deleted feature flag by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := strconv.Atoi(args[0]); err != nil {
				return agenterrors.New("restore needs the numeric flag ID; deleted flags cannot be looked up by key", agenterrors.FixableByAgent)
			}
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				if err := requireProject(resolved); err != nil {
					return err
				}
				id, _ := strconv.Atoi(args[0])
				r := &receipt{
					Outcome:   outcomePlanned,
					Operation: "restore",
					ProjectID: resolved.ProjectID,
					ID:        id,
					Changes:   []flagspec.Change{{Path: "deleted", Before: true, After: false}},
					Warnings:  []string{"PostHog may give the restored flag a suffixed key if its key was reused."},
				}
				return commitWrite(r, opts, globals.Format, func() (json.RawMessage, error) {
					return resolved.Client.Patch(ctx, flagPath(resolved.ProjectID, args[0]), nil, map[string]any{"deleted": false})
				})
			})
		},
	}
	addWriteFlags(cmd, &opts)
	return cmd
}

func earlyExitCommand(globals *GlobalFlags) *cobra.Command {
	var opts writeOptions
	cmd := &cobra.Command{
		Use:   "early-exit <id-or-key> <on|off>",
		Short: "Stop evaluation at the first condition whose properties match, even if its rollout excludes the user",
		Long: "With early exit on, a condition whose properties match but whose rollout excludes the user " +
			"returns false instead of falling through to later conditions, so a 0% condition works as a deny rule. " +
			"PostHog gates it per organization, and SDK local evaluation honors it only in recent SDK versions.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[1] != "on" && args[1] != "off" {
				return agenterrors.New("early-exit takes on or off", agenterrors.FixableByAgent)
			}
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				return updateFlag(ctx, resolved, globals, args[0], "early-exit", opts, func(flag map[string]any) (*flagspec.Patch, []string, error) {
					if err := flagspec.RequireV1(asObject(flag["filters"])); err != nil {
						return nil, nil, err
					}
					p := flagspec.NewPatch()
					p.Filters["early_exit"] = args[1] == "on"
					return p, earlyExitWarnings(args[1] == "on"), nil
				})
			})
		},
	}
	addWriteFlags(cmd, &opts)
	return cmd
}

func earlyExitWarnings(on bool) []string {
	if !on {
		return nil
	}
	return []string{"SDKs evaluating flags locally honor early exit only in recent versions (posthog-python, posthog-go >= 1.15.0); older ones evaluate every condition."}
}
