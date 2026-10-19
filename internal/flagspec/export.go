package flagspec

import (
	"fmt"
	"maps"
	"slices"
)

// Export converts a PostHog flag into a spec. Properties that the --where
// grammar reproduces exactly become expressions; anything else stays a raw
// property object. Encrypted payloads are omitted and the spec is marked
// incomplete so it cannot be applied back over the flag.
func Export(flag map[string]any) (*Ordered, error) {
	filters := Object(flag["filters"])
	version, err := configVersion(filters)
	if err != nil {
		return nil, err
	}
	if version == 2 {
		spec := exportCommon(flag, false)
		spec.Set("filters_document", filters)
		return spec, nil
	}
	spec := exportCommon(flag, true)
	if v, ok := filters["early_exit"]; ok && v != nil {
		spec.Set("early_exit", v)
	}
	if v, ok := filters["aggregation_group_type_index"]; ok && v != nil {
		spec.Set("aggregation_group_type_index", v)
	}
	flagType := TypeOf(flag)
	if flagType != TypeRemoteConfig {
		spec.Set("conditions", ExportConditions(List(filters["groups"])))
	}
	if flagType == TypeMultivariate {
		spec.Set("variants", exportVariants(List(Object(filters["multivariate"])["variants"])))
	}

	payloads := Object(filters["payloads"])
	if flag["has_encrypted_payloads"] == true && len(payloads) > 0 {
		spec.Set("incomplete", []any{"encrypted payloads omitted"})
		return spec, nil
	}
	decoded := map[string]any{}
	for _, key := range slices.Sorted(maps.Keys(payloads)) {
		value, err := DecodePayload(payloads[key])
		if err != nil {
			return nil, fmt.Errorf("payload %q: %w", key, err)
		}
		decoded[key] = value
	}
	switch {
	case flagType == TypeMultivariate && len(decoded) > 0:
		spec.Set("payloads", decoded)
	case flagType != TypeMultivariate:
		if value, ok := decoded[BooleanPayloadKey]; ok {
			spec.Set("payload", value)
		}
	}
	return spec, nil
}

// exportCommon sets the fields every spec carries, whatever the filters format.
func exportCommon(flag map[string]any, withType bool) *Ordered {
	spec := NewOrdered()
	spec.Set("schema_version", number(schemaVersion))
	spec.Set("key", flag["key"])
	if name, ok := flag["name"].(string); ok && name != "" {
		spec.Set("name", name)
	}
	if withType {
		spec.Set("type", TypeOf(flag))
	}
	spec.Set("active", flag["active"] == true)
	for _, field := range []string{"tags", "evaluation_contexts"} {
		if list := List(flag[field]); len(list) > 0 {
			spec.Set(field, list)
		}
	}
	if flag["ensure_experience_continuity"] == true {
		spec.Set("ensure_experience_continuity", true)
	}
	for _, field := range []string{"evaluation_runtime", "bucketing_identifier"} {
		if v, ok := flag[field].(string); ok && v != "" {
			spec.Set(field, v)
		}
	}
	return spec
}

// ExportConditions renders condition groups in spec form.
func ExportConditions(groups []any) []any {
	out := make([]any, 0, len(groups))
	for _, item := range groups {
		group := Object(item)
		condition := NewOrdered()
		if props := List(group["properties"]); len(props) > 0 {
			where := make([]any, 0, len(props))
			for _, p := range props {
				prop := Object(p)
				if expr, ok := FormatWhere(prop); ok {
					where = append(where, expr)
				} else {
					where = append(where, p)
				}
			}
			condition.Set("where", where)
		}
		if rollout, ok := group["rollout_percentage"]; ok {
			condition.Set("rollout", rollout)
		}
		if variant, ok := group["variant"]; ok && variant != nil {
			condition.Set("variant", variant)
		}
		for _, key := range slices.Sorted(maps.Keys(group)) {
			switch key {
			case "properties", "rollout_percentage", "variant":
				continue
			}
			if key == "aggregation_group_type_index" && group[key] == nil {
				continue
			}
			condition.Set(key, group[key])
		}
		out = append(out, condition)
	}
	return out
}

func exportVariants(variants []any) []any {
	out := make([]any, 0, len(variants))
	for _, item := range variants {
		variant := Object(item)
		entry := NewOrdered()
		entry.Set("key", variant["key"])
		entry.Set("rollout", variant["rollout_percentage"])
		if name, ok := variant["name"]; ok {
			entry.Set("name", name)
		}
		for _, key := range slices.Sorted(maps.Keys(variant)) {
			switch key {
			case "key", "rollout_percentage", "name":
				continue
			}
			entry.Set(key, variant[key])
		}
		out = append(out, entry)
	}
	return out
}
