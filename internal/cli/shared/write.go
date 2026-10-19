package shared

import (
	"encoding/json"
	"maps"
	"slices"

	"github.com/shhac/agent-posthog/internal/flagspec"
	"github.com/shhac/agent-posthog/internal/output"
)

// WriteItem writes one record, cleaned but whole; JSON by default.
func WriteItem(data any, flagFormat string) error {
	format, err := output.ResolveFormat(flagFormat, output.FormatJSON)
	if err != nil {
		return err
	}
	return output.Print(data, format, output.Full)
}

// WriteRaw writes a response body cleaned to shape; JSON by default.
func WriteRaw(raw json.RawMessage, flagFormat string, shape output.Shape) error {
	format, err := output.ResolveFormat(flagFormat, output.FormatJSON)
	if err != nil {
		return err
	}
	return output.PrintRaw(raw, format, shape)
}

// WriteList writes records as a list, NDJSON by default, with the
// @pagination trailer when nextURL names more.
func WriteList(items []any, nextURL, flagFormat string, shape output.Shape) error {
	format, err := output.ResolveFormat(flagFormat, output.FormatNDJSON)
	if err != nil {
		return err
	}
	return output.WriteList(format, items, output.PaginationMeta(nextURL), shape)
}

// WriteRawList writes a page of response records as a list.
func WriteRawList(items []json.RawMessage, nextURL, flagFormat string, shape output.Shape) error {
	decoded := make([]any, 0, len(items))
	for _, raw := range items {
		var item any
		if err := json.Unmarshal(raw, &item); err != nil {
			return err
		}
		decoded = append(decoded, item)
	}
	return WriteList(decoded, nextURL, flagFormat, shape)
}

// ShapeOf is the shape --full asks for.
func ShapeOf(globals *GlobalFlags) output.Shape {
	if globals.Full {
		return output.Full
	}
	return output.Compact
}

// WriteExact encodes v without the pruning and redaction the general output
// path applies, for receipts and specs that must carry nulls and payload
// contents as they are. YAML goes through flagspec so numbers stay exact.
func WriteExact(v any, flagFormat string) error {
	format, err := output.ResolveFormat(flagFormat, output.FormatJSON)
	if err != nil {
		return err
	}
	if format != output.FormatYAML {
		return output.PrintExact(v, format)
	}
	data, err := flagspec.MarshalYAML(v)
	if err != nil {
		return err
	}
	_, err = output.Stdout().Write(data)
	return err
}

// WriteExactLine writes v as one exact NDJSON line.
func WriteExactLine(v any) error {
	return output.PrintExact(v, output.FormatNDJSON)
}

// WriteExactList is WriteExact for a list: NDJSON records then one line per
// meta key, or one {"data":[...], <meta>} document under --format json|yaml.
func WriteExactList(items []any, meta map[string]any, flagFormat string) error {
	format, err := output.ResolveFormat(flagFormat, output.FormatNDJSON)
	if err != nil {
		return err
	}
	keys := slices.Sorted(maps.Keys(meta))
	if format != output.FormatNDJSON {
		envelope := flagspec.NewOrdered()
		envelope.Set("data", items)
		for _, key := range keys {
			envelope.Set(key, meta[key])
		}
		return WriteExact(envelope, flagFormat)
	}
	for _, item := range items {
		if err := WriteExactLine(item); err != nil {
			return err
		}
	}
	for _, key := range keys {
		if err := WriteExactLine(map[string]any{key: meta[key]}); err != nil {
			return err
		}
	}
	return nil
}
