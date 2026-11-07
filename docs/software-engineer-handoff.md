# CyberGuard — Initial Go Backend Handoff

This is the implementation helper for the software engineer. Read the
[project plan](project-plan.md) for the wider scope and architecture.

**First objective:** deliver a small, tested vertical slice: log in, submit an incident,
list/read incidents with correct permissions, and persist an assessment job without
waiting for Python. An HTTP client or automated tests can demonstrate it; neither a
finished frontend, a selected model, nor Azure deployment is a prerequisite.

The stack, ownership boundaries, and starting contract conventions below are approved.
Proposed paths, endpoint names, and detailed field layouts remain starting points, not
existing implementation or a frozen API specification. Confirm those details with the
architect and AI specialist before others build against them.

## 1. Ownership and Dependencies

| Owner | Initial responsibility |
| --- | --- |
| Software engineer | Go application, authentication implementation with architect review, incident API, identity/incident persistence, atomic job enqueueing, Go tests |
| Software architect | Local Compose/deployment foundation, architecture and security review, API/data contracts, integration planning; share foundation implementation rather than duplicate it |
| AI specialist | Python/FastAPI application, assessment status/result API, Go authorization/context client, background worker, model integration/evaluation, AI service tests and Dockerfile |
| Domain expert | Initial incident fields and workflow, category/impact definitions, realistic fixtures, permission and acceptance review |

The engineer is not expected to build the Python worker, research models, implement all
frontend screens, and deploy Azure alone.

### Approved starting contract conventions

| Concern | Approved choice |
| --- | --- |
| IDs | UUIDs |
| API field naming | `snake_case` |
| Timestamps | UTC, serialized as RFC 3339 |
| Role values | `employee`, `analyst`, `admin` |
| New incident status | `submitted` |
| Unassessed category/severity | Nullable; do not invent default assessments |
| Errors | One consistent JSON error format; exact envelope and codes still to be specified |
| Local networking | Fixed API ports and an explicit frontend origin, cookie, CORS, and CSRF configuration; exact values still to be specified |

These choices do not need to be reopened before starting. Further incident transitions
and field-level permissions still require domain review.

### Remaining setup agreements

- Pin a supported Go version and compatible dependency/tool versions.
- Confirm local database URLs, API ports, frontend origin, and configuration conventions.
- Confirm field limits, pagination, the exact error envelope/codes, and other payload
  details using the approved conventions above.
- Complete the engineer–AI specialist agreement checklist below at the relevant stage.
- Provide four test accounts: two different employees, one analyst, and one administrator.

These agreements should be small enough to make quickly. Do not wait for every future
screen, workflow transition, or model choice before starting.

### Engineer–AI specialist agreement checklist

The engineer and AI specialist should agree the following together, with architect
review. Record the outcomes in shared SQL migrations and API documentation rather than
leaving them only in chat.

**Before implementing job enqueueing:**

- [ ] **Minimal job schema:** exact table/column names, UUID references, job-kind values,
      initial `queued` state, required timestamps, and which fields may initially be null.
      Identify the incident context/revision reference the worker will need.
- [ ] **Initial-job uniqueness:** the database rule identifying one initial assessment
      per incident; distinguish that identity from retry attempts and leave an explicit
      extension path for regeneration after edits.
- [ ] **Field ownership and grants:** Go creates the incident and queued job atomically;
      Python claims jobs, updates execution fields, and saves results. Specify permitted
      tables/operations for each credential; AI writes must not alter human decisions.
- [ ] **Migration ownership:** name who maintains and applies the shared goose migrations,
      how the AI specialist contributes changes, and how both services coordinate schema
      changes. Do not introduce a competing Python auto-migration process.

**Before integrating the fake worker:**

- [ ] **Internal API contracts:** exact methods, paths, request/response fields, service
      authentication, and failure responses for browser-session authorization and worker
      job-context retrieval. Keep these two authorization paths distinct; workers do not
      depend on a saved browser session.
- [ ] **Result and status contract:** assessment JSON fields, job/result linkage, provenance
      fields, and how Python exposes queued/running/completed/failed states and safe errors.
      The same contract should support a fake result before a real model is selected.
- [ ] **Shared integration fixture:** one agreed sample incident/job and an explicitly
      labelled fake assessment. Demonstrate creation while Python is offline, later job
      completion, authorized status/result retrieval, and denied cross-user access.

Full model selection and worker retry/recovery details need not block Go scaffolding.
They still need resolution before claiming the complete worker behavior described in
[the Python handoff section](#7-python-handoff-and-job-lifecycle).

## 2. Backend Stack

| Concern | Decision / guidance |
| --- | --- |
| Routing | `github.com/go-chi/chi/v5` |
| HTTP and JSON | Standard-library `net/http` and `encoding/json` |
| CORS | `github.com/go-chi/cors`, with explicit credentialed frontend origins |
| PostgreSQL | `github.com/jackc/pgx/v5` and `pgxpool`; a separate pool per database |
| Migrations | goose SQL migrations, independently tracked in each database |
| Queries | sqlc with `sql_package: pgx/v5`; separate generated identity/incident packages |
| Sessions | `github.com/alexedwards/scs/v2` with `github.com/alexedwards/scs/pgxstore` |
| Passwords | `github.com/alexedwards/argon2id` |
| Logging | Standard-library `log/slog`; structured logs to stdout |
| Configuration | A typed configuration struct populated from environment variables |
| Tests | `testing`, `httptest`, and real disposable PostgreSQL databases |

Use explicit validation and a few domain permission functions, not a generic policy
engine. Avoid an ORM alongside sqlc, JWT refresh-token infrastructure, a dependency-
injection framework, or a message broker for this milestone.

For CSRF, evaluate standard-library `http.CrossOriginProtection`, available since Go
1.25, with the pinned supported Go version and actual trusted frontend origins. If a
different vetted approach is selected, document it. CORS and SameSite cookies are not
substitutes for the chosen CSRF policy.

## 3. Suggested Structure and Development Workflow

Keep the application simple: handlers, application/business logic, and generated query
code. Introduce interfaces where they help a real boundary or test, not for every type.

A possible repository layout is:

```text
backend/go/
  cmd/api/                    # Application entry point
  cmd/seed/                   # Explicit local/demo setup, if chosen
  internal/
    config/
    httpapi/                  # Routes, handlers, middleware, API DTOs
    auth/
    incidents/                # Workflow and transaction orchestration
    db/identity/              # Generated sqlc code
    db/incidents/             # Generated sqlc code
  sql/identity/               # Handwritten query definitions
  sql/incidents/
  sqlc.yaml
db/migrations/
  identity/                   # Shared source of truth for identity schema
  incidents/                  # Includes assessment jobs/results
api/                          # OpenAPI contract(s)
```

This is a suggestion, not a requirement to create empty layers. Root-level migration
files can be shared with Python contributors without putting schema ownership inside
a Python ORM. Do not run competing Go and Python auto-migration systems over the same
tables. The AI specialist can contribute AI-table SQL through the coordinated goose
migration process.

Provide short, documented commands for:

- Starting the two local PostgreSQL instances.
- Migrating each database and preparing demo accounts.
- Generating sqlc code and running the Go API.
- Formatting, testing, vetting, and vulnerability checks.

A small Makefile is sufficient. Commit generated sqlc Go files, never edit them by
hand, and verify regeneration produces no changes in CI. Pin goose/sqlc versions.
Use an example environment file with placeholders; never commit working credentials.

## 4. Work Order

### Task 1 — Runnable foundation

- Add configuration parsing and fail clearly on missing/invalid required values.
- Connect to both databases using separate, modestly sized connection pools.
- Add migrations and a repeatable development/demo-account setup.
- Add a process liveness endpoint and database readiness checks.
- Configure request IDs, panic recovery, structured logging, request-body limits,
  server/database timeouts, and graceful shutdown.
- Establish a consistent JSON error shape without leaking SQL or internal errors.

**Done when:** a fresh local setup can migrate both databases and start the API using
documented commands. Go readiness does not require Python to be online.

### Task 2 — Authentication and permissions

- Implement login, logout, and current-user endpoints.
- Hash/verify passwords through the Argon2id library; do not invent password formats.
- Persist sessions through SCS/pgxstore in the identity database.
- Renew the session token after login and destroy the session on logout.
- Set absolute/idle expiry and environment-appropriate cookie settings.
- Implement authenticated-user loading and explicit role/incident-access checks.
- Add CSRF protection, credentialed CORS where needed, and basic login rate limiting.
- Test invalid credentials, unauthenticated access, logout, and ownership restrictions.

SCS owns session persistence; goose creates its documented session table. Do not rewrite
SCS storage through sqlc simply to make every query use the same mechanism.

Fetch current account status/permissions instead of trusting a stale role copied into
the session. A recommended revocation mechanism is a user `auth_version` stored in the
session and compared with the current user row. Increment it when invalidating all
sessions after password resets or blocking. This avoids old sessions becoming valid
again after an account is unblocked. The exact mechanism needs architect review.

Production cookies must be HttpOnly and Secure, with an appropriate SameSite policy.
An explicitly configured localhost HTTP exception is for development only. Never log
passwords, session cookies, or internal service credentials. Avoid logging incident
descriptions and AI prompts as routine request diagnostics.

**Done when:** the four fixture accounts can log in, denied access is enforced by the
server, logout invalidates the session, and current blocking/role changes take effect.
Full administration screens are not required to test these behaviors.

### Task 3 — Minimal incident API

- Create an incident using the authenticated user as reporter; do not accept a trusted
  reporter/owner ID from the request body.
- List only the caller's incidents for employees and all permitted incidents for
  analysts/administrators. Apply visibility rules in database queries, not after
  fetching an unrestricted list.
- Retrieve incident details with the same access rule.
- Add bounded pagination and validate supported filters/sort options.
- Return explicit API DTOs rather than generated database models.
- Keep server-managed fields such as IDs, ownership, timestamps, and job state outside
  the ordinary employee-editable payload.

Use one agreed policy for inaccessible incident IDs, such as returning the same 404
response for nonexistent and non-visible incidents. Distinguish unauthenticated requests
from authenticated-but-forbidden actions consistently in the API contract.

**Done when:** an employee can submit/list/read their incident, a different employee
cannot see it, and an analyst and administrator can read it. API tests are sufficient
before frontend integration.

### Task 4 — Atomically enqueue an assessment

- Add the jointly agreed assessment-job migration and sqlc queries.
- In one incident-database transaction, insert the incident and its initial queued job.
- Use sqlc's transaction-bound queries, for example through `WithTx`, so neither insert
  accidentally runs outside the transaction.
- Roll back both inserts if either fails; return success only after commit.
- Return the new incident promptly without contacting Python or performing inference.
- Define a database uniqueness rule for the initial assessment job; coordinate its key
  with future retry/regeneration semantics.

The queue is persistent PostgreSQL data, not an in-memory Go channel, request-scoped
goroutine, or best-effort HTTP call to Python. No Redis/RabbitMQ is needed initially.

**Done when:** incident creation succeeds while Python is stopped, exactly one initial
job is persisted per newly created incident, rollback tests leave neither a partial
incident nor an orphan job, and committed jobs remain queued across API restarts.

### Task 5 — Python integration boundary

- Implement a protected internal endpoint for Python to validate a browser session's
  incident/action access. Return only the identity/context required for that operation.
- Agree a separate job-context endpoint for the worker, protected by service credentials
  and scoped to a valid assessment job.
- Do not require the original browser cookie to execute a queued job; logout or a closed
  browser must not cancel generation.
- Test invalid service credentials, missing/expired sessions, wrong incident ownership,
  invalid job references, and unavailable authorization dependencies.
- Review the fake-worker integration with the AI specialist before real inference.

An internal service credential is not a substitute for checking the user's access on
browser-originated requests. Keep user-session checks and background-job authorization
as distinct operations. Apply any CSRF exemptions only to narrowly scoped, independently
service-authenticated internal routes, not to a public route prefix by convention.

**Done when:** the Python fake worker can obtain permitted job context and persist a
labelled test result, while browser status/result requests remain subject to current
incident-access checks.

## 5. Initial API Contract to Agree

The following routes are illustrative; document the final names and payloads in OpenAPI.

| Method and path | Responsibility |
| --- | --- |
| `GET /healthz` | Go process liveness; no sensitive internals |
| `GET /readyz` | Readiness of required Go database dependencies, not model availability |
| `POST /auth/login` | Validate credentials and establish a session |
| `POST /auth/logout` | Invalidate the current session |
| `GET /auth/me` | Return the safe current-user identity and role |
| `POST /incidents` | Persist incident and queued job; normally return 201 |
| `GET /incidents` | Return a paginated, permission-filtered list |
| `GET /incidents/{id}` | Return an authorized incident detail |
| `POST /internal/auth/authorize` | Service-protected validation of browser-session incident/action access |
| `GET /internal/assessment-jobs/{id}/context` | Service-protected job context for the Python worker |

Python owns the browser-facing assessment status/result endpoint, for example
`GET /incidents/{id}/assessment` on its own API. The Go creation response may include
job ID/state as handoff metadata, but assessment generation is not a synchronous Go
request and does not need a second Go-owned assessment API.

Use the approved UUID, `snake_case`, role, and UTC/RFC 3339 conventions. Agree remaining
response fields, the exact error envelope/codes, and pagination. Document login
cookie/credential behavior for React. Do not send password hashes or session secrets
in public response payloads. Internal session forwarding must not put secrets into
URLs or logs.

Analyst update endpoints can follow the first create/read slice. Confirm permitted
fields and status transitions with the domain expert rather than exposing unrestricted
row updates or allowing arbitrary state changes.

## 6. Initial Persistence Model

This is a checklist for schema design, not final DDL.

### Identity database

- **Users:** UUID, unique username, password hash, role (`employee`, `analyst`, `admin`),
  active/blocked state, timestamps, and the agreed session-revocation field/mechanism.
- **Sessions:** the schema required by SCS/pgxstore, including expiry indexing.

Only Go connects to this database. Its application credential is not a superuser or
schema-migration credential. Python must not query users or sessions directly.

### Incident database

- **Incidents:** UUID, reporter UUID, title, description, optional affected-system text,
  initial business status `submitted`, timestamps; nullable analyst assignment and
  unassessed category/severity fields.
- **Assessment jobs:** UUID, incident UUID reference, job kind, execution state, timestamps,
  and jointly agreed attempt/recovery metadata and initial-job uniqueness rule.
- **Assessment results:** job/incident reference, generated assessment, model/prompt
  provenance as agreed, and generation timestamp; result format is owned jointly with
  the AI specialist.

Job state is separate from incident status: `queued`, `running`, `completed`, `failed`.
References to identity users are stable IDs, not cross-database foreign keys. Preserve
historical references when accounts are blocked.

Use appropriate constraints and indexes. The Go runtime needs incident CRUD and job
enqueue permissions; Python needs only agreed job/result-table permissions. More
privileged migrations run as a separate controlled step. Do not give Python unrestricted
incident-history access simply to fetch text that Go can supply through the job API.

Keep human decisions separate from AI suggestions. Saving a generated assessment must
not update an analyst's classification, comments, or resolution.

## 7. Python Handoff and Job Lifecycle

The AI specialist should first deliver a fake assessment pipeline, then replace the
fake inference with selected models:

1. Atomically claim a queued job and mark it running.
2. Obtain authorized incident context through Go using service credentials.
3. Produce an explicitly labelled test assessment, with no model dependency.
4. Save the result and mark the job completed in one transaction.
5. Expose authorized job status/results to the frontend.

The worker should release claim-transaction locks before slow inference. PostgreSQL
row claiming, such as `FOR UPDATE SKIP LOCKED`, is a possible implementation, not a
complete restart-recovery policy on its own. Agree leases/timeouts, bounded retries,
attempt ownership, and duplicate-result protection before declaring the worker complete.
Start with one concurrent generation job for the CPU-only deployment.

The frontend can start with polling while queued/running, subject to the agreed API
contract; no streaming system is required for the first demo. It shows progress and
then a saved result or failure without disabling normal incident review.

Still resolve with the AI specialist and architect:

- Which incident revision/snapshot a job assesses, and how stale output is identified.
- Whether edits require regeneration, and how that differs from retrying a failed job.
- Safe public failure messages versus private diagnostics.
- Exact job/result schema and worker recovery semantics.

Do not block the first Go create/read milestone on these advanced behaviors, but do
not silently claim restart-safe execution or current-data assessments without them.

## 8. Tests and First-Milestone Acceptance

Use handler/unit tests where useful and real PostgreSQL integration tests for SQL,
migrations, sessions, transactions, and visibility. Run them against dedicated disposable
test databases, never development or deployed data. Testcontainers is optional; Compose
plus explicit test configuration is sufficient initially.

### Go milestone

- [ ] Fresh databases can be migrated and seeded using documented commands.
- [ ] Both database pools are configured independently and use restricted runtime roles.
- [ ] Valid accounts can log in; invalid credentials do not disclose account details.
- [ ] Login renews the session token; logout invalidates it.
- [ ] Expiry, blocking, password-reset revocation, and role-change behavior are tested.
- [ ] Unauthenticated requests are denied; employees cannot list/read another employee's
      incidents, including by changing IDs or filter parameters.
- [ ] Analysts and administrators can read the incident queue.
- [ ] Incident creation commits the incident and initial job together, returning promptly.
- [ ] Transaction failure leaves neither a partial incident nor an orphan job.
- [ ] Python being offline does not break Go readiness or incident create/list/detail.
- [ ] Queued jobs and incident data persist across Go restarts.
- [ ] Invalid JSON, oversized bodies, invalid fields, and unsupported filters fail safely.
- [ ] Credentialed CORS and CSRF rules allow the configured frontend and reject untrusted
      state-changing browser requests.
- [ ] Public DTOs/errors and logs do not expose credentials, password hashes, or raw
      incident content unnecessarily.

### Joint integration milestone

- [ ] Python can complete a queued job with a clearly labelled fake assessment.
- [ ] Status progresses through queued/running/completed, with a failure path tested.
- [ ] Completed results are persisted and exposed only after access checks.
- [ ] The UI or an API demonstration proves review is possible while generation runs.
- [ ] Closing the original browser or logging out does not cancel the queued job.
- [ ] Worker output does not overwrite human investigation work.

CI should check formatting, `go vet`, tests, sqlc regeneration, and `govulncheck`; add
race-detector runs where practical. Keep the setup lightweight and reproducible.

## 9. What Comes Next, Not First

After the vertical slice and fake-worker integration, add the minimum analyst updates,
comments, user administration, notifications, audit events, and frontend integration in
agreed increments. Coordinate model integration with the AI specialist independently.

Attachments, detailed assets, task management, PDF reporting, complex escalations,
external integrations, advanced analytics, and BPMN are not first-milestone work.
Do not spend the initial milestone on Azure-specific deployment automation or a general
application framework; provide a runnable Go service and coordinate deployment needs
with the architect.
