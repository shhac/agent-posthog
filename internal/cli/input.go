package cli

import (
	"fmt"
	"os"

	agenterrors "github.com/shhac/lib-agent-output"
)

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
