# Go API

The Go service is the authority for user identity and incident changes. The Chi
router is wired in [`backend/go/cmd/api/main.go`](../backend/go/cmd/api/main.go),
with handlers in `internal/httpapi`, auth and session logic in `internal/auth` and
`internal/sessionstore`, and separate pgx pools for the two databases.

| Contract | Responsibility |
| --- | --- |
| [Foundation](../api/go-foundation.openapi.yaml) | Process liveness and connectivity readiness |
| [Authentication](../api/go-authentication.openapi.yaml) | Login, current user, and logout |
| [Incident intake](../api/go-incidents.openapi.yaml) | Create, scoped list, and detail |
| [Incident workflows](../api/go-handoff.openapi.yaml) | Assignment, transitions, archive, comments, history, notifications, administration, and AI feedback |

Direct Go routes are unprefixed; Caddy exposes them under `/api`. The private AI
worker routes are not public. JSON uses UUIDs, `snake_case` fields, UTC/RFC 3339
timestamps, and the error envelope `{"error":{"code":"…","message":"…"}}`.
Responses are no-store. Logs use route templates and request IDs, not report text,
raw URLs, or credentials. HTTP DTOs do not serialize persistence models.

## Sessions and request ordering

- Passwords are Argon2id hashes. SCS manages opaque host-only HttpOnly cookies,
  backed by the identity database. Defaults are an eight-hour absolute lifetime and
  30 minutes of inactivity. Deployment uses Secure cookies; plain HTTP requires
  explicit development settings. Trusted browser origins are exact, and CORS does
  not replace CSRF or authorization checks.
- Every protected request checks the current role, blocked state, and `auth_version`.
  Password reset, role changes, and block/unblock invalidate existing sessions.
  Login limiting and password-verification concurrency limits are per process.
- The revocation-safe store inserts new tokens and updates existing tokens only
  while present and unexpired. An upsert could recreate a concurrently revoked token.
  Preserve request state across SCS persistence when changing this code.
- Auth responses are buffered until session persistence finishes. Only successful
  explicit login or logout may set/delete the cookie; passive reads and failed auth
  requests must not replace it. The cookie lifetime reflects the absolute deadline;
  idle expiry is enforced server-side.
- Incident and other protected business routes validate and refresh the session
  **before** their operation. They do not emit cookies or save the session afterward.
  This avoids a late refresh error after a committed business write. Revocation
  cannot retract an already-authorized in-flight operation or response.

## Workflow and service boundaries

The [domain specification](domain-specification.md) describes roles, visible fields,
and transitions. The incident database stores state, public/internal comments,
append-only action history, and notifications. Comments and history return the
latest 500 visible entries; notifications return the latest 100. These feeds
do not paginate. A create transaction includes the first
assessment job; a worker result never updates incident fields. An authorized human
can record one decision per real assessment and update the category through Go.

Go runs resolution maintenance at startup and hourly to close eligible
seven-day-old resolutions. Account administration has a separate audit trail in the
identity database. Assignment eligibility is read from identity before the incident
transaction; this is not an atomic operation across databases.

The AI API delegates browser authorization to Go's current-user and incident detail
handlers. The worker uses separate bearer-authenticated GETs for credential preflight
and for the saved snapshot tied to the job. See [Python AI](ai-backend.md).

`/healthz` checks the process; `/readyz` checks both database connections, not schema
versions or AI. Startup never bootstraps, migrates, or seeds. Use Go `1.27.4` and
`make go-build` for the API, migration, and seed binaries. See the
[local runbook](docker-setup.md#host-go) and
[database integration checks](database-setup.md#integration-tests).
