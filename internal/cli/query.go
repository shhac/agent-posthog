package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	agenterrors "github.com/shhac/agent-posthog/internal/errors"
	"github.com/shhac/agent-posthog/internal/output"
)

func registerQuery(root *cobra.Command, globals *GlobalFlags) {
	query := &cobra.Command{Use: "query", Short: "Run and inspect PostHog queries"}
	query.AddCommand(queryHogQLCommand(globals))
	query.AddCommand(queryJSONCommand(globals))
	query.AddCommand(queryWaitCommand(globals))
	query.AddCommand(getCommand("get <query-id>", "Get async query status", globals, func(ctx *resolvedContext, id string) (string, error) {
		if err := requireEnvironment(ctx); err != nil {
			return "", err
		}
		return fmt.Sprintf("/api/environments/%d/query/%s/", ctx.EnvironmentID, id), nil
	}))
	query.AddCommand(getCommand("log <query-id>", "Get async query log", globals, func(ctx *resolvedContext, id string) (string, error) {
		if err := requireEnvironment(ctx); err != nil {
			return "", err
		}
		return fmt.Sprintf("/api/environments/%d/query/%s/log/", ctx.EnvironmentID, id), nil
	}))
	query.AddCommand(&cobra.Command{
		Use:   "cancel <query-id>",
		Short: "Cancel an async query",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				if err := requireEnvironment(resolved); err != nil {
					return err
				}
				raw, err := resolved.Client.Delete(ctx, fmt.Sprintf("/api/environments/%d/query/%s/", resolved.EnvironmentID, args[0]), nil)
				if err != nil {
					return err
				}
				return writeRaw(raw, globals.Format)
			})
		},
	})
	root.AddCommand(query)
}

func queryHogQLCommand(globals *GlobalFlags) *cobra.Command {
	var file string
	var async bool
	cmd := &cobra.Command{
		Use:   "hogql <sql>",
		Short: "Run a HogQL query",
		Args: func(cmd *cobra.Command, args []string) error {
			if file != "" {
				return nil
			}
			return cobra.ExactArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			sql := ""
			if file != "" {
				data, err := os.ReadFile(file)
				if err != nil {
					output.WriteError(output.Stderr(), err)
					return nil
				}
				sql = string(data)
			} else {
				sql = args[0]
			}
			body := map[string]any{
				"query": map[string]any{
					"kind":  "HogQLQuery",
					"query": sql,
				},
			}
			if async {
				body["async"] = true
			}
			return runQuery(cmd.Context(), globals, body)
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "Read HogQL from file")
	cmd.Flags().BoolVar(&async, "async", false, "Start an async query and return its query status")
	return cmd
}

func queryJSONCommand(globals *GlobalFlags) *cobra.Command {
	var bodyFile string
	cmd := &cobra.Command{
		Use:   "json",
		Short: "Run a raw PostHog query JSON body",
		RunE: func(cmd *cobra.Command, args []string) error {
			var data []byte
			var err error
			if bodyFile == "" || bodyFile == "-" {
				data, err = os.ReadFile("/dev/stdin")
			} else {
				data, err = os.ReadFile(bodyFile)
			}
			if err != nil {
				output.WriteError(output.Stderr(), err)
				return nil
			}
			var body map[string]any
			if err := json.Unmarshal(data, &body); err != nil {
				output.WriteError(output.Stderr(), err)
				return nil
			}
			return runQuery(cmd.Context(), globals, body)
		},
	}
	cmd.Flags().StringVar(&bodyFile, "body", "-", "JSON body file or - for stdin")
	return cmd
}

func runQuery(cmdCtx context.Context, globals *GlobalFlags, body map[string]any) error {
	return withClient(cmdCtx, globals, func(ctx context.Context, resolved *resolvedContext) error {
		if err := requireEnvironment(resolved); err != nil {
			return err
		}
		raw, err := resolved.Client.Post(ctx, fmt.Sprintf("/api/environments/%d/query/", resolved.EnvironmentID), nil, body)
		if err != nil {
			return err
		}
		format, err := output.ResolveFormat(globals.Format, output.FormatNDJSON)
		if err != nil {
			output.WriteError(output.Stderr(), err)
			return nil
		}
		return writeQueryResult(raw, format)
	})
}

func queryWaitCommand(globals *GlobalFlags) *cobra.Command {
	var interval time.Duration
	var maxWait time.Duration
	cmd := &cobra.Command{
		Use:   "wait <query-id>",
		Short: "Poll an async query until it completes or fails",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				if err := requireEnvironment(resolved); err != nil {
					return err
				}
				deadline := time.Now().Add(maxWait)
				var raw json.RawMessage
				for {
					var err error
					raw, err = resolved.Client.Get(ctx, fmt.Sprintf("/api/environments/%d/query/%s/", resolved.EnvironmentID, args[0]), nil)
					if err != nil {
						return err
					}
					status, ok := queryStatus(raw)
					if !ok {
						return writeRaw(raw, globals.Format)
					}
					if status.Error {
						return writeRaw(raw, globals.Format)
					}
					if status.Complete {
						format, err := output.ResolveFormat(globals.Format, output.FormatNDJSON)
						if err != nil {
							output.WriteError(output.Stderr(), err)
							return nil
						}
						return writeQueryResult(raw, format)
					}
					if time.Now().Add(interval).After(deadline) {
						return agenterrors.New("query did not complete before --max-wait", agenterrors.FixableByRetry).
							WithHint("Run 'agent-posthog query get " + args[0] + "' to inspect the current query status.")
					}
					timer := time.NewTimer(interval)
					select {
					case <-ctx.Done():
						timer.Stop()
						return ctx.Err()
					case <-timer.C:
					}
				}
			})
		},
	}
	cmd.Flags().DurationVar(&interval, "interval", 2*time.Second, "Polling interval")
	cmd.Flags().DurationVar(&maxWait, "max-wait", 2*time.Minute, "Maximum time to wait")
	return cmd
}

type parsedQueryStatus struct {
	Complete bool
	Error    bool
}

func queryStatus(raw json.RawMessage) (parsedQueryStatus, bool) {
	var payload struct {
		QueryStatus *struct {
			Complete bool `json:"complete"`
			Error    bool `json:"error"`
		} `json:"query_status"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil || payload.QueryStatus == nil {
		return parsedQueryStatus{}, false
	}
	return parsedQueryStatus{Complete: payload.QueryStatus.Complete, Error: payload.QueryStatus.Error}, true
}

func writeQueryResult(raw json.RawMessage, format output.Format) error {
	data := queryResultPayload(raw)
	if format != output.FormatNDJSON {
		output.WriteRawJSON(data, format, true)
		return nil
	}
	return writeQueryNDJSON(data)
}

func queryResultPayload(raw json.RawMessage) json.RawMessage {
	var payload struct {
		QueryStatus *struct {
			Results json.RawMessage `json:"results"`
		} `json:"query_status"`
	}
	if err := json.Unmarshal(raw, &payload); err == nil && payload.QueryStatus != nil && len(payload.QueryStatus.Results) > 0 && string(payload.QueryStatus.Results) != "null" {
		return payload.QueryStatus.Results
	}
	return raw
}

func writeQueryNDJSON(raw json.RawMessage) error {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return err
	}
	writer := output.NewNDJSONWriter(output.Stdout())
	if columns, ok := payload["columns"]; ok {
		if err := writer.WriteMetaLine(output.MetaKeyQuery, map[string]any{"columns": columns, "query_id": payload["id"]}); err != nil {
			return err
		}
	}
	results, ok := payload["results"].([]any)
	if !ok {
		return writer.WriteItem(payload)
	}
	columns, _ := payload["columns"].([]any)
	for _, row := range results {
		if rowValues, ok := row.([]any); ok && len(columns) == len(rowValues) {
			record := map[string]any{}
			for i, col := range columns {
				record[fmt.Sprint(col)] = rowValues[i]
			}
			if err := writer.WriteItem(record); err != nil {
				return err
			}
			continue
		}
		if err := writer.WriteItem(row); err != nil {
			return err
		}
	}
	return nil
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
