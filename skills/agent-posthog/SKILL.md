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
- Use `--project` and `--env` explicitly when the profile defaults are unknown.
- Treat feature flag mutations as high stakes.

## Setup

```bash
agent-posthog auth list
agent-posthog auth add prod --form --host https://us.posthog.com
agent-posthog auth check prod
agent-posthog orgs list -p prod
agent-posthog projects list -p prod --org <org-id>
agent-posthog environments list -p prod --project <project-id>
agent-posthog auth update prod --org <org-id> --project <project-id> --env <env-id> --default
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
agent-posthog schema properties list --event "$pageview"
agent-posthog query hogql "select event, count() from events group by event order by count() desc limit 20"
agent-posthog persons list --email user@example.com
agent-posthog persons get <person-id>
agent-posthog flags list --search checkout
agent-posthog flags get checkout-v2
agent-posthog insights list
agent-posthog dashboards run <dashboard-id>
agent-posthog recordings list
agent-posthog experiments list
```

Prefer `schema events list` and `schema properties list` before writing HogQL, so
queries use real event/property names.

## Output

Lists, queries, and investigation commands default to NDJSON. Single resources
default to JSON. Errors include `fixable_by` and usually a `hint`.

Feature flag key lookup is CLI sugar: when a command accepts `<id-or-key>`, the
CLI resolves keys by listing/searching flags and then fetching the numeric ID.
