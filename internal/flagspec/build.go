package flagspec

import (
	"fmt"
	"strconv"
)

// Patch is a minimal write: top-level flag fields plus the filters sections
// that change. PostHog merges filters shallowly, so only sections present here
// are replaced; a nil section value clears it.
type Patch struct {
	Fields  map[string]any
	Filters map[string]any
}

func NewPatch() *Patch {
	return &Patch{Fields: map[string]any{}, Filters: map[string]any{}}
}

// Empty reports whether the patch changes nothing.
func (p *Patch) Empty() bool { return len(p.Fields) == 0 && len(p.Filters) == 0 }

// Body renders the request body.
func (p *Patch) Body() map[string]any {
	body := make(map[string]any, len(p.Fields)+1)
	for k, v := range p.Fields {
		body[k] = v
	}
	if len(p.Filters) > 0 {
		body["filters"] = p.Filters
	}
	return body
}

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
	filters := asMap(current["filters"])
	if err := RequireV1(filters); err != nil {
		return nil, err
	}
	if !creating && current["key"] != spec.Key() {
		return nil, fmt.Errorf("spec key %q does not match flag key %v; renaming is not supported", spec.Key(), current["key"])
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
	variantKeys := map[string]bool{}
	for _, v := range variants {
		variantKeys[fmt.Sprint(asMap(v)["key"])] = true
	}

	switch {
	case spec.Has("conditions"):
		if toType == TypeRemoteConfig {
			return nil, fmt.Errorf("remote_config flags serve one payload to everyone; remove conditions or use type multivariate")
		}
		groups, err := buildGroups(asSlice(spec.Get("conditions")), resolve)
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
		if err := CheckOverrides(asSlice(filters["groups"]), toType, variantKeys); err != nil {
			return nil, fmt.Errorf("%w; supply conditions in the spec as well", err)
		}
	}

	if err := buildPayloads(p, spec, filters, toType, variantKeys, encrypted); err != nil {
		return nil, err
	}
	return p, nil
}

func buildMultivariate(p *Patch, spec *Spec, filters map[string]any, fromType, toType string) ([]any, error) {
	switch {
	case spec.Has("variants"):
		if toType != TypeMultivariate {
			return nil, fmt.Errorf("variants need type multivariate")
		}
		variants := BuildVariants(asSlice(spec.Get("variants")))
		p.Filters["multivariate"] = map[string]any{"variants": variants}
		return variants, nil
	case toType == TypeMultivariate:
		variants := asSlice(asMap(filters["multivariate"])["variants"])
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
	stored := asMap(filters["payloads"])
	switch {
	case spec.Has("payloads"):
		if toType != TypeMultivariate {
			return fmt.Errorf("payloads (per variant) need type multivariate; use payload for boolean and remote_config flags")
		}
		if encrypted {
			return fmt.Errorf("encrypted payloads cannot be changed through agent-posthog")
		}
		out := map[string]any{}
		for key, value := range asMap(spec.Get("payloads")) {
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
		if encrypted {
			return fmt.Errorf("encrypted payloads cannot be changed through agent-posthog")
		}
		encoded, err := KeepOrEncode(stored["true"], spec.Get("payload"))
		if err != nil {
			return fmt.Errorf("payload: %w", err)
		}
		p.Filters["payloads"] = map[string]any{"true": encoded}
	case !encrypted && len(stored) > 0:
		kept := map[string]any{}
		for key, value := range stored {
			if (toType == TypeMultivariate && variantKeys[key]) || (toType != TypeMultivariate && key == "true") {
				kept[key] = value
			}
		}
		if len(kept) != len(stored) {
			p.Filters["payloads"] = kept
		}
	}
	return nil
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
		group, err := BuildGroup(asMap(item), resolve)
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
	props, err := BuildProperties(asSlice(condition["where"]), resolve)
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
			prop = asMap(Clone(w))
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
		variant := asMap(item)
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
		variant, ok := asMap(item)["variant"].(string)
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

// Prune drops patch entries that already match current, so plans list only real
// changes and unchanged writes are skipped.
func (p *Patch) Prune(current map[string]any) {
	if current == nil {
		return
	}
	for key, value := range p.Fields {
		if existing, ok := current[key]; ok && Equal(existing, value) {
			delete(p.Fields, key)
		}
	}
	filters := asMap(current["filters"])
	for key, value := range p.Filters {
		existing, ok := filters[key]
		if !ok && value == nil {
			delete(p.Filters, key)
			continue
		}
		if key == "groups" {
			existing, value = normalizeGroups(existing), normalizeGroups(value)
		}
		if Equal(existing, value) {
			delete(p.Filters, key)
		}
	}
}

// normalizeGroups treats a null variant like a missing one; they evaluate the same.
func normalizeGroups(v any) any {
	groups := asSlice(Clone(v))
	for _, item := range groups {
		group := asMap(item)
		if variant, ok := group["variant"]; ok && variant == nil {
			delete(group, "variant")
		}
	}
	if groups == nil {
		return v
	}
	return groups
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
		group := asMap(item)
		rollout, hasRollout := group["rollout_percentage"]
		full := !hasRollout || rollout == nil || Equal(rollout, number(100))
		if len(asSlice(group["properties"])) == 0 && full {
			warnings = append(warnings, fmt.Sprintf("Condition %d matches everyone at 100%%, so conditions after it never match.", i))
		}
	}
	return warnings
}
