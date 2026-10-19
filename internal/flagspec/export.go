package flagspec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Ordered is a JSON/YAML object that keeps insertion order, so exported specs
// read top-down (key, type, conditions, variants, payloads) instead of sorted.
type Ordered struct {
	keys   []string
	values map[string]any
}

func NewOrdered() *Ordered { return &Ordered{values: map[string]any{}} }

func (o *Ordered) Set(key string, value any) {
	if _, ok := o.values[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.values[key] = value
}

func (o *Ordered) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, key := range o.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		k, _ := json.Marshal(key)
		buf.Write(k)
		buf.WriteByte(':')
		v, err := json.Marshal(o.values[key])
		if err != nil {
			return nil, err
		}
		buf.Write(v)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func (o *Ordered) MarshalYAML() (any, error) {
	node := &yaml.Node{Kind: yaml.MappingNode}
	for _, key := range o.keys {
		value, err := yamlNode(o.values[key])
		if err != nil {
			return nil, err
		}
		node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
	}
	return node, nil
}

// MarshalYAML renders any JSON-encodable value as YAML, keeping numbers exact
// and quoting strings YAML would otherwise retype.
func MarshalYAML(v any) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	decoded, err := Decode(data)
	if err != nil {
		return nil, err
	}
	if ordered, ok := v.(*Ordered); ok {
		return yaml.Marshal(ordered)
	}
	node, err := yamlNode(decoded)
	if err != nil {
		return nil, err
	}
	return yaml.Marshal(node)
}

// yamlNode renders decoded JSON as YAML, keeping json.Number text exact and
// quoting strings that YAML would otherwise read as another type.
func yamlNode(v any) (*yaml.Node, error) {
	switch t := v.(type) {
	case *Ordered:
		out, err := t.MarshalYAML()
		if err != nil {
			return nil, err
		}
		return out.(*yaml.Node), nil
	case json.Number:
		tag := "!!int"
		if _, err := strconv.ParseInt(t.String(), 10, 64); err != nil {
			tag = "!!float"
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: t.String()}, nil
	case string:
		node := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: t}
		var probe any
		if yaml.Unmarshal([]byte(t), &probe) != nil || probe != t {
			node.Style = yaml.DoubleQuotedStyle
		}
		return node, nil
	case map[string]any:
		ordered := NewOrdered()
		for _, key := range sortedKeys(t) {
			ordered.Set(key, t[key])
		}
		return yamlNode(ordered)
	case []any:
		node := &yaml.Node{Kind: yaml.SequenceNode}
		for _, item := range t {
			child, err := yamlNode(item)
			if err != nil {
				return nil, err
			}
			node.Content = append(node.Content, child)
		}
		return node, nil
	default:
		node := &yaml.Node{}
		if err := node.Encode(t); err != nil {
			return nil, err
		}
		return node, nil
	}
}

// Export converts a PostHog flag into a spec. Properties that the --where
// grammar reproduces exactly become expressions; anything else stays a raw
// property object. Encrypted payloads are omitted and the spec is marked
// incomplete so it cannot be applied back over the flag.
func Export(flag map[string]any) (*Ordered, error) {
	filters := asMap(flag["filters"])
	if err := RequireV1(filters); err != nil {
		return nil, err
	}
	spec := NewOrdered()
	spec.Set("schema_version", number(SchemaVersion))
	spec.Set("key", flag["key"])
	if name, ok := flag["name"].(string); ok && name != "" {
		spec.Set("name", name)
	}
	flagType := TypeOf(flag)
	spec.Set("type", flagType)
	spec.Set("active", flag["active"] == true)
	for _, field := range []string{"tags", "evaluation_contexts"} {
		if list := asSlice(flag[field]); len(list) > 0 {
			spec.Set(field, list)
		}
	}
	if v, ok := filters["early_exit"]; ok && v != nil {
		spec.Set("early_exit", v)
	}
	if v, ok := filters["aggregation_group_type_index"]; ok && v != nil {
		spec.Set("aggregation_group_type_index", v)
	}
	if flag["ensure_experience_continuity"] == true {
		spec.Set("ensure_experience_continuity", true)
	}
	for _, field := range []string{"evaluation_runtime", "bucketing_identifier"} {
		if v, ok := flag[field].(string); ok && v != "" {
			spec.Set(field, v)
		}
	}
	if flagType != TypeRemoteConfig {
		spec.Set("conditions", ExportConditions(asSlice(filters["groups"])))
	}
	if flagType == TypeMultivariate {
		spec.Set("variants", exportVariants(asSlice(asMap(filters["multivariate"])["variants"])))
	}

	payloads := asMap(filters["payloads"])
	if flag["has_encrypted_payloads"] == true && len(payloads) > 0 {
		spec.Set("incomplete", []any{"encrypted payloads omitted"})
		return spec, nil
	}
	decoded := map[string]any{}
	for _, key := range sortedKeys(payloads) {
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
		if value, ok := decoded["true"]; ok {
			spec.Set("payload", value)
		}
	}
	return spec, nil
}

// ExportConditions renders condition groups in spec form.
func ExportConditions(groups []any) []any {
	out := make([]any, 0, len(groups))
	for _, item := range groups {
		group := asMap(item)
		condition := NewOrdered()
		if props := asSlice(group["properties"]); len(props) > 0 {
			where := make([]any, 0, len(props))
			for _, p := range props {
				prop := asMap(p)
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
		for _, key := range sortedKeys(group) {
			switch key {
			case "properties", "rollout_percentage", "variant":
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
		variant := asMap(item)
		entry := NewOrdered()
		entry.Set("key", variant["key"])
		entry.Set("rollout", variant["rollout_percentage"])
		if name, ok := variant["name"]; ok {
			entry.Set("name", name)
		}
		for _, key := range sortedKeys(variant) {
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

// DecodePayload turns a stored payload (a JSON-encoded string) into its value.
func DecodePayload(stored any) (any, error) {
	s, ok := stored.(string)
	if !ok {
		return stored, nil
	}
	value, err := Decode([]byte(s))
	if err != nil {
		return nil, fmt.Errorf("stored payload is not valid JSON")
	}
	return value, nil
}

// EncodePayload JSON-encodes a payload value for filters.payloads.
func EncodePayload(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// TypeOf classifies a flag as boolean, multivariate, or remote_config.
func TypeOf(flag map[string]any) string {
	if flag["is_remote_configuration"] == true {
		return TypeRemoteConfig
	}
	if len(asSlice(asMap(asMap(flag["filters"])["multivariate"])["variants"])) > 0 {
		return TypeMultivariate
	}
	return TypeBoolean
}

// RequireV1 refuses filters stored in a config format other than v1, which
// PostHog's classic API will not edit.
func RequireV1(filters map[string]any) error {
	v, ok := filters["version"]
	if !ok || v == nil || Equal(v, number(1)) {
		return nil
	}
	return fmt.Errorf("this flag's filters use config version %v, which agent-posthog cannot edit; change it in the PostHog UI", v)
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
