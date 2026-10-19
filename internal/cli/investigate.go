package cli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/featureflag"
	"github.com/shhac/agent-posthog/internal/hogql"
	"github.com/shhac/agent-posthog/internal/output"
)

func registerInvestigate(root *cobra.Command, globals *GlobalFlags) {
	investigate := &cobra.Command{
		Use:   "investigate",
		Short: "Opinionated PostHog investigation workflows",
	}
	investigate.AddCommand(investigateUser(globals))
	investigate.AddCommand(investigateEvent(globals))
	investigate.AddCommand(investigateFlag(globals))
	root.AddCommand(investigate)
}

func investigateUser(globals *GlobalFlags) *cobra.Command {
	var email, distinctID string
	var limit int
	cmd := &cobra.Command{
		Use:   "user",
		Short: "Resolve a user and its recent context",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				path, err := resolved.path(scopeEnvironment, "persons")
				if err != nil {
					return err
				}
				q := baseValues(limit)
				setIf(q, "email", email)
				setIf(q, "distinct_id", distinctID)
				page, err := resolved.Client.List(ctx, path, q)
				if err != nil {
					return err
				}
				if len(page.Results) == 0 {
					return writeEvidence([]any{findingRecord("warning", "No matching person found", map[string]any{"email": email, "distinct_id": distinctID})}, "", globals.Format)
				}
				var records []any
				for _, raw := range page.Results {
					var person map[string]any
					if err := json.Unmarshal(raw, &person); err != nil {
						return err
					}
					records = append(records, entityRecord("person", person["id"], person))
					if personID, ok := person["id"]; ok {
						records = append(records, nextStepRecord(fmt.Sprintf("agent-posthog persons activity %v --env %d", personID, resolved.EnvironmentID)))
					}
				}
				return writeEvidence(records, page.Next, globals.Format)
			})
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "Email to resolve")
	cmd.Flags().StringVar(&distinctID, "distinct-id", "", "Distinct ID to resolve")
	cmd.Flags().IntVar(&limit, "limit", 10, "Maximum people to return")
	return cmd
}

func investigateEvent(globals *GlobalFlags) *cobra.Command {
	var event string
	var limit int
	cmd := &cobra.Command{
		Use:   "event",
		Short: "Sample recent events through HogQL",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				path, err := resolved.path(scopeEnvironment, "query")
				if err != nil {
					return err
				}
				sql := fmt.Sprintf("select event, timestamp, distinct_id, properties from events where event = {event} order by timestamp desc limit %d", limit)
				raw, err := resolved.Client.Post(ctx, path, nil, hogql.Body(sql, map[string]any{"event": event}))
				if err != nil {
					return err
				}
				var result any
				_ = json.Unmarshal(raw, &result)
				return writeEvidence([]any{
					entityRecord("event", event, map[string]any{"name": event}),
					queryResultRecord("recent_events", result),
				}, "", globals.Format)
			})
		},
	}
	cmd.Flags().StringVar(&event, "event", "", "Event name")
	cmd.Flags().IntVar(&limit, "limit", 20, "Sample size")
	_ = cmd.MarkFlagRequired("event")
	return cmd
}

func investigateFlag(globals *GlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "flag <id-or-key>",
		Short: "Inspect a feature flag and related evidence",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withFlags(cmd, globals, func(ctx context.Context, p featureflag.Project) error {
				id, err := p.ResolveID(ctx, args[0])
				if err != nil {
					return err
				}
				raw, err := p.Get(ctx, id)
				if err != nil {
					return err
				}
				var flag map[string]any
				_ = json.Unmarshal(raw, &flag)
				records := []any{entityRecord("feature_flag", flag["id"], flag)}
				if active, _ := flag["active"].(bool); !active {
					records = append(records, findingRecord("warning", "Feature flag is inactive", map[string]any{"key": flag["key"]}))
				}
				for _, name := range []string{"dependent_flags", "activity"} {
					related, err := p.Related(ctx, id, name)
					if err != nil {
						return err
					}
					for _, item := range related {
						var data any
						_ = json.Unmarshal(item, &data)
						records = append(records, queryResultRecord(name, data))
					}
				}
				records = append(records, nextStepRecord(fmt.Sprintf("agent-posthog flags get %v --full", flag["id"])))
				return writeEvidence(records, "", globals.Format)
			})
		},
	}
}

// writeEvidence prints an investigation's records as a list, NDJSON by
// default.
func writeEvidence(records []any, nextURL, flagFormat string) error {
	format, err := output.ResolveFormat(flagFormat, output.FormatNDJSON)
	if err != nil {
		return err
	}
	return output.WriteList(format, records, output.PaginationMeta(nextURL))
}

func entityRecord(entity string, id any, data any) map[string]any {
	return map[string]any{"type": "entity", "entity": entity, "id": id, "data": data}
}

func findingRecord(severity, summary string, data any) map[string]any {
	return map[string]any{"type": "finding", "severity": severity, "summary": summary, "data": data}
}

func queryResultRecord(name string, data any) map[string]any {
	return map[string]any{"type": "query_result", "name": name, "data": data}
}

func nextStepRecord(command string) map[string]any {
	return map[string]any{"type": "next_step", "command": command}
}
