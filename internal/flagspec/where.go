package flagspec

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

// Property types the --where grammar can produce.
const (
	PropertyPerson = "person"
	PropertyCohort = "cohort"
	PropertyFlag   = "flag"
)

var symbolicOperators = map[string]string{
	"=":  "exact",
	"!=": "is_not",
	">":  "gt",
	">=": "gte",
	"<":  "lt",
	"<=": "lte",
}

var operatorSymbols = invert(symbolicOperators)

func invert(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[v] = k
	}
	return out
}

var personOperators = map[string]string{
	"exact":          "scalar-or-list",
	"is_not":         "scalar-or-list",
	"icontains":      "string",
	"not_icontains":  "string",
	"regex":          "string",
	"not_regex":      "string",
	"gt":             "number",
	"gte":            "number",
	"lt":             "number",
	"lte":            "number",
	"is_set":         "none",
	"is_not_set":     "none",
	"is_date_before": "string",
	"is_date_after":  "string",
}

// parseWhere parses one condition filter: "<key> <op> <value>".
//
// A bare key or person:<name> targets a person property, the bare key "cohort"
// targets a cohort (in/not_in), and flag:<key-or-id> targets a flag dependency
// whose key is left as written for the caller to resolve to a numeric ID. The
// value is read as JSON when it parses as JSON, otherwise as a bare string.
func parseWhere(expr string) (map[string]any, error) {
	key, rest := cutField(strings.TrimSpace(expr))
	op, rest := cutField(rest)
	value := strings.TrimSpace(rest)
	if key == "" || op == "" {
		return nil, fmt.Errorf("where %q: expected '<key> <operator> <value>'", expr)
	}
	if named, ok := symbolicOperators[op]; ok {
		op = named
	}

	switch {
	case key == "cohort":
		return parseCohort(expr, op, value)
	case strings.HasPrefix(key, "flag:"):
		return parseFlagDependency(expr, strings.TrimPrefix(key, "flag:"), op, value)
	default:
		return parsePerson(expr, strings.TrimPrefix(key, "person:"), op, value)
	}
}

func parsePerson(expr, key, op, value string) (map[string]any, error) {
	if key == "" {
		return nil, fmt.Errorf("where %q: missing property name", expr)
	}
	kind, ok := personOperators[op]
	if !ok {
		return nil, fmt.Errorf("where %q: unknown operator %q for a person property", expr, op)
	}
	prop := map[string]any{"key": key, "type": PropertyPerson, "operator": op}
	if kind == "none" {
		if value != "" {
			return nil, fmt.Errorf("where %q: %s takes no value", expr, op)
		}
		prop["value"] = op
		return prop, nil
	}
	if value == "" {
		return nil, fmt.Errorf("where %q: missing value", expr)
	}
	parsed := parseValue(value)
	switch kind {
	case "number":
		if _, ok := parsed.(json.Number); !ok {
			return nil, fmt.Errorf("where %q: %s needs a number", expr, op)
		}
	case "string":
		parsed = stringValue(parsed)
		if _, ok := parsed.(string); !ok {
			return nil, fmt.Errorf("where %q: %s needs a string", expr, op)
		}
	case "scalar-or-list":
		if list, ok := parsed.([]any); ok {
			if len(list) == 0 {
				return nil, fmt.Errorf("where %q: empty list", expr)
			}
			for _, item := range list {
				if !isScalar(item) {
					return nil, fmt.Errorf("where %q: list items must be strings, numbers, or booleans", expr)
				}
			}
		} else if !isScalar(parsed) {
			return nil, fmt.Errorf("where %q: value must be a string, number, boolean, or JSON list", expr)
		}
	}
	prop["value"] = parsed
	return prop, nil
}

func parseCohort(expr, op, value string) (map[string]any, error) {
	if op != "in" && op != "not_in" {
		return nil, fmt.Errorf("where %q: cohort supports only 'in' and 'not_in'", expr)
	}
	id, ok := parseValue(value).(json.Number)
	if !ok {
		return nil, fmt.Errorf("where %q: cohort needs a numeric cohort ID", expr)
	}
	if _, err := id.Int64(); err != nil {
		return nil, fmt.Errorf("where %q: cohort needs a numeric cohort ID", expr)
	}
	return map[string]any{"key": "id", "type": PropertyCohort, "operator": op, "value": id}, nil
}

func parseFlagDependency(expr, ref, op, value string) (map[string]any, error) {
	if ref == "" {
		return nil, fmt.Errorf("where %q: missing flag key or ID after 'flag:'", expr)
	}
	if op != "exact" {
		return nil, fmt.Errorf("where %q: a flag dependency supports only '='", expr)
	}
	parsed := parseValue(value)
	switch parsed.(type) {
	case bool, string:
	default:
		return nil, fmt.Errorf("where %q: a flag dependency matches true, false, or a variant key", expr)
	}
	if parsed == "" {
		return nil, fmt.Errorf("where %q: missing value", expr)
	}
	return map[string]any{"key": ref, "type": PropertyFlag, "operator": "flag_evaluates_to", "value": parsed}, nil
}

// FormatWhere renders a property as a --where expression, reporting false when
// the expression would not parse back to exactly the same property object.
func FormatWhere(prop map[string]any) (string, bool) {
	expr, ok := formatWhere(prop)
	if !ok {
		return "", false
	}
	back, err := parseWhere(expr)
	if err != nil || !Equal(back, prop) {
		return "", false
	}
	return expr, true
}

func formatWhere(prop map[string]any) (string, bool) {
	if len(prop) != 4 {
		return "", false
	}
	key, _ := prop["key"].(string)
	op, _ := prop["operator"].(string)
	typ, _ := prop["type"].(string)
	if key == "" || op == "" || strings.ContainsFunc(key, unicode.IsSpace) {
		return "", false
	}
	switch typ {
	case PropertyCohort:
		return "cohort " + op + " " + formatValue(prop["value"]), true
	case PropertyFlag:
		return "flag:" + key + " = " + formatValue(prop["value"]), true
	case PropertyPerson:
		if key == "cohort" || strings.HasPrefix(key, "person:") || strings.HasPrefix(key, "flag:") {
			key = "person:" + key
		}
		kind := personOperators[op]
		if symbol, ok := operatorSymbols[op]; ok {
			op = symbol
		}
		if kind == "none" {
			return key + " " + op, true
		}
		return key + " " + op + " " + formatValue(prop["value"]), true
	default:
		return "", false
	}
}

func formatValue(v any) string {
	if s, ok := v.(string); ok && s != "" && strings.TrimSpace(s) == s {
		if _, isJSON := tryJSON(s); !isJSON {
			return s
		}
	}
	data, _ := json.Marshal(v)
	return string(data)
}

func parseValue(value string) any {
	if parsed, ok := tryJSON(value); ok {
		return parsed
	}
	return value
}

func tryJSON(value string) (any, bool) {
	parsed, err := Decode([]byte(value))
	if err != nil {
		return nil, false
	}
	return parsed, true
}

func stringValue(v any) any {
	switch t := v.(type) {
	case json.Number:
		return t.String()
	case bool:
		return fmt.Sprint(t)
	default:
		return v
	}
}

func isScalar(v any) bool {
	switch v.(type) {
	case string, bool, json.Number:
		return true
	default:
		return false
	}
}

func cutField(s string) (string, string) {
	s = strings.TrimLeftFunc(s, unicode.IsSpace)
	idx := strings.IndexFunc(s, unicode.IsSpace)
	if idx < 0 {
		return s, ""
	}
	return s[:idx], s[idx:]
}
