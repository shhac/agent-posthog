package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strconv"
	"strings"

	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/flagspec"
)

// lifecycle is a write that sets top-level flag fields, such as enable or
// archive, with the warnings that operation calls for.
type lifecycle struct {
	use, short, operation string
	fields                map[string]any
	warnings              func(ctx context.Context, resolved *resolvedContext, flag map[string]any) []string
}

func registerFlagWrites(flags *cobra.Command, globals *GlobalFlags) {
	for _, l := range []lifecycle{
		{use: "enable <id-or-key>", short: "Enable a feature flag", operation: "enable",
			fields: map[string]any{"active": true}, warnings: disabledDependencies},
		{use: "disable <id-or-key>", short: "Disable a feature flag", operation: "disable",
			fields: map[string]any{"active": false}, warnings: activeDependents},
		{use: "archive <id-or-key>", short: "Disable and archive a feature flag", operation: "archive",
			fields: map[string]any{"archived": true, "active": false}, warnings: activeDependents},
		{use: "unarchive <id-or-key>", short: "Unarchive a feature flag (it stays disabled)", operation: "unarchive",
			fields: map[string]any{"archived": false}},
		{use: "delete <id-or-key>", short: "Soft-delete a feature flag (restore it by ID)", operation: "delete",
			fields: map[string]any{"deleted": true}, warnings: deleteWarnings},
	} {
		flags.AddCommand(lifecycleCommand(globals, l))
	}
	flags.AddCommand(restoreCommand(globals))
	flags.AddCommand(applyPlanCommand(globals))
	flags.AddCommand(earlyExitCommand(globals))
}

func lifecycleCommand(globals *GlobalFlags, l lifecycle) *cobra.Command {
	var opts writeOptions
	cmd := &cobra.Command{
		Use:   l.use,
		Short: l.short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				return updateFlag(ctx, resolved, globals, args[0], l.operation, opts, func(flag map[string]any) (*flagspec.Patch, []string, error) {
					p := flagspec.NewPatch()
					maps.Copy(p.Fields, l.fields)
					if l.warnings == nil {
						return p, nil, nil
					}
					return p, l.warnings(ctx, resolved, flag), nil
				})
			})
		},
	}
	addWriteFlags(cmd, &opts)
	return cmd
}

func deleteWarnings(ctx context.Context, resolved *resolvedContext, flag map[string]any) []string {
	warnings := []string{fmt.Sprintf("Deleted flags drop out of lists; restore with 'agent-posthog flags restore %v'.", flag["id"])}
	if experiments, _ := flag["experiment_set"].([]any); len(experiments) > 0 {
		warnings = append(warnings, "This flag backs an experiment; PostHog may refuse the delete or rename the key.")
	}
	return append(warnings, activeDependents(ctx, resolved, flag)...)
}

// activeDependents warns that switching off a flag that active flags depend
// on may be refused, or stop them matching. The lookup is best effort.
func activeDependents(ctx context.Context, resolved *resolvedContext, flag map[string]any) []string {
	raw, err := resolved.Client.Get(ctx, flagPath(resolved.ProjectID, fmt.Sprint(flag["id"]), "dependent_flags"), nil)
	if err != nil {
		return []string{"Could not check which flags depend on this one."}
	}
	page, _ := flagspec.DecodeObject(raw)
	var active []string
	for _, item := range asList(page["results"]) {
		if dep := asObject(item); dep["active"] == true {
			active = append(active, fmt.Sprint(dep["key"]))
		}
	}
	if len(active) == 0 {
		return nil
	}
	return []string{fmt.Sprintf("Active flags depend on this one (%s): PostHog may refuse this, or they will stop matching.", strings.Join(active, ", "))}
}

// disabledDependencies warns that switching on a flag that depends on a
// disabled flag may be refused. Lookups are best effort.
func disabledDependencies(ctx context.Context, resolved *resolvedContext, flag map[string]any) []string {
	var warnings []string
	for _, group := range asList(asObject(flag["filters"])["groups"]) {
		for _, item := range asList(asObject(group)["properties"]) {
			prop := asObject(item)
			if prop["type"] != flagspec.PropertyFlag {
				continue
			}
			raw, err := resolved.Client.Get(ctx, flagPath(resolved.ProjectID, fmt.Sprint(prop["key"])), nil)
			if err != nil {
				warnings = appendOnce(warnings, "Could not check a flag this one depends on.")
				continue
			}
			dep, _ := flagspec.DecodeObject(raw)
			if dep["active"] != true {
				warnings = appendOnce(warnings, fmt.Sprintf("This flag depends on disabled flag %v; PostHog may refuse to enable it until that one is enabled.", dep["key"]))
			}
		}
	}
	return warnings
}

// restoreCommand undeletes by ID. Deleted flags cannot be listed or read, so
// the plan is the single deleted → false change.
func restoreCommand(globals *GlobalFlags) *cobra.Command {
	var opts writeOptions
	cmd := &cobra.Command{
		Use:   "restore <id>",
		Long:  "Restores by numeric ID (or id:<n>); deleted flags cannot be found by key.",
		Short: "Restore a soft-deleted feature flag by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.refuseSavePlan("restore"); err != nil {
				return err
			}
			ref, _, err := parseFlagRef(args[0])
			if err != nil || ref == "" {
				return agenterrors.New("restore needs the numeric flag ID; deleted flags cannot be looked up by key", agenterrors.FixableByAgent)
			}
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				if err := requireProject(resolved); err != nil {
					return err
				}
				id, _ := strconv.Atoi(ref)
				r := newReceipt("restore", resolved, map[string]any{"id": id})
				r.Changes = []flagspec.Change{{Path: "deleted", Before: true, After: false}}
				r.Warnings = []string{"PostHog may give the restored flag a suffixed key if its key was reused."}
				return commitWrite(r, opts, globals.Format, func() (json.RawMessage, error) {
					return resolved.Client.Patch(ctx, flagPath(resolved.ProjectID, ref), nil, map[string]any{"deleted": false})
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
