package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	agenterrors "github.com/shhac/agent-posthog/internal/errors"
	"github.com/shhac/agent-posthog/internal/flagspec"
)

func registerFlagVariantCommands(flags *cobra.Command, globals *GlobalFlags) {
	variants := &cobra.Command{Use: "variants", Short: "Edit a multivariate flag's variants"}
	variants.AddCommand(variantsSetCommand(globals))
	flags.AddCommand(variants)
}

func variantsSetCommand(globals *GlobalFlags) *cobra.Command {
	var opts writeOptions
	cmd := &cobra.Command{
		Use:   "set <id-or-key> <key=percent>...",
		Short: "Replace a multivariate flag's variants (percents sum to 100); payloads of removed variants are dropped",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			parsed, err := parseVariantArgs(args[1:])
			if err != nil {
				return err
			}
			if err := flagspec.ValidateVariants(parsed); err != nil {
				return agenterrors.Wrap(err, agenterrors.FixableByAgent)
			}
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				return updateFlag(ctx, resolved, globals, args[0], "variants set", opts, func(flag map[string]any) (*flagspec.Patch, []string, error) {
					return variantsPatch(flag, parsed)
				})
			})
		},
	}
	addWriteFlags(cmd, &opts)
	return cmd
}

func variantsPatch(flag map[string]any, parsed []any) (*flagspec.Patch, []string, error) {
	if flagspec.TypeOf(flag) != flagspec.TypeMultivariate {
		return nil, nil, fmt.Errorf("the flag is not multivariate; convert it with 'flags apply' and type: multivariate")
	}
	filters := asObject(flag["filters"])
	existing := map[string]map[string]any{}
	var oldOrder []string
	for _, v := range asList(asObject(filters["multivariate"])["variants"]) {
		variant := asObject(v)
		key := fmt.Sprint(variant["key"])
		existing[key] = variant
		oldOrder = append(oldOrder, key)
	}
	keys := map[string]bool{}
	variants := make([]any, 0, len(parsed))
	var newOrder []string
	for _, item := range parsed {
		spec := asObject(item)
		key := spec["key"].(string)
		entry, _ := flagspec.Clone(existing[key]).(map[string]any)
		if entry == nil {
			entry = map[string]any{"key": key}
		}
		entry["rollout_percentage"] = spec["rollout"]
		variants = append(variants, entry)
		keys[key] = true
		if existing[key] != nil {
			newOrder = append(newOrder, key)
		}
	}
	if err := flagspec.CheckOverrides(asList(filters["groups"]), flagspec.TypeMultivariate, keys); err != nil {
		return nil, nil, fmt.Errorf("%w; change or remove that condition first", err)
	}
	p := flagspec.NewPatch()
	p.Filters["multivariate"] = map[string]any{"variants": variants}
	if kept, changed := flagspec.PrunePayloads(asObject(filters["payloads"]), flagspec.TypeMultivariate, keys); changed {
		p.Filters["payloads"] = kept
	}
	var warnings []string
	if !sameOrder(oldOrder, newOrder, keys) {
		warnings = append(warnings, "Variant order changed; users are bucketed by position, so some will move to a different variant.")
	}
	return p, warnings, nil
}

// sameOrder reports whether the variants kept from before are still in their
// old relative order.
func sameOrder(oldOrder, newOrder []string, keys map[string]bool) bool {
	var kept []string
	for _, key := range oldOrder {
		if keys[key] {
			kept = append(kept, key)
		}
	}
	if len(kept) != len(newOrder) {
		return false
	}
	for i := range kept {
		if kept[i] != newOrder[i] {
			return false
		}
	}
	return true
}
