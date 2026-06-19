// Package output re-exports the shared output contract from lib-agent-output,
// keeping the internal/output import path while the wire mechanism (format
// parsing, JSON/YAML encoding, error rendering) lives in one place. What stays
// local is agent-posthog policy: the writer indirection used by tests, the
// always-on secret redaction, and the PostHog-shaped pagination trailer.
// (Migration shim.)
package output

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"

	out "github.com/shhac/lib-agent-output"
	"gopkg.in/yaml.v3"
)

// Format and its values come from the shared contract; ParseFormat is therefore
// the family's lenient parser (accepts "ndjson"/"yml", case-insensitive).
type Format = out.Format

const (
	FormatJSON   = out.FormatJSON
	FormatYAML   = out.FormatYAML
	FormatNDJSON = out.FormatNDJSON
)

const (
	MetaKeyPagination = "@pagination"
	MetaKeyQuery      = "@query"
	MetaKeyCounts     = "@counts"
	MetaKeySkipped    = "@skipped"
)

var (
	ParseFormat   = out.ParseFormat
	ResolveFormat = out.ResolveFormat
	WriteError    = out.WriteError
)

// init registers agent-posthog's YAML encoder with lib-agent-output, so YAML
// support (and its yaml.v3 dependency) stays in this CLI while the core library
// remains dependency-free.
func init() {
	out.RegisterEncoder(out.FormatYAML, func(v any) ([]byte, error) {
		var buf bytes.Buffer
		enc := yaml.NewEncoder(&buf)
		enc.SetIndent(2)
		if err := enc.Encode(v); err != nil {
			return nil, err
		}
		_ = enc.Close()
		return buf.Bytes(), nil
	})
}

var (
	writersMu sync.RWMutex
	stdout    io.Writer = os.Stdout
	stderr    io.Writer = os.Stderr
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

func SetWritersForTest(o, e io.Writer) func() {
	writersMu.Lock()
	previousOut := stdout
	previousErr := stderr
	if o != nil {
		stdout = o
	}
	if e != nil {
		stderr = e
	}
	writersMu.Unlock()
	return func() {
		writersMu.Lock()
		stdout = previousOut
		stderr = previousErr
		writersMu.Unlock()
	}
}

// Print cleans (prune + redact) then encodes data in the given format via the
// shared encoder. Redaction is always applied; pruning is opt-in.
func Print(data any, format Format, prune bool) {
	cleaned, ok := toCleanAny(data, prune)
	if !ok {
		return
	}
	// Data is already cleaned, so pass a nil pruner — out.Print just encodes.
	_ = out.Print(Stdout(), cleaned, format, nil)
}

func WriteRawJSON(raw json.RawMessage, format Format, prune bool) {
	var data any
	if err := json.Unmarshal(raw, &data); err != nil {
		_ = out.Print(Stdout(), raw, FormatJSON, nil)
		return
	}
	Print(data, format, prune)
}

// Pagination is PostHog-shaped (a next URL, not an opaque cursor), so it stays
// local rather than using out.Pagination.
type Pagination struct {
	HasMore bool   `json:"has_more"`
	NextURL string `json:"next_url,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

// NDJSONWriter wraps the shared writer with agent-posthog's clean step (prune +
// redact) on every record.
type NDJSONWriter struct {
	w   io.Writer
	enc *json.Encoder
}

func NewNDJSONWriter(w io.Writer) *NDJSONWriter {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return &NDJSONWriter{w: w, enc: enc}
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

func pruneNulls(v any) any {
	switch val := v.(type) {
	case map[string]any:
		o := make(map[string]any, len(val))
		for k, v := range val {
			if v == nil {
				continue
			}
			o[k] = pruneNulls(v)
		}
		return o
	case []any:
		o := make([]any, len(val))
		for i, v := range val {
			o[i] = pruneNulls(v)
		}
		return o
	default:
		return v
	}
}

func redactSensitive(v any) any {
	switch val := v.(type) {
	case map[string]any:
		o := make(map[string]any, len(val))
		for key, value := range val {
			if isSensitiveKey(key) {
				o[key] = "REDACTED"
				continue
			}
			o[key] = redactSensitive(value)
		}
		return o
	case []any:
		o := make([]any, len(val))
		for i, v := range val {
			o[i] = redactSensitive(v)
		}
		return o
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
