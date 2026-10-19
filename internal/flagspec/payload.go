package flagspec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// BooleanPayloadKey is the filters.payloads key of a boolean or remote_config
// flag's single payload.
const BooleanPayloadKey = "true"

// PrunePayloads drops payloads no longer reachable on a flag of flagType with
// the given variants, reporting whether anything was dropped.
func PrunePayloads(stored map[string]any, flagType string, variantKeys map[string]bool) (map[string]any, bool) {
	kept := map[string]any{}
	for key, value := range stored {
		if flagType == TypeMultivariate && variantKeys[key] || flagType != TypeMultivariate && key == BooleanPayloadKey {
			kept[key] = value
		}
	}
	return kept, len(kept) != len(stored)
}

// KeepOrEncode reuses the stored payload string when it already holds value,
// so untouched payloads are not re-encoded.
func KeepOrEncode(stored, value any) (any, error) {
	if stored != nil {
		if decoded, err := DecodePayload(stored); err == nil && Equal(decoded, value) {
			return stored, nil
		}
	}
	return encodePayload(value)
}

// DecodePayload turns a stored payload (a JSON-encoded string) into its value.
func DecodePayload(stored any) (any, error) {
	s, ok := stored.(string)
	if !ok {
		return stored, nil
	}
	value, err := Decode([]byte(s))
	if err != nil {
		return nil, fmt.Errorf("stored payload is not valid JSON")
	}
	return value, nil
}

// encodePayload JSON-encodes a payload value for filters.payloads, leaving
// characters such as < and & as they are, as PostHog stores them.
func encodePayload(value any) (string, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}
