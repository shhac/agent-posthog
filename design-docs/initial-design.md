# agent-posthog initial design

## Goal

Build a PostHog CLI for AI agents: read-heavy product analytics investigation,
compact structured output, secret-safe setup, and enough mutation support for
feature-flag and metadata workflows without becoming a full PostHog admin CLI.

The CLI should feel like the sibling projects:

- `agent-stripe`: profile-based auth, `--form` setup, Keychain-only secrets,
  resource commands plus investigation commands, `usage` reference cards,
  JSON/NDJSON/YAML output, and classified JSON errors.
- `agent-dd`: incident/triage focus, compact defaults, multi-account support,
  domain commands, and `fixable_by: agent|human|retry`.

## PostHog model

PostHog private API calls use a personal API key and the app host, for example
`https://us.posthog.com` or `https://eu.posthog.com`. Public ingestion/evaluation
endpoints use project tokens and different ingestion hosts, for example
`https://us.i.posthog.com`. This CLI should start with private API workflows and
avoid exposing either personal keys or project tokens by default.

The important containment hierarchy is:

```text
profile
  host: https://us.posthog.com | https://eu.posthog.com | self-hosted
  personal API key: stored in Keychain
  default organization: uuid
  default project: numeric id
  default environment: numeric id

organization
  projects
    environments
      persons, dashboards, insights, queries, recordings
    project-scoped resources
      feature flags, event definitions, property definitions
```

PostHog is in a transition period where some endpoints are project-scoped and
others are environment-scoped. The CLI should expose both concepts but make the
default path easy: resolve a profile to a default environment for most analytics
commands, and a default project for feature flag/schema commands.

## Command shape

```text
agent-posthog
├── auth              add, update, remove, list, default, check
├── orgs              list, get
├── projects          list, get
├── environments      list, get
├── schema            events, properties, usage
├── query             hogql, json, get, cancel, usage
├── persons           list, get, activity, usage
├── insights          list, get, analyze, usage
├── dashboards        list, get, run, usage
├── flags             list, get, create, update, disable, usage
├── recordings        list, get, usage
├── experiments       list, get, usage
├── investigate       user, funnel, flag, dashboard, event, usage
├── api               get, post
├── config            show, path, set, unset
├── usage
└── version
```

Top-level global flags:

```text
-p, --profile <alias>      profile alias; default from config or AGENT_POSTHOG_PROFILE
    --org <uuid>           organization override
    --project <id>         project override
    --env <id>             environment override
    --host <url>           private API host override for tests/self-hosted
-f, --format <fmt>         json, yaml, jsonl/ndjson
-t, --timeout <ms>         request timeout
    --max-retries <n>      bounded retries for 429/5xx
-d, --debug               JSON debug records to stderr, no secrets
    --full                disable compact shaping/truncation
```

Environment variables:

```text
AGENT_POSTHOG_PROFILE
AGENT_POSTHOG_HOST
AGENT_POSTHOG_BASE_URL      test/mock override
POSTHOG_PERSONAL_API_KEY    direct auth escape hatch; never print
POSTHOG_PROJECT_ID
POSTHOG_ENVIRONMENT_ID
POSTHOG_ORGANIZATION_ID
```

Prefer the `AGENT_POSTHOG_*` names in examples. Accept PostHog's native env var
for quick local use, but mark direct env auth as less safe for LLM-guided setup.
Resolution order is: explicit flags, profile defaults, `AGENT_POSTHOG_*`
environment variables, PostHog-native environment variables, then built-in
defaults such as `https://us.posthog.com`. `AGENT_POSTHOG_BASE_URL` is a test
and mock-server override for the private API host and should win over profile
host metadata when present.

## Auth and profiles

Use the `agent-stripe` pattern rather than the older `agent-dd` fallback:
secrets are stored in OS Keychain; `credentials.json` is only a non-secret index.
If Keychain storage fails, return a human-fixable error instead of writing the key
to disk.

```bash
agent-posthog auth add prod --form --host https://us.posthog.com
agent-posthog auth add ci --api-key <key> --host http://127.0.0.1:18118
agent-posthog auth check prod
agent-posthog orgs list -p prod
agent-posthog projects list -p prod --org <uuid>
agent-posthog environments list -p prod --project 123
agent-posthog auth update prod --org <uuid> --project 123 --env 456 --default
agent-posthog auth list
```

`auth add --form` opens a native OS dialog for the personal API key, so the LLM
never sees the secret. `--api-key` exists for tests, automation, and human-driven
terminal setup, but agents should prefer `--form`. The setup receipt must include
only redacted metadata:

```json
{
  "status": "added",
  "profile": "prod",
  "storage": "keychain",
  "host": "https://us.posthog.com",
  "default": true
}
```

Config file shape:

```json
{
  "default_profile": "prod",
  "defaults": {
    "timeout_ms": 30000,
    "max_retries": 2
  },
  "profiles": {
    "prod": {
      "host": "https://us.posthog.com",
      "organization_id": "00000000-0000-0000-0000-000000000000",
      "project_id": 123,
      "environment_id": 456
    }
  }
}
```

## Output contract

Default formats:

- list/search/query/investigation streams: NDJSON (`jsonl`)
- entity gets (`get <id>...`): NDJSON by default — one line per id (the record,
  or `{"@unresolved":{"id","reason","fixable_by","hint"?}}` for missing ids).
  Pass `--format json` for a single pretty object (one-id case), or
  `--format json|yaml` to collapse to `{"data":[…],"@unresolved":[…]}` envelope.
- `--format yaml` allowed for humans
- `--full` returns unshaped API payloads where possible
- compact output is null-pruned by default

**Get contract.** `get <id>...` takes 1..N ids and emits one stdout line per
input in order. Item-level misses (not found, bad key) → `@unresolved` line on
stdout, exit 0. Command-level failures (auth, network, zero args) → structured
`{error}` on stderr, empty stdout, exit 1. This is the only use of stderr/exit 1.

NDJSON should use data rows plus `@` meta rows:

```jsonl
{"type":"person","id":123,"distinct_ids":["u_1"],"email":"a@example.com","last_seen_at":"2026-05-30T12:00:00Z"}
{"@pagination":{"has_more":true,"next_url":"https://us.posthog.com/api/..."}}
{"@query":{"columns":["event","count"],"elapsed_ms":124}}
```

Errors are JSON on stderr:

```json
{
  "error": "Missing environment id",
  "fixable_by": "agent",
  "hint": "Run 'agent-posthog environments list --project <id>' or set one with 'agent-posthog auth update <profile> --env <id>'."
}
```

Classifications:

- `agent`: bad arguments, unknown format, missing IDs that can be discovered
- `human`: auth, permissions, missing scopes, keychain or setup problems
- `retry`: rate limits, transient 5xx, network timeouts

Debug logs should be structured JSON records to stderr and must redact:

- `Authorization`
- personal API keys (`phx_...`)
- OAuth tokens (`pha_...`, `phr_...`)
- project tokens and secure keys (`phc_...`, `phs_...`)
- request bodies for auth setup

## Read commands

### Organization and project discovery

These commands help an LLM bootstrap the right IDs after auth:

```bash
agent-posthog orgs list
agent-posthog orgs get <org-id>
agent-posthog projects list --org <org-id>
agent-posthog projects get <project-id> --org <org-id>
agent-posthog environments list --project <project-id>
agent-posthog environments get <environment-id> --project <project-id>
```

Lists emit compact records with names, IDs, URLs, timezone, and default markers.

### Schema

Schema is the highest-value LLM onboarding surface because it tells the agent what
events/properties exist before writing HogQL.

```bash
agent-posthog schema events --search signup --exclude-hidden --limit 100
agent-posthog schema events get "$pageview"
agent-posthog schema properties --type event --event "$pageview" --search browser
agent-posthog schema properties --type person --verified true
```

`schema events --search` is client-side filtering over the event definitions page
because PostHog's documented endpoint does not expose a general search parameter.
`schema properties --event` maps to PostHog's `event_names` parameter with event
name filtering enabled.

### Query

HogQL should be first-class and streamable.

```bash
agent-posthog query hogql "select event, count() from events where timestamp > now() - interval 7 day group by event order by count() desc limit 20"
agent-posthog query hogql --file query.sql --var key=value --format jsonl
agent-posthog query json --body query.json
agent-posthog query get <query-id>
agent-posthog query cancel <query-id>
```

For tabular responses, emit one NDJSON row per result and include a meta row for
columns/types. If the API returns async query IDs, surface them in `@query` and
offer the exact follow-up command in an error or meta hint.

### Persons

```bash
agent-posthog persons list --email user@example.com
agent-posthog persons list --distinct-id user_123
agent-posthog persons get <person-id>
agent-posthog persons activity <person-id> --limit 50
```

Compact person output should include ID, uuid, name, selected distinct IDs, email
if present, created/last-seen timestamps, and a pruned properties map.

### Insights and dashboards

```bash
agent-posthog insights list --search activation
agent-posthog insights get <insight-id>
agent-posthog insights analyze <insight-id>
agent-posthog dashboards list --search growth
agent-posthog dashboards get <dashboard-id>
agent-posthog dashboards run <dashboard-id>
```

`dashboards run` should stream each tile/insight result as its own row so large
dashboards do not force one giant JSON blob into context. Per-tile failures should
emit `type:"tile_error"` records rather than failing the whole dashboard run when
PostHog returns partial results.

### Feature flags

```bash
agent-posthog flags list --active true --type experiment
agent-posthog flags get <id-or-key>
agent-posthog flags dependent <id-or-key>
agent-posthog flags activity <id-or-key>
```

The private feature flag get endpoint is ID-addressed. Key lookup should resolve
by listing/searching flags first, then fetching the matched numeric ID. Ambiguous
or missing key matches are agent-fixable errors with a hint to run `flags list`.

Create/update should exist but require explicit inputs and produce a clear diff:

```bash
agent-posthog flags create --key checkout-v2 --name "Checkout v2" --active false --body flag.json
agent-posthog flags update checkout-v2 --set active=false
agent-posthog flags disable checkout-v2
```

The first implementation can make mutating commands opt-in behind `--yes` for
anything that changes rollout, active state, or targeting.

## Investigation commands

These are opinionated LLM workflows that gather multiple resources and emit
evidence records.

```bash
agent-posthog investigate user --email user@example.com --since 14d
agent-posthog investigate user --distinct-id user_123 --since 14d
agent-posthog investigate event --event "$pageview" --since 24h --sample 20
agent-posthog investigate funnel --events "$pageview,signup,checkout" --since 7d
agent-posthog investigate flag checkout-v2 --distinct-id user_123
agent-posthog investigate dashboard <dashboard-id> --since 7d
```

Evidence record contract:

```jsonl
{"type":"entity","entity":"person","id":"123","data":{}}
{"type":"query_result","name":"recent_events","data":{}}
{"type":"finding","severity":"warning","summary":"Flag is disabled in this environment","data":{}}
{"type":"next_step","command":"agent-posthog flags get checkout-v2 --full"}
```

Initial workflows to prioritize:

1. `investigate user`: resolve person by email/distinct ID, recent events,
   cohorts/flags if available, and session recording links.
2. `investigate event`: schema, recent samples, volume trend, top properties.
3. `investigate flag`: flag definition, rollout conditions, dependent flags,
   recent `$feature_flag_called` events, and optional user evaluation context.
4. `investigate dashboard`: dashboard metadata, tile/insight results, stale or
   failing query hints.

## Raw API escape hatch

Like `agent-stripe api`, include a constrained raw endpoint command for gaps:

```bash
agent-posthog api get /api/environments/456/persons/ --query email=a@example.com
agent-posthog api post /api/environments/456/query/ --body query.json
```

Rules:

- require paths to start with `/api/`
- no custom `Authorization` header flag
- redact secrets in debug
- default `post` to JSON body files or stdin
- allow `post` by default only for query endpoints; require `--yes` for other raw
  POST endpoints because many PostHog POST routes mutate state
- do not ship raw `patch`/`delete` until there is a specific need
- support `--print-request` to show the resolved method, path, query, host, and
  redacted headers without sending the request

## Time flags

Investigation and query-helper commands should accept the same time grammar:

- relative durations: `now-15m`, `now-1h`, `7d`, `24h`
- RFC3339 timestamps: `2026-05-31T12:00:00Z`
- Unix epoch seconds

Prefer explicit `--from` and `--to` on resource/query commands, and `--since` on
investigation shortcuts where the default end is `now`.

## Skill

Ship `skills/agent-posthog/SKILL.md` with:

- triggers: "posthog", "hogql", "product analytics", "feature flag",
  "session replay", "dashboard", "insight", "funnel", "person properties"
- safety: never ask for or echo API keys; use `auth add --form`
- setup flow: auth, org/project/environment discovery, set defaults
- common commands and investigation workflows
- output guidance: lists/query/investigate are NDJSON, errors are JSON stderr
- when to use `schema` before writing HogQL

## Release and command integration

Mirror `agent-stripe`:

- `.agents/commands/release.md`
- `.claude/commands -> ../.agents/commands`
- `.opencode/commands -> ../.agents/commands`

The release command should cover:

1. clean working tree and `main` branch checks
2. `make test` and `go vet ./...`
3. semver bump and tag
4. `goreleaser release --clean` with manual fallback
5. GitHub release verification
6. Homebrew tap update for `../homebrew-tap/Formula/agent-posthog.rb`

## Build plan

1. Scaffold Go project matching sibling layout.
2. Implement config, Keychain credentials, `--form` dialog, output/errors.
3. Implement API client with pagination, retry, and PostHog error mapping.
4. Implement auth/org/project/environment/schema/query/persons.
5. Add insights/dashboards/flags.
6. Add first investigation commands.
7. Add `mockposthog` for E2E fixtures and local development.
8. Keep dependencies injectable: API client HTTP transport, credential store,
   config directory, prompt/dialog function, and output writers should be
   replaceable in tests.
9. Add skill, release command, symlinks, README, GoReleaser, linting,
   formatting, and tests from the start.

## Open decisions

- Whether to name the account command `auth`, `profile`, or `org`. Current choice:
  `auth`, because one profile may contain host plus default org/project/env.
- Whether to prefer environment IDs or project IDs in commands. Current choice:
  make environment the analytics default, but allow project fallback where the
  official API still supports both.
- Whether direct project-token public endpoints belong in v1. Current choice:
  no, except maybe later `capture`/`flags eval` commands if a real LLM workflow
  needs them.
- Whether mutating feature flag commands should require `--yes`. Current choice:
  yes for active/rollout/targeting changes.
