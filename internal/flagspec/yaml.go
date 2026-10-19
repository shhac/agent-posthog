package flagspec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

var decimalInt = regexp.MustCompile(`^[-+]?(0|[1-9][0-9]*)$`)

// jsonNumber matches YAML number text that is already a valid JSON number, so
// it can be kept verbatim; anything else (.5, +1.5, 1_000.0) is re-rendered.
var jsonNumber = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][-+]?[0-9]+)?$`)

// yamlDocument decodes a single YAML document strictly: string keys only, no
// duplicate keys, no custom tags. Scalars keep their YAML types, and numbers
// become json.Number so they survive the JSON round trip.
func yamlDocument(data []byte) (any, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err == nil {
		return nil, fmt.Errorf("expected a single YAML document")
	}
	return yamlValue(&doc)
}

func yamlValue(node *yaml.Node) (any, error) {
	switch node.Kind {
	case yaml.DocumentNode:
		if len(node.Content) != 1 {
			return nil, fmt.Errorf("expected a single YAML document")
		}
		return yamlValue(node.Content[0])
	case yaml.MappingNode:
		out := make(map[string]any, len(node.Content)/2)
		for i := 0; i+1 < len(node.Content); i += 2 {
			keyNode := node.Content[i]
			if keyNode.Kind != yaml.ScalarNode || keyNode.Tag != "!!str" {
				return nil, fmt.Errorf("line %d: mapping keys must be strings (quote %q)", keyNode.Line, keyNode.Value)
			}
			if _, dup := out[keyNode.Value]; dup {
				return nil, fmt.Errorf("line %d: duplicate key %q", keyNode.Line, keyNode.Value)
			}
			value, err := yamlValue(node.Content[i+1])
			if err != nil {
				return nil, err
			}
			out[keyNode.Value] = value
		}
		return out, nil
	case yaml.SequenceNode:
		out := make([]any, 0, len(node.Content))
		for _, child := range node.Content {
			value, err := yamlValue(child)
			if err != nil {
				return nil, err
			}
			out = append(out, value)
		}
		return out, nil
	case yaml.ScalarNode:
		return yamlScalar(node)
	case yaml.AliasNode:
		return nil, fmt.Errorf("line %d: YAML aliases are not supported", node.Line)
	default:
		return nil, fmt.Errorf("line %d: unsupported YAML node", node.Line)
	}
}

func yamlScalar(node *yaml.Node) (any, error) {
	switch node.Tag {
	case "!!str", "!!timestamp":
		return node.Value, nil
	case "!!null":
		return nil, nil
	case "!!bool":
		var b bool
		if err := node.Decode(&b); err != nil {
			return nil, err
		}
		return b, nil
	case "!!int":
		if decimalInt.MatchString(node.Value) {
			return json.Number(strings.TrimPrefix(node.Value, "+")), nil
		}
		var i int64
		if err := node.Decode(&i); err != nil {
			return nil, fmt.Errorf("line %d: %w", node.Line, err)
		}
		return json.Number(strconv.FormatInt(i, 10)), nil
	case "!!float":
		var f float64
		if err := node.Decode(&f); err != nil {
			return nil, fmt.Errorf("line %d: %w", node.Line, err)
		}
		if math.IsInf(f, 0) || math.IsNaN(f) {
			return nil, fmt.Errorf("line %d: numbers must be finite", node.Line)
		}
		if jsonNumber.MatchString(node.Value) {
			return json.Number(node.Value), nil
		}
		return json.Number(strconv.FormatFloat(f, 'g', -1, 64)), nil
	default:
		return nil, fmt.Errorf("line %d: unsupported YAML tag %s", node.Line, node.Tag)
	}
}
