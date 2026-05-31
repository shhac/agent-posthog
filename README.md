# agent-posthog

PostHog product analytics CLI for AI agents. It is designed for read-heavy
investigation workflows where an LLM needs compact structured output, actionable
error hints, and no direct access to PostHog secrets.

## Features

- Keychain-first credentials: personal API keys are never printed back.
- Multi-profile support: profiles carry host plus default organization, project,
  and environment IDs.
- LLM-shaped output: lists and queries default to NDJSON, single resources to JSON.
- Structured errors: stderr JSON includes `fixable_by: agent|human|retry`.
- Mock server: `mockposthog` provides deterministic E2E fixtures.
- Agent onboarding: ships with `skills/agent-posthog/SKILL.md`.

## Quick Start

```bash
make build
./agent-posthog auth add prod --form --host https://us.posthog.com
./agent-posthog auth check prod
./agent-posthog orgs list
./agent-posthog projects list --org <org-id>
./agent-posthog environments list --project <project-id>
./agent-posthog auth update prod --org <org-id> --project <project-id> --env <env-id> --default
./agent-posthog schema events list --search signup
./agent-posthog query hogql "select event, count() from events group by event order by count() desc limit 20"
./agent-posthog flags get checkout-v2
```

When an LLM is guiding setup, prefer `--form` over `--api-key`. A native OS
dialog asks the user for the key, and the CLI returns only a redacted receipt.

## Development

```bash
make test
make vet
make build
make build-mock
make mock
make mock-dev ARGS="orgs list"
```

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
POSTHOG_PERSONAL_API_KEY=phx_mock ./agent-posthog flags get missing-flag
POSTHOG_PERSONAL_API_KEY=phx_mock ./agent-posthog flags get ambiguous-flag
POSTHOG_PERSONAL_API_KEY=phx_mock ./agent-posthog orgs list --limit 1
POSTHOG_PERSONAL_API_KEY=phx_mock ./agent-posthog query get query_failed
POSTHOG_PERSONAL_API_KEY=phx_mock ./agent-posthog api get /api/projects/123/session_recordings/rec_1/sharing/
```

## License

MIT
