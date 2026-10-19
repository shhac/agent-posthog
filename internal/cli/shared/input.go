package shared

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"
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

// Input is a pair of flags that give the same content inline or from a file.
type Input struct {
	inline, file, fileDefault string
	inlineName, fileName      string
}

// AddInput registers --inlineName and --fileName on cmd. A file flag with a
// default (such as "-" for stdin) gives way to the inline flag.
func AddInput(cmd *cobra.Command, inlineName, inlineUsage, fileName, fileUsage, fileDefault string) *Input {
	in := &Input{fileDefault: fileDefault, inlineName: inlineName, fileName: fileName}
	cmd.Flags().StringVar(&in.inline, inlineName, "", inlineUsage)
	cmd.Flags().StringVar(&in.file, fileName, fileDefault, fileUsage)
	return in
}

// Given reports whether either flag holds content.
func (in *Input) Given() bool { return in.inline != "" || in.file != "" }

// Read returns the content.
func (in *Input) Read() ([]byte, error) {
	file := in.file
	if in.inline != "" && file == in.fileDefault {
		file = ""
	}
	return ReadInput(in.inline, file, in.inlineName, in.fileName)
}

// ReadObject returns the content as a JSON object.
func (in *Input) ReadObject() (map[string]any, error) {
	data, err := in.Read()
	if err != nil {
		return nil, err
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, agenterrors.New(fmt.Sprintf("--%s/--%s must hold a JSON object: %v", in.inlineName, in.fileName, err), agenterrors.FixableByAgent)
	}
	return body, nil
}
