# Authentication Handoff — Backend Increment

## 1. Delivered scope

Implemented in `feat/go-authentication`: login/logout/current-user HTTP endpoints,
PostgreSQL-backed SCS sessions, Argon2id verification, fresh account checks, session
revocation, secure cookie defaults, explicit CORS/CSRF policy, bounded login attempts
and password-check concurrency, and unit/PostgreSQL integration tests.

No new schema migration is needed: existing `users`, `sessions`, grants and sqlc queries
are reused. Frontend, public registration, administrative account APIs, MFA, password
recovery, JWT, incident access policies, and internal Python authorization endpoints
are **not** implemented in this increment.

The machine-readable contract is [authentication OpenAPI](../api/go-authentication.openapi.yaml).
The [foundation handoff](architect-handoff.md) still defines bootstrap/build/network
ownership. This file adds authentication-specific tasks, not a second login design.

## 2. API and session behavior

| Endpoint | Success | Expected failures |
| --- | --- | --- |
| `POST /auth/login` | 200 with safe `user` and a renewed cookie | 400 invalid JSON/fields; 401 invalid credentials; 403 CSRF; 413 body limit; 415 media type; 429 rate limit; 503 dependency/capacity |
| `GET /auth/me` | 200 with current safe `user` | 401 missing/expired/revoked identity; 503 dependency |
| `POST /auth/logout` | 204, session deleted and cookie expired | 403 CSRF; 503 if session storage cannot be read/updated |

Login accepts `{"username":"employee_a","password":"..."}` as application/json.
Only these fields are allowed; one JSON value, maximum 4096-byte body. Both strings
are 1–128 UTF-8 bytes. Username is trimmed/lowercased then checked against the existing
3–64 ASCII policy. Password is never trimmed. Unknown users, wrong passwords, blocked
accounts and unsupported hashes share `401 invalid_credentials` / "Invalid username
or password." Do not surface a different message for an existing or blocked account.

Login and `/me` return only:

```json
{"user":{"id":"UUID","username":"employee_a","role":"employee"}}
```

No password hash, auth version, token or session payload enters response JSON. Errors
use the existing `{"error":{"code":"...","message":"..."}}` envelope. Auth responses
are non-cacheable and carry the request ID from the common HTTP middleware. JSON
responses are buffered (maximum 16 KiB) until session persistence finishes: a failed
commit must not expose a login success body/cookie or authenticated `/me` data.

Logout is idempotent without an active session, but never reports success after a
store failure. A failed logout must be retried; do not imply that a server session was
revoked merely because a frontend cleared its own state.

### Identity and revocation

- SCS stores the SHA-256-derived token identifier, not the raw browser cookie.
- Session values contain only user UUID and `auth_version`, not a trusted cached role.
- Login renews the token and clears previous session values. Old supplied tokens fail.
- Every protected `/me` request loads current account status, role and auth version.
- Password reset, blocking/unblocking and role-change sqlc queries increment the
  version; an old session is rejected and removed when possible. Role changes require
  a fresh login. Unblocking never restores old sessions.
- Login rechecks the account after password verification to avoid creating a new
  session from a snapshot invalidated during the expensive hash operation.
- No incident-level authorization is implied by successfully calling `/auth/me`.
- Backend restart preserves sessions when the same database and cookie policy remain.

### Concurrent refresh, logout and rotation

Architect review identified that pgxstore's default commit upsert could recreate a
session deleted by a concurrent logout or token rotation. Production now uses
`internal/sessionstore`, a context-aware SCS persistence adapter:

- SCS still generates/hashes tokens, serializes values and computes session expiry.
  The HTTP response policy below controls when its cookie writer may emit a cookie.
- Request-local bookkeeping distinguishes loaded tokens from newly generated tokens.
  Install `sessionstore.WithRequest` outside SCS `LoadAndSave`, as `withSession` does;
  missing request state fails closed. It must not be reset between load and save.
- A new token uses INSERT without upsert; collisions fail without overwriting data.
- A loaded token uses UPDATE only while its row exists and has not expired. PostgreSQL
  orders UPDATE against DELETE; a zero-row update is a revoked session, never an INSERT.
- The adapter retains pgxstore's context-aware lookup/delete operations with cleanup
  disabled. It replaces only the unsafe commit behavior. No migration/grant change,
  process-wide session map, tombstones or distributed lock service is needed.
- If revocation wins before a delayed refresh, the buffered response becomes
  `401 authentication_required`, without user data or **any** Set-Cookie. A stale
  response must not replace or delete a fresh cookie from a concurrent login.
- SQL/deadline errors still produce 503, not 401 or false success. Other independent
  sessions of the same account are unaffected. Already authorized in-flight work is
  not retroactively canceled; the guarantee is that stale saves cannot restore access.

Deterministic regression tests pause a refresh after its successful lookup, complete
logout or rotation on another manager, then release the refresh. Both cases failed
before the fix (the old cookie returned 200). PostgreSQL tests use independent stores
and connection pools to verify the same behavior without shared process locks, check
that the old row is absent, and check both the new token and an independent session.

### Browser response ordering and cookie policy

A second review found a separate client-side race: a stale `/me` missing-token 401
could delete a fresh login cookie. Cookies are replaced/deleted by name/domain/path,
not by comparing their previous value. Suppressing only the explicit deletion was
insufficient: SCS automatic deletion, a completed 200 refresh, and a failed login
with an existing session could also overwrite a new login cookie on late delivery.

The auth response buffer now discards all automatic SCS Set-Cookie headers. After
successful persistence, only explicit successful login/logout handlers authorize
one replacement/deletion cookie. `/me` responses (including 200, 401, 503) and failed
login/logout responses never emit Set-Cookie. Server-side deletion of invalid identity
sessions still occurs; cookie suppression does not weaken revocation.

Login issues a persistent cookie until SCS's **absolute deadline** (8h by default),
not its current idle expiry. This avoids losing an active browser session at the first
30m idle interval now that passive requests no longer rewrite cookies. PostgreSQL
still rejects sessions after 30m inactivity, refreshes idle expiry on activity, and
never extends the absolute deadline. A cookie may remain in the browser after server
expiry: handle 401 regardless of cookie presence, and clear frontend identity state.

Cookie-jar regression tests cover a lookup paused **before** storage, a completed
successful `/me` delivered late, a failed login delivered late, and SCS destruction
of an invalid identity. Explicit cases include incorrect-password 401, invalid-body
400, and old-account-version 401. Apply the new login response first, then the stale response;
the fresh cookie must survive and authenticate. Tests cover both cookie environments
and, separately, independent PostgreSQL pools/managers.

See [authentication security review](authentication-security-review.md) for the
review matrix, evidence and remaining deployment/frontend responsibilities. This
does not impose an ordering on two explicit concurrent login/logout operations;
the frontend must serialize those mutations and ignore obsolete identity responses.

### Operational limits

- Absolute lifetime: 8h; inactivity timeout: 30m. Activity refreshes idle expiry only.
- Five POST login attempts per peer per one-minute fixed window, including successes
  and malformed attempts. State is capped at 10,000 peer entries and cleaned lazily.
  Capacity exhaustion also fails closed with 429. This is process-local and resets
  on restart; it is not distributed protection or a permanent account lockout.
- At most two password checks run concurrently; excess requests receive 503 auth_busy
  and Retry-After: 1. Unknown users perform dummy hash work to reduce simple timing
  enumeration; identical timing is not promised.
- Stored hash verification caps memory at 128 MiB, iterations at 10, salt/key sizes
  at 64 bytes, and validates nonzero lanes/work before deriving. Existing library-default
  hashes with host-dependent lanes remain supported. Password verification itself
  is CPU work, not instantly interruptible by a canceled request.
- Auth load/query/save operations share DATABASE_TIMEOUT. Session-store/user-query
  failures return safe 503 responses and never bypass authentication.
- pgxstore's unbounded/raw-logging built-in cleaner is disabled. A lifecycle-bound
  five-minute maintenance task deletes only expired sessions with DATABASE_TIMEOUT
  and safe logging. SCS owns serialization; the adapter above owns safe persistence.
- Probes do not load sessions: identity failure does not break `/healthz`; `/readyz`
  continues checking both DB connections, not authentication/schema correctness.

## 3. Environment contract

| Variable | Default | Rules |
| --- | --- | --- |
| `APP_ENV` | production | Explicit production or development |
| `SESSION_COOKIE_SECURE` | true | false allowed only with APP_ENV=development |
| `TRUSTED_ORIGINS` | empty | Exact comma-separated origins, no wildcard/path/userinfo/query/fragment; HTTPS in production, loopback HTTP allowed in development |
| `SESSION_LIFETIME` | 8h | 1m–24h |
| `SESSION_IDLE_TIMEOUT` | 30m | 1m–24h, no greater than lifetime |
| `TRUSTED_PROXY_CIDRS` | empty | At most 32 explicit proxy CIDRs; no catch-all or IPv4-mapped CIDRs |

Root `.env.example` now includes explicit local HTTP development settings. Existing
local env files do not gain these settings automatically: without them production
defaults apply. Do not pass the privileged `db/.env` into the API. Runtime credentials
and all existing bootstrap/migration steps remain as documented in database setup.

Secure cookies are named **`__Host-cyberguard_session`**; the explicit insecure local
HTTP exception is named **`cyberguard_session`**. Both are HttpOnly, SameSite=Lax,
host-only (no Domain), Path=/, with browser expiry at the absolute session deadline;
server idle expiry can reject them earlier. Do not
weaken Domain/SameSite to work around a routing mistake. Different-host/cross-site
frontend/Python deployment requires a separate review; CORS permission alone does
not make SameSite cookies travel across sites.

Local canonical addresses: frontend `http://127.0.0.1:5173`, API
`http://127.0.0.1:8080`. Do not mix localhost and 127.0.0.1 unintentionally. Empty trusted
origins means same-origin-only: no CORS middleware is installed, because an empty
go-chi/cors allow-list would otherwise default to '*'. CSRF uses standard-library
CrossOriginProtection, including login/logout; there is no blanket route bypass.
Non-browser requests without Origin/Sec-Fetch-Site are permitted, not treated as a
substitute for authentication. Rejected unsafe browser requests get JSON 403.

### Proxy trust

By default only the TCP peer determines the login-limit key; X-Forwarded-For,
X-Real-IP and Forwarded cannot override it. With explicit trusted proxy CIDRs,
X-Forwarded-For is used only if the immediate peer is trusted. The complete bare-IP
chain (maximum 16 addresses) is validated, then read right-to-left, skipping trusted
proxies. The nearest untrusted address is the client; malformed/missing/all-trusted
chains fall back to the TCP peer. IPv4-mapped peers are normalized.

The ingress must strip/rebuild or correctly append the real peer to forwarded headers.
Trust only actual ingress IPs/networks, not all Docker/private networks by default,
and prevent public traffic from directly reaching Go as a trusted proxy. Without
correct proxy trust, all clients behind one ingress share that ingress's attempt limit.
No arbitrary forwarded hostname/protocol header controls cookie security.

## 4. Architect tasks — current integration/review

- [ ] Review the defaults, cookie naming, browser policy, failure handling and limits.
- [ ] Configure final public HTTPS routing and TLS termination; preserve the public
      Host where appropriate and provide exact trusted origins for cross-origin use.
      Do not leave a separate plaintext production API port publicly reachable.
- [ ] Inject the new runtime settings only. Do not add admin/migration/demo secrets.
- [ ] Configure the actual ingress CIDRs and forwarding policy; verify spoofed headers
      from an untrusted connection cannot bypass login limits.
- [ ] Keep the databases/volumes from the existing SQL contract; no new DB, broker or
      schema migration is required. Keep one-off bootstrap/migrate/seed separate.
- [ ] Verify Secure HttpOnly __Host- cookies in the deployed browser, credentialed
      requests from the allowed frontend origin, CSRF rejection, and logout.
- [ ] Verify sessions survive Go/container restart with DB volumes retained; stopping
      identity PostgreSQL denies auth access without losing process liveness.
- [ ] Allow shutdown grace beyond HTTP shutdown/DB deadlines. The app stops the
      session-maintenance loop before closing its database pools.

The engineer owns implementation and fixes; these are deployment/security-review
tasks, not a request to implement a second authentication system.

## 5. AI specialist tasks — contract preparation, not a blocker

- [ ] Review cookie/session behavior and identify the incident/action checks needed
      by browser-originated Python status/result requests in the next integration.
- [ ] Plan to forward only the required session cookie safely to a service-protected
      internal Go authorization endpoint; never put it in URLs, logs or stored jobs.
- [ ] Confirm Python will not verify passwords, issue competing sessions, read identity
      PostgreSQL, or trust browser-supplied roles/ownership.
- [ ] Keep worker job-context authorization separate: saved jobs do not depend on a
      browser cookie, logout, or the original browser remaining open.

Internal Go authorization/job-context endpoints and incident-level permissions are
**not delivered here**. `/auth/me` supplies identity, not permission for an arbitrary
incident. AI model selection and worker implementation do not block this auth PR.

## 6. Domain expert tasks — acceptance/business review

- [ ] Confirm active employees, analysts and admins can all log in; login itself is
      not restricted to security staff.
- [ ] Review the generic invalid-credentials message and help/support expectations
      without exposing whether an account exists or is blocked.
- [ ] Confirm blocking and password reset revoke existing sessions, unblocking does
      not restore them, and role changes require a new login.
- [ ] Review 8h/30m session defaults and the temporary per-peer attempt restriction.
- [ ] Accept realistic scenarios with two distinct employees, one analyst and one admin.

These are business/acceptance decisions, not framework or SQL implementation tasks.
Already approved role names are not being reopened. Feedback can change explicit
defaults/wording through review without blocking basic implementation.

## 7. Engineer checks and next frontend contract

```sh
make go-check
make go-build
# With the three dedicated test DB URLs from database-setup.md:
make go-test-integration
```

Unit tests cover credentials, bounded verification, stale snapshots, configuration,
cookie policy, rotation/logout, current identity/revocation, bad bodies/methods,
CORS/CSRF, trusted proxy parsing, attempts, store failures and safe DTOs. PostgreSQL
tests use a new owned schema, real migrations, restricted runtime credentials and
SCS storage. They verify all roles, hashed tokens/payload/expiry, manager recreation,
block/unblock/password/role revocation, housekeeping, actual failed session INSERT/
UPDATE, and dependency loss. Only the newly created test schema is cleaned up.

Focused concurrency checks (integration requires the same dedicated test DB URLs):

```sh
cd backend/go
go test -race ./internal/httpapi -run TestAuthHTTPConcurrentSessionRevocation -count=20
go test -race ./internal/httpapi -run 'TestAuthHTTP(Cookie|Concurrent|Passive|Rejected|FailedLogin)' -count=20
go test -race -tags=integration ./internal/httpapi -run 'TestAuthenticationPostgresIntegration/(concurrent_|cookie_ordering)' -count=20
go test -race -tags=integration ./internal/sessionstore -count=20
```

A local binary smoke test also verified an existing cookie across a real Go process
restart, hostile-origin logout rejection, trusted logout, database outage/recovery,
and graceful SIGTERM with runtime credentials only and no secrets in API logs.

Follow the existing module-only vulnerability advisory note in database-setup.md;
govulncheck remains part of checks. Local passing tests are not a deployed-browser,
TLS/proxy or multi-service acceptance claim; the architect checks those above.

Frontend is a later separate branch, also engineer-owned. It will use
`credentials: 'include'`, obtain identity through `/auth/me`, never store the token
in localStorage, and distinguish 401 (session/credentials), 403 (request policy),
429 (respect Retry-After), and 503 (temporary failure, not proof of logout).
Serialize login/logout and discard obsolete in-flight identity responses after an
authentication change. Cookie presence is never proof of a live server session.
No frontend implementation or internal Python endpoint is included here.
