package flagspec

import (
	"fmt"
	"maps"
	"slices"

	agentoutput "github.com/shhac/lib-agent-output"
)

// Export converts a PostHog flag into a spec. Properties that the --where
// grammar reproduces exactly become expressions; anything else stays a raw
// property object. Encrypted payloads are omitted and the spec is marked
// incomplete so it cannot be applied back over the flag.
func Export(flag map[string]any) (agentoutput.Ordered, error) {
	filters := Object(flag["filters"])
	version, err := configVersion(filters)
	if err != nil {
		return nil, err
	}
	if version == 2 {
		spec := exportCommon(flag, false)
		spec = append(spec, agentoutput.Field{Key: "filters_document", Value: filters})
		return spec, nil
	}
	spec := exportCommon(flag, true)
	if v, ok := filters["early_exit"]; ok && v != nil {
		spec = append(spec, agentoutput.Field{Key: "early_exit", Value: v})
	}
	if v, ok := filters["aggregation_group_type_index"]; ok && v != nil {
		spec = append(spec, agentoutput.Field{Key: "aggregation_group_type_index", Value: v})
	}
	flagType := TypeOf(flag)
	if flagType != TypeRemoteConfig {
		spec = append(spec, agentoutput.Field{Key: "conditions", Value: ExportConditions(List(filters["groups"]))})
	}
	if flagType == TypeMultivariate {
		spec = append(spec, agentoutput.Field{Key: "variants", Value: exportVariants(List(Object(filters["multivariate"])["variants"]))})
	}

	payloads := Object(filters["payloads"])
	if flag["has_encrypted_payloads"] == true && len(payloads) > 0 {
		spec = append(spec, agentoutput.Field{Key: "incomplete", Value: []any{"encrypted payloads omitted"}})
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
		spec = append(spec, agentoutput.Field{Key: "payloads", Value: decoded})
	case flagType != TypeMultivariate:
		if value, ok := decoded[BooleanPayloadKey]; ok {
			spec = append(spec, agentoutput.Field{Key: "payload", Value: value})
		}
	}
	return spec, nil
}

// exportCommon sets the fields every spec carries, whatever the filters format.
func exportCommon(flag map[string]any, withType bool) agentoutput.Ordered {
	spec := agentoutput.Ordered{
		{Key: "schema_version", Value: number(schemaVersion)},
		{Key: "key", Value: flag["key"]},
	}
	if name, ok := flag["name"].(string); ok && name != "" {
		spec = append(spec, agentoutput.Field{Key: "name", Value: name})
	}
	if withType {
		spec = append(spec, agentoutput.Field{Key: "type", Value: TypeOf(flag)})
	}
	spec = append(spec, agentoutput.Field{Key: "active", Value: flag["active"] == true})
	for _, field := range []string{"tags", "evaluation_contexts"} {
		if list := List(flag[field]); len(list) > 0 {
			spec = append(spec, agentoutput.Field{Key: field, Value: list})
		}
	}
	if flag["ensure_experience_continuity"] == true {
		spec = append(spec, agentoutput.Field{Key: "ensure_experience_continuity", Value: true})
	}
	for _, field := range []string{"evaluation_runtime", "bucketing_identifier"} {
		if v, ok := flag[field].(string); ok && v != "" {
			spec = append(spec, agentoutput.Field{Key: field, Value: v})
		}
	}
	return spec
}

// ExportConditions renders condition groups in spec form.
func ExportConditions(groups []any) []any {
	out := make([]any, 0, len(groups))
	for _, item := range groups {
		group := Object(item)
		condition := agentoutput.Ordered{}
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
			condition = append(condition, agentoutput.Field{Key: "where", Value: where})
		}
		if rollout, ok := group["rollout_percentage"]; ok {
			condition = append(condition, agentoutput.Field{Key: "rollout", Value: rollout})
		}
		if variant, ok := group["variant"]; ok && variant != nil {
			condition = append(condition, agentoutput.Field{Key: "variant", Value: variant})
		}
		for _, key := range slices.Sorted(maps.Keys(group)) {
			switch key {
			case "properties", "rollout_percentage", "variant":
				continue
			}
			if key == "aggregation_group_type_index" && group[key] == nil {
				continue
			}
			condition = append(condition, agentoutput.Field{Key: key, Value: group[key]})
		}
		out = append(out, condition)
	}
	return out
}

func exportVariants(variants []any) []any {
	out := make([]any, 0, len(variants))
	for _, item := range variants {
		variant := Object(item)
		entry := agentoutput.Ordered{
			{Key: "key", Value: variant["key"]},
			{Key: "rollout", Value: variant["rollout_percentage"]},
		}
		if name, ok := variant["name"]; ok {
			entry = append(entry, agentoutput.Field{Key: "name", Value: name})
		}
		for _, key := range slices.Sorted(maps.Keys(variant)) {
			switch key {
			case "key", "rollout_percentage", "name":
				continue
			}
			entry = append(entry, agentoutput.Field{Key: key, Value: variant[key]})
		}
		out = append(out, entry)
	}
	return out
}
