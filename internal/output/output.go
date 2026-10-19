// Package output is agent-posthog's output policy on top of lib-agent-output,
// which owns the wire mechanism (format parsing, JSON/YAML encoding, list
// shapes, error rendering). What stays here is PostHog-specific: which fields
// a compact record keeps, the always-on secret redaction, the PostHog-shaped
// pagination trailer, and the writer indirection tests use.
package output

import (
	"encoding/json"
	"io"
	"os"
	"sync"

	out "github.com/shhac/lib-agent-output"

	"github.com/shhac/agent-posthog/internal/secret"

	// YAML support (and its yaml.v3 dependency) comes from the shared encoder in
	// lib-agent-cli; the blank import registers it for out.FormatYAML, keeping the
	// core lib-agent-output module dependency-free.
	_ "github.com/shhac/lib-agent-cli/yaml"
)

// Format and its values come from the shared contract.
type Format = out.Format

const (
	FormatJSON   = out.FormatJSON
	FormatYAML   = out.FormatYAML
	FormatNDJSON = out.FormatNDJSON
)

// MetaKeyQuery carries a query's columns and ID ahead of its rows.
const MetaKeyQuery = "@query"

var (
	ResolveFormat = out.ResolveFormat
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

// Shape is how much of a PostHog record a command prints.
type Shape int

const (
	// Full prints every field.
	Full Shape = iota
	// Compact keeps the fields agents usually need (see compactKeep).
	Compact
)

// cleaner is the policy every cleaned write applies: shape the record, drop
// nulls, then mask secrets. Nulls go before masking so a null secret leaves
// no @redacted note. It reads --expose when called, after flags are parsed.
func cleaner(shape Shape) out.Pruner {
	var compact out.Pruner
	if shape == Compact {
		compact = compactValue
	}
	return out.Chain(compact, out.PruneNils, out.Redactor(postHogSecrets(), redactExpose))
}

// Print writes a whole record, cleaned.
func Print(data any, format Format) error {
	return out.Print(Stdout(), data, format, cleaner(Full))
}

// PrintRaw is Print for a response body; a body that is not JSON is written
// as it came.
func PrintRaw(raw json.RawMessage, format Format, shape Shape) error {
	var data any
	if err := json.Unmarshal(raw, &data); err != nil {
		_, err := Stdout().Write(append(raw, '\n'))
		return err
	}
	return out.Print(Stdout(), data, format, cleaner(shape))
}

// PrintExact encodes data with neither pruning nor redaction, for output whose
// nulls and values are the point (flag write receipts, exported specs).
func PrintExact(data any, format Format) error {
	return out.Print(Stdout(), data, format, nil)
}

// WriteExactLines writes records as NDJSON lines, then one line per meta key,
// without cleaning, for records whose nulls and values are the point.
func WriteExactLines(items []any, meta map[string]any) error {
	return out.WriteList(Stdout(), FormatNDJSON, items, meta, nil)
}

// Clean prunes nils and redacts secrets from a decoded value, for records
// handed to a writer that does not clean (such as the shared multi-get
// printer).
func Clean(data any) any {
	return cleaner(Full)(data)
}

// WriteList renders a list in the family's two shapes (NDJSON records then
// @-prefixed meta lines, or one {"data":[...], <meta>} document), cleaning
// each record to the given shape.
func WriteList(format Format, items []any, meta map[string]any, shape Shape) error {
	return out.WriteList(Stdout(), format, items, meta, cleaner(shape))
}

// PaginationMeta is the meta map for a page that has more after it, keyed
// @pagination in every format.
func PaginationMeta(nextURL string) map[string]any {
	if nextURL == "" {
		return nil
	}
	return map[string]any{out.MetaKeyPagination: pagination{HasMore: true, NextURL: nextURL}}
}

// pagination is PostHog-shaped (a next URL, not an opaque cursor), so it stays
// local rather than using out.pagination.
type pagination struct {
	HasMore bool   `json:"has_more"`
	NextURL string `json:"next_url,omitempty"`
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
	return n.inner.WriteItem(Clean(item))
}

func (n *NDJSONWriter) WriteMetaLine(key string, value any) error {
	return n.inner.WriteMetaLine(key, Clean(value))
}

// redactExpose is the --expose allowlist, set once from the global flag.
var redactExpose []string

// SetExpose records the --expose allowlist used by redaction (the global
// --expose flag, wired from the root command's defaults hook).
func SetExpose(expose []string) { redactExpose = expose }

// postHogSecrets is agent-posthog's redaction POLICY: mask known secret-named
// fields and any string value matching a PostHog key prefix. The shared
// out.Redact owns the MECHANISM — the walk, the [REDACTED] placeholder, the
// @redacted notes, and --expose handling.
func postHogSecrets() out.RedactRule {
	return func(field out.RedactField) bool {
		if secret.IsField(field.Key) {
			return true
		}
		s, ok := field.Value.(string)
		return ok && secret.IsKey(s)
	}
}
