package flagspec

import (
	"bytes"
	"encoding/json"

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
		k, _ := marshalUnescaped(key)
		buf.Write(k)
		buf.WriteByte(':')
		v, err := marshalUnescaped(o.values[key])
		if err != nil {
			return nil, err
		}
		buf.Write(v)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// marshalUnescaped is json.Marshal without HTML escaping, the CLI family's
// convention, so URLs in payloads print as written.
func marshalUnescaped(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
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
