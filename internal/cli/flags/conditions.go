package flags

import (
	"context"
	"strconv"

	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/cli/shared"
	"github.com/shhac/agent-posthog/internal/featureflag"
	"github.com/shhac/agent-posthog/internal/flagspec"
	"github.com/shhac/agent-posthog/internal/output"
)

func registerFlagConditionCommands(flags *cobra.Command, globals *shared.GlobalFlags) {
	conditions := &cobra.Command{
		Use:   "conditions",
		Short: "List and edit a flag's release conditions (tried top to bottom; the first match wins)",
	}
	conditions.AddCommand(conditionsListCommand(globals))
	conditions.AddCommand(conditionsAddCommand(globals))
	conditions.AddCommand(conditionsSetCommand(globals))
	conditions.AddCommand(conditionsRemoveCommand(globals))
	conditions.AddCommand(conditionsMoveCommand(globals))
	flags.AddCommand(conditions)
}

func conditionsListCommand(globals *shared.GlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list <id-or-key>",
		Short: "List conditions in evaluation order, with the flag version to pass as --if-version",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return WithProject(cmd, globals, func(ctx context.Context, p featureflag.Project) error {
				flag, err := p.Fetch(ctx, args[0])
				if err != nil {
					return err
				}
				conditions := flagspec.ExportConditions(flagspec.List(flagspec.Object(flag["filters"])["groups"]))
				format, err := output.ResolveFormat(globals.Format, output.FormatNDJSON)
				if err != nil {
					return err
				}
				if format != output.FormatNDJSON {
					return shared.WriteExact(map[string]any{"key": flag["key"], "version": flag["version"], "early_exit": flagspec.Object(flag["filters"])["early_exit"], "conditions": conditions}, globals.Format)
				}
				lines := make([]any, 0, len(conditions))
				for i, condition := range conditions {
					line := output.Ordered{
						{Key: "index", Value: i},
						{Key: "version", Value: flag["version"]},
						{Key: "condition", Value: condition},
					}
					lines = append(lines, line)
				}
				return output.WriteExactLines(lines, nil)
			})
		},
	}
}

// conditionFlags are the inputs that describe one condition.
type conditionFlags struct {
	where     []string
	rollout   float64
	variant   string
	noVariant bool
	clear     bool
}

// edit is the change the flags the caller passed make to one condition.
func (c conditionFlags) edit(changed func(string) bool) featureflag.ConditionEdit {
	var e featureflag.ConditionEdit
	switch {
	case changed("where"):
		e.Where = &c.where
	case c.clear:
		e.Where = &[]string{}
	}
	if changed("rollout") {
		e.Rollout = &c.rollout
	}
	switch {
	case changed("variant"):
		e.Variant = &c.variant
	case c.noVariant:
		e.Variant = new(string)
	}
	return e
}

func addConditionFlags(cmd *cobra.Command, c *conditionFlags) {
	cmd.Flags().StringArrayVar(&c.where, "where", nil, "Condition filter, e.g. 'app_env = staging'; repeat to AND (replaces existing filters)")
	cmd.Flags().Float64Var(&c.rollout, "rollout", 100, "Percent of matching users served")
	cmd.Flags().StringVar(&c.variant, "variant", "", "Serve this variant to matching users (multivariate)")
}

func conditionsAddCommand(globals *shared.GlobalFlags) *cobra.Command {
	var opts writeOptions
	var c conditionFlags
	var at int
	cmd := &cobra.Command{
		Use:   "add <id-or-key>",
		Short: "Add a condition (appended unless --at; no --where means everyone)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := featureflag.CheckRollout(c.rollout); err != nil {
				return err
			}
			index := -1
			if cmd.Flags().Changed("at") {
				if err := opts.requireVersion(args[0]); err != nil {
					return err
				}
				if at < 0 {
					return agenterrors.New("--at must be 0 or more", agenterrors.FixableByAgent)
				}
				index = at
			}
			condition := featureflag.Condition{Where: c.where, Rollout: c.rollout, Variant: c.variant}
			return opts.edit(cmd, globals, args[0], "conditions add", featureflag.AddCondition(condition, index))
		},
	}
	addConditionFlags(cmd, &c)
	cmd.Flags().IntVar(&at, "at", 0, "Insert at this index instead of appending (needs --if-version)")
	addWriteFlags(cmd, &opts)
	return cmd
}

func conditionsSetCommand(globals *shared.GlobalFlags) *cobra.Command {
	var opts writeOptions
	var c conditionFlags
	cmd := &cobra.Command{
		Use:   "set <id-or-key> <index>",
		Short: "Change one condition; flags you omit stay as they are",
		Long: "Edits the condition at <index> (from 'flags conditions list'). --if-version is required because an " +
			"index only names the condition you read. A rollout-only change uses PostHog's strict rollout endpoint.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			index, err := parseIndex(args[1])
			if err != nil {
				return err
			}
			if err := featureflag.CheckRollout(c.rollout); err != nil {
				return err
			}
			if err := opts.requireVersion(args[0]); err != nil {
				return err
			}
			changed := func(name string) bool { return cmd.Flags().Changed(name) }
			switch {
			case c.noVariant && changed("variant"):
				return agenterrors.New("use --variant or --no-variant, not both", agenterrors.FixableByAgent)
			case c.clear && changed("where"):
				return agenterrors.New("use --where or --clear-where, not both", agenterrors.FixableByAgent)
			}
			edit := c.edit(changed)
			if edit.Where == nil && edit.Rollout == nil && edit.Variant == nil {
				return agenterrors.New("nothing to change: pass --where, --clear-where, --rollout, --variant, or --no-variant", agenterrors.FixableByAgent)
			}
			if edit.Where == nil && edit.Variant == nil {
				return opts.run(cmd, globals, func(ctx context.Context, p featureflag.Project) (*featureflag.Pending, error) {
					return p.PlanConditionRollout(ctx, args[0], opts.ifVersion, index, *edit.Rollout)
				})
			}
			return opts.edit(cmd, globals, args[0], "conditions set", featureflag.EditCondition(index, edit))
		},
	}
	addConditionFlags(cmd, &c)
	cmd.Flags().BoolVar(&c.clear, "clear-where", false, "Remove every filter, so the condition matches everyone")
	cmd.Flags().BoolVar(&c.noVariant, "no-variant", false, "Remove the variant override")
	addWriteFlags(cmd, &opts)
	return cmd
}

func conditionsRemoveCommand(globals *shared.GlobalFlags) *cobra.Command {
	var opts writeOptions
	cmd := &cobra.Command{
		Use:   "remove <id-or-key> <index>",
		Short: "Remove one condition (needs --if-version)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			index, err := parseIndex(args[1])
			if err != nil {
				return err
			}
			if err := opts.requireVersion(args[0]); err != nil {
				return err
			}
			return opts.edit(cmd, globals, args[0], "conditions remove", featureflag.RemoveCondition(index))
		},
	}
	addWriteFlags(cmd, &opts)
	return cmd
}

func conditionsMoveCommand(globals *shared.GlobalFlags) *cobra.Command {
	var opts writeOptions
	cmd := &cobra.Command{
		Use:   "move <id-or-key> <from> <to>",
		Short: "Move a condition so it ends up at index <to> (needs --if-version)",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			from, err := parseIndex(args[1])
			if err != nil {
				return err
			}
			to, err := parseIndex(args[2])
			if err != nil {
				return err
			}
			if err := opts.requireVersion(args[0]); err != nil {
				return err
			}
			return opts.edit(cmd, globals, args[0], "conditions move", featureflag.MoveCondition(from, to))
		},
	}
	addWriteFlags(cmd, &opts)
	return cmd
}

func parseIndex(arg string) (int, error) {
	index, err := strconv.Atoi(arg)
	if err != nil || index < 0 {
		return 0, agenterrors.New("index "+arg+" must be a whole number from 0", agenterrors.FixableByAgent)
	}
	return index, nil
}
