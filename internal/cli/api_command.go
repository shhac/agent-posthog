package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/api"
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
	var bodyFile, inline string
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
			if method == http.MethodPost && !isQueryPath(path) && !yes {
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
				raw, err := sendRaw(ctx, resolved.Client, method, path, q, inline, bodyFile)
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
		cmd.Flags().StringVar(&inline, "json", "", "JSON body inline (for callers without a shared filesystem, such as MCP)")
		cmd.Flags().BoolVar(&yes, "yes", false, "Allow raw POST outside query endpoints")
	}
	return cmd
}

// sendRaw sends a raw request. Query POSTs are safe to repeat; any other POST
// may change state, so it goes through Mutate (never retried, and refused
// through MCP unless writes are on).
func sendRaw(ctx context.Context, client *api.Client, method, path string, q url.Values, inline, bodyFile string) (json.RawMessage, error) {
	if method == http.MethodGet {
		return client.Get(ctx, path, q)
	}
	body := map[string]any{}
	if inline != "" || bodyFile != "" {
		parsed, err := readJSONObject(inline, bodyFile, "json", "body")
		if err != nil {
			return nil, err
		}
		body = parsed
	}
	if isQueryPath(path) {
		return client.Post(ctx, path, q, body)
	}
	return client.Mutate(ctx, http.MethodPost, path, q, body)
}

func isQueryPath(path string) bool {
	return strings.Contains(path, "/query/")
}

func queryValuesFromPairs(pairs []string) url.Values {
	q := url.Values{}
	for _, pair := range pairs {
		key, value, found := strings.Cut(pair, "=")
		if found {
			q.Add(key, value)
		}
	}
	return q
}
