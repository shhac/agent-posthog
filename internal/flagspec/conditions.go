package flagspec

import (
	"fmt"
	"strconv"
)

func buildGroups(conditions []any, resolve FlagResolver) ([]any, error) {
	groups := make([]any, 0, len(conditions))
	for i, item := range conditions {
		group, err := BuildGroup(Object(item), resolve)
		if err != nil {
			return nil, fmt.Errorf("conditions[%d]: %w", i, err)
		}
		groups = append(groups, group)
	}
	return groups, nil
}

// BuildGroup converts one spec condition into a PostHog condition group.
// Keys other than where/rollout/variant pass through unchanged.
func BuildGroup(condition map[string]any, resolve FlagResolver) (map[string]any, error) {
	group := map[string]any{}
	for key, value := range condition {
		switch key {
		case "where", "rollout", "variant":
			continue
		}
		group[key] = Clone(value)
	}
	props, err := BuildProperties(List(condition["where"]), resolve)
	if err != nil {
		return nil, err
	}
	group["properties"] = props
	group["rollout_percentage"] = number(100)
	if rollout, ok := condition["rollout"]; ok {
		group["rollout_percentage"] = rollout
	}
	if variant, ok := condition["variant"]; ok {
		group["variant"] = variant
	}
	return group, nil
}

// BuildProperties parses --where strings and passes property objects through,
// resolving flag dependency keys to IDs.
func BuildProperties(where []any, resolve FlagResolver) ([]any, error) {
	props := make([]any, 0, len(where))
	for _, item := range where {
		prop, err := buildProperty(item)
		if err != nil {
			return nil, err
		}
		if err := resolveFlagDependency(prop, resolve); err != nil {
			return nil, err
		}
		props = append(props, prop)
	}
	return props, nil
}

// buildProperty reads one where item: an expression, or a property object
// used as is.
func buildProperty(item any) (map[string]any, error) {
	switch w := item.(type) {
	case string:
		return parseWhere(w)
	case map[string]any:
		return Object(Clone(w)), nil
	default:
		return nil, fmt.Errorf("where items must be expressions or property objects")
	}
}

// resolveFlagDependency replaces a flag dependency's key with its numeric ID,
// which is what PostHog stores.
func resolveFlagDependency(prop map[string]any, resolve FlagResolver) error {
	if prop["type"] != PropertyFlag {
		return nil
	}
	ref := fmt.Sprint(prop["key"])
	if _, err := strconv.Atoi(ref); err == nil {
		return nil
	}
	if resolve == nil {
		return fmt.Errorf("flag dependency %q needs resolving to an ID", ref)
	}
	id, err := resolve(ref)
	if err != nil {
		return fmt.Errorf("flag dependency %q: %w", ref, err)
	}
	prop["key"] = id
	return nil
}

// CheckOverrides verifies every condition's variant override names an existing
// variant, and that non-multivariate flags carry none.
func CheckOverrides(groups []any, flagType string, variantKeys map[string]bool) error {
	for i, item := range groups {
		variant, ok := Object(item)["variant"].(string)
		if !ok {
			continue
		}
		if flagType != TypeMultivariate {
			return fmt.Errorf("condition %d overrides variant %q, but only multivariate flags have variants", i, variant)
		}
		if !variantKeys[variant] {
			return fmt.Errorf("condition %d overrides variant %q, which the flag does not define", i, variant)
		}
	}
	return nil
}

// ConditionWarnings flags condition orders that probably do not do what was
// meant. Conditions are tried top to bottom and the first match wins, so a
// catch-all at 100% hides everything after it.
func ConditionWarnings(groups []any, flagType string) []string {
	if flagType == TypeRemoteConfig {
		return nil
	}
	if len(groups) == 0 {
		return []string{"The flag has no conditions, so it matches nobody."}
	}
	var warnings []string
	for i, item := range groups[:len(groups)-1] {
		group := Object(item)
		rollout, hasRollout := group["rollout_percentage"]
		full := !hasRollout || rollout == nil || Equal(rollout, number(100))
		if len(List(group["properties"])) == 0 && full {
			warnings = append(warnings, fmt.Sprintf("Condition %d matches everyone at 100%%, so conditions after it never match.", i))
		}
	}
	return warnings
}
