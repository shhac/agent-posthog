package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
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
				return writeList(page.Results, page.Next, globals.Format)
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
			sql := fmt.Sprintf("select event, timestamp, distinct_id, properties from events where event = '%s' order by timestamp desc limit %d", escapeHogQLString(event), limit)
			return runQuery(cmd.Context(), globals, map[string]any{"query": map[string]any{"kind": "HogQLQuery", "query": sql}})
		},
	}
	cmd.Flags().StringVar(&event, "event", "", "Event name")
	cmd.Flags().IntVar(&limit, "limit", 20, "Sample size")
	_ = cmd.MarkFlagRequired("event")
	return cmd
}

func investigateFlag(globals *GlobalFlags) *cobra.Command {
	return flagGetCommand(globals, "flag <id-or-key>", "Inspect a feature flag", "")
}

func escapeHogQLString(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}
