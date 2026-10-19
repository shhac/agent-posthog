# agent-posthog

PostHog product analytics CLI for AI agents. It is designed for investigation
workflows where an LLM needs compact structured output, actionable error hints,
and no direct access to PostHog secrets, and for setting up feature flags
(gates and per-rule JSON config) with plans, tests, and staging-to-prod copies.

## Features

- Keychain-first credentials: personal API keys are never printed back. When
  no keychain is available the key goes in a 0600 file; `auth list` reports
  where each profile's key really is (`credential: keychain|file|missing`).
- Multi-profile support: profiles carry host plus default organization, project,
  and environment IDs. The environment defaults to the project when unset — a
  project's default environment shares the project's ID, so most setups only set
  `--project`. Override `--env` only to target a non-default environment.
- LLM-shaped output: lists and queries default to NDJSON, and every command
  honors `--format json|yaml|jsonl`. A list prints one line per record, then
  `{"@pagination":{"has_more":true,"next_url":…}}` when more remain;
  `--format json|yaml` gives `{"data":[…],"@pagination":{…}}`. Entity gets (`get <id>...`) print one
  NDJSON line per id, in order; `--format json|yaml` wraps them in one
  `{"data":[…],"@unresolved":[…]}` envelope. An id that is not found becomes an
  `{"@unresolved":{...}}` line (exit 0); a command-level failure (auth,
  permissions, network, or a missing org/project/environment setting, checked
  before any request) goes to stderr with exit 1 and nothing on stdout. Gets,
  like lists, prune nulls and redact secrets; `--expose <field>` reveals one.
- Structured errors: stderr JSON `{"error":"...","fixable_by":"agent"|"human"|"retry","hint"?:"...","retry_after_seconds"?:N}`, exit 1.
  Reads retry rate limits and gateway errors within `--timeout`; a
  `Retry-After` over 30 seconds comes back as a `retry` error instead of a wait.
  `--debug` logs mask keys and tokens, including ones inside request URLs, and
  the personal API key is only sent to the configured host (a `next` link to
  another host is refused).
- Feature flag management: create, apply specs, narrow edits, archive/delete,
  evaluate, scenario test suites, and diff/copy between projects. Every write
  prints a plan unless `--yes` is passed, and refuses stale overlapping edits.
  Over MCP, every PostHog write (flag commands, `query cancel`, `api post`
  outside query endpoints) stays off until a person runs `agent-posthog config set mcp_writes on`; after
  that a host-confirmed call applies (the bridge adds `--yes`), and
  `--dry-run` previews.
- Mock server: `mockposthog` provides deterministic E2E fixtures, including a
  stateful flag store.

## Quick Start

```bash
make build
./agent-posthog auth add prod --form --host https://us.posthog.com
./agent-posthog auth check prod
./agent-posthog orgs list
./agent-posthog projects list --org <org-id>
./agent-posthog auth update prod --org <org-id> --project <project-id> --default
# Only if you use multiple environments per project:
./agent-posthog environments list --project <project-id>
./agent-posthog auth update prod --env <env-id>
./agent-posthog schema events list --search signup
./agent-posthog query hogql "select event, count() from events group by event order by count() desc limit 20"
./agent-posthog flags get checkout-v2
./agent-posthog flags create beta-gate --where "email icontains @example.com"   # plan; add --yes
```

When an LLM is guiding setup, prefer `--form` over `--api-key`. A native OS
dialog asks the user for the key, and the CLI returns only a redacted receipt.

## Claude Code / AI agent skill

```bash
npx skills add shhac/agent-skills --skill agent-posthog --global
```

Installs the `agent-posthog` skill globally so Claude Code (and other AI agents) can discover and use it automatically. It ships from [`shhac/agent-skills`](https://github.com/shhac/agent-skills) — the whole family's skills in one repo, so `npx skills update` checks a single source no matter how many you use. Want several at once? Run `npx skills add shhac/agent-skills --global` and pick from the list.

## Development

```bash
make test
make vet
make build
make build-mock
make mock
make mock-dev ARGS="orgs list"
```

`make smoke-mock` runs the read checklist and a flag lifecycle smoke test
(create, apply, positional edits, the strict rollout endpoint, `/flags`
scenarios, archive, and delete) against the mock PostHog.

`make lint` runs `golangci-lint` when installed. `make fmt` runs `gofmt` and
`goimports`.

## Mock PostHog

```bash
make build-mock
./mockposthog --routes
./mockposthog --addr 127.0.0.1:18118
AGENT_POSTHOG_BASE_URL=http://127.0.0.1:18118 POSTHOG_PERSONAL_API_KEY=phx_mock ./agent-posthog orgs list
make smoke-mock
```

Useful mock edge cases:

```bash
POSTHOG_PERSONAL_API_KEY=phx_invalid ./agent-posthog orgs list
POSTHOG_PERSONAL_API_KEY=phx_no_scope ./agent-posthog orgs list
POSTHOG_PERSONAL_API_KEY=phx_mock ./agent-posthog --max-retries 0 api get /api/mock/rate_limit
POSTHOG_PERSONAL_API_KEY=phx_mock ./agent-posthog api get /api/mock/validation_error
POSTHOG_PERSONAL_API_KEY=phx_mock ./agent-posthog --project 123 flags get missing-flag
POSTHOG_PERSONAL_API_KEY=phx_mock ./agent-posthog --project 123 flags get ambiguous-flag
POSTHOG_PERSONAL_API_KEY=phx_mock ./agent-posthog orgs list --limit 1
POSTHOG_PERSONAL_API_KEY=phx_mock ./agent-posthog --env 456 query get query_failed
POSTHOG_PERSONAL_API_KEY=phx_mock ./agent-posthog --env 456 query wait query_failed   # exits 1
POSTHOG_PERSONAL_API_KEY=phx_mock ./agent-posthog api get /api/projects/123/session_recordings/rec_1/sharing/
```

## License

PolyForm Perimeter License 1.0.0 — see [LICENSE](LICENSE).
