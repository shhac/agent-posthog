---
name: agent-posthog
description: |
  Investigate PostHog product analytics and write HogQL. Covers persons,
  events, event and property schema, feature flags, dashboards, insights,
  session recordings, experiments, and project/environment discovery. Use
  when debugging a user journey or funnel, writing or validating HogQL,
  looking up persons, distinct IDs, or event samples, or discovering PostHog
  organizations, projects, and properties.
allowed-tools: Bash(agent-posthog *) Bash(mockposthog *) Read Grep Glob
---

# agent-posthog

Use `agent-posthog` when investigating PostHog analytics, users, events, feature
flags, dashboards, recordings, experiments, or HogQL questions.

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
  `--dry-run` forces a plan even when `--yes` is present (the MCP bridge adds
  `--yes` once the host confirms a tool call, so use `--dry-run` to preview there).

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
```

Prefer `schema events list` and `schema properties list` before writing HogQL, so
queries use real event/property names.

## Output

**Get (single + multi).** `get <id>...` takes one or more ids and returns one
result per id, in input order. Default output is NDJSON: one line per id —
the record, or `{"@unresolved":{"id","reason","fixable_by","hint"?}}` for an id
that couldn't be resolved (e.g. not found / bad id). `--format json|yaml`
collapses to one `{"data":[…], "@unresolved":[…]}` envelope. A single
`get <id>` is just the one-element case (NDJSON one line by default; was pretty
JSON before — pass `--format json` for the object). Item-level misses stay on
stdout and exit 0; only a command-level failure (auth, network) goes to stderr
with exit 1 and empty stdout.

Lists, queries, and investigation commands default to NDJSON. Errors include
`fixable_by` and usually a `hint`. Full error shape: stderr JSON
`{"error":"...","fixable_by":"agent"|"human"|"retry","hint"?:"...","retry_after_seconds"?:N}`, exit 1.

Feature flag key lookup is CLI sugar: when a command accepts `<id-or-key>`, the
CLI resolves keys by listing/searching flags and then fetching the numeric ID.
A key that matches no flag (or matches multiple) yields an `@unresolved` line
(exit 0) rather than a command-level stderr failure.

## Feature flag writes

Writes read the flag, print a receipt, and only write with `--yes`:
`{"outcome":"planned|applied|unchanged","changes":[{"path","before","after"}],"base_version","version"?}`.
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
- An approval policy refusal is `fixable_by: human` and names the change
  request; a stale-version conflict is `fixable_by: agent`: re-read and decide again.
- A numeric argument is a flag ID; a flag whose key is numeric must be
  addressed by its ID.

### Create, apply, export

```bash
agent-posthog flags create beta-gate --where "email icontains @letsdothis.com" --yes
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
agent-posthog flags test beta-gate --cases cases.yaml        # or --cases-json '[...]'
agent-posthog flags reach beta-gate                          # users matching each condition
```

`evaluate` reports `enabled`, `variant`, `payload`, `reason`, and the matched
`condition`. The default `--engine public` calls PostHog's `/flags` as an SDK
would, using the project token it reads from the project (never printed) and
any `--props-json` overrides on top of stored properties. `--engine diagnostic`
uses stored properties only. Cloud `/flags` hosts are derived (`us.posthog.com`
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
`@summary`, and exits 1 unless every case passes. An `error` means PostHog could
not evaluate the flag; it is never a false result. Tests evaluate the **saved**
flag: a disabled flag is false for everyone. `distinct_id` defaults to
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
