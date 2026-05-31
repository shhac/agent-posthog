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

## Fixture values

Stable IDs:

- organization: `org_1`
- project: `123`
- environment: `456`
- person: `44`
- feature flag: `22`, key `checkout-v2`
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
