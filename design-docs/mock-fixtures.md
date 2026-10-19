# mockposthog fixture plan

`mockposthog` is the deterministic E2E fixture server for `agent-posthog`.
It should model useful PostHog behavior, not the entire product.

Sources checked on 2026-05-31:

- https://posthog.com/docs/api
- https://posthog.com/docs/api/query
- https://posthog.com/docs/api/feature-flags
- https://posthog.com/docs/api/session-recordings
- https://posthog.com/docs/api/experiments

## Scenario controls

Use request paths, query parameters, and fake token values to trigger edge cases:

- `Authorization: Bearer phx_invalid`: 401 invalid personal API key
- `Authorization: Bearer phx_no_scope`: 403 missing scope for non-`/users/@me/`
- `GET /api/mock/rate_limit`: 429 with `Retry-After`
- `GET /api/mock/validation_error`: 400 validation error with `attr`
- `flags get missing-flag`: key lookup miss
- `flags get ambiguous-flag`: multiple key matches
- `orgs list --limit 1`: paginated response with absolute `next` URL
- `api post /api/environments/456/query/ --body async.json`: async query creation
- `query get query_pending`: incomplete async query
- `query get query_failed`: completed async query with error
- `query get query_complete`: completed async query with results
- `query log query_complete`: archived query log details
- `api get /api/projects/123/session_recordings/rec_1/sharing/`: sharing payload with redacted access token

Feature flags live in a stateful per-server store (`internal/mockposthog/flagstore.go`),
so a test can create, edit, and evaluate a flag across several CLI invocations
against one server:

- projects `123` (default) and `124` (a separate staging project); each has a
  project token (`phc_mock_project_<id>`) on `GET /api/projects/<id>/`
- PATCH merges `filters` shallowly and detects stale writes from `version` +
  `original_flag` the way PostHog does (field overlap only)
- `approval-flag` (26): any PATCH returns the approval-policy 409
- `policy-flag` (27): any PATCH returns the 400 `policy_conflict`
- `v2-flag` (29): config version 2 filters; PATCH replaces the document whole
  with a strict version check, and creates must be disabled
- `legacy-flag` (30): predates versioning, so its `version` is null
- project `126` requires a tag on every new flag
- `/flags` for distinct ID `mock-quota` reports the project as quota-limited
- `copy_flags` without `copy_dependencies` drops flag dependencies with a warning
  and disables the copy
- `secret-config` (28): remote config with encrypted payloads, which the CLI
  must refuse to rewrite or export
- archiving an active flag is a 400; DELETE is a 405; delete is `deleted: true`
- `POST /flags?v=2` evaluates with the project token, no personal key. Its
  evaluator supports only person properties with `exact`, `is_not`, `icontains`,
  `is_set`, `is_not_set`, and rollouts of 0 or 100; anything else marks that
  flag `failed`. Distinct ID `user_123` carries the fixture person's properties;
  `mock-error` returns `errorsWhileComputingFlags: true`.
- project `125` is readable but `copy_flags` to it comes back approval-pending inside the HTTP 200

## Fixture values

Stable IDs:

- organization: `org_1`
- project: `123`
- environment: `456`
- person: `44`
- feature flag: `22`, key `checkout-v2` (also `23`/`24` sharing key `ambiguous-flag`)
- session recording: `rec_1`
- dashboard: `66`
- experiment: `33`

Stable analytics schema:

- events: `$pageview`, `signup completed`, `checkout started`
- event properties: `$browser`, `$current_url`
- person properties: `email`, `plan`

## Boundaries

- Session recording fixtures expose metadata and sharing configuration only.
  PostHog documents that the API does not provide raw replay JSON exports.
- Raw POST mutation fixtures should stay narrow. Use typed commands for real
  mutation workflows once the CLI supports them.
- Secrets in fixture responses are intentional only when they test redaction.
  Keep them fake and recognizable, such as `phs_recording_share_secret`.
