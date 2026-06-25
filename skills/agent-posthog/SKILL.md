---
name: agent-posthog
description: |
  Investigate PostHog product analytics, HogQL, persons, events, event/property schema, feature flags, dashboards, insights, session recordings, experiments, and project/environment discovery. Use when:
  - Debugging product analytics or user journeys in PostHog
  - Writing or validating HogQL
  - Looking up persons, distinct IDs, event samples, feature flags, dashboards, insights, recordings, or experiments
  - Discovering PostHog organizations, projects, environments, events, or properties
  Triggers: "posthog", "hogql", "product analytics", "feature flag", "session replay", "session recording", "dashboard", "insight", "funnel", "person properties", "distinct id"
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
- Treat feature flag mutations as high stakes.

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
