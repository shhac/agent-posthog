package featureflag

import (
	"context"
	"fmt"
	"slices"

	"github.com/shhac/agent-posthog/internal/flagspec"
)

// SetVariants replaces a multivariate flag's variants with parsed (from
// flagspec variant specs), keeping what PostHog stored for variants that stay
// and dropping the payloads of those that go.
func SetVariants(parsed []any) Build {
	return func(_ context.Context, _ Project, flag map[string]any) (*flagspec.Patch, []string, error) {
		if flagspec.TypeOf(flag) != flagspec.TypeMultivariate {
			return nil, nil, fmt.Errorf("the flag is not multivariate; convert it with 'flags apply' and type: multivariate")
		}
		filters := flagspec.Object(flag["filters"])
		existing := map[string]map[string]any{}
		var oldOrder []string
		for _, v := range flagspec.List(flagspec.Object(filters["multivariate"])["variants"]) {
			variant := flagspec.Object(v)
			key := fmt.Sprint(variant["key"])
			existing[key] = variant
			oldOrder = append(oldOrder, key)
		}
		keys := map[string]bool{}
		variants := make([]any, 0, len(parsed))
		var newOrder []string
		for _, item := range parsed {
			spec := flagspec.Object(item)
			key := fmt.Sprint(spec["key"])
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
		if err := flagspec.CheckOverrides(flagspec.List(filters["groups"]), flagspec.TypeMultivariate, keys); err != nil {
			return nil, nil, fmt.Errorf("%w; change or remove that condition first", err)
		}
		patch := flagspec.NewPatch()
		patch.Filters["multivariate"] = map[string]any{"variants": variants}
		if kept, changed := flagspec.PrunePayloads(flagspec.Object(filters["payloads"]), flagspec.TypeMultivariate, keys); changed {
			patch.Filters["payloads"] = kept
		}
		var warnings []string
		if !sameOrder(oldOrder, newOrder, keys) {
			warnings = append(warnings, "Variant order changed; users are bucketed by position, so some will move to a different variant.")
		}
		return patch, warnings, nil
	}
}

// sameOrder reports whether the variants kept from before are still in their
// old relative order.
func sameOrder(oldOrder, newOrder []string, keys map[string]bool) bool {
	kept := slices.DeleteFunc(slices.Clone(oldOrder), func(key string) bool { return !keys[key] })
	return slices.Equal(kept, newOrder)
}
