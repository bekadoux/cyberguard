# Repository instructions

## Dev environment tips

- The only implemented application backend is the Go API, including incident core create/read.
  The frontend implements login/session UI, themes and scoped incident create/list/detail.
  Python/AI, assessment enqueueing and investigation mutations remain planned. See
  `docs/frontend-setup.md` and `docs/incidents-handoff.md` for the explicit boundaries. Application
  wiring is in `backend/go/cmd/api/main.go`; deployment ownership and integration
  contracts are in `docs/architect-handoff.md`.
- Local Docker runs Caddy, Go and two PostgreSQL instances; commands and prerequisites
  are in `docs/docker-setup.md`. Set `DOCKER_SECRET_GID=$(id -g)` for file permissions.
  Bootstrap/migrate/seed are explicit one-off tools, never API startup actions. Do not
  enable the `tools` profile for ordinary `up`, or remove volumes as routine cleanup.
  Caddy exposes Go at `http://127.0.0.1:8088/api`; direct development access on 8080
  uses the existing unprefixed routes. Caddy serves the packaged frontend at
  `http://127.0.0.1:8088/login`; Vite at 127.0.0.1:5173 remains available for hot reload.
  `make docker-build` builds separate Go and frontend/Caddy images; Node is build-only.
  AI paths remain unimplemented. Rebuild the web image after frontend/Caddy changes;
  Compose does not bind-mount application source or the Caddyfile.
- Business rules are in `docs/domain-specification.md`. `docs/playbooks.md` and
  `docs/demo-incidents.csv` supply domain reference material and labelled scenarios,
  not an implemented incident importer/seed or measured AI evaluation results.
- `docs/assessment-job-contract.md` is a proposal, not an implemented/final contract.
  Confirm schema, context revision/snapshot, grants and service contracts with the Go
  engineer, AI specialist and architect before implementing the assessment handoff.
- The Go module is `backend/go` (Go 1.27.3). Run Make targets from the repository
  root; run direct Go commands from `backend/go`, not the root.
- Make targets use GNU Make. Database bootstrap requires `psql` 15+ for `\getenv`;
  see `docs/database-setup.md` for PostgreSQL provisioning.
- sqlc v1.31.1 and govulncheck v1.9.0 run through pinned `go run` commands in
  `Makefile`; no global installation is needed. Tool/dependency downloads and the
  vulnerability database require network access.

## Testing instructions

- Frontend: Node 24.22.2/npm 12.2.2, `make frontend-install`, then
  `make frontend-check`. `make frontend-test-e2e` runs Firefox and Chromium with
  Playwright-managed browsers; the separate live suite requires the running demo
  backend and `E2E_PASSWORD_FILE`. `make frontend-test-incidents-live` performs writes
  only against an explicitly selected disposable fixture. See `docs/frontend-setup.md`.
  Never put secrets in frontend configuration, browser storage or test artifacts.
  `DOCKER_TEST_FRONTEND=1 make docker-smoke` also runs all browser regressions against
  the packaged assets plus real same-origin flows, without Vite or API interception
  in those real flows. Requires host Node/npm, installed dependencies and browsers.
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
- Before changing auth middleware/store behavior, consult
  `docs/authentication-handoff.md`, `docs/authentication-security-review.md` and
  `api/go-authentication.openapi.yaml` for the contract and concurrency regressions.

## PR instructions

- No required PR-title format.
- For Docker/setup changes, run `make docker-test-secrets` and `make docker-smoke`;
  the latter includes Compose/Caddy validation and the first-initialization readiness
  regression. Use `DOCKER_TEST_GO_INTEGRATION=1` when also checking Go/SQL integration.
- Before submitting Go changes, run `make go-check`, then `make go-build` (API,
  migration and seed binaries under `backend/go/bin/`). For database/session changes,
  also run `make go-test-integration` with the dedicated test instances.
- Report checks run and any blocked by missing tools, network access or database
  prerequisites; do not remove required checks or describe blocked checks as passing.
