# Authentication Security Review — P1/P2 Follow-up

## Scope and method

Review of the currently implemented Go authentication increment: handlers and response
buffering, SCS/persistence lifecycle, current-account checks, browser origin policy,
proxy trust, configuration, bounded password work and login attempts, safe errors/logs,
and the corresponding tests. This is a source review plus local automated validation,
not an independent penetration test, deployed-browser acceptance, or a guarantee that
all vulnerabilities have been eliminated. Incidents, internal Python authorization,
administrative endpoints and frontend implementation are outside this increment.

## Findings addressed

1. **P1 — revoked-session resurrection:** default store upsert could recreate a row
   deleted after another request loaded it. New-token INSERT and loaded-token UPDATE
   are now distinct; UPDATE cannot insert. Deterministic logout/rotation regression
   tests failed before the fix and now pass. Independent PostgreSQL pools/managers
   exercise the actual persistence path without a process-wide lock.
2. **P2 — stale response overwrites fresh cookie:** late missing-token 401 responses
   could delete the new login cookie. The expanded reproduction also failed for a
   completed 200 refresh, a failed login with an existing session, and SCS automatic
   destruction of invalid identity. All passive/error responses now omit Set-Cookie.
   Only successfully persisted, explicit login/logout responses change it.

The persistent login cookie now uses the absolute SCS deadline. Server idle expiry
remains shorter, authoritative and refreshed without browser cookie writes. This is
necessary to preserve active sessions beyond the initial idle interval; it does not
grant access to an idle-expired token retained in a browser.

## Review matrix and regression evidence

| Boundary | Expected behavior | Evidence |
| --- | --- | --- |
| Refresh vs logout/rotation after load | Deleted old row stays absent; old cookie gets 401; new/independent sessions work | `TestAuthHTTPConcurrentSessionRevocation`, PostgreSQL `concurrent_*` |
| Stale lookup before rotation | Apply new login response, then delayed 401; new browser cookie survives | `TestAuthHTTPCookieResponseOrdering/*/before_lookup`, PostgreSQL `cookie_ordering/before_lookup` |
| Already completed successful read delivered late | No old Set-Cookie can replace the new cookie | `completed_me` cases in both suites |
| Failed login delivered late | Existing session refresh must not emit a cookie on an error response | `failed_login` cases; `TestAuthHTTPFailedLoginDoesNotChangeExistingCookie` |
| Loaded identity subsequently denied | Delete invalid server session but never let SCS delete a newer browser cookie | `destroyed_identity` cases; `TestAuthHTTPPassiveDenialsNeverSetCookies` |
| Browser and server expiry | Cookie uses absolute deadline; idle refresh preserves absolute deadline; server expiry rejects retained cookie | `TestAuthHTTPCookieExpiryAndIdleRefresh`, PostgreSQL payload/expiry and housekeeping tests |
| Blocking, password reset, role change | Read current account/auth_version; unblock cannot restore old sessions | service, HTTP and PostgreSQL revocation tests |
| DB read/write failure | Safe 503; no user data or cookie success leak; logout never falsely succeeds | HTTP store-failure tests; actual INSERT/UPDATE permission failures in PostgreSQL |
| Session creation collisions | Reject duplicate INSERT without overwriting another session | `TestPostgresConditionalPersistence` |
| Persistence context | Missing request state cannot silently fall back to upsert | `TestPersistenceRequiresRequestContext`, `TestRequestScopedCreationAndRefresh` |
| CSRF/CORS | Unsafe cross-site/null/unlisted origins rejected before mutation; exact allow-list, no wildcard default | `TestAuthHTTPBrowserPolicy`, `TestAuthHTTPRejectedOriginsCannotChangeSession`, secure-default tests |
| Proxy trust | Ignore forwarded identities unless immediate peer is trusted; validate whole bounded chain | `TestLoginPeerTrustBoundary`, forwarded-header rate-limit test |
| Credential work | Validate inputs/hash costs; uniform credential failures; fresh account recheck; bounded parallel work | service tests including unsafe hashes, recheck, cancellation and capacity |
| Login attempts | Concurrent requests cannot bypass per-peer window; bounded peer map | limiter window/capacity/concurrency tests |
| Transport/response hygiene | Secure host-only production cookie, HttpOnly/Lax/Path=/; no-store; safe DTO/errors/logs; buffer before persistence | secure defaults, response/cookie, store-failure and log-exclusion tests |

Validation commands are in [authentication handoff](authentication-handoff.md#7-engineer-checks-and-next-frontend-contract).
The follow-up validation includes `make go-check`, `make go-build`, real PostgreSQL
integration tests with `-race`, integration-tagged vet, OpenAPI validation, and 20
repetitions of cookie-ordering/concurrency tests. These are local results, not CI or
deployment claims. The existing module-only unused OpenPGP advisory remains documented
in database setup; no affected imported symbols were reported by govulncheck.

A process smoke check also ran the built API against the isolated PostgreSQL fixture
using an actual HTTP cookie jar: absolute-lifetime login cookie, cookie-free `/me` and
failed login, token rotation with old-cookie 401/new-cookie 200, exactly one explicit
logout deletion cookie, readiness, safe tested logs and graceful SIGTERM all passed.

## Explicit limits and remaining acceptance

- **Explicit concurrent mutations:** two login/logout requests can still have their
  responses delivered out of order. A server cannot compare a cookie deletion against
  the browser's current value. The frontend must serialize authentication mutations
  and coordinate tabs if required. If ordering across independent tabs/devices is a
  product requirement, agree a dedicated protocol; do not silently promise it here.
- **Already authorized in-flight responses:** revocation cannot retract data already
  produced or sent. A stale successful `/me` may still contain an old identity; the
  frontend must discard obsolete responses when its authentication generation changes.
  No passive response may alter the cookie, even if it was a successful read.
- **Ambiguous database/network failures:** a failed acknowledgment can occur after
  PostgreSQL committed. A 503 means the client must not assume success or revocation;
  it does not prove that the database performed no operation. Retrying logout is safe.
- **Deployment:** architect still verifies HTTPS, actual proxy-header sanitization,
  configured origins, browser credential behavior, runtime grants/secret injection,
  volumes/restarts and shutdown. A secure cookie does not itself configure TLS.
- **Abuse controls:** login limits and password slots are per process, not a distributed
  per-account defense or a DDoS shield. Internet-facing deployment needs appropriate
  ingress limits; MFA, account recovery and distributed throttling are not delivered.
- **Data authority:** browser cookie presence and browser-supplied roles never establish
  access. Future incident/Python endpoints must apply current account and per-incident
  authorization; passing `/auth/me` alone is not an authorization contract for them.
- **Ongoing review:** expand regression tests with every new auth/security finding and
  new protected endpoint. Re-review cookie/session behavior when changing providers,
  SCS versions, deployments or frontend authentication flows.
