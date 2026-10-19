package flagspec

import (
	"fmt"
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
