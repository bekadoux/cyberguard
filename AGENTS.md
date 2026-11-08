# Repository instructions

## Dev environment tips

- The only implemented service is the Go API; frontend, Python/AI and incident APIs
  in `docs/project-plan.md` are planned, not existing functionality. Application
  wiring is in `backend/go/cmd/api/main.go`; deployment ownership and integration
  contracts are in `docs/architect-handoff.md`.
- The Go module is `backend/go` (Go 1.27.3). Run Make targets from the repository
  root; run direct Go commands from `backend/go`, not the root.
- Make targets use GNU Make. Database bootstrap requires `psql` 15+ for `\getenv`;
  see `docs/database-setup.md` for PostgreSQL provisioning.
- sqlc v1.31.1 and govulncheck v1.9.0 run through pinned `go run` commands in
  `Makefile`; no global installation is needed. Tool/dependency downloads and the
  vulnerability database require network access.

## Testing instructions

- No CI workflow is checked in; `Makefile` defines the available checks.
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
  `TEST_INCIDENT_DATABASE_URL`, `TEST_IDENTITY_MIGRATION_DATABASE_URL`.
  Missing settings fail tests, not skip them. Identity fixtures migrate into random
  schemas and drop those schemas afterward; do not point them at ordinary dev or
  deployed data. See `docs/database-setup.md` for preparation.

## Database and environment

- Neither the API nor Make automatically loads `.env` files. Export the root
  `.env`, then use `make go-run`; `db/.env` is for privileged one-off commands.
  Never launch the API from a shell still exporting administrator/migration/demo
  secrets.
- Local HTTP authentication requires `APP_ENV=development` and
  `SESSION_COOKIE_SECURE=false`; otherwise defaults are production/Secure.
  Use the consistent `127.0.0.1` addresses in `.env.example`, not mixed localhost
  and IP hosts. Trusted origins are exact; CORS does not override cookie policy.
- API startup does not bootstrap, migrate or seed. For fresh instances, run
  `make db-bootstrap-identity` and `make db-bootstrap-incidents`, then
  `make db-migrate-identity`; `make db-seed-demo` is optional and development-only.
  Credentials, grants and setup details: `docs/database-setup.md`.
- Identity migrations live in `db/migrations/identity/`; add numbered migrations
  rather than editing shared/applied ones. New tables need explicit runtime grants.
- Edit handwritten queries in `backend/go/sql/identity/` and schema migrations,
  then run `make go-sqlc` and commit the generated changes. Never hand-edit
  `backend/go/internal/db/identity/`. `make go-sqlc-check` checks without rewriting.

## Authentication safeguards

- Use `internal/sessionstore`, not bare pgxstore: new tokens INSERT, loaded tokens
  UPDATE only while present/unexpired. Upsert can resurrect concurrently revoked
  sessions. Keep `sessionstore.WithRequest` outside SCS `LoadAndSave` and preserve
  its request state through persistence.
- Preserve auth response buffering until persistence completes. Only successful
  explicit login/logout may emit Set-Cookie; `/auth/me` and failed auth responses
  must not replace/delete cookies. Login cookie expiry is the absolute deadline;
  idle expiry is enforced server-side. These rules prevent stale-response races.
- Protected requests must check current account status/role and `auth_version`,
  not trust cached session roles. Password, block/unblock and role changes increment
  `auth_version`. Generated `identity.User` includes the password hash: return safe
  HTTP DTOs, never serialize the persistence model.
- Before changing auth middleware/store behavior, consult
  `docs/authentication-handoff.md`, `docs/authentication-security-review.md` and
  `api/go-authentication.openapi.yaml` for the contract and concurrency regressions.

## PR instructions

- No required PR-title format.
- Before submitting Go changes, run `make go-check`, then `make go-build` (API,
  migration and seed binaries under `backend/go/bin/`). For database/session changes,
  also run `make go-test-integration` with the dedicated test instances.
- Report checks run and any blocked by missing tools, network access or database
  prerequisites; do not remove required checks or describe blocked checks as passing.
