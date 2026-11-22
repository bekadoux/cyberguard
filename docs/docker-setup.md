# Local Docker runbook

These are **HTTP development environments**, not production deployments. Neither
setup needs host Go, Node.js, or Python. See [deployment architecture](deployment.md)
for routing.

## One-command setup with real AI

Use a Linux amd64 machine with a local Docker engine, current Docker Compose,
Bash, OpenSSL, and standard Linux utilities, including `ip` and `ss` (iproute2)
and `flock` (util-linux). Run from the checkout:

```bash
bash docker/local-setup.sh
```

On the first run, choose loopback (default) or explicitly approve binding to a
private LAN IPv4 address, choose the web port (default `8088`), and decide whether
to seed four development accounts. Without them there is no initial login. The
script asks for confirmation before preparing about 3 GB of pinned Kev and Qwen
artifacts. First setup needs internet access for missing models, images, and
build dependencies. Only Caddy is published. Go, PostgreSQL, FastAPI, the worker
and model servers remain private.

**LAN mode is unencrypted.** Use it only on a trusted development network with
synthetic incident data and passwords you do not use elsewhere. Limit inbound
traffic to the intended clients or subnet with network/firewall policy and verify
it from a second machine; the script does not manage firewall rules. Use the
printed URL consistently. Do not forward the port to the internet.

The script shows `employee_a`, `employee_b`, `analyst`, and `admin` passwords
once, directly in the terminal immediately after seeding, before preparing the
AI models. If you miss that display, retrieve an initial password from the
checkout:

```bash
cat .local/full-stack/secrets/dev_admin_password
```

The other accounts have corresponding `dev_<username>_password` files. These
files do not reflect later password changes in the app. Keep the secrets and
database volumes together.

The core app starts before model preparation finishes, so you can sign in and
submit synthetic reports while waiting. Those reports queue assessments for
the worker; the full stack is ready once the worker passes its checks.

| Command | Effect |
| --- | --- |
| `bash docker/local-setup.sh` | First setup, continue an interrupted setup, or start the existing stack. |
| `bash docker/local-setup.sh stop` | Stop the containers without removing them or their data. |
| `bash docker/local-setup.sh resume` | Restart with saved settings. With cached images and verified models, no downloads are required. |
| `bash docker/local-setup.sh status` / `logs` | Show container status / recent logs. |
| `bash docker/local-setup.sh check` | Run non-mutating dependency checks inside the running worker. |
| `bash docker/local-setup.sh destroy` | After typed confirmation, remove only this checkout's full-stack containers, networks, database volumes, secrets and settings. Keep the model cache and Docker build artifacts. |
| `bash docker/local-setup.sh destroy --purge-models` | Also remove `.local/full-stack-models/`. |

The project is isolated from the loopback-only core setup and other checkouts;
its name is derived from the checkout path. Keep the checkout at a stable path.
Settings are saved under `.local/full-stack/`. If the LAN address changes, stop
the stack and edit the first line of `.local/full-stack/settings` to an active
private IPv4 address or `127.0.0.1` before resuming. Normal restarts do not rebuild
the project images or apply new migrations. For a disposable environment, destroy
and run setup again to build from the current checkout; for retained data, plan
an explicit backup and migration rather than deleting the volumes.

Runtime inference is CPU-only, with one worker and one generation slot.
The model servers are capped at two CPUs each and 8 GiB (Kev) / 4 GiB
(llama.cpp); those caps are not measured requirements. Model files are mounted
read-only. If verification fails, stop the stack and investigate the reported
artifact. `bash docker/local-setup.sh prepare-models` can fetch missing files,
but will not overwrite a mismatched file. Failed jobs are not reset.

To verify **real inference**, submit a fresh synthetic incident as an employee,
then sign in as staff and refresh its assessment until it completes. `check`
and `/ai/readyz` do not run inference. In LAN mode also check normal login and
submission from a second machine, and verify that Go, SQL, and model ports are
not reachable from there.

## Core application setup (loopback only)

The manual setup below uses Linux or WSL2, Docker with Compose v2, GNU Make,
OpenSSL, and jq. First builds need network access.

### Fresh local environment

```sh
export DOCKER_SECRET_GID="$(id -g)"
make docker-secrets docker-config-check
make docker-build docker-caddy-check
make docker-db-up docker-bootstrap docker-migrate docker-status
make docker-seed-demo  # Optional: four development accounts, not incidents
make docker-up
curl --fail http://127.0.0.1:8088/api/readyz
```

Open [http://127.0.0.1:8088/login](http://127.0.0.1:8088/login). The optional demo
accounts are `employee_a`, `employee_b`, `analyst`, and `admin`; their passwords
are the corresponding `dev_*_password` files in `.local/docker-secrets/`. Keep
them private. Repeated seed preserves existing accounts and passwords.

`make docker-up` starts runtime services with prebuilt images. Do not enable the
`tools` profile for ordinary startup. Bootstrap may reapply database role
passwords; migration and seed are explicit one-off steps, not startup actions.

| Loopback service | Default | Host-port override |
| --- | --- | --- |
| Caddy and the packaged UI | `8088` | `DOCKER_WEB_PORT` |
| Direct Go, without `/api` | `8080` | `DOCKER_GO_PORT` |
| Identity PostgreSQL | `5433` | `DOCKER_IDENTITY_PORT` |
| Incident PostgreSQL | `5434` | `DOCKER_INCIDENT_PORT` |

Set database port overrides before creating secrets, because generated host URL
files capture them. Container URLs use service names and port 5432. Compose can
interpolate the root `.env`; it does not inject it wholesale. Never pass the
privileged `db/.env` to runtime services.

### Demo with self-registration

After `docker-migrate`, skip `docker-seed-demo` if participants should register
themselves. From the checkout in Bash, initialize the owner once using a protected
file containing the owner's initial password:

```bash
export DOCKER_SECRET_GID="$(id -g)"
docker compose -f compose.yaml -f compose.dev.yaml run --rm -T \
  migrate-identity /app/seed -owner first_admin -create-owner < /secure/path/owner-password
SIGNUP_ENABLED=true make docker-up
```

If the old demo seed already created `admin`, select that account instead:
run the same Compose command ending in `/app/seed -owner admin`, without a
password file. Keep the signup setting on subsequent restarts while enrollment
is needed. Use only synthetic data. Until a signup screen is available, use
[`POST /auth/signup`](../api/go-authentication.openapi.yaml) through the API.
The owner can then promote registered employees through the admin role API.

## Updates and persistence

- Keep `.local/docker-secrets/` and the data volumes together. Secret generation
  refuses overwrite. The directory is mode `0700`; files are `0640` with the
  source group. `DOCKER_SECRET_GID` makes them readable in containers without
  making them world-readable. Do not regenerate credentials to repair an existing
  installation. A file edit is not password rotation.
- Use `make docker-build-web docker-caddy-check docker-up` after frontend or
  Caddy changes, and `make docker-build-go` for Go/migration images. Rebuild and
  recreate; the packaged web container does not watch source or config files.
- Before upgrading an existing installation, stop writers, drain compatible jobs
  where possible, stop all workers, and back up retained data. Rebuild the
  migration image before `make docker-migrate`, then start updated services.
  See the [legacy-job and worker upgrade rules](azure-ai-deployment.md#upgrading-an-existing-deployment).
- `make docker-logs` shows logs; `make docker-down` stops the base local stack
  without deleting data. Do not use `down -v` as routine cleanup. Use the same
  Compose files to stop or diagnose a stack started with the optional AI overlay.

## Host Go

With the database services provisioned, start host Go from a **clean runtime-only**
shell. Do not run it alongside a container publishing the same Go port:

```sh
export IDENTITY_DATABASE_URL_FILE="$PWD/.local/docker-secrets/identity_runtime_host_url"
export INCIDENT_DATABASE_URL_FILE="$PWD/.local/docker-secrets/incidents_runtime_host_url"
export APP_ENV=development SESSION_COOKIE_SECURE=false
export TRUSTED_ORIGINS=http://127.0.0.1:5173
make go-run
```

Do not also export direct URL values. Use `127.0.0.1` consistently, not a mix of
localhost and IP origins. For Vite, see [frontend development](frontend.md#development-and-checks).

## Optional local AI

Apply both migration chains first. The base stack already queues new jobs; this
overlay starts the AI API and worker. Use the same project and secret directory
as the base setup:

```bash
export DOCKER_SECRET_GID="$(id -g)"
ai_compose=(docker compose -f compose.yaml -f compose.dev.yaml -f compose.ai.yaml)
```

On a **first** AI installation only, create the extra secret files and provision
the restricted incident role. Do not reuse this as a migration or grant-repair step:

```bash
sh docker/ai/create-ai-secrets.sh
"${ai_compose[@]}" --profile tools run --rm bootstrap-ai
```

### Disposable fixture or real models

For synthetic output on explicitly disposable data only:

```bash
"${ai_compose[@]}" build go-api ai-api ai-worker
AI_MODE=fake "${ai_compose[@]}" up -d identity-db incident-db go-api ai-api ai-worker web
```

This verifies wiring, not model behavior. Use separate Docker projects, credentials,
and volumes for fixture and real-model data.

For real local inference, stop the worker during model preparation. Review the
[manifest](../deploy/ai-vm/models.json) and model licenses. Create `.local/models/`
and a non-secret config under `.local/` from
[`deploy/ai-vm/config.env.example`](../deploy/ai-vm/config.env.example), with
`AI_MODEL_DIR` set to the absolute path of `.local/models/`. Then run:

```bash
bash deploy/ai-vm/prepare-models.sh --config .local/local-models.env
"${ai_compose[@]}" build go-api ai-api ai-worker
"${ai_compose[@]}" --profile ai-models build ai-kev
"${ai_compose[@]}" --profile ai-models up -d ai-kev ai-llm
```

The preparation downloads roughly 3 GB of pinned artifacts; builds alone do not
download weights. After model readiness, start the runtime services:

```bash
AI_MODE=models "${ai_compose[@]}" up -d identity-db incident-db go-api ai-api ai-worker web
curl --fail http://127.0.0.1:8088/ai/readyz
```

That endpoint checks storage, not inference. For an end-to-end check, submit a
new synthetic incident and refresh its assessment using a staff session. Do
not reset a failed job. Baked Caddy handlers preserve the SPA and internal
route denial when the AI overlay is enabled.

## Checks

| Target | Scope |
| --- | --- |
| `make docker-config-check docker-caddy-check` | Compose and baked gateway configuration |
| `make docker-test-secrets docker-test-readiness docker-test-web` | Secret generation, PostgreSQL TCP versus first-init socket readiness, gateway checkers |
| `make docker-test-local-setup` | Full-stack Compose isolation and mocked interactive setup, resume, and teardown; requires `jq` and `script` (util-linux) |
| `make docker-smoke` | Disposable runtime, persistence, database outage, and HTTP checks |

`docker-smoke` accepts independent flags: `DOCKER_TEST_GO_INTEGRATION=1` adds the
Go/PostgreSQL race suite; `DOCKER_TEST_FRONTEND=1` adds packaged Playwright and
live same-origin flows; `DOCKER_TEST_AI=1` adds the real AI service/job path with
synthetic output only. The frontend flag needs installed dependencies and
Playwright browsers; the Go flag needs host Go. Run from the repository root
with Bash, curl, jq, and OpenSSL available. The fixture creates and cleans up
its **own** project and volumes, not normal development data.

The separate `make frontend-test-e2e-live` uses a running seeded local backend
and `E2E_PASSWORD_FILE`. Incident write tests (`make frontend-test-incidents-live`
and the packaged live flows) require an explicitly selected disposable backend.
For manual targeting, retain one with `DOCKER_KEEP_TEST_ENV=1 make docker-smoke`,
then set `E2E_INCIDENT_SECRETS_DIR` to its printed project's `secrets/` directory
and the actual nonstandard loopback gateway origin in `E2E_INCIDENT_API_ORIGIN`
(Vite) or `E2E_PACKAGED_ORIGIN` (packaged). Protect retained secret files and
failed-run diagnostics; remove only the fixture when done.
