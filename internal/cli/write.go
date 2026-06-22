package cli

import (
	"encoding/json"

	"github.com/shhac/agent-posthog/internal/output"
)

func writeItem(data any, flagFormat string) error {
	format, err := output.ResolveFormat(flagFormat, output.FormatJSON)
	if err != nil {
		return err
	}
	output.Print(data, format, true)
	return nil
}

func writeRaw(raw json.RawMessage, flagFormat string) error {
	return writeRawResource(raw, flagFormat, false)
}

func writeRawResource(raw json.RawMessage, flagFormat string, full bool) error {
	format, err := output.ResolveFormat(flagFormat, output.FormatJSON)
	if err != nil {
		return err
	}
	if !full {
		raw = compactRaw(raw)
	}
	output.WriteRawJSON(raw, format, true)
	return nil
}

func writeListResource(items []json.RawMessage, nextURL string, flagFormat string, full bool) error {
	format, err := output.ResolveFormat(flagFormat, output.FormatNDJSON)
	if err != nil {
		return err
	}
	if !full {
		items = compactRawItems(items)
	}
	if format != output.FormatNDJSON {
		output.Print(listPayload(items, nextURL), format, true)
		return nil
	}
	return writeListNDJSON(items, nextURL)
}

func listPayload(items []json.RawMessage, nextURL string) map[string]any {
	var decoded []any
	for _, raw := range items {
		var item any
		if err := json.Unmarshal(raw, &item); err == nil {
			decoded = append(decoded, item)
		}
	}
	payload := map[string]any{"results": decoded}
	if nextURL != "" {
		payload["next"] = nextURL
	}
	return payload
}

func writeListNDJSON(items []json.RawMessage, nextURL string) error {
	writer := output.NewNDJSONWriter(output.Stdout())
	for _, raw := range items {
		var item any
		if err := json.Unmarshal(raw, &item); err != nil {
			return err
		}
		if err := writer.WriteItem(item); err != nil {
			return err
		}
	}
	if nextURL != "" {
		return writer.WritePagination(&output.Pagination{HasMore: true, NextURL: nextURL})
	}
	return nil
}

func compactRawItems(items []json.RawMessage) []json.RawMessage {
	out := make([]json.RawMessage, 0, len(items))
	for _, item := range items {
		out = append(out, compactRaw(item))
	}
	return out
}

func compactRaw(raw json.RawMessage) json.RawMessage {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return raw
	}
	compacted := compactValue(value)
	data, err := json.Marshal(compacted)
	if err != nil {
		return raw
	}
	return data
}

// compactKeep is the allow-list of object keys retained when compacting API
// responses (the default, non-full output). It is package-level so the literal
// is built once rather than per object on every recursive compactValue call.
var compactKeep = map[string]bool{
	"id": true, "uuid": true, "name": true, "key": true, "type": true, "event": true,
	"active": true, "archived": true, "created_at": true, "updated_at": true,
	"start_time": true, "start_date": true, "end_date": true, "last_seen_at": true,
	"distinct_ids": true, "properties": true, "email": true, "person_id": true,
	"viewed": true, "recording_duration": true, "console_error_count": true,
	"feature_flag_key": true, "rollout_percentage": true, "filters": true,
	"multivariate": true, "variants": true, "results": true, "columns": true,
	"query": true, "query_async": true, "complete": true, "error": true,
	"error_message": true, "query_progress": true, "access_token": true,
	"enabled": true, "password_required": true, "share_passwords": true,
	"detail": true, "activity": true, "scope": true, "item_id": true,
	"runtime_ms": true, "status": true, "metrics": true, "tiles": true,
}

func compactValue(value any) any {
	switch v := value.(type) {
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = compactValue(item)
		}
		return out
	case map[string]any:
		if nested, ok := v["query_status"]; ok {
			return map[string]any{"query_status": compactValue(nested)}
		}
		out := make(map[string]any)
		for key, item := range v {
			if compactKeep[key] {
				out[key] = compactValue(item)
			}
		}
		if len(out) == 0 {
			return v
		}
		return out
	default:
		return v
	}
}
