# Incident Core API — Implementation and Integration Handoff

## Scope and explicit integration gap

Implemented in `feat/go-incidents-core`: incident creation, scoped list/detail reads,
input validation, bounded keyset pagination, reporter/professional response projections,
incident PostgreSQL migration/grants, sqlc queries, and unit/real-PostgreSQL tests.

**This is the incident core, not the completed initial vertical slice.** Creation in
this branch saves an incident only: there is no assessment job, queued state or fake
AI success. The shared job schema is being agreed in
[Issue #3](https://github.com/bekadoux/cyberguard/issues/3). Before accepting the complete
creation workflow, add the agreed job migration and save incident + initial job in
one transaction with rollback/uniqueness tests. Do not present this intermediate
create contract as the final production incident-and-assessment workflow. Keep its
review separate from acceptance of the full milestone.

No workflow mutations, assignment, comments, archive/restore API, incident history,
notifications, AI endpoints, Python implementation or frontend is delivered here.
The schema has nullable investigation fields so read/visibility rules are testable;
their presence is not evidence that update workflows exist. Transition reasons belong
to future immutable transition history, not an overwritten `status_reason` column.

## Domain rules applied

Source: [domain specification](domain-specification.md).

- Required title/description: non-whitespace text, at most 200/10,000 Unicode code
  points, not UTF-8 bytes. Combining sequences count as multiple code points.
- Optional affected system: at most 500 code points; omitted/null/whitespace-only is
  stored as SQL NULL. Preserve nonblank original text; do not silently trim descriptions.
- Reject NUL characters and invalid UTF-8. Body is bounded at 128 KiB, including JSON
  escapes. Exactly one JSON value and only the three submission fields are accepted.
- PostgreSQL generates UUID/timestamps; reporter is the currently authenticated user.
  New status is `submitted`, category/severity/assignment/resolution are NULL.
- Runtime cannot UPDATE or DELETE incidents, or INSERT server-managed columns.
  Later migrations must explicitly grant the appropriate mutable columns.
- SQL enforces ownership before pagination. Employees see only their reports;
  analysts/admins may read the company queue. Missing, malformed, non-visible and
  archived IDs use the same 404. Archived rows are excluded for all roles in this
  increment; future privileged archive access requires an explicit contract.

### Visibility and open interpretation

Reporters see category, assigned analyst identity and the full reporter-facing
resolution note, but no severity/internal/AI fields. A professional view includes
severity (nullable) for an analyst/admin reading someone else's report. No severity
filter/sort is supported, including for staff, so filtering cannot expose a reporter's
hidden priority. Do not return persistence models directly.

The current domain document says the reporter view applies regardless of role, while
the matrix grants professional capabilities to analysts/admins. This implementation
uses the **stricter reporter projection for own reports**, including an analyst/admin's
own report, pending clarification of dual-role precedence. Do not silently expand
that access. Comment write permissions for unassigned analysts also need clarification
before comment implementation; they do not block this read/create increment.

List is a summary: it omits long description/resolution text and exposes the assigned
analyst UUID only. Detail includes `assigned_analyst: {id, username}`; username is null
if the referenced account no longer exists. Identity lookup errors fail with 503
without partially returning incident data. UUID references span separate databases,
so there are intentionally no cross-database foreign keys or password/hash snapshots.

## HTTP contract

See [OpenAPI](../api/go-incidents.openapi.yaml). All endpoints require the existing
session cookie, current account/auth_version checks and exact browser-origin policy.

| Endpoint | Result |
| --- | --- |
| `POST /incidents` | 201 `{incident: ...}`, Location header, no Set-Cookie; incident only until job integration |
| `GET /incidents` | 200 `{incidents: [...], next_cursor: string|null}`; empty list is `[]` |
| `GET /incidents/{id}` | 200 `{incident: ...}` or concealed 404 |

List accepts only `limit` (1–100, default 20), `cursor`, `status`, `category`.
Reject unknown, duplicate, empty or malformed parameters. Order is fixed:
`created_at DESC, id DESC`. Cursors are bounded, validated continuation positions,
not authorization tokens; every request reapplies current access rules. Changing
filters uses a fresh pagination traversal. Keyset traversal is not a database snapshot:
newer inserts and later investigation changes may alter subsequent results.

Errors use the existing envelope: 400 `invalid_request`, 401
`authentication_required`, 403 `csrf_rejected`/`forbidden`, 404 `not_found`, 413
`body_too_large`, 415 `unsupported_media_type`, 503 `auth_unavailable` or
`incidents_unavailable`. Responses are no-store and carry request IDs. Never log
descriptions, passwords, raw cookies, database errors or request bodies.

### Authentication and write ordering

Incident routes reuse the auth CORS/CSRF policy, deadline and current-user check.
Their session middleware explicitly loads and refreshes the session **before** any
incident operation, with the revocation-safe persistence adapter. It never emits a
cookie, including on failures, and does not save again after business logic.

This is deliberately not the auth endpoints' small 16 KiB LoadAndSave response buffer:
incident detail may contain 10,000 multibyte characters, and a late session-save failure
must not obscure a successful write in the other database. Existing auth buffering and
cookie policy remain unchanged. A revocation that wins before refresh blocks creation;
already-authorized in-flight work may finish after logout, without resurrecting a
session or overwriting a newer cookie. This is not a distributed transaction across
identity and incident databases. Network/commit acknowledgment loss is still ambiguous;
this increment does not promise retry idempotency for POST.

## Setup and checks

Use the existing bootstrap for two separate PostgreSQL instances. From a migration-only
environment (not the API's runtime environment):

```sh
make db-migrate-identity
# Requires INCIDENT_MIGRATION_DATABASE_URL as incidents_migrator:
make db-migrate-incidents
make db-status-incidents
```

Equivalent binary: `backend/go/bin/migrate -database incidents -dir db/migrations/incidents`.
Identity remains the default migration target for existing commands. API startup never
bootstraps/migrates/seeds; connectivity readiness is still not a schema check.

```sh
make go-check
make go-build
# Four dedicated URLs from database-setup.md, including incident migration access:
make go-test-integration
```

Tests cover Unicode/whitespace/length boundaries, server-field injection, media/body
limits, employee isolation, professional/reporter DTOs, malformed/unsupported filters,
stable pagination with tied timestamps, archive exclusion, CSRF/CORS, dependency loss,
failed session refresh before writes, and concurrent logout before/after authorization.
PostgreSQL tests apply real migrations into isolated owned schemas, verify column
grants/constraints and runtime restrictions, and check actual identity + incident API
integration. They do not introduce a placeholder job table or claim AI integration.

Local validation also ran incident HTTP/PostgreSQL tests and existing auth cookie/
revocation regressions 20 times with the race detector, integration-tagged vet,
OpenAPI 3.1 validation, and 10 seconds of cursor fuzzing (over 550,000 inputs).
A built-binary smoke check used clean runtime-only environments and real HTTP cookie
jars: both migration targets/repeat/status, rejected runtime-role migration, a
10,000-character Unicode report, employee isolation, professional access, protected
fields, restart persistence and logout denial passed. Actual reporter/professional
detail responses were checked against the OpenAPI JSON schemas. This is not a deployed
TLS/browser or complete AI-workflow acceptance claim. The previously documented
module-only unused OpenPGP advisory remains; govulncheck found no affected called symbols.

## Next review/implementation steps

1. Architect reviews migration/role grants, the pre-operation session middleware,
   pagination and public DTOs; updates deployment to run both migration targets.
2. Domain expert confirms dual-role view precedence before expanding own-report access.
3. Go developer and AI specialist agree Issue #3; add atomic incident/job transaction
   before accepting the full create-and-enqueue workflow.
4. Implement assignment/classification, history, comments and lifecycle in later
   increments under the agreed permissions; no unrestricted PATCH endpoint.
