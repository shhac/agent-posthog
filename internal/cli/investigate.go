package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

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
		Short: "Resolve a user and stream recent context",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				if err := requireEnvironment(resolved); err != nil {
					return err
				}
				q := baseValues(limit)
				if email != "" {
					q.Set("email", email)
				}
				if distinctID != "" {
					q.Set("distinct_id", distinctID)
				}
				page, err := resolved.Client.List(ctx, fmt.Sprintf("/api/environments/%d/persons/", resolved.EnvironmentID), q)
				if err != nil {
					return err
				}
				writer := output.NewNDJSONWriter(output.Stdout())
				if len(page.Results) == 0 {
					return writer.WriteItem(findingRecord("warning", "No matching person found", map[string]any{"email": email, "distinct_id": distinctID}))
				}
				for _, raw := range page.Results {
					var person map[string]any
					if err := json.Unmarshal(raw, &person); err != nil {
						return err
					}
					if err := writer.WriteItem(entityRecord("person", person["id"], person)); err != nil {
						return err
					}
					if personID, ok := person["id"]; ok {
						if err := writer.WriteItem(nextStepRecord(fmt.Sprintf("agent-posthog persons activity %v --env %d", personID, resolved.EnvironmentID))); err != nil {
							return err
						}
					}
				}
				if page.Next != "" {
					return writer.WritePagination(&output.Pagination{HasMore: true, NextURL: page.Next})
				}
				return nil
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
				if err := requireEnvironment(resolved); err != nil {
					return err
				}
				writer := output.NewNDJSONWriter(output.Stdout())
				if err := writer.WriteItem(entityRecord("event", event, map[string]any{"name": event})); err != nil {
					return err
				}
				sql := fmt.Sprintf("select event, timestamp, distinct_id, properties from events where event = '%s' order by timestamp desc limit %d", escapeHogQLString(event), limit)
				raw, err := resolved.Client.Post(ctx, fmt.Sprintf("/api/environments/%d/query/", resolved.EnvironmentID), nil, map[string]any{"query": map[string]any{"kind": "HogQLQuery", "query": sql}})
				if err != nil {
					return err
				}
				var result any
				_ = json.Unmarshal(raw, &result)
				return writer.WriteItem(queryResultRecord("recent_events", result))
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
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				if err := requireProject(resolved); err != nil {
					return err
				}
				id, err := resolveFlagID(ctx, resolved, args[0])
				if err != nil {
					return err
				}
				writer := output.NewNDJSONWriter(output.Stdout())
				flagRaw, err := resolved.Client.Get(ctx, fmt.Sprintf("/api/projects/%d/feature_flags/%s/", resolved.ProjectID, id), nil)
				if err != nil {
					return err
				}
				var flag map[string]any
				_ = json.Unmarshal(flagRaw, &flag)
				if err := writer.WriteItem(entityRecord("feature_flag", flag["id"], flag)); err != nil {
					return err
				}
				if active, _ := flag["active"].(bool); !active {
					if err := writer.WriteItem(findingRecord("warning", "Feature flag is inactive", map[string]any{"key": flag["key"]})); err != nil {
						return err
					}
				}
				for _, evidence := range []struct {
					name string
					path string
				}{
					{name: "dependent_flags", path: fmt.Sprintf("/api/projects/%d/feature_flags/%s/dependent_flags/", resolved.ProjectID, id)},
					{name: "activity", path: fmt.Sprintf("/api/projects/%d/feature_flags/%s/activity/", resolved.ProjectID, id)},
				} {
					page, err := resolved.Client.List(ctx, evidence.path, nil)
					if err != nil {
						return err
					}
					for _, raw := range page.Results {
						var data any
						_ = json.Unmarshal(raw, &data)
						if err := writer.WriteItem(queryResultRecord(evidence.name, data)); err != nil {
							return err
						}
					}
				}
				return writer.WriteItem(nextStepRecord(fmt.Sprintf("agent-posthog flags get %v --full", flag["id"])))
			})
		},
	}
}

func escapeHogQLString(value string) string {
	return strings.ReplaceAll(value, "'", "''")
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
