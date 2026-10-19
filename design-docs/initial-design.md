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
├── schema            events list|get, properties list
├── query             hogql, json, get, wait, log, cancel
├── persons           list, get, activity
├── insights          list, get
├── dashboards        list, get, run
├── flags             list, get, dependent, activity, create, apply, apply-plan,
│                     export, enable, disable, archive, unarchive, delete,
│                     restore, early-exit, conditions, variants, payload,
│                     evaluate, test, reach, diff, copy
├── recordings        list, get
├── experiments       list, get
├── investigate       user, event, flag
├── api               get, post
├── config            get, set, unset, list, show, path, alias
├── mcp               stdio or --http server; pair, usage
├── usage
└── completion
```

`--version` prints the version.

Top-level global flags:

```text
-p, --profile <alias>      profile alias; default from config or AGENT_POSTHOG_PROFILE
    --org <uuid>           organization override
    --project <id|alias>   project override; an alias from 'config alias'
    --env <id>             environment override
    --host <url>           private API host override for tests/self-hosted
    --api-key <key>        personal API key override; never printed or persisted
-f, --format <fmt>         json, yaml, jsonl/ndjson
-t, --timeout <ms>         request timeout
    --max-retries <n>      bounded retries for 429/5xx
-d, --debug               JSON debug records to stderr, no secrets
    --full                disable compact shaping/truncation
    --expose <field>       reveal a redacted field (repeatable; 'all')
    --color <mode>         auto, always, never
```

Environment variables:

```text
AGENT_POSTHOG_PROFILE
AGENT_POSTHOG_BASE_URL      host override above the profile (tests, mock)
AGENT_POSTHOG_HOST          host default below the profile
AGENT_POSTHOG_ORGANIZATION_ID
AGENT_POSTHOG_PROJECT_ID
AGENT_POSTHOG_ENVIRONMENT_ID
AGENT_POSTHOG_FLAGS_HOST    /flags host for flag evaluation (self-hosted proxies)
POSTHOG_PERSONAL_API_KEY    direct auth escape hatch; never print
AGENT_POSTHOG_PERSONAL_API_KEY
POSTHOG_PROJECT_ID
POSTHOG_ENVIRONMENT_ID
POSTHOG_ORGANIZATION_ID
```

Prefer the `AGENT_POSTHOG_*` names in examples. Accept PostHog's native env var
for quick local use, but mark direct env auth as less safe for LLM-guided setup.
Resolution order is: explicit flags, `AGENT_POSTHOG_*` environment variables,
profile defaults, PostHog-native environment variables, then built-in defaults
such as `https://us.posthog.com`. The namespaced `AGENT_POSTHOG_*` variables win
over profile metadata for organization, project, and environment, and
`AGENT_POSTHOG_BASE_URL` does for host, so an agent can repoint a configured
CLI (e.g. at a mock server) without editing the profile; `AGENT_POSTHOG_HOST`
and the PostHog-native `POSTHOG_*` variables stay below the profile as
quick-local-use defaults.

## Auth and profiles

Secrets go in the OS Keychain, and `credentials.json` is then only an index.
Where no keychain is available, the key is kept in that 0600 file instead; the
`auth add` receipt says which (`storage: keychain|file`), and `auth list`
reports where each profile's key really is (`credential:
keychain|file|missing`). If saving the profile fails after the key was stored,
the key is removed again, and `auth remove` still removes a profile whose key
is already gone.

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
      "environment_id": 456,
      "project_aliases": {"staging": 124, "prod": 123}
    }
  },
  "mcp_writes": false
}
```

`config get|set|unset|list` (lib-agent-cli's `ConfigCommand`) manage
`timeout_ms`, `max_retries`, and `mcp_writes` (`on`/`off`), each printing
`{key, value, set}`; `config show`, `config path`, and `config alias` stay
bespoke.

## Output contract

Default formats:

- list/search/query/investigation streams: NDJSON (`jsonl`)
- entity gets (`get <id>...`): NDJSON by default — one line per id (the record,
  or `{"@unresolved":{"id","reason","fixable_by","hint"?}}` for missing ids).
  `--format json|yaml` collapses to one `{"data":[…],"@unresolved":[…]}`
  envelope, even for a single id.
- `--format yaml` allowed for humans; every command honors `--format`,
  including `auth`, `config`, and `usage`
- `--full` returns unshaped API payloads where possible
- read output (lists, gets, queries) is null-pruned and secret-redacted;
  `--expose <field>` reveals a redacted field. Flag write receipts and exported
  specs print exactly, since their nulls are meaningful

**Get contract.** `get <id>...` takes 1..N ids and emits one stdout line per
input in order. Item-level misses (not found, bad key) → `@unresolved` line on
stdout, exit 0. Command-level failures (auth, permissions, network, zero args,
or an organization/project/environment the command needs but the profile does
not set, checked once before any fetch) → structured `{error}` on stderr, empty
stdout, exit 1. This is the only use of stderr/exit 1. `--timeout` bounds the
whole command.

NDJSON should use data rows plus `@` meta rows:

```jsonl
{"type":"person","id":123,"distinct_ids":["u_1"],"email":"a@example.com","last_seen_at":"2026-05-30T12:00:00Z"}
{"@pagination":{"has_more":true,"next_url":"https://us.posthog.com/api/..."}}
{"@query":{"columns":["event","count"],"elapsed_ms":124}}
```

Under `--format json|yaml` a list is one document with the same meta keys, as
lib-agent-output's `WriteList` renders it for the whole family:
`{"data":[…],"@pagination":{"has_more":true,"next_url":"…"}}`.

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
- any of these in request URLs, including secret-named query parameters and
  keys inside an encoded next-page URL
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
agent-posthog schema events list --search signup --exclude-hidden --limit 100
agent-posthog schema events get "$pageview"
agent-posthog schema properties list --type event --event "$pageview" --search browser
agent-posthog schema properties list --type person
```

`schema events list --search` is client-side filtering over the event definitions page
because PostHog's documented endpoint does not expose a general search parameter.
`schema properties list --event` maps to PostHog's `event_names` parameter with event
name filtering enabled.

### Query

HogQL should be first-class and streamable.

```bash
agent-posthog query hogql "select event, count() from events where timestamp > now() - interval 7 day group by event order by count() desc limit 20"
agent-posthog query hogql --file query.sql --format jsonl
agent-posthog query hogql --async "select ..."
agent-posthog query json --body query.json
agent-posthog query json --json '{"query":{"kind":"HogQLQuery","query":"select 1"}}'
agent-posthog query get <query-id>
agent-posthog query wait <query-id>
agent-posthog query cancel <query-id>
```

For tabular responses, emit one NDJSON row per result and include a meta row for
columns/types. If the API returns async query IDs, surface them in `@query` and
offer the exact follow-up command in an error or meta hint.

`query json` reads its body from `--body <file|->` (stdin by default) or inline
`--json`, for callers such as MCP that have no stdin or shared filesystem.
`query wait` polls until the query completes or fails; a failed query prints
its status on stdout and exits 1 with PostHog's error (`fixable_by: agent`), so
an agent never reads a HogQL error as success. `query cancel` is a DELETE and
counts as a write.

### Persons

```bash
agent-posthog persons list --email user@example.com
agent-posthog persons list --distinct-id user_123
agent-posthog persons get <person-id>
agent-posthog persons activity <person-id>
```

Compact person output should include ID, uuid, name, selected distinct IDs, email
if present, created/last-seen timestamps, and a pruned properties map.

### Insights and dashboards

```bash
agent-posthog insights list
agent-posthog insights get <insight-id>
agent-posthog dashboards list
agent-posthog dashboards get <dashboard-id>
agent-posthog dashboards run <dashboard-id>
```

`dashboards run` currently prints PostHog's `run_insights` response as one
compacted JSON object (`--full` for the whole payload). Streaming each
tile/insight result as its own row, with `type:"tile_error"` records for
per-tile failures, is still the goal for large dashboards; it is not built.

### Feature flags

```bash
agent-posthog flags list --active true --search checkout
agent-posthog flags get <id-or-key>
agent-posthog flags dependent <id-or-key>
agent-posthog flags activity <id-or-key>
```

The private feature flag get endpoint is ID-addressed. Key lookup should resolve
by listing/searching flags first, then fetching the matched numeric ID. Ambiguous
or missing key matches are agent-fixable errors with a hint to run `flags list`.

Writes follow one path: read the flag, build the minimal change, print a plan
(`outcome: planned`, leaf-level `changes`), and write only with `--yes`.
`--dry-run` forces a plan even with `--yes`, because the MCP bridge appends
`--yes` once the host has confirmed a tool call. Every PATCH sends `version`
and `original_flag`, PostHog's field-level conflict check; edits that address a
condition by index require `--if-version`, and rollout-only edits use the
strict `set_release_condition_rollout` endpoint. PostHog merges `filters`
shallowly, so the CLI sends only the sections that change.

```bash
agent-posthog flags create beta-gate --where "email icontains @example.com" --yes
agent-posthog flags apply --file spec.yaml --yes        # JSON/YAML spec; upsert by key
agent-posthog flags export beta-gate --format yaml
agent-posthog flags conditions add|set|remove|move ...
agent-posthog flags variants set checkout-config a=50 b=50 --yes
agent-posthog flags payload set checkout-config --variant a --json '{}' --yes
agent-posthog flags enable|disable|archive|unarchive|delete|restore ...
agent-posthog flags evaluate beta-gate --distinct-id u_1 --props-json '{"app_env":"staging"}'
agent-posthog flags test beta-gate --cases cases.yaml
agent-posthog flags diff beta-gate --against prod
agent-posthog flags copy beta-gate --to prod --yes
```

The spec and `--where` grammar live in `internal/flagspec` (pure, no HTTP).
Reading, planning, and writing flags against PostHog live in
`internal/featureflag`, which returns receipts, plans, and results and never
prints; `internal/cli` parses flags, calls it, and presents what comes back.
HogQL requests and async-query waits are `internal/query`.
Specs are presence-aware (an omitted field is left alone), decode numbers
losslessly, keep unknown condition and variant keys, and refuse encrypted
payload edits and remote-config conditions. Flags in PostHog's config version
2 format round-trip as a whole `filters_document`, which PostHog validates;
narrow edits refuse them. Evaluation errors are never reported as `false`.

Writes through MCP need a local opt-in: the `mcp` command marks its tool-call
subprocesses (`AGENT_POSTHOG_VIA_MCP`), and the API client's write guard
refuses every state-changing request under that mark unless a person ran
`config set mcp_writes on`. Connecting an agent therefore grants reads; plans and
`--dry-run` always work. TestMCPFlagWritesNeedLocalOptIn pins this through the
real bridge. Copying flags with encrypted payloads is supported
(PostHog re-encrypts server side); plans never include the payloads.

## Investigation commands

These are opinionated LLM workflows that gather multiple resources and emit
evidence records.

```bash
agent-posthog investigate user --email user@example.com
agent-posthog investigate user --distinct-id user_123 --limit 10
agent-posthog investigate event --event "$pageview" --limit 20
agent-posthog investigate flag checkout-v2
```

Evidence record contract:

```jsonl
{"type":"entity","entity":"person","id":"123","data":{}}
{"type":"query_result","name":"recent_events","data":{}}
{"type":"finding","severity":"warning","summary":"Flag is disabled in this environment","data":{}}
{"type":"next_step","command":"agent-posthog flags get checkout-v2 --full"}
```

Records print once the workflow has gathered them, one per line, or as
`{"data":[…]}` under `--format json|yaml`.

`investigate event` sends the event name in the HogQL query's `values` and
refers to it as `{event}`, so a name is never spliced into SQL text.

Built so far: `user` resolves persons and suggests `persons activity`; `event`
samples recent events; `flag` reports the definition, an inactive finding,
dependent flags, and activity. Workflows still to build toward:

1. `investigate user`: recent events, cohorts/flags if available, and session
   recording links.
2. `investigate event`: schema, volume trend, top properties.
3. `investigate flag`: recent `$feature_flag_called` events and optional user
   evaluation context.
4. A dashboard investigation: dashboard metadata, tile/insight results, stale
   or failing query hints.
5. Time windows: none of these take a time flag yet (see Time flags).

## Raw API escape hatch

Like `agent-stripe api`, include a constrained raw endpoint command for gaps:

```bash
agent-posthog api get /api/environments/456/persons/ --query email=a@example.com
agent-posthog api post /api/environments/456/query/ --body query.json
agent-posthog api post /api/environments/456/query/ --json '{"query":{"kind":"HogQLQuery","query":"select 1"}}'
```

Rules:

- require paths to start with `/api/`
- no custom `Authorization` header flag
- redact secrets in debug
- `post` reads its body from `--body <file|->` (`-` is stdin) or inline
  `--json`; with neither it sends `{}`
- allow `post` by default only for query endpoints; require `--yes` for other raw
  POST endpoints because many PostHog POST routes mutate state. Those go through
  the client's write path: never replayed after a `5xx`, and refused through MCP
  unless `config set mcp_writes on`
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

Not built yet: no command takes these flags today, so time windows go in HogQL
(`where timestamp > now() - interval 7 day`). The one time input that exists is
`flags evaluate --at` (and a test case's `at`), an ISO 8601 time.

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
  only `/flags`, for `flags evaluate`/`flags test`, using the project token read
  from the project (never printed); no `capture`.
- Whether mutating feature flag commands should require `--yes`. Current choice:
  every flag write plans by default and applies only with `--yes`.
