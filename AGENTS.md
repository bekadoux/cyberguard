# Repository instructions

## Dev environment tips

- Start with the architecture section of the root `README.md`. Go API wiring is in
  `backend/go/cmd/api/main.go`; see `docs/go-backend.md` and `docs/frontend.md`
  for component boundaries. The English and Slovak domain specs both describe
  workflow and permissions. Unfinished work and acceptance checks are tracked
  in `docs/backlog.md`, not treated as delivered behavior.
- Staff can read stored AI assessments, including on their own reports; employee
  views never fetch AI output. Assessment refresh is manual, not a keepalive poll.
  Incident inserts queue an initial job against an immutable snapshot even with
  AI disabled. Processing requires the optional FastAPI/worker stack under
  `backend/python/`. See `docs/ai-backend.md` and the local/VM runbooks.
- Local Docker runs Caddy, Go and two PostgreSQL instances; commands and prerequisites
  are in `docs/docker-setup.md`. Set `DOCKER_SECRET_GID=$(id -g)` for file permissions.
  Bootstrap/migrate/seed are explicit one-off tools, never API startup actions. Do not
  enable the `tools` profile for ordinary `up`, or remove volumes as routine cleanup.
  Caddy exposes Go at `http://127.0.0.1:8088/api`; direct development access on 8080
  uses the existing unprefixed routes. Caddy serves the packaged frontend at
  `http://127.0.0.1:8088/login`; Vite at 127.0.0.1:5173 remains available for hot reload.
  `make docker-build` builds separate Go and frontend/Caddy images; Node is build-only.
  The optional AI overlay enables AI paths via a baked Caddy snippet selected by
  `AI_HANDLER`, preserving the same SPA/security handlers. Rebuild the web image after
  frontend/Caddy changes; Compose does not bind-mount source or the Caddyfile.
- AI-VM post-install tooling is in `deploy/ai-vm/`; see `docs/azure-ai-deployment.md`.
  Its Compose is standalone, not an overlay for the local application stack. Bootstrap
  targets Ubuntu 26.04 amd64 and never provisions Azure, databases or migrations.
  Model preparation is explicit; ordinary startup never builds or downloads weights.
  Cross-VM TLS/NSGs and application-side provisioning are required separately. Keep
  runtime secrets outside the checkout and never copy application admin/migrator secrets
  to the AI VM. `make ai-vm-test` needs ShellCheck; `make ai-vm-test-bootstrap` mocks
  privileged actions in disposable Ubuntu; `make ai-vm-smoke` uses real services/TLS but
  synthetic model responses. None validates real inference or an Azure reboot.
- Application-VM post-install tooling is in `deploy/app-vm/`; see
  `docs/azure-app-deployment.md`. It explicitly combines base Compose with its deployment
  overlay, never the local dev/AI overlays. Public Caddy uses persistent certificate
  state; private Go is a read-only TLS allowlist; both databases use verified TLS.
  Provisioning/migrations/seed are explicit. `maintenance-on` pauses public writes;
  `quiesce` stops/blocks Go until successful `migrate`. Remote workers must be stopped
  separately by the operator. Never regenerate credentials or remove volumes to upgrade.
  `make app-vm-test` needs ShellCheck, `app-vm-test-bootstrap` mocks host actions, and
  `app-vm-smoke` uses disposable Linux Docker routed networking plus synthetic AI, not
  Azure/ACME/real inference. Keep fixture and real deployment acceptance distinct.
- Business rules are in `docs/domain-specification.md`. `docs/playbooks.md` and
  `docs/demo-incidents.csv` supply domain reference material and labelled scenarios,
  not a bulk incident importer or measured AI evaluation results. Both files have
  code consumers; do not move them as prose-only docs.
- The Go module is `backend/go` (Go 1.27.4). Run Make targets from the repository
  root; run direct Go commands from `backend/go`, not the root.
- Make targets use GNU Make. Database bootstrap requires `psql` 15+ for `\getenv`;
  see `docs/database-setup.md` for PostgreSQL provisioning.
- sqlc v1.31.1 and govulncheck v1.9.0 run through pinned `go run` commands in
  `Makefile`; no global installation is needed. Tool/dependency downloads and the
  vulnerability database require network access.

## Testing instructions

### Test selection policy

- **Run only the checks needed for the change.** Choose the smallest set that
  meaningfully covers the changed behavior and affected contracts. Do not run every
  suite, rebuild every image or start every fixture just to finish a task or commit.
  These rules govern routine agent work; linked release acceptance checklists are
  not instructions to repeat full acceptance testing after every edit.
- Select checks from the actual diff and risk:
  - **Documentation/comments only:** `git diff --check` and applicable documentation
    checks. No application tests, builds, browsers or Docker fixtures unless executable
    examples or configuration behavior also change.
  - **Go:** formatting and focused tests for affected packages/test cases; targeted
    vet/build checks when relevant. Add `-race` for concurrency, authentication and
    session behavior. Use broader package coverage for shared-code changes.
  - **SQL, migrations, database permissions or session persistence:** relevant
    PostgreSQL integration tests against the two disposable instances, plus affected
    HTTP/race regressions. Mocked tests alone are not sufficient for these changes.
  - **Frontend:** affected unit tests and relevant type/lint/format checks. For browser
    behavior, start with affected Playwright specs in Chromium; add Firefox for
    browser-sensitive changes. Shared navigation, authentication, themes, layout or
    accessibility changes may warrant broader browser coverage, not unrelated backends.
  - **Python AI:** focused tests and static checks for changed modules; add the AI
    fixture only when worker/API/database or cross-service behavior needs validation.
    Never download models or run real inference as a routine check.
  - **Docker/Caddy:** affected configuration validation and relevant secret, readiness
    or web assertion tests. Use base Docker smoke when image/runtime/network/routing
    behavior needs real-container validation; enable optional Go integration, frontend
    or AI coverage only for affected contracts.
  - **VM deployment:** the affected VM's safety tests; bootstrap tests only for host
    preparation changes; deployment smoke for affected TLS, service wiring, upgrade,
    backup or persistence behavior. Do not run the other VM suite unless its contract
    is also affected.
- Expand coverage when focused checks fail, shared dependencies/toolchains or security
  boundaries change, or focused tests cannot establish the required behavior. Explain
  why a broad/slow suite is needed; do not use uncertainty to run everything by default.
- Full merge/release validation is a separate scope: run the relevant aggregate and
  integration/deployment suites when explicitly requested or justified by the change,
  not automatically for every patch. `go-check`, `frontend-check`, full browser suites
  and combined Docker/VM smokes are available gates, not a mandatory per-edit sequence.
- Avoid duplicate coverage: the frontend-enabled Docker smoke already runs the full
  browser regressions against packaged assets plus real same-origin flows. Do not
  routinely rerun that same full suite through Vite, or rerun passed checks when only
  unrelated documentation/comments changed afterward.
- Report checks run, why they were selected, and relevant checks not run or blocked.
  Never imply unrun integration, deployment or real-inference checks passed.

### Available checks and prerequisites

- Frontend: Node 24.22.2/npm 12.2.2; use `make frontend-install` when dependencies
  need installation. `make frontend-check` is the aggregate quality/build check.
  `make frontend-test-e2e` runs Firefox and Chromium with
  Playwright-managed browsers; the separate live suite requires the running demo
  backend and `E2E_PASSWORD_FILE`. `make frontend-test-incidents-live` performs writes
  only against an explicitly selected disposable fixture. See `docs/frontend.md`
  and `docs/docker-setup.md`.
  Never put secrets in frontend configuration, browser storage or test artifacts.
  `DOCKER_TEST_FRONTEND=1 make docker-smoke` also runs all browser regressions against
  the packaged assets plus real same-origin flows, without Vite or API interception
  in those real flows. Requires host Node/npm, installed dependencies and browsers.
  Add `DOCKER_TEST_AI=1` to include the real AI API/worker/DB pipeline in explicitly
  labelled fixture mode. This provisions only disposable AI secrets/roles and never
  downloads models or enables model profiles. It does not validate real inference.
- AI unit tests and integration boundaries are documented in `docs/ai-backend.md`.
  Mocked unit tests alone do not validate PostgreSQL or Docker.
  The disposable fixture smoke covers the real happy-path DB/worker/API integration,
  not concurrent claims, lease expiry, real model inference or production resource usage.
- No CI workflow is checked in; `Makefile` defines the available checks.
- `make docker-smoke` builds and tests a fresh disposable four-container project,
  including restart persistence, SQL outages and HTTP access rules. It deletes only
  its own test volumes. `DOCKER_TEST_GO_INTEGRATION=1 make docker-smoke` also runs the
  full Go/PostgreSQL race suite there; requires host Go, Bash, curl, jq and OpenSSL.
- PostgreSQL healthchecks must probe TCP (`pg_isready -h 127.0.0.1`), not the temporary
  socket-only server used during first initialization. `make docker-test-readiness`
  deterministically verifies that distinction in isolated containers; it is also
  included in `make docker-smoke`.
- `make go-test` runs unit/HTTP tests without PostgreSQL; some tests need permission
  to bind ephemeral loopback ports. `make go-test-race` adds the race detector,
  requiring cgo enabled (`CGO_ENABLED=1`) and, on Linux, a working C compiler.
- `make go-check` checks formatting, generated sqlc output, vet, race tests and
  vulnerabilities; it does **not** run integration tests or build binaries.
  Fix formatting with `make go-fmt`.
- Focused test example, from `backend/go`:
  `go test -race ./internal/httpapi -run '^TestAuthHTTPCookieExpiryAndIdleRefresh$' -count=1`.
- `make go-test-integration` requires **two separate disposable PostgreSQL
  instances**, bootstrapped roles, and `TEST_IDENTITY_DATABASE_URL`,
  `TEST_INCIDENT_DATABASE_URL`, `TEST_IDENTITY_MIGRATION_DATABASE_URL` and
  `TEST_INCIDENT_MIGRATION_DATABASE_URL`.
  Missing settings fail tests, not skip them. Persistence fixtures migrate into random
  schemas and drop those schemas afterward; do not point them at ordinary dev or
  deployed data. See `docs/database-setup.md` for preparation.

## Database and environment

- Neither the API nor Make automatically loads `.env` files. Export the root
  `.env`, then use `make go-run`; `db/.env` is for privileged one-off commands.
  Never launch the API from a shell still exporting administrator/migration/demo
  secrets.
- Go database URLs and demo passwords accept native `_FILE` alternatives. Configure
  exactly one source per setting; even an empty direct value conflicts with `_FILE`.
  Files remove one terminal LF/CRLF only. Compose uses scoped `/run/secrets` mounts;
  its automatic `.env` interpolation does not inject that file into the API wholesale.
- Local HTTP authentication requires `APP_ENV=development` and
  `SESSION_COOKIE_SECURE=false`; otherwise defaults are production/Secure.
  Use the consistent `127.0.0.1` addresses in `.env.example`, not mixed localhost
  and IP hosts. Trusted origins are exact; CORS does not override cookie policy.
- API startup does not bootstrap, migrate or seed. For fresh instances, run
  `make db-bootstrap-identity` and `make db-bootstrap-incidents`, then
  `make db-migrate-identity` and `make db-migrate-incidents`; `make db-seed-demo` is
  optional and development-only.
  Credentials, grants and setup details: `docs/database-setup.md`.
- Migrations live in `db/migrations/identity/` and `db/migrations/incidents/`; add numbered migrations
  rather than editing shared/applied ones. New tables need explicit runtime grants.
- Edit handwritten queries in `backend/go/sql/{identity,incidents}/` and schema migrations,
  then run `make go-sqlc` and commit the generated changes. Never hand-edit
  `backend/go/internal/db/{identity,incidents}/`. `make go-sqlc-check` checks without rewriting.

## Authentication safeguards

- Use `internal/sessionstore`, not bare pgxstore: new tokens INSERT, loaded tokens
  UPDATE only while present/unexpired. Upsert can resurrect concurrently revoked
  sessions. Keep `sessionstore.WithRequest` outside SCS `LoadAndSave` and preserve
  its request state through persistence.
- Preserve auth response buffering until persistence completes. Only successful
  explicit login/logout may emit Set-Cookie; `/auth/me` and failed auth responses
  must not replace/delete cookies. Login cookie expiry is the absolute deadline;
  idle expiry is enforced server-side. These rules prevent stale-response races.
- Incident routes explicitly load/check/refresh before business operations, never
  emit cookies and never save sessions after writes. Do not apply the small auth
  LoadAndSave response buffer to incident payloads or reintroduce late session-save
  errors after a committed business operation.
- Protected requests must check current account status/role and `auth_version`,
  not trust cached session roles. Password, block/unblock and role changes increment
  `auth_version`. Generated `identity.User` includes the password hash: return safe
  HTTP DTOs, never serialize the persistence model.
- Before changing auth middleware/store behavior, consult `docs/go-backend.md`,
  `api/go-authentication.openapi.yaml`, and the `internal/httpapi` and
  `internal/sessionstore` regression tests.

## PR instructions

- No required PR-title format.
- Follow the test selection policy above; preparing a PR or commit does not by itself
  require all aggregate, browser or deployment suites.
- When full Go validation is in scope, use `make go-check` and `make go-build` (API,
  migration and seed binaries under `backend/go/bin/`); add relevant database/session
  integration coverage with the dedicated test instances.
- When Docker smoke is selected, it includes Compose/Caddy validation and the
  first-initialization readiness regression. Secret-generation changes also need
  `make docker-test-secrets`. Enable `DOCKER_TEST_GO_INTEGRATION=1`,
  `DOCKER_TEST_FRONTEND=1` or `DOCKER_TEST_AI=1` only as required by the selected scope.
- Report focused validation and any relevant checks not run or blocked by missing tools,
  network access or database prerequisites. Do not remove tests or weaken assertions to
  reduce runtime, or describe blocked checks as passing.

## Workflow and AI migration safeguards

- See `docs/database-setup.md` for migration heads and `docs/domain-specification.md`
  for workflow rights.
- Do not edit sqlc-generated files to accommodate `resolved_at`: resolution times
  live in a separate table to keep generated incident row shapes compatible.
- Apply `db/migrations/identity/00003_user_admin_audit.sql`,
  incident migrations `00003_context_and_investigations.sql`,
  `00004_job_context_read.sql`, `00005_snapshot_worker_safety.sql`,
  `00006_ai_feedback.sql` and `00007_ai_feedback_uuid.sql` before restarting
  the updated application/AI worker. Migration 00007 refuses conflicting
  feedback decisions for the same normalized job UUID; it never rewrites history.
  Historical jobs are not backfilled. Migration 00005 preserves snapshot-less jobs
  and grants a boolean `ai_worker_ready()` probe;
  pending legacy jobs block worker readiness until an explicit operator decision.
  Never silently retire/requeue/backfill them. See the AI deployment upgrade guide.
- Staff retain professional severity/AI visibility on their own reports; employees
  remain denied AI access. Preserve current-account/incident authorization on each read.
