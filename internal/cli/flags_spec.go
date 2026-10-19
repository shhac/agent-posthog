package cli

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/featureflag"
	"github.com/shhac/agent-posthog/internal/flagspec"
)

func registerFlagSpecCommands(flags *cobra.Command, globals *GlobalFlags) {
	flags.AddCommand(exportCommand(globals))
	flags.AddCommand(createCommand(globals))
	flags.AddCommand(applyCommand(globals))
}

func exportCommand(globals *GlobalFlags) *cobra.Command {
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
				return WriteExact(spec, globals.Format)
			})
		},
	}
}

func applyCommand(globals *GlobalFlags) *cobra.Command {
	var opts writeOptions
	var specJSON, file string
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Create or update a flag from a spec (fields the spec omits stay as they are)",
		Long: "Reads a spec from --spec-json or --file (JSON or YAML). If no flag has the spec's key, it is created " +
			"(inactive unless the spec sets active); otherwise only the fields the spec names are changed. " +
			"Start from 'flags export <key>' to edit an existing flag.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := ReadInput(specJSON, file, "spec-json", "file")
			if err != nil {
				return err
			}
			spec, err := flagspec.ParseSpec(data)
			if err != nil {
				return agenterrors.Wrap(err, agenterrors.FixableByAgent)
			}
			return WithProject(cmd, globals, func(ctx context.Context, p featureflag.Project) error {
				pending, err := p.PlanApply(ctx, spec, opts.ifVersion)
				if err != nil {
					return err
				}
				return writeFlag(ctx, pending, opts, globals.Format)
			})
		},
	}
	cmd.Flags().StringVar(&specJSON, "spec-json", "", "Spec as inline JSON")
	cmd.Flags().StringVar(&file, "file", "", "Spec file (JSON or YAML)")
	addWriteFlags(cmd, &opts)
	return cmd
}

func createCommand(globals *GlobalFlags) *cobra.Command {
	var opts writeOptions
	var flagType, name, payloadJSON string
	var active bool
	var variants, tags, contexts, where []string
	var rollout float64
	cmd := &cobra.Command{
		Use:   "create <key>",
		Short: "Create a feature flag (inactive unless --active)",
		Long: "Creates a flag with one condition built from --where (all must match) at --rollout percent. " +
			"Add more conditions afterwards with 'flags conditions add', or create from a full spec with 'flags apply'.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fields := map[string]any{"key": args[0], "type": flagType, "active": active}
			if name != "" {
				fields["name"] = name
			}
			if len(tags) > 0 {
				fields["tags"] = tags
			}
			if len(contexts) > 0 {
				fields["evaluation_contexts"] = contexts
			}
			if flagType != flagspec.TypeRemoteConfig {
				fields["conditions"] = []any{map[string]any{"where": append([]string{}, where...), "rollout": rollout}}
			} else if len(where) > 0 || cmd.Flags().Changed("rollout") {
				return agenterrors.New("remote_config flags serve one payload to everyone; drop --where and --rollout", agenterrors.FixableByAgent)
			}
			if len(variants) > 0 {
				parsed, err := parseVariantArgs(variants)
				if err != nil {
					return err
				}
				fields["variants"] = parsed
			}
			if payloadJSON != "" {
				payload, err := flagspec.Decode([]byte(payloadJSON))
				if err != nil {
					return agenterrors.New("--payload-json is not valid JSON: "+err.Error(), agenterrors.FixableByAgent)
				}
				fields["payload"] = payload
			}
			data, _ := json.Marshal(fields)
			spec, err := flagspec.ParseSpec(data)
			if err != nil {
				return agenterrors.Wrap(err, agenterrors.FixableByAgent)
			}
			if err := opts.refuseSavePlan("create"); err != nil {
				return err
			}
			return WithProject(cmd, globals, func(ctx context.Context, p featureflag.Project) error {
				pending, err := p.PlanCreate(ctx, spec)
				if err != nil {
					return err
				}
				return writeFlag(ctx, pending, opts, globals.Format)
			})
		},
	}
	cmd.Flags().StringVar(&flagType, "type", flagspec.TypeBoolean, "boolean (gate), multivariate (variants with payloads), or remote_config (one payload for everyone)")
	cmd.Flags().StringVar(&name, "name", "", "Description shown in PostHog")
	cmd.Flags().BoolVar(&active, "active", false, "Create the flag enabled")
	cmd.Flags().StringArrayVar(&where, "where", nil, "Condition filter, e.g. 'email icontains @example.com'; repeat to AND")
	cmd.Flags().Float64Var(&rollout, "rollout", 100, "Percent of matching users the condition serves")
	cmd.Flags().StringArrayVar(&variants, "variant", nil, "Variant as key=percent (multivariate); repeat, percents sum to 100")
	cmd.Flags().StringVar(&payloadJSON, "payload-json", "", "Payload JSON for boolean and remote_config flags")
	cmd.Flags().StringArrayVar(&tags, "tag", nil, "Tag; repeat (some projects require one)")
	cmd.Flags().StringArrayVar(&contexts, "evaluation-context", nil, "Evaluation context; repeat (some projects require one)")
	addWriteFlags(cmd, &opts)
	return cmd
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
