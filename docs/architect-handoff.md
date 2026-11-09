# Architect Handoff — Go and SQL Foundation

## Status and responsibilities

Delivered: Go HTTP foundation, two PostgreSQL pools, health/readiness, bootstrap SQL,
identity migrations, sqlc queries, explicit demo seed, and tests.

The subsequent authentication increment adds HTTP login/logout/current-user, SCS and
current-account checks; see [authentication handoff](authentication-handoff.md) for
its settings and integration/review tasks. The [incident core handoff](incidents-handoff.md)
adds incident migration/create/read; AI/job tables and enqueueing, Python database role,
frontend and deployment automation remain outside these increments.

The [local Docker setup](docker-setup.md) now supplies Compose, the Go image, Caddy,
secret files and one-off tooling. The [containerization handoff](containerization-handoff.md)
records the separately packaged frontend served by Caddy and a later application/AI VM split.
The build and SQL contracts below remain applicable; Azure deployment is deferred.

- **Architect:** Dockerfile/Compose, networking, volumes, secret delivery, health checks,
  orchestration of one-off setup commands.
- **Engineer:** SQL roles/grants, goose migrations, sqlc, seed, application code and tests.
- **Joint:** security/schema review, startup contract, incident/job agreement with domain
  and AI specialists. Python must not introduce competing migrations.

## Build contract

- Go **1.27.3**; pin the toolchain/build image explicitly, not `latest`.
- Module: `backend/go`; build `./cmd/api`, `./cmd/migrate`, `./cmd/seed` as separate binaries.
- `make go-build` builds all three; `make go-check` runs local quality checks.
- Copy/mount `db/migrations/identity` and pass the actual directory to `migrate -dir PATH`.
- Also mount `db/migrations/incidents`; run `migrate -database incidents -dir PATH`
  with only `INCIDENT_MIGRATION_DATABASE_URL` for that controlled migration step.
- Bootstrap requires psql 15+, normally available in PostgreSQL images; no additional
  permanently running bootstrap service is needed.

## Network and database contract

| Component | Development host binding | Container port |
| --- | --- | --- |
| Caddy local HTTP gateway | `127.0.0.1:8088` | 8080 |
| Go HTTP | `127.0.0.1:8080` | 8080 |
| Identity PostgreSQL | `127.0.0.1:5433` | 5432 |
| Incident PostgreSQL | `127.0.0.1:5434` | 5432 |

Inside Docker use the selected PostgreSQL service names, not container-local localhost,
and set Go's `HTTP_ADDR=:8080`. Database host publishing is development-only; keep DB
ports private in deployment. Use two server instances, not two databases in one server.

| Database | Owner / migration login | Go runtime login |
| --- | --- | --- |
| `cyberguard_identity` | `identity_migrator` | `go_identity` |
| `cyberguard_incidents` | `incidents_migrator` | `go_incidents` |

Do not use runtime names as `POSTGRES_USER`: the image's initial administrator is
typically a superuser. Create a separate bootstrap administrator, then run our SQL
to create restricted migration/runtime logins. Keep ownership with migration roles.

## Scoped process environments

**API only**, from root `.env.example`:

```text
HTTP_ADDR
IDENTITY_DATABASE_URL
INCIDENT_DATABASE_URL
DATABASE_TIMEOUT        # optional: default 3s, maximum 1m
SHUTDOWN_TIMEOUT        # optional: default 10s, maximum 1m
```

**Bootstrap only**, from `db/.env.example`: respective administrator URL plus raw
migration/runtime passwords. Execute `db/bootstrap/identity.sql` and `incidents.sql`
with psql on the correct instance. Preserve ON_ERROR_STOP and propagate failures.
Repeat bootstrap resets managed passwords but preserves data; do not run on every API restart.

**Migration only:** `IDENTITY_MIGRATION_DATABASE_URL` as `identity_migrator`.
Incident migration only: `INCIDENT_MIGRATION_DATABASE_URL` as `incidents_migrator`.

**Explicit demo seed only:** migration URL plus the four `DEV_*_PASSWORD` variables
in [database setup](database-setup.md). Never routine production startup.

Go commands also accept the corresponding `_FILE` alternatives, exclusively of direct
values. Compose mounts only each service's required files and reuses the existing SQL
through a one-off psql adapter. See Docker setup for permission/group requirements.

Never inject admin/migration/demo secrets into API or Python. Templates contain no
working credentials. Handle password URL encoding and deployment TLS explicitly.

## Startup order and commands

```text
Start two PostgreSQL instances with persistent volumes
  → wait for administrator connections
  → controlled bootstrap: databases and restricted roles
  → migrate identity and incident databases with separate one-off migrate invocations
  → optionally seed an explicitly requested local/demo setup
  → start API with runtime credentials only
  → verify readiness
```

Host-side commands after exporting the appropriate administrative environment:

```sh
make db-bootstrap-identity
make db-bootstrap-incidents
make db-migrate-identity
make db-status-identity
make db-migrate-incidents
make db-status-incidents
# Only for an explicitly requested demo:
make db-seed-demo
```

Start the API from a **clean runtime-only environment**, not the privileged admin shell.
The API never bootstraps, migrates or seeds. Migrate supports up/status, requires the
migration role, and uses a one-minute deadline and bounded PostgreSQL session lock.

## Joint acceptance

1. Fresh dedicated volumes bootstrap/migrate using documented commands.
2. Repeated migrations do nothing; repeat seed does not reset or unblock accounts.
3. Runtime roles are restricted; Go cannot create tables or delete users.
4. Both DBs online: `/readyz` returns 200. Stop either: readiness 503, `/healthz` still 200.
5. Restart the failed DB: readiness recovers without restarting Go.
6. Data and migration versions survive container recreation with volumes retained.
7. Probes work with Python offline; SIGTERM stops the API gracefully.
8. Integration tests run against separate disposable DBs, not ordinary demo volumes.

Readiness is currently connectivity-only, not proof of schema/authentication readiness.
Check available probe tooling in the selected runtime image; do not assume curl exists.
Allow a container stop grace period beyond the configured HTTP shutdown deadline.

Initial engineer validation used two real temporary PostgreSQL instances without Docker.
The local Docker increment additionally verified fresh/repeated setup, full container
recreation with retained data/sessions, database outage/recovery and graceful shutdown.
This is not public TLS/proxy or deployed-browser acceptance. See
[database setup](database-setup.md) for schema decisions, exact commands, test isolation
and the module-only vulnerability scanner note. Public probes are in [OpenAPI](../api/go-foundation.openapi.yaml).
