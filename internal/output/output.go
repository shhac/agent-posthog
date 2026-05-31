package output

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"

	agenterrors "github.com/shhac/agent-posthog/internal/errors"
	"gopkg.in/yaml.v3"
)

var (
	writersMu sync.RWMutex
	stdout    io.Writer = os.Stdout
	stderr    io.Writer = os.Stderr
)

type Format string

const (
	FormatJSON   Format = "json"
	FormatYAML   Format = "yaml"
	FormatNDJSON Format = "jsonl"
)

const (
	MetaKeyPagination = "@pagination"
	MetaKeyQuery      = "@query"
	MetaKeyCounts     = "@counts"
	MetaKeySkipped    = "@skipped"
)

func Stdout() io.Writer {
	writersMu.RLock()
	defer writersMu.RUnlock()
	return stdout
}

func Stderr() io.Writer {
	writersMu.RLock()
	defer writersMu.RUnlock()
	return stderr
}

func SetWritersForTest(out, err io.Writer) func() {
	writersMu.Lock()
	previousOut := stdout
	previousErr := stderr
	if out != nil {
		stdout = out
	}
	if err != nil {
		stderr = err
	}
	writersMu.Unlock()
	return func() {
		writersMu.Lock()
		stdout = previousOut
		stderr = previousErr
		writersMu.Unlock()
	}
}

func ParseFormat(s string) (Format, error) {
	switch s {
	case "json":
		return FormatJSON, nil
	case "yaml":
		return FormatYAML, nil
	case "jsonl", "ndjson":
		return FormatNDJSON, nil
	default:
		return "", agenterrors.Newf(agenterrors.FixableByAgent, "unknown format %q, expected: json, yaml, jsonl", s)
	}
}

func ResolveFormat(flagFormat string, defaultFormat Format) (Format, error) {
	if flagFormat == "" {
		return defaultFormat, nil
	}
	return ParseFormat(flagFormat)
}

func Print(data any, format Format, prune bool) {
	switch format {
	case FormatYAML:
		printYAML(data, prune)
	default:
		printJSON(data, prune)
	}
}

func WriteRawJSON(raw json.RawMessage, format Format, prune bool) {
	var data any
	if err := json.Unmarshal(raw, &data); err != nil {
		printJSON(raw, false)
		return
	}
	Print(data, format, prune)
}

func WriteError(w io.Writer, err error) {
	var aerr *agenterrors.APIError
	if !agenterrors.As(err, &aerr) {
		aerr = agenterrors.Wrap(err, agenterrors.FixableByAgent)
	}
	payload := map[string]any{
		"error":      aerr.Message,
		"fixable_by": string(aerr.FixableBy),
	}
	if aerr.Hint != "" {
		payload["hint"] = aerr.Hint
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(payload)
}

type NDJSONWriter struct {
	enc *json.Encoder
}

type Pagination struct {
	HasMore bool   `json:"has_more"`
	NextURL string `json:"next_url,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

func NewNDJSONWriter(w io.Writer) *NDJSONWriter {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return &NDJSONWriter{enc: enc}
}

func (n *NDJSONWriter) WriteItem(item any) error {
	cleaned, ok := toCleanAny(item, true)
	if !ok {
		return nil
	}
	return n.enc.Encode(cleaned)
}

func (n *NDJSONWriter) WriteMetaLine(key string, value any) error {
	cleaned, ok := toCleanAny(value, true)
	if !ok {
		return nil
	}
	return n.enc.Encode(map[string]any{key: cleaned})
}

func (n *NDJSONWriter) WritePagination(p *Pagination) error {
	return n.WriteMetaLine(MetaKeyPagination, p)
}

func toCleanAny(data any, prune bool) (any, bool) {
	b, err := json.Marshal(data)
	if err != nil {
		return nil, false
	}
	var decoded any
	if err := json.Unmarshal(b, &decoded); err != nil {
		return nil, false
	}
	if prune {
		decoded = pruneNulls(decoded)
	}
	decoded = redactSensitive(decoded)
	return decoded, true
}

func printJSON(data any, prune bool) {
	cleaned, ok := toCleanAny(data, prune)
	if !ok {
		return
	}
	enc := json.NewEncoder(Stdout())
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	_ = enc.Encode(cleaned)
}

func printYAML(data any, prune bool) {
	cleaned, ok := toCleanAny(data, prune)
	if !ok {
		return
	}
	enc := yaml.NewEncoder(Stdout())
	enc.SetIndent(2)
	_ = enc.Encode(cleaned)
}

func pruneNulls(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, v := range val {
			if v == nil {
				continue
			}
			out[k] = pruneNulls(v)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, v := range val {
			out[i] = pruneNulls(v)
		}
		return out
	default:
		return v
	}
}

func redactSensitive(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for key, value := range val {
			if isSensitiveKey(key) {
				out[key] = "REDACTED"
				continue
			}
			out[key] = redactSensitive(value)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, v := range val {
			out[i] = redactSensitive(v)
		}
		return out
	case string:
		if looksLikePostHogSecret(val) {
			return "REDACTED"
		}
		return val
	default:
		return v
	}
}

func isSensitiveKey(key string) bool {
	switch key {
	case "access_token", "api_key", "personal_api_key", "project_token", "secret", "token":
		return true
	default:
		return false
	}
}

func looksLikePostHogSecret(value string) bool {
	return strings.HasPrefix(value, "phx_") ||
		strings.HasPrefix(value, "phc_") ||
		strings.HasPrefix(value, "phs_") ||
		strings.HasPrefix(value, "pha_") ||
		strings.HasPrefix(value, "phr_")
}
