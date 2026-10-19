package flagspec

import (
	"fmt"
	"iter"
)

// TypeOf classifies a flag as boolean, multivariate, or remote_config.
func TypeOf(flag map[string]any) string {
	if flag["is_remote_configuration"] == true {
		return TypeRemoteConfig
	}
	if len(List(Object(Object(flag["filters"])["multivariate"])["variants"])) > 0 {
		return TypeMultivariate
	}
	return TypeBoolean
}

// ConfigVersion reports a filters document's PostHog config format: 1 (the
// classic groups/multivariate/payloads shape) or 2 (a pilot document PostHog
// validates itself).
func ConfigVersion(filters map[string]any) (int, error) {
	v, ok := filters["version"]
	switch {
	case !ok || v == nil || Equal(v, number(1)):
		return 1, nil
	case Equal(v, number(2)):
		return 2, nil
	default:
		return 0, fmt.Errorf("this flag's filters use config version %v, which agent-posthog does not support", v)
	}
}

// RequireV1 refuses edits that address parts of the classic filters shape on
// a flag stored in another format.
func RequireV1(filters map[string]any) error {
	version, err := ConfigVersion(filters)
	if err != nil || version == 1 {
		return err
	}
	return fmt.Errorf("this flag uses PostHog's config version 2 filters; edit them as a whole with 'flags export' and 'flags apply' (filters_document)")
}

// VariantKeys returns the keys of a flag's variants.
func VariantKeys(flag map[string]any) map[string]bool {
	return variantKeySet(List(Object(Object(flag["filters"])["multivariate"])["variants"]))
}

func variantKeySet(variants []any) map[string]bool {
	keys := map[string]bool{}
	for _, v := range variants {
		keys[fmt.Sprint(Object(v)["key"])] = true
	}
	return keys
}

// ConditionProperties yields every property filter of a flag's release
// conditions, as the live maps, so callers may edit them in place.
func ConditionProperties(flag map[string]any) iter.Seq[map[string]any] {
	return func(yield func(map[string]any) bool) {
		for _, group := range List(Object(flag["filters"])["groups"]) {
			for _, item := range List(Object(group)["properties"]) {
				prop, ok := item.(map[string]any)
				if ok && !yield(prop) {
					return
				}
			}
		}
	}
}
