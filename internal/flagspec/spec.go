package flagspec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// Flag types a spec can declare. PostHog has no type field: the CLI maps these
// onto is_remote_configuration and filters.multivariate.
const (
	TypeBoolean      = "boolean"
	TypeMultivariate = "multivariate"
	TypeRemoteConfig = "remote_config"
)

// SchemaVersion is the spec format version, unrelated to the flag row's
// version and to filters.version.
const SchemaVersion = 1

var specFields = map[string]bool{
	"schema_version": true, "key": true, "name": true, "type": true, "active": true,
	"tags": true, "evaluation_contexts": true, "early_exit": true,
	"aggregation_group_type_index": true, "ensure_experience_continuity": true,
	"evaluation_runtime": true, "bucketing_identifier": true,
	"conditions": true, "variants": true, "payload": true, "payloads": true, "incomplete": true,
}

// Spec is a validated flag spec. Every field is presence-aware: a field that is
// absent leaves the flag's current value alone on update.
type Spec struct {
	fields map[string]any
}

// ParseDocument reads a JSON or strict-YAML document with lossless numbers.
func ParseDocument(data []byte) (any, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("document is empty")
	}
	if trimmed[0] == '{' || trimmed[0] == '[' {
		return Decode(trimmed)
	}
	return yamlDocument(trimmed)
}

// ParseSpec reads a spec from JSON or YAML.
func ParseSpec(data []byte) (*Spec, error) {
	doc, err := ParseDocument(data)
	if err != nil {
		return nil, fmt.Errorf("spec: %w", err)
	}
	fields, ok := doc.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("spec: expected an object")
	}
	spec := &Spec{fields: fields}
	if err := spec.validate(); err != nil {
		return nil, fmt.Errorf("spec: %w", err)
	}
	return spec, nil
}

func (s *Spec) Has(field string) bool {
	_, ok := s.fields[field]
	return ok
}

func (s *Spec) Get(field string) any { return s.fields[field] }

func (s *Spec) Key() string {
	key, _ := s.fields["key"].(string)
	return key
}

// Type reports the declared type, if any.
func (s *Spec) Type() (string, bool) {
	t, ok := s.fields["type"].(string)
	return t, ok
}

func (s *Spec) validate() error {
	var unknown []string
	for field := range s.fields {
		if !specFields[field] {
			unknown = append(unknown, field)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fmt.Errorf("unknown field(s) %s", strings.Join(unknown, ", "))
	}
	if incomplete := asSlice(s.fields["incomplete"]); len(incomplete) > 0 {
		return fmt.Errorf("this spec is marked incomplete (%v) and cannot be applied", incomplete)
	}
	if v, ok := s.fields["schema_version"]; ok && !Equal(v, number(SchemaVersion)) {
		return fmt.Errorf("unsupported schema_version %v (supported: %d)", v, SchemaVersion)
	}
	if s.Key() == "" {
		return fmt.Errorf("key is required")
	}
	if t, ok := s.fields["type"]; ok {
		if t != TypeBoolean && t != TypeMultivariate && t != TypeRemoteConfig {
			return fmt.Errorf("type must be boolean, multivariate, or remote_config")
		}
	}
	for _, field := range []string{"active", "early_exit", "ensure_experience_continuity"} {
		if err := expectType[bool](s.fields, field); err != nil {
			return err
		}
	}
	for _, field := range []string{"name", "evaluation_runtime", "bucketing_identifier"} {
		if err := expectType[string](s.fields, field); err != nil {
			return err
		}
	}
	for _, field := range []string{"tags", "evaluation_contexts"} {
		if err := expectStringList(s.fields, field); err != nil {
			return err
		}
	}
	if v, ok := s.fields["aggregation_group_type_index"]; ok && v != nil && !isInteger(v) {
		return fmt.Errorf("aggregation_group_type_index must be an integer or null")
	}
	if s.Has("payload") && s.Has("payloads") {
		return fmt.Errorf("use payload (boolean, remote_config) or payloads (multivariate), not both")
	}
	if v, ok := s.fields["payloads"]; ok && asMap(v) == nil {
		return fmt.Errorf("payloads must be an object keyed by variant")
	}
	if s.Has("conditions") {
		if err := validateConditions(s.fields["conditions"]); err != nil {
			return err
		}
	}
	if s.Has("variants") {
		return ValidateVariants(s.fields["variants"])
	}
	return nil
}

func validateConditions(v any) error {
	conditions, ok := v.([]any)
	if !ok {
		return fmt.Errorf("conditions must be a list")
	}
	for i, item := range conditions {
		condition := asMap(item)
		if condition == nil {
			return fmt.Errorf("conditions[%d] must be an object", i)
		}
		if where, ok := condition["where"]; ok {
			list, ok := where.([]any)
			if !ok {
				return fmt.Errorf("conditions[%d].where must be a list", i)
			}
			for j, filter := range list {
				switch f := filter.(type) {
				case string:
					if _, err := ParseWhere(f); err != nil {
						return fmt.Errorf("conditions[%d].where[%d]: %w", i, j, err)
					}
				case map[string]any:
				default:
					return fmt.Errorf("conditions[%d].where[%d] must be a string or property object", i, j)
				}
			}
		}
		if rollout, ok := condition["rollout"]; ok && rollout != nil && !isPercentage(rollout) {
			return fmt.Errorf("conditions[%d].rollout must be a number from 0 to 100, or null", i)
		}
		if variant, ok := condition["variant"]; ok && variant != nil {
			if _, ok := variant.(string); !ok {
				return fmt.Errorf("conditions[%d].variant must be a string or null", i)
			}
		}
	}
	return nil
}

// ValidateVariants checks a spec variants list: unique keys, percentages that
// sum to 100.
func ValidateVariants(v any) error {
	variants, ok := v.([]any)
	if !ok || len(variants) == 0 {
		return fmt.Errorf("variants must be a non-empty list")
	}
	seen := map[string]bool{}
	total := 0.0
	for i, item := range variants {
		variant := asMap(item)
		key, _ := variant["key"].(string)
		if key == "" {
			return fmt.Errorf("variants[%d].key is required", i)
		}
		if seen[key] {
			return fmt.Errorf("variant %q is listed twice", key)
		}
		seen[key] = true
		if !isPercentage(variant["rollout"]) {
			return fmt.Errorf("variants[%d].rollout must be a number from 0 to 100", i)
		}
		pct, _ := variant["rollout"].(json.Number).Float64()
		total += pct
		if name, ok := variant["name"]; ok && name != nil {
			if _, ok := name.(string); !ok {
				return fmt.Errorf("variants[%d].name must be a string", i)
			}
		}
	}
	if math.Abs(total-100) > 1e-9 {
		return fmt.Errorf("variant rollouts must sum to 100 (got %v)", total)
	}
	return nil
}

func expectType[T any](fields map[string]any, field string) error {
	v, ok := fields[field]
	if !ok {
		return nil
	}
	if _, ok := v.(T); !ok {
		var zero T
		return fmt.Errorf("%s must be a %T", field, zero)
	}
	return nil
}

func expectStringList(fields map[string]any, field string) error {
	v, ok := fields[field]
	if !ok {
		return nil
	}
	list, ok := v.([]any)
	if !ok {
		return fmt.Errorf("%s must be a list of strings", field)
	}
	for _, item := range list {
		if _, ok := item.(string); !ok {
			return fmt.Errorf("%s must be a list of strings", field)
		}
	}
	return nil
}

func isPercentage(v any) bool {
	n, ok := v.(json.Number)
	if !ok {
		return false
	}
	f, err := n.Float64()
	return err == nil && f >= 0 && f <= 100
}

func isInteger(v any) bool {
	n, ok := v.(json.Number)
	if !ok {
		return false
	}
	_, err := n.Int64()
	return err == nil
}
