package flagspec

import (
	"fmt"
	"strconv"
)

// FlagResolver maps a flag dependency's key (or ID) to its numeric ID.
type FlagResolver func(ref string) (string, error)

var passthroughFields = []string{
	"name", "active", "tags", "evaluation_contexts", "ensure_experience_continuity",
	"evaluation_runtime", "bucketing_identifier",
}

// Build turns a spec into the patch that brings current (nil when creating) in
// line with it. Fields the spec omits are left as they are.
func Build(current map[string]any, spec *Spec, resolve FlagResolver) (*Patch, error) {
	creating := current == nil
	if creating {
		current = map[string]any{}
	}
	if !creating && current["key"] != spec.Key() {
		return nil, fmt.Errorf("spec key %q does not match flag key %v; renaming is not supported", spec.Key(), current["key"])
	}
	filters := Object(current["filters"])
	if spec.Has("filters_document") {
		return buildDocument(current, creating, spec)
	}
	if err := RequireV1(filters); err != nil {
		return nil, err
	}
	encrypted := current["has_encrypted_payloads"] == true
	fromType := TypeBoolean
	if !creating {
		fromType = TypeOf(current)
	}
	toType := fromType
	if t, ok := spec.Type(); ok {
		toType = t
	}
	if encrypted && toType != TypeRemoteConfig {
		return nil, fmt.Errorf("this flag has encrypted payloads, which PostHog only allows on remote_config flags")
	}

	p := NewPatch()
	if creating {
		p.Fields["key"] = spec.Key()
		p.Fields["active"] = false
	}
	for _, field := range passthroughFields {
		if spec.Has(field) {
			p.Fields[field] = Clone(spec.Get(field))
		}
	}
	if creating || spec.Has("type") {
		p.Fields["is_remote_configuration"] = toType == TypeRemoteConfig
	}
	for _, section := range []string{"early_exit", "aggregation_group_type_index"} {
		if spec.Has(section) {
			p.Filters[section] = spec.Get(section)
		}
	}

	variants, err := buildMultivariate(p, spec, filters, fromType, toType)
	if err != nil {
		return nil, err
	}
	variantKeys := variantKeySet(variants)

	switch {
	case spec.Has("conditions"):
		if toType == TypeRemoteConfig {
			return nil, fmt.Errorf("remote_config flags serve one payload to everyone; remove conditions or use type multivariate")
		}
		groups, err := buildGroups(List(spec.Get("conditions")), resolve)
		if err != nil {
			return nil, err
		}
		if err := CheckOverrides(groups, toType, variantKeys); err != nil {
			return nil, err
		}
		p.Filters["groups"] = groups
	case creating && toType == TypeRemoteConfig:
		p.Filters["groups"] = []any{map[string]any{"properties": []any{}, "rollout_percentage": number(100)}}
	case creating:
		p.Filters["groups"] = []any{}
	case fromType != toType || spec.Has("variants"):
		if err := CheckOverrides(List(filters["groups"]), toType, variantKeys); err != nil {
			return nil, fmt.Errorf("%w; supply conditions in the spec as well", err)
		}
	}

	if err := buildPayloads(p, spec, filters, toType, variantKeys, encrypted); err != nil {
		return nil, err
	}
	return p, nil
}

// buildDocument writes a config version 2 filters document whole. PostHog
// validates the document itself and admits these writes per project.
func buildDocument(current map[string]any, creating bool, spec *Spec) (*Patch, error) {
	if !creating {
		version, err := ConfigVersion(Object(current["filters"]))
		if err != nil {
			return nil, err
		}
		if version != 2 {
			return nil, fmt.Errorf("this flag uses classic filters; describe them with conditions, variants, and payloads instead of filters_document")
		}
	}
	p := NewPatch()
	if creating {
		if spec.Get("active") == true {
			return nil, fmt.Errorf("PostHog creates config version 2 flags disabled; create it, then run 'flags enable'")
		}
		p.Fields["key"] = spec.Key()
		p.Fields["active"] = false
	}
	for _, field := range passthroughFields {
		if spec.Has(field) {
			p.Fields[field] = Clone(spec.Get(field))
		}
	}
	p.Fields["filters"] = Clone(spec.Get("filters_document"))
	return p, nil
}

func buildMultivariate(p *Patch, spec *Spec, filters map[string]any, fromType, toType string) ([]any, error) {
	switch {
	case spec.Has("variants"):
		if toType != TypeMultivariate {
			return nil, fmt.Errorf("variants need type multivariate")
		}
		variants := BuildVariants(List(spec.Get("variants")))
		p.Filters["multivariate"] = map[string]any{"variants": variants}
		return variants, nil
	case toType == TypeMultivariate:
		variants := List(Object(filters["multivariate"])["variants"])
		if len(variants) == 0 {
			return nil, fmt.Errorf("multivariate flags need variants")
		}
		return variants, nil
	case fromType == TypeMultivariate:
		p.Filters["multivariate"] = nil
	}
	return nil, nil
}

func buildPayloads(p *Patch, spec *Spec, filters map[string]any, toType string, variantKeys map[string]bool, encrypted bool) error {
	stored := Object(filters["payloads"])
	editing := spec.Has("payloads") || spec.Has("payload")
	if encrypted && editing {
		return fmt.Errorf("encrypted payloads cannot be changed through agent-posthog")
	}
	switch {
	case spec.Has("payloads"):
		if toType != TypeMultivariate {
			return fmt.Errorf("payloads (per variant) need type multivariate; use payload for boolean and remote_config flags")
		}
		out := map[string]any{}
		for key, value := range Object(spec.Get("payloads")) {
			if !variantKeys[key] {
				return fmt.Errorf("payload %q does not name a variant", key)
			}
			encoded, err := KeepOrEncode(stored[key], value)
			if err != nil {
				return fmt.Errorf("payload %q: %w", key, err)
			}
			out[key] = encoded
		}
		p.Filters["payloads"] = out
	case spec.Has("payload"):
		if toType == TypeMultivariate {
			return fmt.Errorf("multivariate flags take payloads keyed by variant, not payload")
		}
		encoded, err := KeepOrEncode(stored[BooleanPayloadKey], spec.Get("payload"))
		if err != nil {
			return fmt.Errorf("payload: %w", err)
		}
		p.Filters["payloads"] = map[string]any{BooleanPayloadKey: encoded}
	case !encrypted:
		if kept, changed := PrunePayloads(stored, toType, variantKeys); changed {
			p.Filters["payloads"] = kept
		}
	}
	return nil
}

// BooleanPayloadKey is the filters.payloads key of a boolean or remote_config
// flag's single payload.
const BooleanPayloadKey = "true"

// PrunePayloads drops payloads no longer reachable on a flag of flagType with
// the given variants, reporting whether anything was dropped.
func PrunePayloads(stored map[string]any, flagType string, variantKeys map[string]bool) (map[string]any, bool) {
	kept := map[string]any{}
	for key, value := range stored {
		if flagType == TypeMultivariate && variantKeys[key] || flagType != TypeMultivariate && key == BooleanPayloadKey {
			kept[key] = value
		}
	}
	return kept, len(kept) != len(stored)
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

// KeepOrEncode reuses the stored payload string when it already holds value,
// so untouched payloads are not re-encoded.
func KeepOrEncode(stored, value any) (any, error) {
	if stored != nil {
		if decoded, err := DecodePayload(stored); err == nil && Equal(decoded, value) {
			return stored, nil
		}
	}
	return EncodePayload(value)
}

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
		var prop map[string]any
		switch w := item.(type) {
		case string:
			parsed, err := ParseWhere(w)
			if err != nil {
				return nil, err
			}
			prop = parsed
		case map[string]any:
			prop = Object(Clone(w))
		default:
			return nil, fmt.Errorf("where items must be expressions or property objects")
		}
		if prop["type"] == PropertyFlag {
			ref := fmt.Sprint(prop["key"])
			if _, err := strconv.Atoi(ref); err != nil {
				if resolve == nil {
					return nil, fmt.Errorf("flag dependency %q needs resolving to an ID", ref)
				}
				id, err := resolve(ref)
				if err != nil {
					return nil, fmt.Errorf("flag dependency %q: %w", ref, err)
				}
				prop["key"] = id
			}
		}
		props = append(props, prop)
	}
	return props, nil
}

// BuildVariants converts spec variants into PostHog multivariate variants.
func BuildVariants(variants []any) []any {
	out := make([]any, 0, len(variants))
	for _, item := range variants {
		variant := Object(item)
		entry := map[string]any{}
		for key, value := range variant {
			if key == "rollout" {
				continue
			}
			entry[key] = Clone(value)
		}
		entry["rollout_percentage"] = variant["rollout"]
		out = append(out, entry)
	}
	return out
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
