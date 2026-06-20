package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"

	agenterrors "github.com/shhac/agent-posthog/internal/errors"
)

func registerAPI(root *cobra.Command, globals *GlobalFlags) {
	apiCmd := &cobra.Command{
		Use:   "api",
		Short: "Constrained raw PostHog API escape hatch",
	}
	apiCmd.AddCommand(rawAPICommand(globals, http.MethodGet))
	apiCmd.AddCommand(rawAPICommand(globals, http.MethodPost))
	root.AddCommand(apiCmd)
}

func rawAPICommand(globals *GlobalFlags, method string) *cobra.Command {
	var queryPairs []string
	var bodyFile string
	var yes, printRequest bool
	cmd := &cobra.Command{
		Use:   strings.ToLower(method) + " <api-path>",
		Short: method + " a raw /api/ path",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			if !strings.HasPrefix(path, "/api/") {
				return agenterrors.New("raw API paths must start with /api/", agenterrors.FixableByAgent)
			}
			if method == http.MethodPost && !strings.Contains(path, "/query/") && !yes {
				return agenterrors.New("raw POST outside query endpoints requires --yes", agenterrors.FixableByHuman).
					WithHint("Many PostHog POST endpoints mutate state. Use a typed command when available.")
			}
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				q := queryValuesFromPairs(queryPairs)
				if printRequest {
					return writeItem(map[string]any{
						"method":  method,
						"host":    resolved.Host,
						"path":    path,
						"query":   q,
						"headers": map[string]string{"Authorization": "Bearer phx_..."},
					}, globals.Format)
				}
				var raw json.RawMessage
				var err error
				if method == http.MethodGet {
					raw, err = resolved.Client.Get(ctx, path, q)
				} else {
					body := map[string]any{}
					if bodyFile != "" {
						data, readErr := os.ReadFile(bodyFile)
						if readErr != nil {
							return readErr
						}
						if err := json.Unmarshal(data, &body); err != nil {
							return err
						}
					}
					raw, err = resolved.Client.Post(ctx, path, q, body)
				}
				if err != nil {
					return err
				}
				return writeRaw(raw, globals.Format)
			})
		},
	}
	cmd.Flags().StringArrayVar(&queryPairs, "query", nil, "Query parameter as key=value; repeatable")
	cmd.Flags().BoolVar(&printRequest, "print-request", false, "Print a redacted request preview without sending")
	if method == http.MethodPost {
		cmd.Flags().StringVar(&bodyFile, "body", "", "JSON body file")
		cmd.Flags().BoolVar(&yes, "yes", false, "Allow raw POST outside query endpoints")
	}
	return cmd
}
