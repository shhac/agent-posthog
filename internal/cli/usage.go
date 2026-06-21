package cli

import "github.com/spf13/cobra"

func registerUsageCommand(root *cobra.Command) {
	root.AddCommand(&cobra.Command{
		Use:   "usage",
		Short: "Agent-friendly command reference",
		RunE: func(cmd *cobra.Command, args []string) error {
			return writeItem(map[string]any{
				"setup": []string{
					"agent-posthog auth add prod --form --host https://us.posthog.com",
					"agent-posthog auth check prod",
					"agent-posthog orgs list -p prod",
					"agent-posthog projects list -p prod --org <org-id>",
					"agent-posthog environments list -p prod --project <project-id>",
					"agent-posthog auth update prod --org <org-id> --project <project-id> --env <env-id> --default",
				},
				"common": []string{
					"agent-posthog schema events list --search signup",
					"agent-posthog schema properties list --event '$pageview'",
					"agent-posthog query hogql \"select event, count() from events group by event order by count() desc limit 20\"",
					"agent-posthog persons list --email user@example.com",
					"agent-posthog flags get checkout-v2",
					"agent-posthog dashboards run <dashboard-id>",
				},
				"output": "Lists, queries, and investigations default to NDJSON. Get (single + multi): `get <id>...` takes one or more ids and returns one result per id, in input order — NDJSON by default: the record, or {\"@unresolved\":{\"id\",\"reason\",\"fixable_by\",\"hint\"?}} for an id that couldn't be resolved. `--format json|yaml` collapses to one {\"data\":[…],\"@unresolved\":[…]} envelope. A single `get <id>` is the one-element case (NDJSON one line by default; pass `--format json` for the object). Item-level misses stay on stdout and exit 0; only a command-level failure (auth, network) goes to stderr with exit 1 and empty stdout. Errors: stderr JSON {\"error\":\"...\",\"fixable_by\":\"agent\"|\"human\"|\"retry\",\"hint\"?:\"...\",\"retry_after_seconds\"?:N}, exit 1.",
				"safety": "Never paste API keys into chat. Use auth add --form so the secret goes directly to a local OS dialog.",
			}, "")
		},
	})
}
