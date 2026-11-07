# Go Backend Foundation

This increment provides a runnable HTTP service, typed configuration, two independent
PostgreSQL pools, liveness/readiness probes, and tests. Authentication, business
schemas/migrations, role-provisioning scripts, and incident/assessment APIs are not
implemented yet. It is not the complete first milestone in the
[engineer handoff](software-engineer-handoff.md).

## Prerequisites and ownership

- Use Go **1.27.3** for local development and the architect's build image/CI. The
  `go` directive in `backend/go/go.mod` sets the minimum version; it does not prevent
  a newer installed toolchain from being used.
- Run commands below from the repository root. GNU Make is optional; each target
  displays the underlying Go command.
- The architect owns Compose, database containers, networking, and volumes. This
  increment intentionally adds no competing Compose configuration.
- The engineer owns database roles/grants and coordinated goose migrations. Their
  implementation follows with the identity/authentication work. No application
  tables are needed for the current connectivity-only readiness probe.
- Successful readiness requires both PostgreSQL instances and working runtime roles.
  Without them, the API can still start and expose liveness, but stays unready.

### Local connection contract

| Component | Host-side address | Database | Runtime role |
| --- | --- | --- | --- |
| Go API | `127.0.0.1:8080` | — | — |
| Identity PostgreSQL | `127.0.0.1:5433` | `cyberguard_identity` | `go_identity` |
| Incident PostgreSQL | `127.0.0.1:5434` | `cyberguard_incidents` | `go_incidents` |

Give these settings to the architect. Database port publishing is for local development
only and must bind to loopback, not all interfaces. Credentials must be generated locally;
the example file deliberately contains no usable passwords. Runtime roles must not be
superusers or have `CREATEDB`/`CREATEROLE` privileges. Future migrations use separate
credentials and do not run automatically during API startup.

Inside Compose, use the architect's database service names and their internal `5432`
ports, not the host-side addresses above. Set `HTTP_ADDR=:8080` for a container to accept
traffic through its Docker network/published port. The default loopback binding is for
running Go directly on the host.

## Configure and run

1. Copy the example and replace both password placeholders with local credentials:

   ```sh
   cp .env.example .env
   ```

2. Export the configuration in a POSIX-compatible shell, then start the API:

   ```sh
   set -a
   . ./.env
   set +a
   make go-run
   ```

   Only source a file you trust. The API and Makefile do **not** automatically read
   `.env`; configuration comes from the process environment. Passwords containing
   reserved URL characters must be percent-encoded in the connection URLs.

3. In another terminal:

   ```sh
   curl -i http://127.0.0.1:8080/healthz
   curl -i http://127.0.0.1:8080/readyz
   ```

For a directly executable binary, run `make go-build`, then `backend/go/bin/api` with
the same environment. Ctrl+C or SIGTERM initiates graceful HTTP shutdown and closes
both pools. A shutdown deadline forces remaining HTTP connections closed and exits
with a nonzero status. Signal the binary directly when testing process lifecycle;
`go run` adds a tool-process wrapper.

### Environment variables

| Variable | Default | Validation / purpose |
| --- | --- | --- |
| `HTTP_ADDR` | `127.0.0.1:8080` | `host:port`; numeric port 1–65535; `:8080` binds all interfaces |
| `IDENTITY_DATABASE_URL` | Required | `postgres://` or `postgresql://` URL with explicit host, user, database |
| `INCIDENT_DATABASE_URL` | Required | Independently configured PostgreSQL URL with explicit host, user, database |
| `DATABASE_TIMEOUT` | `3s` | Positive duration, at most `1m`; readiness deadline, connection and pool ping timeouts |
| `SHUTDOWN_TIMEOUT` | `10s` | Positive duration, at most `1m`; graceful HTTP shutdown deadline |

Explicit empty values are invalid rather than silently using defaults. Driver-level
connection options are also parsed before the HTTP listener opens. Configuration
errors name the affected setting/dependency without printing its secret value.

`sslmode=disable` in the example is only for trusted local development. Use the TLS
policy agreed with the architect for deployment; do not copy local security exceptions
to a remote deployment.

## HTTP contract

The machine-readable contract is [Go foundation OpenAPI](../api/go-foundation.openapi.yaml).
These public probes contain no incident or account data:

| Route | Healthy response | Failure |
| --- | --- | --- |
| `GET /healthz` | `200 {"status":"ok"}` | Independent of databases, Python, models, and application schemas |
| `GET /readyz` | `200 {"status":"ready"}` | `503` if either database cannot be pinged before the deadline |

Both database probes run concurrently within one `DATABASE_TIMEOUT` budget. Pools
are lazy: unavailable databases do not stop HTTP startup, while malformed configuration
does. Readiness is a connectivity check, not proof that migrations, authentication,
or incident functionality are ready. A recovered database is checked on subsequent
readiness requests; no process restart is required.

Readiness failure has this deliberately generic body:

```json
{"error":{"code":"not_ready","message":"Service is not ready."}}
```

Unknown routes, unsupported methods, and recovered handler panics use the same error
envelope with `not_found`, `method_not_allowed`, and `internal_error` respectively.
If a handler panics after starting a response, the connection is aborted rather than
appending an error to the partially written response.
Responses are JSON, use `Cache-Control: no-store`, and include `X-Request-ID` and
`X-Content-Type-Options: nosniff`.

### Operational defaults

- Maximum 4 connections per database; no eagerly opened minimum connections.
- Connection lifetime 30 minutes plus up to 1 minute jitter; idle lifetime 5 minutes.
- HTTP header/read/write/idle timeouts: 5s / 15s / 65s / 60s. The write timeout exceeds
  the maximum configurable readiness deadline.
- Maximum request body: 1 MiB, enforced when a handler reads it; maximum headers: 1 MiB.
- JSON logs go to stdout. Request logs contain route templates, status, duration and
  request ID, not raw URLs/query strings, bodies, cookies or authorization headers.
  Driver errors and panic values are not exposed in probe responses or routine logs.
- No authentication or browser CORS/CSRF policy exists yet. Add the agreed policies
  before introducing state-changing application routes, not after deploying them.

## Checks

```sh
make go-check
make go-build
```

`go-check` verifies formatting without editing files, runs `go vet`, tests with the
race detector, and `govulncheck` pinned to `v1.9.0`. Dependency/tool downloads and
vulnerability database access need network connectivity. Go module dependencies are
pinned in `go.mod` and verified by `go.sum`. This increment provides local check
commands, not a CI workflow.

Ordinary tests require no PostgreSQL. They cover configuration, safe errors/logging,
pool settings, stalled dependency deadlines, liveness/readiness HTTP behavior, request
body limits, graceful shutdown, and forced shutdown on deadline. HTTP lifecycle tests
and stalled-connection tests bind ephemeral loopback ports.

### PostgreSQL integration suite

Use **two dedicated disposable PostgreSQL instances**, never development or deployed
data. Provision the database names and restricted runtime roles before running:

```sh
export TEST_IDENTITY_DATABASE_URL='postgres://TEST_USER:URL_ENCODED_TEST_PASSWORD@127.0.0.1:15433/TEST_IDENTITY_DB?sslmode=disable'
export TEST_INCIDENT_DATABASE_URL='postgres://TEST_USER:URL_ENCODED_TEST_PASSWORD@127.0.0.1:15434/TEST_INCIDENT_DB?sslmode=disable'
make go-test-integration
```

Replace placeholders with the dedicated test credentials. The suite fails clearly if
either variable is missing. It checks actual connectivity, configured database/user,
restricted runtime role attributes, and readiness failure after closing one pool.
It performs only read-only probes and does not provision roles, apply migrations, or
manage containers. It is intentionally separate from the default test target.

For the joint Compose acceptance check, start both databases and confirm readiness
returns `200`. Stop either instance and confirm readiness changes to `503` while
liveness stays `200`; restart it and confirm readiness recovers. Python need not run.

## Next increment

Agree role provisioning and migration execution with the architect, then implement
identity migrations, repeatable development accounts, and login/logout/current-user
with SCS sessions and Argon2id. Incident create/read and atomic assessment enqueueing
follow; assessment schema requires the engineer–AI specialist agreement first.
