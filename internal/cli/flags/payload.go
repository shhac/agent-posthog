package flags

import (
	"context"

	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/cli/shared"
	"github.com/shhac/agent-posthog/internal/featureflag"
	"github.com/shhac/agent-posthog/internal/flagspec"
)

func registerFlagPayloadCommands(flags *cobra.Command, globals *shared.GlobalFlags) {
	payload := &cobra.Command{Use: "payload", Short: "Read and edit flag payloads (the JSON a matching user receives)"}
	payload.AddCommand(payloadGetCommand(globals))
	payload.AddCommand(payloadSetCommand(globals))
	payload.AddCommand(payloadUnsetCommand(globals))
	flags.AddCommand(payload)
}

func payloadGetCommand(globals *shared.GlobalFlags) *cobra.Command {
	var variant string
	cmd := &cobra.Command{
		Use:   "get <id-or-key>",
		Short: "Show the payload a matching user receives (per --variant for multivariate flags)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return WithProject(cmd, globals, func(ctx context.Context, p featureflag.Project) error {
				flag, err := p.Fetch(ctx, args[0])
				if err != nil {
					return err
				}
				payload, err := featureflag.ReadPayload(flag, variant)
				if err != nil {
					return err
				}
				result := flagspec.NewOrdered()
				result.Set("key", payload.Key)
				result.Set("payload_key", payload.Slot)
				result.Set("set", payload.Set)
				if payload.Set {
					result.Set("payload", payload.Value)
				}
				return shared.WriteExact(result, globals.Format)
			})
		},
	}
	cmd.Flags().StringVar(&variant, "variant", "", "Variant key (multivariate flags)")
	return cmd
}

func payloadSetCommand(globals *shared.GlobalFlags) *cobra.Command {
	var opts writeOptions
	var variant, inline, file string
	cmd := &cobra.Command{
		Use:   "set <id-or-key>",
		Short: "Set the payload for a boolean/remote_config flag, or for one --variant",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := shared.ReadInput(inline, file, "json", "file")
			if err != nil {
				return err
			}
			value, err := flagspec.Decode(data)
			if err != nil {
				return agenterrors.New("payload is not valid JSON: "+err.Error(), agenterrors.FixableByAgent)
			}
			return WithProject(cmd, globals, func(ctx context.Context, p featureflag.Project) error {
				return editFlag(ctx, p, args[0], "payload set", opts, featureflag.SetPayload(variant, value), globals.Format)
			})
		},
	}
	cmd.Flags().StringVar(&variant, "variant", "", "Variant key (multivariate flags)")
	cmd.Flags().StringVar(&inline, "json", "", "Payload as inline JSON")
	cmd.Flags().StringVar(&file, "file", "", "File holding the payload JSON")
	addWriteFlags(cmd, &opts)
	return cmd
}

func payloadUnsetCommand(globals *shared.GlobalFlags) *cobra.Command {
	var opts writeOptions
	var variant string
	cmd := &cobra.Command{
		Use:   "unset <id-or-key>",
		Short: "Remove the payload for a boolean/remote_config flag, or for one --variant",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return WithProject(cmd, globals, func(ctx context.Context, p featureflag.Project) error {
				return editFlag(ctx, p, args[0], "payload unset", opts, featureflag.UnsetPayload(variant), globals.Format)
			})
		},
	}
	cmd.Flags().StringVar(&variant, "variant", "", "Variant key (multivariate flags)")
	addWriteFlags(cmd, &opts)
	return cmd
}
