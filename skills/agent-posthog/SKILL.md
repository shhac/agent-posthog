---
name: agent-posthog
description: |
  Investigate PostHog product analytics and manage feature flags. Covers
  persons, events, schema, HogQL, dashboards, insights, recordings,
  experiments, and flags: create, edit conditions/variants/payloads,
  archive/delete, evaluate, test, and copy staging to prod. Use when debugging
  a user journey or funnel, writing or validating HogQL, looking up persons or
  events, or setting up a feature gate or per-rule JSON config with if/else
  conditions, then testing and promoting it.
allowed-tools: Bash(agent-posthog *) Bash(mockposthog *) Read Grep Glob
---

# agent-posthog

Use `agent-posthog` when investigating PostHog analytics, users, events, feature
flags, dashboards, recordings, experiments, or HogQL questions, and when setting
up feature flags: gates (pass/fail by ordered rules) and dynamic config (rule →
JSON payload).

## Safety

- Never ask the tool to reveal a personal API key or project token.
- Never accept pasted PostHog API keys in chat. Ask the user to run
  `agent-posthog auth add <profile> --form` locally so the key goes directly into
  an OS dialog.
- Prefer read-only commands.
- Use `--project` explicitly when the profile default is unknown. The environment
  defaults to the project (a project's default environment shares its ID), so set
  `--env` only to target a non-default environment.
- Treat feature flag mutations as high stakes. Every flag write prints a plan
  and changes nothing unless you pass `--yes`; show the user the plan first.
  `--dry-run` forces a plan even when `--yes` is present.
- Over MCP, writes are **off** until a person runs
  `agent-posthog config set mcp_writes on` on the machine serving MCP (this covers
  every PostHog write: flag commands, `query cancel`, and `api post` outside
  query endpoints). A refused write is `fixable_by: human`;
  ask the user, never work around it. Once on, the bridge adds `--yes` after the
  host confirms a tool call, so a confirmed call applies: preview with
  `--dry-run` first. Plans work either way.

## Setup

```bash
agent-posthog auth list
agent-posthog auth add prod --form --host https://us.posthog.com
agent-posthog auth check prod
agent-posthog orgs list -p prod
agent-posthog projects list -p prod --org <org-id>
agent-posthog auth update prod --org <org-id> --project <project-id> --default
# Only if the project has multiple environments:
agent-posthog environments list -p prod --project <project-id>
agent-posthog auth update prod --env <env-id>
agent-posthog usage
```

`auth list` shows each profile's `credential`: `keychain`, `file` (no keychain
available), or `missing` (ask the user to re-run `auth add --form`).

For local testing:

```bash
mockposthog
AGENT_POSTHOG_BASE_URL=http://127.0.0.1:18118 POSTHOG_PERSONAL_API_KEY=phx_mock agent-posthog orgs list
```

## Common Commands

```bash
agent-posthog schema events list --search signup
agent-posthog schema events get "$pageview"
agent-posthog schema events get <id1> <id2>
agent-posthog schema properties list --event "$pageview"
agent-posthog query hogql "select event, count() from events group by event order by count() desc limit 20"
agent-posthog query hogql --async "select ..."     # returns query_status with an id
agent-posthog query wait <query-id>                # exits 1 if the query failed
agent-posthog query json --json '{"query":{"kind":"HogQLQuery","query":"select 1"}}'
agent-posthog persons list --email user@example.com
agent-posthog persons get <person-id>
agent-posthog persons get <id1> <id2>
agent-posthog flags list --search checkout
agent-posthog flags get checkout-v2
agent-posthog flags get <key1> <key2>
agent-posthog insights list
agent-posthog dashboards run <dashboard-id>
agent-posthog recordings list
agent-posthog experiments list
agent-posthog investigate user --email user@example.com
agent-posthog investigate event --event "$pageview" --limit 20
agent-posthog investigate flag checkout-v2
```

Prefer `schema events list` and `schema properties list` before writing HogQL, so
queries use real event/property names.

`query json` and `api post` take a body as `--json '<inline>'` (use this over
MCP) or `--body <file>`; `-` reads stdin, which is `query json`'s default.
`query wait` prints a failed query's status on stdout, then exits 1 with
PostHog's error. `api post` outside `/query/` paths needs `--yes`.

## Output

**Get (single + multi).** `get <id>...` takes one or more ids and returns one
result per id, in input order. Default output is NDJSON: one line per id —
the record, or `{"@unresolved":{"id","reason","fixable_by","hint"?}}` for an id
that couldn't be resolved (e.g. not found / bad id). `--format json|yaml`
collapses to one `{"data":[…], "@unresolved":[…]}` envelope, even for a single
id. Item-level misses stay on stdout and exit 0; a command-level failure (auth,
permissions, network, or a missing org/project/environment, checked before any
request) goes to stderr with exit 1 and empty stdout.

Lists print one NDJSON line per record, then `{"@pagination":{"has_more":true,"next_url":…}}`
when more remain (`--all` follows it); `--format json|yaml` gives one
`{"data":[…],"@pagination":{…}}` document.

Lists, queries, and investigation commands default to NDJSON; every command
(including `auth`, `config`, and `usage`) honors `--format`. Read output prunes
nulls and redacts secrets (tokens show as `[REDACTED]` with an `@redacted`
note); `--expose <field>` reveals one only when the user needs it. Errors
include `fixable_by` and usually a `hint`. Full error shape: stderr JSON
`{"error":"...","fixable_by":"agent"|"human"|"retry","hint"?:"...","retry_after_seconds"?:N}`, exit 1.
The CLI waits out short rate limits itself; a `Retry-After` over 30 seconds
comes back as `fixable_by: retry` with `retry_after_seconds`.

Feature flag key lookup is CLI sugar: when a command accepts `<id-or-key>`, the
CLI resolves keys by listing/searching flags and then fetching the numeric ID.
In `flags get`, a key that matches no flag (or matches multiple) yields an
`@unresolved` line (exit 0); single-flag commands (`export`, writes, `evaluate`)
report the same miss on stderr with exit 1.

## Feature flag writes

Writes read the flag, print a receipt, and only write with `--yes`:
`{"outcome":"planned|applied|unchanged","host","project_id","key","changes":[{"path","before","after"}],"base_version","version"?,"warnings"?}`.
Check `host` and `project_id` name the project you meant before passing `--yes`.
Each PATCH carries the version and the flag as read, so PostHog refuses it if
someone changed the same fields since. `--if-version N` refuses unless the flag
is still at version N.

```bash
agent-posthog flags disable checkout-v2            # plan
agent-posthog flags disable checkout-v2 --yes      # apply
agent-posthog flags enable|archive|unarchive <id-or-key> --yes
agent-posthog flags delete <id-or-key> --yes       # soft delete
agent-posthog flags restore <id> --yes             # deleted flags are found by ID only
agent-posthog flags early-exit <id-or-key> on --yes
```

- `archive` disables in the same write (PostHog requires it); `unarchive`
  leaves the flag disabled.
- To have a person review a change before it runs, save it:
  `--save-plan plan.json` writes the exact patch, and
  `flags apply-plan --file plan.json --yes` applies exactly that patch, refused
  if the flag changed since (edits to existing flags only).
- Plans warn when active flags depend on the one being disabled, archived, or
  deleted, and when an enable depends on a disabled flag.
- An approval policy refusal is `fixable_by: human` and names the change
  request; a stale-version conflict is `fixable_by: agent`: re-read and decide again.
- Flag references: `id:<n>` or `key:<k>` say which you mean. A bare number is
  an ID and anything else a key, so reach a flag whose key is a number with
  `key:2024`. `restore` takes an ID only (deleted flags are not listed).

### Create, apply, export

```bash
agent-posthog flags create beta-gate --where "email icontains @example.com" --yes
agent-posthog flags create checkout-config --type multivariate --variant debug=0 --variant default=100 --yes
agent-posthog flags create site-config --type remote_config --payload-json '{"banner":"hi"}' --yes
agent-posthog flags export checkout-config --format yaml > spec.yaml   # edit, then:
agent-posthog flags apply --file spec.yaml           # plan; add --yes to apply
agent-posthog flags apply --spec-json '{"key":"beta-gate","active":true}' --yes
```

New flags are created **disabled** unless `--active` / `active: true`.
`apply` creates the flag if its key is new, otherwise it changes only the
fields the spec names. A spec:

```yaml
schema_version: 1
key: checkout-config
type: multivariate          # boolean (gate) | multivariate | remote_config (one payload for all)
active: true
early_exit: true            # a matching 0% condition denies instead of falling through
conditions:                 # top to bottom; the first match wins
  - where: ["email icontains @contractor.com"]
    rollout: 0              # deny (needs early_exit)
  - where: ["app_env = staging"]
    variant: debug
  - variant: default        # no where = everyone (the else branch)
variants: [{key: debug, rollout: 0}, {key: default, rollout: 100}]
payloads: {debug: {logLevel: debug}, default: {logLevel: warn}}
# payload: {...}            # boolean / remote_config: the one payload
```

`--where` is `<key> <op> <value>`. Ops: `=` `!=` `>` `>=` `<` `<=` `icontains`
`not_icontains` `regex` `not_regex` `is_set` `is_not_set` `is_date_before`
`is_date_after`. The value is JSON when it parses as JSON (`["a@x.com","b@x.com"]`,
`5`, `true`, `"true"`), otherwise a bare string. `cohort in <id>` / `cohort not_in <id>`
targets a cohort; `flag:<key> = true|false|<variant>` depends on another flag;
`person:<name>` targets a person property named like a keyword. Other property
types (group properties) go in a spec as raw PostHog property objects.

Flags stored in PostHog's newer **config version 2** format export a
`filters_document` instead of conditions/variants/payloads. Edit that document
and `apply` it back whole; PostHog validates it and admits these writes per
project. Narrow edits refuse v2 flags; enable, disable, archive, and delete work.

Rule → JSON config is a **multivariate** flag: conditions pick a variant with
`variant:`, and each variant carries its payload. `remote_config` serves one
payload to everyone and ignores conditions.

### Narrow edits

```bash
agent-posthog flags conditions list beta-gate          # index + version per condition
agent-posthog flags conditions add beta-gate --where "app_env = staging" --yes   # appends
agent-posthog flags conditions add beta-gate --where "email = qa@x.com" --at 0 --if-version 3 --yes
agent-posthog flags conditions set beta-gate 1 --rollout 25 --if-version 3 --yes
agent-posthog flags conditions set beta-gate 0 --where "plan = pro" --variant debug --if-version 4 --yes
agent-posthog flags conditions move beta-gate 2 0 --if-version 5 --yes   # <to> is the final index
agent-posthog flags conditions remove beta-gate 1 --if-version 6 --yes
agent-posthog flags variants set checkout-config debug=0 default=100 --yes
agent-posthog flags payload set checkout-config --variant debug --json '{"logLevel":"debug"}' --yes
agent-posthog flags payload get|unset <id-or-key> [--variant v]
```

Edits that address a condition by index (`set`, `remove`, `move`, `add --at`)
need `--if-version` from your last `conditions list`, so a reorder by someone
else cannot make you edit the wrong condition. A rollout-only `set` uses
PostHog's strict endpoint. `--where` on `set` replaces that condition's
filters; `--clear-where` makes it match everyone; `--no-variant` drops the
override. `variants set` drops payloads of removed variants and refuses while
a condition still overrides a removed variant. Encrypted payloads are never
edited.

### Evaluate and test

```bash
agent-posthog flags evaluate beta-gate --distinct-id u_1 --props-json '{"app_env":"staging"}'
agent-posthog flags evaluate beta-gate --distinct-id user_123 --engine diagnostic
agent-posthog flags evaluate beta-gate --distinct-id user_123 --engine diagnostic --at 2026-04-29T15:30:00Z
agent-posthog flags test beta-gate --cases cases.yaml        # or --cases-json '[...]'
agent-posthog flags reach beta-gate                          # users matching each condition
```

`evaluate` reports `enabled`, `variant`, `payload`, `reason`, and the matched
`condition`. The default `--engine public` calls PostHog's `/flags` as an SDK
would, using the project token it reads from the project (never printed) and
any `--props-json` overrides on top of stored properties (plus `--device-id`
and `--evaluation-context` as an SDK would send them). `--engine diagnostic`
uses stored properties only, and with `--at` (or a case's `at`) replays the
evaluation with the flag and person as they were at that time. Cloud `/flags` hosts are derived (`us.posthog.com`
→ `us.i.posthog.com`); set `--flags-host` or `AGENT_POSTHOG_FLAGS_HOST` for
self-hosted proxies.

A test suite is a list of cases; only the fields under `expect` are checked,
and `payload` must match exactly:

```yaml
- name: contractor in staging is denied
  props: {email: pat@contractor.com, app_env: staging}
  expect: {enabled: false, condition_index: 0}
- name: staging gets debug config
  props: {app_env: staging}
  expect: {variant: debug, payload: {logLevel: debug}}
```

It prints one line per case (`pass`, `fail` with mismatches, or `error`), then
`@summary`, and exits 1 unless every case passes. `--parallel N` (up to 8)
evaluates N cases at once; output stays in case order, and `--timeout` bounds
the whole suite. An `error` means PostHog could
not evaluate the flag; it is never a false result. Tests evaluate the **saved**
flag: a disabled flag is false for everyone. A change takes a few seconds to
reach `/flags`, so a suite run right after a write may report "did not return
this flag" errors; wait a moment and run it again. `distinct_id` defaults to
`agent-posthog-test:<name>`, keeping percentage rollouts stable.

`reach` counts users whose properties pass each condition's filters before
rollout; conditions overlap, so the counts do not add up to the audience.

## Staging recipes

**One project + a property.** Add a condition such as `app_env = staging` and
have the app send that property when it evaluates flags (`personProperties` /
`setPersonPropertiesForFlags`). An event property does not reach flag
evaluation. Test with `--props-json '{"app_env":"staging"}'` and include a case
with no `app_env` so a missing property cannot accidentally pass. The property
is not a security boundary: clients choose what they send.

**Separate staging project.** PostHog flags belong to a project, not an
environment, so staging needs its own project. Name both in your profile:

```bash
agent-posthog config alias set staging <staging-project-id>
agent-posthog config alias set prod <prod-project-id>
agent-posthog --project staging flags apply --file spec.yaml --yes
agent-posthog --project staging flags test beta-gate --cases cases.yaml
agent-posthog --project staging flags diff beta-gate --against prod
agent-posthog --project staging flags copy beta-gate --to prod          # plan
agent-posthog --project staging flags copy beta-gate --to prod --yes    # copy
```

`copy` uses PostHog's own copy: it plans per target (`create` or `overwrite`
with the differences), refuses to overwrite without `--overwrite`, copies
scheduled changes only with `--copy-schedule`, matches
cohorts by name, and does not copy tags, evaluation contexts, or archived state.
Its plan is advisory, since PostHog cannot check a target is unchanged since
the plan. Flags with encrypted payloads can be copied (PostHog re-encrypts
them for the target); plans never show the payloads. Targets that fail or wait on approval come back in `failed[]` and
exit 1. `--project` takes an ID or an alias from the current profile.
