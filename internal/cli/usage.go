package cli

import "github.com/spf13/cobra"

func registerUsageCommand(root *cobra.Command, globals *GlobalFlags) {
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
					"agent-posthog query wait <query-id>        # exits 1 if the query failed",
					"agent-posthog query json --json '{\"query\":{\"kind\":\"HogQLQuery\",\"query\":\"select 1\"}}'",
					"agent-posthog persons list --email user@example.com",
					"agent-posthog flags get checkout-v2",
					"agent-posthog flags disable checkout-v2        # prints the plan; add --yes to apply",
					"agent-posthog flags export checkout-v2 --format yaml",
					"agent-posthog flags apply --file spec.yaml --yes",
					"agent-posthog flags evaluate checkout-v2 --distinct-id u_1 --props-json '{\"app_env\":\"staging\"}'",
					"agent-posthog flags test checkout-v2 --cases cases.yaml",
					"agent-posthog config alias set staging <project-id>",
					"agent-posthog --project staging flags copy checkout-v2 --to prod",
					"agent-posthog dashboards run <dashboard-id>",
				},
				"output": "Lists, queries, and investigations default to NDJSON; a list ends with {\"@pagination\":{\"has_more\",\"next_url\"}} when more remain, and --format json|yaml gives {\"data\":[…],\"@pagination\":{…}}. Get (single + multi): `get <id>...` takes one or more ids and returns one result per id, in input order — NDJSON by default: the record, or {\"@unresolved\":{\"id\",\"reason\",\"fixable_by\",\"hint\"?}} for an id that couldn't be resolved. `--format json|yaml` collapses to one {\"data\":[…],\"@unresolved\":[…]} envelope, even for one id. Item-level misses stay on stdout and exit 0; a command-level failure (auth, permissions, network, or a missing org/project/environment) goes to stderr with exit 1 and empty stdout. Reads prune nulls and redact secrets; --expose <field> reveals one. Every command honors --format. Errors: stderr JSON {\"error\":\"...\",\"fixable_by\":\"agent\"|\"human\"|\"retry\",\"hint\"?:\"...\",\"retry_after_seconds\"?:N}, exit 1. A Retry-After over 30s returns as fixable_by retry with retry_after_seconds.",
				"safety": "Never paste API keys into chat. Use auth add --form so the secret goes directly to a local OS dialog. Flag writes print a plan and change nothing without --yes; --dry-run forces a plan even with --yes. Through MCP, every write (flag commands, query cancel, api post outside query endpoints) is off until a person runs 'agent-posthog config mcp-writes on'. Flag references take id:<n> or key:<k>.",
			}, globals.Format)
		},
	})
}
