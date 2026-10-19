package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"

	agenterrors "github.com/shhac/lib-agent-output"
)

func asObject(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func jsonInt(v any) int {
	switch t := v.(type) {
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	case float64:
		return int(t)
	case int:
		return t
	default:
		return 0
	}
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

// readInput returns inline JSON when given, else the file's contents. MCP
// callers have no stdin and a different filesystem, so inline is first-class.
func readInput(inline, file, inlineFlag, fileFlag string) ([]byte, error) {
	switch {
	case inline != "" && file != "":
		return nil, agenterrors.New(fmt.Sprintf("use --%s or --%s, not both", inlineFlag, fileFlag), agenterrors.FixableByAgent)
	case inline != "":
		return []byte(inline), nil
	case file != "":
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, agenterrors.Wrap(err, agenterrors.FixableByAgent)
		}
		return data, nil
	default:
		return nil, agenterrors.New(fmt.Sprintf("pass --%s or --%s", inlineFlag, fileFlag), agenterrors.FixableByAgent)
	}
}

func appendOnce(list []string, items ...string) []string {
	for _, item := range items {
		if !slices.Contains(list, item) {
			list = append(list, item)
		}
	}
	return list
}

func stringsToAny(values []string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}
