// Package output re-exports the shared output contract from lib-agent-output,
// keeping the internal/output import path while the wire mechanism (format
// parsing, JSON/YAML encoding, error rendering) lives in one place. What stays
// local is agent-posthog policy: the writer indirection used by tests, the
// always-on secret redaction, and the PostHog-shaped pagination trailer.
// (Migration shim.)
package output

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"

	out "github.com/shhac/lib-agent-output"

	// YAML support (and its yaml.v3 dependency) comes from the shared encoder in
	// lib-agent-cli; the blank import registers it for out.FormatYAML, keeping the
	// core lib-agent-output module dependency-free.
	_ "github.com/shhac/lib-agent-cli/yaml"
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
// redact) on every record, delegating the encode to the shared writer so NDJSON
// output picks up the family's per-stream color.
type NDJSONWriter struct {
	inner *out.NDJSONWriter
}

func NewNDJSONWriter(w io.Writer) *NDJSONWriter {
	return &NDJSONWriter{inner: out.NewNDJSONWriter(w)}
}

func (n *NDJSONWriter) WriteItem(item any) error {
	cleaned, ok := toCleanAny(item, true)
	if !ok {
		return nil
	}
	return n.inner.WriteItem(cleaned)
}

func (n *NDJSONWriter) WriteMetaLine(key string, value any) error {
	cleaned, ok := toCleanAny(value, true)
	if !ok {
		return nil
	}
	return n.inner.WriteMetaLine(key, cleaned)
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
		decoded = out.PruneNils(decoded)
	}
	decoded = redactSensitive(decoded)
	return decoded, true
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
