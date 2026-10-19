package shared

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	agenterrors "github.com/shhac/lib-agent-output"
)

// ReadInput returns inline JSON when given, else the file's contents ("-" is
// stdin). MCP callers have no stdin and a different filesystem, so inline is
// first-class.
func ReadInput(inline, file, inlineFlag, fileFlag string) ([]byte, error) {
	switch {
	case inline != "" && file != "":
		return nil, agenterrors.New(fmt.Sprintf("use --%s or --%s, not both", inlineFlag, fileFlag), agenterrors.FixableByAgent)
	case inline != "":
		return []byte(inline), nil
	case file == "-":
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, agenterrors.Wrap(err, agenterrors.FixableByAgent)
		}
		return data, nil
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

// ReadJSONObject reads a JSON object from inline text or a file.
func ReadJSONObject(inline, file, inlineFlag, fileFlag string) (map[string]any, error) {
	data, err := ReadInput(inline, file, inlineFlag, fileFlag)
	if err != nil {
		return nil, err
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, agenterrors.New(fmt.Sprintf("--%s/--%s must hold a JSON object: %v", inlineFlag, fileFlag, err), agenterrors.FixableByAgent)
	}
	return body, nil
}
