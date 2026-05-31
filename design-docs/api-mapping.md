# PostHog API mapping

Sources checked on 2026-05-31:

- https://posthog.com/docs/api
- https://posthog.com/docs/api/organizations
- https://posthog.com/docs/api/projects
- https://posthog.com/docs/api/environments
- https://posthog.com/docs/api/query
- https://posthog.com/docs/api/events
- https://posthog.com/docs/api/persons
- https://posthog.com/docs/api/insights
- https://posthog.com/docs/api/dashboards
- https://posthog.com/docs/api/feature-flags
- https://posthog.com/docs/api/event-definitions
- https://posthog.com/docs/api/property-definitions

## API fundamentals

Private endpoints use:

```text
Authorization: Bearer <personal-api-key>
<ph_app_host>/api/...
```

PostHog Cloud app hosts:

```text
US: https://us.posthog.com
EU: https://eu.posthog.com
Self-hosted: user-provided instance URL
```

Public endpoints use project tokens and ingestion hosts such as
`https://us.i.posthog.com` or `https://eu.i.posthog.com`. Do not use public
project-token endpoints in the first read-focused CLI unless explicitly needed.

## Rate limits and retry policy

PostHog documents separate private API limits:

- analytics endpoints: `240/minute`, `1200/hour`
- `events/values`: `60/minute`, `300/hour`
- query endpoint: `2400/hour`
- CRUD endpoints: `480/minute`, `4800/hour`
- feature flag local evaluation: `600/minute`

CLI behavior:

- retry `429`, `502`, `503`, and `504` with bounded exponential backoff
- respect `Retry-After` when present
- after retries, emit `fixable_by: retry`
- include endpoint family and limit hints when known

## Pagination

Most list responses use:

```json
{
  "next": "<url-or-null>",
  "previous": "<url-or-null>",
  "results": []
}
```

CLI behavior:

- default to one page unless `--all` is set
- support `--limit` and `--page-limit`
- stream each result as an NDJSON row
- emit `{"@pagination":{...}}` with `next_url` when more data exists
- if `--all`, follow `next` URLs exactly

## Resource mapping

| CLI command | API endpoint | Scope | Required scope |
|---|---|---|---|
| `orgs list` | `GET /api/organizations/` | profile | `organization:read` |
| `orgs get` | `GET /api/organizations/:id/` | profile | `organization:read` |
| `projects list` | `GET /api/organizations/:organization_id/projects/` | org | `project:read` |
| `projects get` | `GET /api/organizations/:organization_id/projects/:id/` | org | `project:read` |
| `environments list` | `GET /api/projects/:project_id/environments/` | project | `project:read` |
| `environments get` | `GET /api/projects/:project_id/environments/:id/` | project | `project:read` |
| `schema events` | `GET /api/projects/:project_id/event_definitions/` | project | `event_definition:read` |
| `schema event get` | `GET /api/projects/:project_id/event_definitions/:id/` or `by_name` | project | `event_definition:read` |
| `schema properties` | `GET /api/projects/:project_id/property_definitions/` | project | `property_definition:read` |
| `query hogql` | `POST /api/environments/:environment_id/query/` | environment | `query:read` |
| `query get` | `GET /api/environments/:environment_id/query/:id/` | environment | `query:read` |
| `query cancel` | `DELETE /api/environments/:environment_id/query/:id/` | environment | `query:read` |
| `persons list` | `GET /api/environments/:environment_id/persons/` | environment | `person:read` |
| `persons get` | `GET /api/environments/:environment_id/persons/:id/` | environment | `person:read` |
| `persons activity` | `GET /api/environments/:environment_id/persons/:id/activity/` | environment | `person:read` |
| `insights list` | `GET /api/environments/:environment_id/insights/` | environment | likely insight/query read |
| `insights get` | `GET /api/environments/:environment_id/insights/:id/` | environment | likely insight/query read |
| `insights analyze` | `GET /api/environments/:environment_id/insights/:id/analyze/` | environment | likely insight/query read |
| `dashboards list` | `GET /api/environments/:environment_id/dashboards/` | environment | `dashboard:read` |
| `dashboards get` | `GET /api/environments/:environment_id/dashboards/:id/` | environment | `dashboard:read` |
| `dashboards run` | `GET /api/environments/:environment_id/dashboards/:id/run_insights/` | environment | `dashboard:read` |
| `flags list` | `GET /api/projects/:project_id/feature_flags/` | project | `feature_flag:read` |
| `flags get` | `GET /api/projects/:project_id/feature_flags/:id/` | project | `feature_flag:read` |
| `flags dependent` | `GET /api/projects/:project_id/feature_flags/:id/dependent_flags/` | project | `feature_flag:read` |
| `flags activity` | `GET /api/projects/:project_id/feature_flags/:id/activity/` | project | `feature_flag:read` |
| `flags create` | `POST /api/projects/:project_id/feature_flags/` | project | `feature_flag:write` |
| `flags update` | `PATCH /api/projects/:project_id/feature_flags/:id/` | project | `feature_flag:write` |
| `flags disable` | `PATCH /api/projects/:project_id/feature_flags/:id/` | project | `feature_flag:write` |

## Deprecated events API

PostHog marks the events API as deprecated for export. It should not be a primary
CLI surface.

Use HogQL through the query endpoint instead:

```sql
select
  event,
  timestamp,
  distinct_id,
  properties
from events
where timestamp > now() - interval 1 day
order by timestamp desc
limit 20
```

The CLI can include an `api get /api/.../events/` escape hatch, but user-facing
commands should say:

```json
{
  "error": "The PostHog events API is deprecated for exports",
  "fixable_by": "agent",
  "hint": "Use 'agent-posthog query hogql ...' for ad-hoc event reads or batch exports for large exports."
}
```

## Error mapping

PostHog API errors commonly include:

```json
{
  "type": "authentication_error",
  "code": "invalid_personal_api_key",
  "detail": "Invalid Personal API key."
}
```

Mapping:

- `401` with `invalid_personal_api_key`: `fixable_by: human`
- `403` or missing scope text: `fixable_by: human`
- `404`: `fixable_by: agent`, hint to list resources in the current scope
- `400` validation errors: `fixable_by: agent`, include `attr` when present
- `429`: retry, then `fixable_by: retry`
- `5xx`: retry, then `fixable_by: retry`

Useful hints should mention the current resolved profile, host, organization,
project, and environment without printing secrets.

## First implementation slice

The minimum useful CLI should include:

1. `auth add --form`, `auth check`, `auth list`, `auth update`
2. `orgs list`
3. `projects list`
4. `environments list`
5. `schema events`
6. `schema properties`
7. `query hogql`
8. `persons list/get`
9. `flags list/get`
10. `usage`

This gives an LLM enough to authenticate safely, discover IDs, understand the
event/property model, run analytics queries, inspect users, and read flag state.
