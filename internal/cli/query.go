package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/shhac/lib-agent-cli/creds"
	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/output"
)

func registerQuery(root *cobra.Command, globals *GlobalFlags) {
	query := &cobra.Command{Use: "query", Short: "Run and inspect PostHog queries"}
	query.AddCommand(queryHogQLCommand(globals))
	query.AddCommand(queryJSONCommand(globals))
	query.AddCommand(queryWaitCommand(globals))
	query.AddCommand(subresourceCommand("get <query-id>", "Get async query status", globals, scopeEnvironment, func(id string) []string {
		return []string{"query", id}
	}))
	query.AddCommand(subresourceCommand("log <query-id>", "Get async query log", globals, scopeEnvironment, func(id string) []string {
		return []string{"query", id, "log"}
	}))
	query.AddCommand(&cobra.Command{
		Use:   "cancel <query-id>",
		Short: "Cancel an async query",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				path, err := resolved.path(scopeEnvironment, "query", args[0])
				if err != nil {
					return err
				}
				raw, err := resolved.Client.Delete(ctx, path, nil)
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
			sql, err := hogQLText(args, file)
			if err != nil {
				return err
			}
			body := hogQLBody(sql, nil)
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

// hogQLText is the query from --file, or else the argument.
func hogQLText(args []string, file string) (string, error) {
	if file == "" {
		return args[0], nil
	}
	data, err := readInput("", file, "", "file")
	return string(data), err
}

func queryJSONCommand(globals *GlobalFlags) *cobra.Command {
	var bodyFile, inline string
	cmd := &cobra.Command{
		Use:   "json",
		Short: "Run a raw PostHog query JSON body",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if inline != "" {
				bodyFile = ""
			}
			body, err := readJSONObject(inline, bodyFile, "json", "body")
			if err != nil {
				return err
			}
			return runQuery(cmd.Context(), globals, body)
		},
	}
	cmd.Flags().StringVar(&bodyFile, "body", "-", "JSON body file or - for stdin")
	cmd.Flags().StringVar(&inline, "json", "", "JSON body inline (for callers without stdin, such as MCP)")
	return cmd
}

// hogQLBody builds a HogQL query request. values fill {placeholder}s in sql,
// so user input never has to be escaped into the query text.
func hogQLBody(sql string, values map[string]any) map[string]any {
	query := map[string]any{"kind": "HogQLQuery", "query": sql}
	if len(values) > 0 {
		query["values"] = values
	}
	return map[string]any{"query": query}
}

func runQuery(cmdCtx context.Context, globals *GlobalFlags, body map[string]any) error {
	return withClient(cmdCtx, globals, func(ctx context.Context, resolved *resolvedContext) error {
		path, err := resolved.path(scopeEnvironment, "query")
		if err != nil {
			return err
		}
		raw, err := resolved.Client.Post(ctx, path, nil, body)
		if err != nil {
			return err
		}
		return writeQueryResult(raw, globals.Format)
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
				path, err := resolved.path(scopeEnvironment, "query", args[0])
				if err != nil {
					return err
				}
				fetch := func(ctx context.Context) (json.RawMessage, error) {
					return resolved.Client.Get(ctx, path, nil)
				}
				raw, status, err := waitForQuery(ctx, fetch, interval, maxWait)
				if err != nil {
					return agenterrors.Wrap(err, agenterrors.FixableByRetry).
						WithHint("Run 'agent-posthog query get " + args[0] + "' to inspect the current query status.")
				}
				if status.Error {
					if err := writeRaw(raw, globals.Format); err != nil {
						return err
					}
					return agenterrors.New("query failed: "+creds.FirstNonEmpty(status.Message, "PostHog reported an error"), agenterrors.FixableByAgent).
						WithHint("Fix the HogQL and run it again; the full status is on stdout.")
				}
				if !status.Known || !status.Complete {
					return writeRaw(raw, globals.Format)
				}
				return writeQueryResult(raw, globals.Format)
			})
		},
	}
	cmd.Flags().DurationVar(&interval, "interval", 2*time.Second, "Polling interval")
	cmd.Flags().DurationVar(&maxWait, "max-wait", 2*time.Minute, "Maximum time to wait")
	return cmd
}

type parsedQueryStatus struct {
	Known    bool
	Complete bool
	Error    bool
	Message  string
}

func queryStatus(raw json.RawMessage) parsedQueryStatus {
	var payload struct {
		QueryStatus *struct {
			Complete     bool   `json:"complete"`
			Error        bool   `json:"error"`
			ErrorMessage string `json:"error_message"`
		} `json:"query_status"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil || payload.QueryStatus == nil {
		return parsedQueryStatus{}
	}
	q := payload.QueryStatus
	return parsedQueryStatus{Known: true, Complete: q.Complete, Error: q.Error, Message: q.ErrorMessage}
}

// waitForQuery polls until the query status is missing, failed, or complete,
// or maxWait runs out.
func waitForQuery(ctx context.Context, fetch func(context.Context) (json.RawMessage, error), interval, maxWait time.Duration) (json.RawMessage, parsedQueryStatus, error) {
	deadline := time.Now().Add(maxWait)
	for {
		raw, err := fetch(ctx)
		if err != nil {
			return nil, parsedQueryStatus{}, err
		}
		status := queryStatus(raw)
		if !status.Known || status.Error || status.Complete {
			return raw, status, nil
		}
		if time.Now().Add(interval).After(deadline) {
			return nil, status, fmt.Errorf("query did not complete before --max-wait")
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, status, ctx.Err()
		case <-timer.C:
		}
	}
}

func writeQueryResult(raw json.RawMessage, flagFormat string) error {
	format, err := output.ResolveFormat(flagFormat, output.FormatNDJSON)
	if err != nil {
		return err
	}
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
