package flags

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/cli/shared"
	"github.com/shhac/agent-posthog/internal/featureflag"
	"github.com/shhac/agent-posthog/internal/flagspec"
)

func registerFlagSpecCommands(flags *cobra.Command, globals *shared.GlobalFlags) {
	flags.AddCommand(exportCommand(globals))
	flags.AddCommand(createCommand(globals))
	flags.AddCommand(applyCommand(globals))
}

func exportCommand(globals *shared.GlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "export <id-or-key>",
		Short: "Export a flag as a spec that 'flags apply' accepts (json, or --format yaml)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return WithProject(cmd, globals, func(ctx context.Context, p featureflag.Project) error {
				flag, err := p.Fetch(ctx, args[0])
				if err != nil {
					return err
				}
				spec, err := flagspec.Export(flag)
				if err != nil {
					return agenterrors.Wrap(err, agenterrors.FixableByHuman)
				}
				return shared.WriteExact(spec, globals.Format)
			})
		},
	}
}

func applyCommand(globals *shared.GlobalFlags) *cobra.Command {
	var opts writeOptions
	var input *shared.Input
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Create or update a flag from a spec (fields the spec omits stay as they are)",
		Long: "Reads a spec from --spec-json or --file (JSON or YAML). If no flag has the spec's key, it is created " +
			"(inactive unless the spec sets active); otherwise only the fields the spec names are changed. " +
			"Start from 'flags export <key>' to edit an existing flag.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := input.Read()
			if err != nil {
				return err
			}
			spec, err := flagspec.ParseSpec(data)
			if err != nil {
				return agenterrors.Wrap(err, agenterrors.FixableByAgent)
			}
			return opts.run(cmd, globals, func(ctx context.Context, p featureflag.Project) (*featureflag.Pending, error) {
				return p.PlanApply(ctx, spec, opts.ifVersion)
			})
		},
	}
	input = shared.AddInput(cmd, "spec-json", "Spec as inline JSON", "file", "Spec file (JSON or YAML)", "")
	addWriteFlags(cmd, &opts)
	return cmd
}

// createFlags are the inputs of 'flags create'.
type createFlags struct {
	flagType, name, payloadJSON     string
	active                          bool
	variants, tags, contexts, where []string
	rollout                         float64
}

func createCommand(globals *shared.GlobalFlags) *cobra.Command {
	var opts writeOptions
	var f createFlags
	cmd := &cobra.Command{
		Use:   "create <key>",
		Short: "Create a feature flag (inactive unless --active)",
		Long: "Creates a flag with one condition built from --where (all must match) at --rollout percent. " +
			"Add more conditions afterwards with 'flags conditions add', or create from a full spec with 'flags apply'.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := f.spec(args[0], cmd.Flags().Changed("rollout"))
			if err != nil {
				return err
			}
			if err := opts.refuseSavePlan("create"); err != nil {
				return err
			}
			return opts.run(cmd, globals, func(ctx context.Context, p featureflag.Project) (*featureflag.Pending, error) {
				return p.PlanCreate(ctx, spec)
			})
		},
	}
	cmd.Flags().StringVar(&f.flagType, "type", flagspec.TypeBoolean, "boolean (gate), multivariate (variants with payloads), or remote_config (one payload for everyone)")
	cmd.Flags().StringVar(&f.name, "name", "", "Description shown in PostHog")
	cmd.Flags().BoolVar(&f.active, "active", false, "Create the flag enabled")
	cmd.Flags().StringArrayVar(&f.where, "where", nil, "Condition filter, e.g. 'email icontains @example.com'; repeat to AND")
	cmd.Flags().Float64Var(&f.rollout, "rollout", 100, "Percent of matching users the condition serves")
	cmd.Flags().StringArrayVar(&f.variants, "variant", nil, "Variant as key=percent (multivariate); repeat, percents sum to 100")
	cmd.Flags().StringVar(&f.payloadJSON, "payload-json", "", "Payload JSON for boolean and remote_config flags")
	cmd.Flags().StringArrayVar(&f.tags, "tag", nil, "Tag; repeat (some projects require one)")
	cmd.Flags().StringArrayVar(&f.contexts, "evaluation-context", nil, "Evaluation context; repeat (some projects require one)")
	addWriteFlags(cmd, &opts)
	return cmd
}

// spec is the flag the inputs describe, as a spec for key.
func (f createFlags) spec(key string, rolloutChanged bool) (*flagspec.Spec, error) {
	if f.flagType == flagspec.TypeRemoteConfig && (len(f.where) > 0 || rolloutChanged) {
		return nil, agenterrors.New("remote_config flags serve one payload to everyone; drop --where and --rollout", agenterrors.FixableByAgent)
	}
	fields := map[string]any{"key": key, "type": f.flagType, "active": f.active}
	if f.name != "" {
		fields["name"] = f.name
	}
	if len(f.tags) > 0 {
		fields["tags"] = f.tags
	}
	if len(f.contexts) > 0 {
		fields["evaluation_contexts"] = f.contexts
	}
	if f.flagType != flagspec.TypeRemoteConfig {
		fields["conditions"] = []any{map[string]any{"where": append([]string{}, f.where...), "rollout": f.rollout}}
	}
	if len(f.variants) > 0 {
		parsed, err := parseVariantArgs(f.variants)
		if err != nil {
			return nil, err
		}
		fields["variants"] = parsed
	}
	if f.payloadJSON != "" {
		payload, err := flagspec.Decode([]byte(f.payloadJSON))
		if err != nil {
			return nil, agenterrors.New("--payload-json is not valid JSON: "+err.Error(), agenterrors.FixableByAgent)
		}
		fields["payload"] = payload
	}
	data, _ := json.Marshal(fields)
	spec, err := flagspec.ParseSpec(data)
	if err != nil {
		return nil, agenterrors.Wrap(err, agenterrors.FixableByAgent)
	}
	return spec, nil
}

func parseVariantArgs(args []string) ([]any, error) {
	out := make([]any, 0, len(args))
	for _, arg := range args {
		key, pct, ok := strings.Cut(arg, "=")
		if !ok || key == "" {
			return nil, agenterrors.New("variant "+arg+" must be key=percent", agenterrors.FixableByAgent)
		}
		if _, err := strconv.ParseFloat(pct, 64); err != nil {
			return nil, agenterrors.New("variant "+arg+" needs a numeric percent", agenterrors.FixableByAgent)
		}
		out = append(out, map[string]any{"key": key, "rollout": json.Number(pct)})
	}
	return out, nil
}
