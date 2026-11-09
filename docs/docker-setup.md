# Local Docker Setup

This is the runnable local setup, not an Azure deployment. See the
[containerization handoff](containerization-handoff.md) for ownership and future
frontend/AI boundaries. The current database/tooling increment runs Go and two separate
PostgreSQL instances; Caddy gateway integration follows separately. No frontend,
Python service or assessment queue is implied by a healthy container.

## Prerequisites

- Linux containers, a working Docker daemon and Docker Compose v2+.
- GNU Make, a POSIX shell, OpenSSL and standard Unix tools; use Linux or WSL2.
- Network access to download pinned images, Go modules and runtime Debian packages.
- For host Go development/tests, the repository's Go toolchain and normal test tools.

Run commands from the repository root. Docker builds use root context and
`backend/go/Dockerfile`. The Dockerfile also works without Buildx; Docker may warn that
its legacy builder is deprecated. Building does not require host Go or Node.js.

## Fresh local environment

```sh
export DOCKER_SECRET_GID="$(id -g)"
make docker-secrets
make docker-config-check
make docker-build
make docker-db-up
make docker-bootstrap
make docker-migrate
make docker-status
# Optional, explicit development/demo accounts:
make docker-seed-demo
make docker-up
curl --fail http://127.0.0.1:8080/readyz
```

The build produces `cyberguard-go:local`, containing the API, migration and seed
binaries plus shared SQL migrations. Go runs as UID/GID 10001 with a read-only root
filesystem, no Linux capabilities and a small writable temporary filesystem. CPU,
memory, process and log limits are local defaults, not measured deployment sizing.

`make docker-up` starts runtime services only. It never bootstraps/migrates/seeds;
those actions use explicitly invoked, disposable tooling containers. Repeating
migrations is a no-op; repeating seed preserves existing accounts. Bootstrap is
repeatable but reapplies managed-role passwords, so it is not routine startup.

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
| `DOCKER_GO_PORT` | `8080` |
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
```

For Go changes, also run the checks/build/integration suite in repository instructions.
Real container lifecycle checks are additional evidence, not a replacement for Go tests.
