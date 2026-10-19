package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	agenterrors "github.com/shhac/agent-posthog/internal/errors"
	"github.com/shhac/agent-posthog/internal/flagspec"
)

func registerFlagPayloadCommands(flags *cobra.Command, globals *GlobalFlags) {
	payload := &cobra.Command{Use: "payload", Short: "Read and edit flag payloads (the JSON a matching user receives)"}
	payload.AddCommand(payloadGetCommand(globals))
	payload.AddCommand(payloadSetCommand(globals))
	payload.AddCommand(payloadUnsetCommand(globals))
	flags.AddCommand(payload)
}

func payloadGetCommand(globals *GlobalFlags) *cobra.Command {
	var variant string
	cmd := &cobra.Command{
		Use:   "get <id-or-key>",
		Short: "Show the payload a matching user receives (per --variant for multivariate flags)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				if err := requireProject(resolved); err != nil {
					return err
				}
				flag, err := fetchFlag(ctx, resolved, resolved.ProjectID, args[0])
				if err != nil {
					return err
				}
				payloadKey, err := payloadKeyFor(flag, variant)
				if err != nil {
					return err
				}
				stored, ok := asObject(asObject(flag["filters"])["payloads"])[payloadKey]
				result := flagspec.NewOrdered()
				result.Set("key", flag["key"])
				result.Set("payload_key", payloadKey)
				result.Set("set", ok)
				if ok {
					value, err := flagspec.DecodePayload(stored)
					if err != nil {
						return agenterrors.Wrap(err, agenterrors.FixableByHuman)
					}
					result.Set("payload", value)
				}
				return writeExact(result, globals.Format)
			})
		},
	}
	cmd.Flags().StringVar(&variant, "variant", "", "Variant key (multivariate flags)")
	return cmd
}

func payloadSetCommand(globals *GlobalFlags) *cobra.Command {
	var opts writeOptions
	var variant, inline, file string
	cmd := &cobra.Command{
		Use:   "set <id-or-key>",
		Short: "Set the payload for a boolean/remote_config flag, or for one --variant",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := readInput(inline, file, "json", "file")
			if err != nil {
				return err
			}
			value, err := flagspec.Decode(data)
			if err != nil {
				return agenterrors.New("payload is not valid JSON: "+err.Error(), agenterrors.FixableByAgent)
			}
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				return updateFlag(ctx, resolved, globals, args[0], "payload set", opts, func(flag map[string]any) (*flagspec.Patch, []string, error) {
					return payloadPatch(flag, variant, func(payloads map[string]any, key string) error {
						encoded, err := flagspec.KeepOrEncode(payloads[key], value)
						payloads[key] = encoded
						return err
					})
				})
			})
		},
	}
	cmd.Flags().StringVar(&variant, "variant", "", "Variant key (multivariate flags)")
	cmd.Flags().StringVar(&inline, "json", "", "Payload as inline JSON")
	cmd.Flags().StringVar(&file, "file", "", "File holding the payload JSON")
	addWriteFlags(cmd, &opts)
	return cmd
}

func payloadUnsetCommand(globals *GlobalFlags) *cobra.Command {
	var opts writeOptions
	var variant string
	cmd := &cobra.Command{
		Use:   "unset <id-or-key>",
		Short: "Remove the payload for a boolean/remote_config flag, or for one --variant",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				return updateFlag(ctx, resolved, globals, args[0], "payload unset", opts, func(flag map[string]any) (*flagspec.Patch, []string, error) {
					return payloadPatch(flag, variant, func(payloads map[string]any, key string) error {
						delete(payloads, key)
						return nil
					})
				})
			})
		},
	}
	cmd.Flags().StringVar(&variant, "variant", "", "Variant key (multivariate flags)")
	addWriteFlags(cmd, &opts)
	return cmd
}

func payloadPatch(flag map[string]any, variant string, edit func(payloads map[string]any, key string) error) (*flagspec.Patch, []string, error) {
	if flag["has_encrypted_payloads"] == true {
		return nil, nil, fmt.Errorf("this flag has encrypted payloads, which agent-posthog does not change; edit them in PostHog")
	}
	if err := flagspec.RequireV1(asObject(flag["filters"])); err != nil {
		return nil, nil, err
	}
	key, err := payloadKeyFor(flag, variant)
	if err != nil {
		return nil, nil, err
	}
	payloads, _ := flagspec.Clone(asObject(asObject(flag["filters"])["payloads"])).(map[string]any)
	if payloads == nil {
		payloads = map[string]any{}
	}
	if err := edit(payloads, key); err != nil {
		return nil, nil, err
	}
	p := flagspec.NewPatch()
	p.Filters["payloads"] = payloads
	return p, nil, nil
}

// payloadKeyFor maps a variant choice to the filters.payloads key: the variant
// for multivariate flags, "true" otherwise.
func payloadKeyFor(flag map[string]any, variant string) (string, error) {
	if flagspec.TypeOf(flag) != flagspec.TypeMultivariate {
		if variant != "" {
			return "", agenterrors.New("only multivariate flags have per-variant payloads; drop --variant", agenterrors.FixableByAgent)
		}
		return flagspec.BooleanPayloadKey, nil
	}
	if variant == "" {
		return "", agenterrors.New("multivariate flags have one payload per variant; pass --variant", agenterrors.FixableByAgent)
	}
	if !flagspec.VariantKeys(flag)[variant] {
		return "", agenterrors.New("the flag has no variant "+variant, agenterrors.FixableByAgent)
	}
	return variant, nil
}
