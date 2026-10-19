package output

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
