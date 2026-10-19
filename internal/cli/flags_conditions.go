package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"

	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/flagspec"
	"github.com/shhac/agent-posthog/internal/output"
)

func registerFlagConditionCommands(flags *cobra.Command, globals *GlobalFlags) {
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

func conditionsListCommand(globals *GlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list <id-or-key>",
		Short: "List conditions in evaluation order, with the flag version to pass as --if-version",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				flag, err := currentFlag(ctx, resolved, args[0])
				if err != nil {
					return err
				}
				conditions := flagspec.ExportConditions(asList(asObject(flag["filters"])["groups"]))
				format, err := output.ResolveFormat(globals.Format, output.FormatNDJSON)
				if err != nil {
					return err
				}
				if format != output.FormatNDJSON {
					return writeExact(map[string]any{"key": flag["key"], "version": flag["version"], "early_exit": asObject(flag["filters"])["early_exit"], "conditions": conditions}, globals.Format)
				}
				for i, condition := range conditions {
					line := flagspec.NewOrdered()
					line.Set("index", i)
					line.Set("version", flag["version"])
					line.Set("condition", condition)
					if err := writeExactLine(line); err != nil {
						return err
					}
				}
				return nil
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

func (c conditionFlags) validate() error {
	if c.rollout < 0 || c.rollout > 100 {
		return agenterrors.New("--rollout must be from 0 to 100", agenterrors.FixableByAgent)
	}
	return nil
}

// edit applies the flags the caller passed to one condition group.
func (c conditionFlags) edit(group map[string]any, changed func(string) bool, resolve flagspec.FlagResolver) error {
	switch {
	case changed("where"):
		props, err := flagspec.BuildProperties(stringsToAny(c.where), resolve)
		if err != nil {
			return err
		}
		group["properties"] = props
	case c.clear:
		group["properties"] = []any{}
	}
	if changed("rollout") {
		group["rollout_percentage"] = rolloutNumber(c.rollout)
	}
	switch {
	case changed("variant"):
		group["variant"] = c.variant
	case c.noVariant:
		group["variant"] = nil
	}
	return nil
}

func addConditionFlags(cmd *cobra.Command, c *conditionFlags) {
	cmd.Flags().StringArrayVar(&c.where, "where", nil, "Condition filter, e.g. 'app_env = staging'; repeat to AND (replaces existing filters)")
	cmd.Flags().Float64Var(&c.rollout, "rollout", 100, "Percent of matching users served")
	cmd.Flags().StringVar(&c.variant, "variant", "", "Serve this variant to matching users (multivariate)")
}

func conditionsAddCommand(globals *GlobalFlags) *cobra.Command {
	var opts writeOptions
	var c conditionFlags
	var at int
	cmd := &cobra.Command{
		Use:   "add <id-or-key>",
		Short: "Add a condition (appended unless --at; no --where means everyone)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := c.validate(); err != nil {
				return err
			}
			if cmd.Flags().Changed("at") {
				if err := opts.requireVersion(args[0]); err != nil {
					return err
				}
			}
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				resolve := flagResolver(ctx, resolved, resolved.ProjectID)
				return updateFlag(ctx, resolved, globals, args[0], "conditions add", opts, func(flag map[string]any) (*flagspec.Patch, []string, error) {
					condition := map[string]any{"where": stringsToAny(c.where), "rollout": rolloutNumber(c.rollout)}
					if c.variant != "" {
						condition["variant"] = c.variant
					}
					group, err := flagspec.BuildGroup(condition, resolve)
					if err != nil {
						return nil, nil, err
					}
					groups, err := editableGroups(flag)
					if err != nil {
						return nil, nil, err
					}
					index := len(groups)
					if cmd.Flags().Changed("at") {
						index = at
					}
					if index < 0 || index > len(groups) {
						return nil, nil, fmt.Errorf("--at %d is outside 0..%d", index, len(groups))
					}
					return groupsPatch(flag, slices.Insert(groups, index, any(group)))
				})
			})
		},
	}
	addConditionFlags(cmd, &c)
	cmd.Flags().IntVar(&at, "at", 0, "Insert at this index instead of appending (needs --if-version)")
	addWriteFlags(cmd, &opts)
	return cmd
}

func conditionsSetCommand(globals *GlobalFlags) *cobra.Command {
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
			if err := c.validate(); err != nil {
				return err
			}
			if err := opts.requireVersion(args[0]); err != nil {
				return err
			}
			changed := func(name string) bool { return cmd.Flags().Changed(name) }
			whereEdit := changed("where") || c.clear
			variantEdit := changed("variant") || c.noVariant
			rolloutEdit := changed("rollout")
			switch {
			case !whereEdit && !variantEdit && !rolloutEdit:
				return agenterrors.New("nothing to change: pass --where, --clear-where, --rollout, --variant, or --no-variant", agenterrors.FixableByAgent)
			case c.noVariant && changed("variant"):
				return agenterrors.New("use --variant or --no-variant, not both", agenterrors.FixableByAgent)
			case c.clear && changed("where"):
				return agenterrors.New("use --where or --clear-where, not both", agenterrors.FixableByAgent)
			}
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				if rolloutEdit && !whereEdit && !variantEdit {
					return setConditionRollout(ctx, resolved, globals, args[0], index, c.rollout, opts)
				}
				resolve := flagResolver(ctx, resolved, resolved.ProjectID)
				return updateFlag(ctx, resolved, globals, args[0], "conditions set", opts, func(flag map[string]any) (*flagspec.Patch, []string, error) {
					groups, err := editableGroups(flag)
					if err != nil {
						return nil, nil, err
					}
					if err := checkConditionIndex(groups, index); err != nil {
						return nil, nil, err
					}
					if err := c.edit(asObject(groups[index]), changed, resolve); err != nil {
						return nil, nil, err
					}
					return groupsPatch(flag, groups)
				})
			})
		},
	}
	addConditionFlags(cmd, &c)
	cmd.Flags().BoolVar(&c.clear, "clear-where", false, "Remove every filter, so the condition matches everyone")
	cmd.Flags().BoolVar(&c.noVariant, "no-variant", false, "Remove the variant override")
	addWriteFlags(cmd, &opts)
	return cmd
}

// setConditionRollout uses PostHog's strict endpoint, which refuses the write
// if the flag changed at all since the version given.
func setConditionRollout(ctx context.Context, resolved *resolvedContext, globals *GlobalFlags, ref string, index int, rollout float64, opts writeOptions) error {
	build := func(flag map[string]any) (*flagspec.Patch, []string, error) {
		groups, err := editableGroups(flag)
		if err != nil {
			return nil, nil, err
		}
		if err := checkConditionIndex(groups, index); err != nil {
			return nil, nil, err
		}
		asObject(groups[index])["rollout_percentage"] = rolloutNumber(rollout)
		p := flagspec.NewPatch()
		p.Filters["groups"] = groups
		return p, nil, nil
	}
	send := func(ctx context.Context, flag map[string]any, _ *flagspec.Patch) (json.RawMessage, error) {
		body := map[string]any{"condition_index": index, "rollout_percentage": rolloutNumber(rollout), "version": flag["version"]}
		return resolved.Client.Mutate(ctx, http.MethodPost, flagPath(resolved.ProjectID, fmt.Sprint(flag["id"]), "set_release_condition_rollout"), nil, body)
	}
	return updateFlagVia(ctx, resolved, globals, ref, "conditions set", opts, build, send)
}

func checkConditionIndex(groups []any, index int) error {
	if index >= len(groups) {
		return fmt.Errorf("the flag has no condition %d (it has %d)", index, len(groups))
	}
	return nil
}

func conditionsRemoveCommand(globals *GlobalFlags) *cobra.Command {
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
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				return updateFlag(ctx, resolved, globals, args[0], "conditions remove", opts, func(flag map[string]any) (*flagspec.Patch, []string, error) {
					groups, err := editableGroups(flag)
					if err != nil {
						return nil, nil, err
					}
					if err := checkConditionIndex(groups, index); err != nil {
						return nil, nil, err
					}
					return groupsPatch(flag, slices.Delete(groups, index, index+1))
				})
			})
		},
	}
	addWriteFlags(cmd, &opts)
	return cmd
}

func conditionsMoveCommand(globals *GlobalFlags) *cobra.Command {
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
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				return updateFlag(ctx, resolved, globals, args[0], "conditions move", opts, func(flag map[string]any) (*flagspec.Patch, []string, error) {
					groups, err := editableGroups(flag)
					if err != nil {
						return nil, nil, err
					}
					moved, err := moveGroup(groups, from, to)
					if err != nil {
						return nil, nil, err
					}
					groups = moved
					return groupsPatch(flag, groups)
				})
			})
		},
	}
	addWriteFlags(cmd, &opts)
	return cmd
}

// moveGroup moves the condition at from so it ends up at index to.
func moveGroup(groups []any, from, to int) ([]any, error) {
	if from >= len(groups) || to >= len(groups) {
		return nil, fmt.Errorf("indexes must be below %d", len(groups))
	}
	moved := groups[from]
	return slices.Insert(slices.Delete(groups, from, from+1), to, moved), nil
}

// editableGroups returns a copy of the flag's conditions after checking the
// flag is one whose conditions can be edited.
func editableGroups(flag map[string]any) ([]any, error) {
	filters := asObject(flag["filters"])
	if err := flagspec.RequireV1(filters); err != nil {
		return nil, err
	}
	if flagspec.TypeOf(flag) == flagspec.TypeRemoteConfig {
		return nil, fmt.Errorf("remote_config flags serve one payload to everyone and have no conditions to edit")
	}
	groups, _ := flagspec.Clone(filters["groups"]).([]any)
	return append([]any{}, groups...), nil
}

func groupsPatch(flag map[string]any, groups []any) (*flagspec.Patch, []string, error) {
	flagType := flagspec.TypeOf(flag)
	if err := flagspec.CheckOverrides(groups, flagType, flagspec.VariantKeys(flag)); err != nil {
		return nil, nil, err
	}
	p := flagspec.NewPatch()
	p.Filters["groups"] = groups
	return p, flagspec.ConditionWarnings(groups, flagType), nil
}

func parseIndex(arg string) (int, error) {
	index, err := strconv.Atoi(arg)
	if err != nil || index < 0 {
		return 0, agenterrors.New("index "+arg+" must be a whole number from 0", agenterrors.FixableByAgent)
	}
	return index, nil
}

func rolloutNumber(pct float64) json.Number {
	return json.Number(strconv.FormatFloat(pct, 'f', -1, 64))
}
