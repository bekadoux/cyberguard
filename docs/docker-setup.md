# Local Docker Setup

This is the runnable local setup, not an Azure deployment. See the
[containerization handoff](containerization-handoff.md) for ownership and future
AI/deployment boundaries. Four long-running containers provide Caddy with the compiled
React frontend, Go and two separate PostgreSQL instances. No Python service or
assessment queue is implied by a healthy container.

For the separate Ubuntu 26.04 AI VM, use [AI VM post-install deployment](azure-ai-deployment.md).
Its standalone Compose/private TLS configuration is not this development override.

## Prerequisites

- Linux containers, a working Docker daemon and Docker Compose v2+.
- GNU Make, a POSIX shell, OpenSSL, jq and standard Unix tools; use Linux or WSL2.
- Network access to download pinned images, Go modules, npm dependencies and runtime
  Debian packages.
- For host Go development/tests, the repository's Go toolchain and normal test tools.

Run commands from the repository root. Docker builds use root context with
`backend/go/Dockerfile` and `frontend/Dockerfile`. Both work without Buildx; Docker may
warn that its legacy builder is deprecated. Building does not require host Go or Node.js.

## Fresh local environment

```sh
export DOCKER_SECRET_GID="$(id -g)"
make docker-secrets
make docker-config-check
make docker-build
make docker-caddy-check
make docker-db-up
make docker-bootstrap
make docker-migrate
make docker-status
# Optional, explicit development/demo accounts:
make docker-seed-demo
make docker-up
curl --fail http://127.0.0.1:8088/api/readyz
# Open http://127.0.0.1:8088/login
```

The build produces two independently rebuildable images:

- `cyberguard-go:local`: API, migration and seed binaries plus shared SQL migrations.
- `cyberguard-web:local`: static frontend and the baked Caddyfile. Pinned Node 24.22.2
  and npm 12.2.2 build locked dependencies; only `dist/` is copied into the pinned Caddy
  runtime. Node, npm, source, tests, `.env`, secrets and host LAN overrides are not served.

Both run as UID/GID 10001 with a read-only root
filesystem, no Linux capabilities and a small writable temporary filesystem. CPU,
memory, process and log limits are local defaults, not measured deployment sizing.

For an already provisioned environment, update only the web package with
`make docker-build-web docker-caddy-check docker-up`; this does not rerun database
setup or delete volumes. `make docker-build-go` builds only Go. Compose deliberately
uses prebuilt images (`up --no-build`), with no host source/config mounts into Caddy.
Rebuild the web image after frontend or Caddyfile changes, then recreate it with
`make docker-up`. Rebuilding an image alone does not update a running container.

`make docker-up` starts runtime services only. It never bootstraps/migrates/seeds;
those actions use explicitly invoked, disposable tooling containers. Repeating
migrations is a no-op; repeating seed preserves existing accounts. Bootstrap is
repeatable but reapplies managed-role passwords, so it is not routine startup.
Never enable the `tools` profile for ordinary `up`: invoke its services individually
through the documented targets, not as parallel runtime dependencies.

## Secret files and configuration

`make docker-secrets` generates independent random passwords, container connection
URLs and host connection URLs in `.local/docker-secrets/`. It refuses to overwrite an
existing directory. Retain these files with the matching data volumes; do not delete
them to fix setup errors or generate new credentials over populated databases.

The containing directory is mode 0700; files are 0640 with the invoking user's group.
Go receives that supplementary group through `DOCKER_SECRET_GID` (default 1000).
Set it to `id -g`, especially on hosts using another group. Compose file-backed secrets
are read-only bind mounts: YAML secret uid/gid/mode cannot be relied on to change source
file permissions. Do not make the files world-readable to work around a permission error.

Only required secret files are mounted into each service. The API gets two runtime
URLs, not administrator/migration credentials or demo passwords. PostgreSQL gets its
own bootstrap administrator secret. The narrow psql setup adapter exports privileged
values only into that one-off setup process. Go reads files natively; see
[file-backed credential semantics](database-setup.md#file-backed-go-credentials).

Local Compose secrets are plaintext host files, not an encrypted vault. `.local/` is
excluded from Git and Docker builds. If choosing a different directory, keep it outside
the build context and protect it yourself. Never commit secrets, print them in logs,
or pass them as image build arguments. Editing files alone does not rotate database
roles or reload running applications. Changing initial `POSTGRES_PASSWORD_FILE` input
does not change the administrator password in an existing data directory.

The Make targets select `compose.yaml` plus `compose.dev.yaml` explicitly. The base
defines services, private database networks and no published ports; the development
override enables loopback access and explicitly insecure local HTTP cookies. Never
treat that override as production-ready configuration.

Optional non-secret settings can be exported before provisioning/startup:

| Variable | Default |
| --- | --- |
| `COMPOSE_PROJECT_NAME` | `cyberguard` |
| `DOCKER_SECRETS_DIR` | `.local/docker-secrets` relative to repository root |
| `DOCKER_SECRET_GID` | `1000` |
| `DOCKER_GO_IMAGE` | `cyberguard-go:local` |
| `DOCKER_WEB_IMAGE` | `cyberguard-web:local` |
| `DOCKER_GO_PORT` | `8080` |
| `DOCKER_WEB_PORT` | `8088` |
| `DOCKER_IDENTITY_PORT` | `5433` |
| `DOCKER_INCIDENT_PORT` | `5434` |

Set alternate database ports **before** creating secrets: generated host URL files
capture those ports. Container URL files always use service DNS and port 5432. If ports
later change, update only the host connection files without changing role passwords.

Compose reads the root `.env` for interpolation, but does not inject it wholesale.
Existing host-oriented `.env` and privileged `db/.env` must not be passed as a runtime
`env_file`. The API's Compose environment specifies secret paths, not their contents.

## Host Go development and tests

Start/setup only the database services, then use the generated host URL files:

```sh
export IDENTITY_DATABASE_URL_FILE="$PWD/.local/docker-secrets/identity_runtime_host_url"
export INCIDENT_DATABASE_URL_FILE="$PWD/.local/docker-secrets/incidents_runtime_host_url"
export APP_ENV=development SESSION_COOKIE_SECURE=false
export TRUSTED_ORIGINS=http://127.0.0.1:5173
make go-run
```

Use a clean runtime-only shell. Do not also export direct database URL values: Go
rejects configuring both sources. Do not run host Go alongside a container publishing
the same HTTP port. For dedicated integration tests, use a **different Compose project,
secrets directory and database ports**, bootstrap its roles, and supply the four test
URLs documented in [database setup](database-setup.md). Never use ordinary dev volumes.

## Gateway contract

Caddy listens on `http://127.0.0.1:8088`. Its container runs non-root on port 8080;
the host port is just development publishing, not another host-installed server.

| Request | Behavior |
| --- | --- |
| `/api/auth/*`, `/api/incidents*`, other `/api/*` | Strip `/api` and proxy to existing Go routes |
| `/__gateway_health` | Caddy process health, independent of Go/database readiness |
| `/internal` path segments, including `/api/internal/*` | Concealed 404, never forwarded |
| `/ai`, `/ai/*` | Base: explicit 503; optional AI overlay: proxy to the AI API |
| `/`, `/login`, `/account`, `/incidents`, `/incidents/*` | Built SPA shell for GET/HEAD, including direct navigation/reload |
| Existing `/assets/*` fingerprinted files | Static assets with one-year immutable caching |
| `/index.html`, `/theme.js`, `/favicon.svg` | Static files with `Cache-Control: no-store` |
| Missing assets/source/config files, other paths | 404, never rewritten to the SPA |
| Non-GET/HEAD requests outside API/AI/internal handling | 405, never served as frontend HTML |

HTML, unfingerprinted public files and errors are no-store; only existing fingerprinted
assets are immutable. Static responses preserve nosniff and never set session cookies.
API/AI/internal matching precedes all static/SPA handlers. When adding a new client
route, update the explicit SPA allowlist and `docker/test-web.sh` as needed. The app's
authentication guard still controls protected screens; static HTML contains no user data.

The AI overlay sets `AI_HANDLER=Caddyfile.ai` to import the baked proxy snippet inside
the shared `/ai` handler. The base selects `Caddyfile.no-ai`. It does not mount a second
full Caddyfile or remove static/SPA/security handlers; both configurations are validated
by `make docker-caddy-check`. Rebuild the web image when changing any Caddy snippet.

Go's root-relative `Location` response headers are rewritten with `/api`; session
cookies keep Path=/ so the optional AI API can receive the same host-only cookie.
Host/Origin and authentication policy are preserved; packaged UI/API requests are
same-origin and need no new trusted-origin exception. Credentialed Vite requests from
`http://127.0.0.1:5173` remain allowed. Frontend code uses `/api` as the Go base URL;
the direct `127.0.0.1:8080` API remains available for local diagnostics/tests.

Do not weaken CSRF, cookie scope or SameSite to solve a routing problem. The local
configuration does not trust forwarded IP headers: Go's per-peer login limit sees
Caddy as one peer. This is acceptable for loopback-only development; production must
configure narrowly scoped proxy trust and forwarding policy before public use.

Caddy access-body/cookie logging and its admin API are disabled/not enabled. Local
HTTP uses no certificate state; `/data` and `/config` are temporary filesystems.
Future HTTPS deployment needs persistent certificate state, verified private upstream
connections and a production configuration. Do not expose the development override.

## Lifecycle and safety

```sh
make docker-logs
make docker-down
# Existing data and secrets are retained; no bootstrap/seed is needed on restart:
make docker-up
```

The default local ports are bound to `127.0.0.1`, not all interfaces. Within Compose,
Go uses `identity-db:5432` and `incident-db:5432`. Development database networks are
ordinary bridges so Docker can publish loopback ports; the base networks remain
internal. Go connects to both; future Python must never join the identity network.

Go `/healthz` is process liveness and `/readyz` checks both database connections. Neither
requires Python or proves schema readiness. Compose waits for startup health; it does
not restart dependent services merely because a database later becomes unhealthy.
Database recovery should restore readiness without restarting Go. Stop grace periods
allow graceful shutdown. Development SQL traffic is unencrypted; public/cross-VM TLS
and production cookie/proxy settings belong to the later deployment increment.

Named volumes preserve data across container recreation. `make docker-down` does not
remove them. **`docker compose down -v` deletes data**: do not use it as routine cleanup.
PostgreSQL major upgrades require a deliberate data migration. Version/digest pins
must be reviewed and updated deliberately, including runtime package updates.

No scheduled/off-host backup infrastructure is supplied. Manual local dumps may be a
follow-up; dumps contain password hashes and sessions and need the same care as secrets.

## Checks

```sh
make docker-test-secrets
make docker-config-check
# After building the web image; validates its baked config, not a host bind mount:
make docker-caddy-check
make docker-test-readiness
# Synthetic positive/negative HTTP-checker tests; no Docker or network required:
make docker-test-web
```

The readiness regression holds fresh, network-isolated PostgreSQL containers in their
socket-only initialization phase. The configured probes must reject that temporary
server and become healthy only once the final TCP server accepts connections.

The HTTP-checker regression verifies that forbidden cookies, Vite source references,
HTML in non-HTML responses and grep errors fail explicitly. It is also included in
`make docker-smoke`, alongside checks against the actual packaged gateway.

For Go changes, also run the checks/build/integration suite in repository instructions.
Real container lifecycle checks are additional evidence, not a replacement for Go tests.

### Disposable end-to-end checks

```sh
make docker-smoke
# Additionally exercise the complete Go/PostgreSQL race suite in that fixture:
DOCKER_TEST_GO_INTEGRATION=1 make docker-smoke
# All UI regressions against the built assets, then real same-origin browser flows:
DOCKER_TEST_FRONTEND=1 make docker-smoke
# Additionally enable the real queue/worker/API using labelled synthetic output:
DOCKER_TEST_AI=1 DOCKER_TEST_FRONTEND=1 make docker-smoke
```

Requires Bash, curl and jq in addition to normal Docker setup prerequisites. The runner
creates a fresh `cyberguard-smoke-*` project with separate secrets/volumes. It uses
loopback test ports 15433/15434 (SQL), 18080 (Go) and 18088 (Caddy), never ordinary dev
volumes. Override conflicts using `DOCKER_TEST_IDENTITY_PORT`,
`DOCKER_TEST_INCIDENT_PORT`, `DOCKER_TEST_GO_PORT` and `DOCKER_TEST_WEB_PORT`.

Checks cover first-initialization TCP readiness, repeated setup, all-role login,
Unicode payloads, access isolation, CORS/CSRF, reserved routes, Location rewriting,
rate limits, secret scoping/logs,
session/data persistence across full container recreation, both database outages and
graceful shutdown. `docker/test-web.sh` also verifies the built shell, deep links, HEAD,
asset MIME/cache/ETag behavior, missing files and reserved-path/method boundaries.
The shell stays available while Go is stopped, independently of API readiness.

`DOCKER_TEST_FRONTEND=1` additionally requires the host Node/npm versions and browser
dependencies from [frontend setup](frontend-setup.md). It uses the actual disposable
Caddy origin, disables Vite startup, runs the normal Firefox/Chromium regressions with
their synthetic APIs, then packaged live flows **without interception**. Real flows
verify login/cookie flags, account/new/detail reloads, Unicode creation, Location
rewriting, persistence, employee isolation, staff visibility and revoked-session rejection.
Credentials/cookies remain inside a discarded-output subprocess. It never uses ordinary
dev data or lowers the login limit; one throttled login may wait for `Retry-After`.
`DOCKER_TEST_AI=1` provisions separate AI secrets/role in that disposable project,
starts the API and worker in `AI_MODE=fake`, and verifies trigger enqueueing, completed
fixture provenance, staff access including own reports, and employee denial. The AI API readiness
probe waits for its DB connection. No model profiles/downloads are enabled. Combined
with the frontend flag, live browsers verify the visible fixture warning and staff
staff own-report access and employee restrictions through actual Caddy requests. Normal development services,
credentials and models are not used. Without this flag, the original four-service
smoke remains AI-disabled (new DB inserts may still queue jobs after migration).
All optional flags can be combined. Without the browser flag, smoke remains an
HTTP/cookie-jar test, not browser acceptance or a production deployment check.

The runner intentionally removes **its own disposable volumes** on exit, unlike
`make docker-down`. It removes successful test state; failed diagnostic files are kept
under protected `.local/cyberguard-smoke-*` directories and may contain secrets/cookies.
For debugging, `DOCKER_KEEP_TEST_ENV=1` retains that test project and its sensitive state;
the script prints the project name for explicit later cleanup. Do not reuse its test
credentials as a normal development environment.

### Local validation evidence

Validation on 9 November 2026 used Docker Engine 29.9.3 / Compose 5.7.0 on Linux,
Go 1.27.3, PostgreSQL 17.11 and Caddy 2.11.9, with the pinned images in source control:

- `make go-check`, then `make go-build` passed. Govulncheck reported no affected called
  symbols/imported packages; the previously documented unused OpenPGP module advisory
  remains, not a new containerization finding.
- `make docker-test-secrets`, Compose validation and Caddy validation passed.
- All three Go OpenAPI specifications validated with the local Caddy server URL.
- The full disposable smoke test, including `DOCKER_TEST_GO_INTEGRATION=1`, passed
  repeatedly with the race detector and real PostgreSQL. Its cleanup removed its own
  containers/volumes; ordinary development data was not used.
- An adversarial run supplied parent Make project/secret/Compose overrides. The runner
  still used its own disposable project and left existing secret files unchanged.
- Credential-source tests also passed with inherited `_FILE` settings exported, matching
  the host-development workflow. Direct environment compatibility is retained.

The preceding bullets record the original foundation checks, before frontend packaging.
Manual backup/restore tooling is not included.

Frontend packaging was subsequently verified on the same local platform:

- `make docker-build` built the separate Go and web images with the legacy Docker
  builder. Caddy's baked config and base/development Compose settings validated.
- `make docker-test-secrets` and `DOCKER_TEST_FRONTEND=1 make docker-smoke` passed.
  The latter included static/cache/routing HTTP regressions, **118** browser regressions
  against built assets and **4** packaged shell/real same-origin Firefox/Chromium checks.
- `make frontend-check` passed **92** unit/component tests and all static/build checks;
  the separate Vite browser run also passed all **118** checks. `npm audit` reported
  no known vulnerabilities. No Go/SQL application code changed;
  Go race/integration checks were not rerun for the packaging-only change.
- Disposable resources were removed automatically; ordinary development services and
  volumes were not recreated or removed. The packaged image is ready for an explicit
  `make docker-up` to update the ordinary local stack.

No Azure VM, Python worker or production TLS was deployed or accepted by these checks.
