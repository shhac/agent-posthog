package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"

	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/api"
	"github.com/shhac/agent-posthog/internal/cli/shared"
)

func registerAPI(root *cobra.Command, globals *shared.GlobalFlags) {
	apiCmd := &cobra.Command{
		Use:   "api",
		Short: "Constrained raw PostHog API escape hatch",
	}
	apiCmd.AddCommand(rawAPICommand(globals, http.MethodGet))
	apiCmd.AddCommand(rawAPICommand(globals, http.MethodPost))
	root.AddCommand(apiCmd)
}

func rawAPICommand(globals *shared.GlobalFlags, method string) *cobra.Command {
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
			if hasDotSegment(path) {
				return agenterrors.New("raw API paths must not contain . or .. segments", agenterrors.FixableByAgent)
			}
			if method == http.MethodPost && !isQueryPath(path) && !yes {
				return agenterrors.New("raw POST outside query endpoints requires --yes", agenterrors.FixableByHuman).
					WithHint("Many PostHog POST endpoints mutate state. Use a typed command when available.")
			}
			return shared.WithClient(cmd.Context(), globals, func(ctx context.Context, resolved *shared.Resolved) error {
				q := queryValuesFromPairs(queryPairs)
				if printRequest {
					return shared.WriteItem(map[string]any{
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
				return shared.WriteRaw(raw, globals.Format)
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
		parsed, err := shared.ReadJSONObject(inline, bodyFile, "json", "body")
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

// queryPath matches the query endpoints a raw POST may reach without --yes:
// running a query in an environment or project.
var queryPath = regexp.MustCompile(`^/api/(environments|projects)/[^/]+/query/`)

func isQueryPath(path string) bool {
	return queryPath.MatchString(path) && !hasDotSegment(path)
}

// hasDotSegment reports a path a proxy might resolve to a different
// endpoint than the one checked here.
func hasDotSegment(path string) bool {
	return slices.ContainsFunc(strings.Split(path, "/"), func(segment string) bool {
		return segment == "." || segment == ".."
	})
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
