# Containerization Handoff — Local Docker Foundation

Status: local Docker foundation implemented and exercised, 9 November 2026.
Use [local Docker setup](docker-setup.md) for runnable commands and validation evidence.
Frontend, AI and Azure deployment remain pending.

## 1. Objective and scope

Deliver a reproducible local Docker Compose environment for the implemented Go API
and its databases, with Caddy as the gateway. Make the images and configuration
portable enough for later deployment, without provisioning Azure resources now.

The frontend and Python/AI service do not exist yet. Do not add dummy applications,
fabricated assessments, or builds that depend on missing frontend/Python directories.
Incident creation currently saves no assessment job; containerization does not close
the integration gap documented in [incident core handoff](incidents-handoff.md).

Public deployment is deferred until at least the frontend is implemented. Domain/DNS,
public HTTPS, Azure region, VM sizes, cloud provisioning and automated deployment are
not acceptance requirements for this increment.

This handoff updates the older [project plan](project-plan.md): the frontend will be
served by Caddy, not Go, and the intended deployment separates application and AI VMs.
The existing [architect](architect-handoff.md), [database](database-setup.md) and
[authentication](authentication-handoff.md) contracts still apply unless explicitly
updated here. This document retains the design/acceptance contract; the setup guide
describes delivered commands and known local-only limitations.

## 2. Agreed architecture

| Service | Current containerization increment | Later responsibility |
| --- | --- | --- |
| `web` | Caddy HTTP gateway to Go | Serve compiled React files, terminate public HTTPS, route both APIs |
| `go-api` | Existing authentication and incident core API | Continue owning application workflows and identity |
| `identity-db` | Independent PostgreSQL instance and persistent volume | Remain beside Go on the application VM |
| `incident-db` | Independent PostgreSQL instance and persistent volume | Store incidents and agreed AI/job tables; remain on application VM |
| `ai` | Not supplied yet | Python API, worker and local inference on a separate AI VM |

The initial local stack has **four long-running containers**. The eventual baseline
has **five**. One-off bootstrap/migration/seed containers are not permanent services.
The assignment requires two separate databases and roughly three or four containers;
there is no stated maximum. Do not manufacture services merely to increase the count.

The databases are two PostgreSQL server instances, not two databases in one server.
Use Docker-hosted PostgreSQL, not Azure-managed databases, for the agreed deployment.
Do not introduce a separate database VM, Redis, a broker, Kubernetes or a permanent
Node.js server in this increment.

**Decision to record again when AI is integrated:** the AI specialist must confirm
whether API, worker and inference can share one container. A separate model server or
worker container is allowed if justified; it changes the baseline count and must be
documented rather than hidden. Never run CPU inference on the application VM merely
to preserve the container count.

## 3. Ownership

| Owner | Work |
| --- | --- |
| Architect | Go image packaging, Compose, Caddy, networks, volumes, secret mounts, setup orchestration and container acceptance |
| Go engineer | Native secret-file configuration support and tests; retain ownership of bootstrap SQL, migrations, grants and application behavior |
| Frontend implementer | Later React build and integration into the Caddy runtime image; no frontend credentials |
| AI specialist | Later Python image, API/worker lifecycle, inference resource controls and packaging decision |
| Joint review | Secret scoping, browser/proxy policy, database lifecycle and integration contracts |

Do not duplicate the existing SQL/migration/authentication implementations in Docker
entrypoints. Coordinate the small Go configuration increment with the Go engineer.

## 4. Deliverables and development workflow

Implemented layout; additional setup/test helpers live under `docker/`:

```text
compose.yaml                  # Reusable services, networks, volumes and tooling
compose.dev.yaml              # Explicit local ports and HTTP development settings
.dockerignore                 # Exclude secrets, dumps, local state and build output
backend/go/Dockerfile         # API, migrate and seed binaries
docker/caddy/Caddyfile        # Local gateway configuration
docker/                       # Narrow setup helpers and non-secret templates
```

Keep base services free of published development ports and insecure-cookie defaults.
Select the development override explicitly and document the complete Compose command.
A production override can be added when deployment work starts; it is not required now.

Support both workflows:

- **Containerized application:** Caddy, Go and both PostgreSQL instances in Compose.
- **Host development:** start just the database services and run Go and, later, Vite
  on the host. Do not require rebuilding images for every application edit.

Provide documented commands or Make targets for build, database start, bootstrap,
migration/status, optional demo seed, application start, logs, stop and recreation.
Do not invent command names in documentation without implementing them. Preserve
existing host-run Make targets and environment examples.

## 5. Images and frontend boundary

### Go

- Use a multi-stage build with the repository's pinned Go **1.27.3** toolchain.
- Build `./cmd/api`, `./cmd/migrate` and `./cmd/seed` from `backend/go`.
- Reuse the image with different commands for the API and one-off tools. Include
  `db/migrations/identity` and `db/migrations/incidents` at documented runtime paths.
- Run the API as a non-root user. Include required CA certificates and intentional
  healthcheck tooling; do not assume a minimal image contains a shell or `curl`.
- Run the binary directly, or use `exec` in a necessary wrapper, so signals reach it.
  The API startup command must not bootstrap, migrate or seed.
- Copy only required files. Do not bake credentials, database contents or host-built
  binaries into the image. Pin builder/runtime images rather than using `latest`.

### PostgreSQL

Pin a supported, patched PostgreSQL image and record its version. Bootstrap requires
`psql` 15+. Use a separate bootstrap administrator on each instance; never configure
`go_identity` or `go_incidents` as the image's `POSTGRES_USER`, which is privileged.
Mount data at the path appropriate to the selected image version. Major-version
upgrades require a migration procedure, not an image-tag change over an old volume.

### Caddy and the future frontend

Use a pinned Caddy image as the local HTTP gateway now. Until a frontend exists,
unimplemented frontend paths should return an honest response, not a placeholder
application presented as delivered functionality.

Later, build React with Node.js in a build stage and copy the resulting static files
into a dedicated Caddy runtime image. Frontend and Go builds/releases remain separate.
The files are served **inside the Caddy container**, not by a host-installed web server
or Go. Node/Vite is not a production runtime dependency. Host-run Vite remains useful
for hot reload. All frontend configuration is browser-visible; never include database
credentials or service tokens in `VITE_*` variables or the compiled bundle.

## 6. Local networking, routing and browser policy

Use service DNS names and container ports internally. Go listens on `:8080`; database
URLs inside containers use `identity-db:5432` and `incident-db:5432`, not host loopback.
Host-run clients keep the existing addresses:

| Component | Local host address |
| --- | --- |
| Go direct access, when selected | `127.0.0.1:8080` |
| Identity PostgreSQL, development only | `127.0.0.1:5433` |
| Incident PostgreSQL, development only | `127.0.0.1:5434` |
| Future Vite development server | `127.0.0.1:5173` |

Suggested gateway address: `http://127.0.0.1:8088`, avoiding a conflict with direct Go
access. Record the selected address in examples and tests. Publish local ports on
loopback only. Do not run host Go and a container publishing the same port together.

Suggested external routing contract:

| Gateway path | Behavior |
| --- | --- |
| `/api/*` | Forward to Go, removing `/api`; existing Go routes remain unchanged |
| `/ai/*` | Reserve for Python; enable forwarding only when implemented |
| Other paths | Later static frontend and SPA navigation; currently no frontend |

Document the gateway prefixes in API/frontend configuration when implemented. Keep
API routing separate from SPA fallback: backend errors and unknown API paths must not
become successful `index.html` responses. Explicitly deny public access to internal
authorization/context namespaces, including prefixed forms such as `/api/internal/*`.
Do not expose them through a catch-all route when they are added later.

Attach Go to both database networks; Caddy needs neither database network. Future
Python access is limited to Go's internal APIs and the incident database, never the
identity database. Do not mount the Docker socket into application containers.

Local HTTP requires `APP_ENV=development` and `SESSION_COOKIE_SECURE=false`. Preserve
HttpOnly, host-only, Path=/ and SameSite cookie behavior. Use `127.0.0.1` consistently.
Trusted origins are exact, including the frontend port; preserve the browser-facing
Host through Caddy. CORS does not replace CSRF or authentication. Trust forwarded
client addresses only from the actual Caddy address/network boundary, not every
Docker/private network. Review [authentication security limits](authentication-security-review.md)
and test spoofed headers as well as direct access.

## 7. Secret-file delivery

Use Compose secrets mounted as files under `/run/secrets/`, with explicit access per
service. Keep ordinary environment variables for non-secret settings such as ports,
timeouts, trusted origins and environment name.

Local Compose secret sources are plaintext host files, not an encrypted secret store.
Protect their host permissions and verify that only the intended container user can
read them. Host/Docker administrators remain trusted. Exclude actual secret files
from Git **and every Docker build context**; `.gitignore` alone does not do the latter.
Supply templates/instructions, never working credentials. Avoid logging secret values,
shell tracing, image `ENV` credentials or build arguments containing secrets.

`.env`/`--env-file` supplies Compose interpolation values; service `env_file:` injects
container environment variables. Neither is automatically a secret-file mechanism.
The existing API and Make targets do not automatically load the repository's `.env`.
Keep container-specific addresses/configuration distinct from existing host examples.

### Implemented Go configuration

Go commands now accept native file alternatives, rather than exporting all mounted
secrets into the API environment:

| Consumer | File-based inputs to support |
| --- | --- |
| API | `IDENTITY_DATABASE_URL_FILE`, `INCIDENT_DATABASE_URL_FILE` |
| Identity migrator / demo seed | `IDENTITY_MIGRATION_DATABASE_URL_FILE` |
| Incident migrator | `INCIDENT_MIGRATION_DATABASE_URL_FILE` |
| Optional demo seed | File alternatives for its four existing `DEV_*_PASSWORD` settings |

Retain direct environment-value support for existing host workflows. If both a value
and its file alternative are configured, reject the ambiguity. Missing, empty or
unreadable required files fail safely without printing their contents. Define terminal
newline handling; do not indiscriminately trim passwords. Reuse existing URL and
credential validation. Read configuration at startup; do not imply automatic rotation.

### Credential scoping and setup adapters

- Each PostgreSQL service receives its own bootstrap administrator secret, using the
  image's supported file interface such as `POSTGRES_PASSWORD_FILE`.
- Each bootstrap invocation receives only its instance's administrator access and raw
  migration/runtime passwords. The existing SQL uses `\getenv`; a narrow one-off
  adapter may read files into that setup process's environment. Do not rewrite SQL or
  expand the API environment merely to accommodate file delivery.
- Each migrator receives only its corresponding migration connection secret. Runtime
  roles must still be rejected by the migration command.
- Demo seeding receives only the identity migration connection and demo passwords.
  Do not pass all of `db/.env` to a service.
- Go receives only its two runtime connections. Future Python receives a separate,
  restricted incident connection and required service credential, not identity access.

Keep raw role passwords consistent with percent-encoded credentials in URL files.
Use independent passwords for distinct roles. Editing a secret file does not rotate
an existing PostgreSQL role; initial-image password settings do not update a populated
volume. Document deliberate role/password rotation without deleting volumes.

## 8. Database lifecycle and process health

Preserve this explicit sequence, using existing SQL and migration binaries:

```text
Start both database services and wait for connections
  -> bootstrap roles/databases through controlled one-off commands
  -> migrate identity and incidents through separate one-off commands
  -> optionally seed explicitly requested local/demo accounts
  -> start Go and Caddy with runtime-only configuration
  -> check readiness and exercise authenticated functionality
```

Reuse `db/bootstrap/*.sql` with `ON_ERROR_STOP` and failure propagation. Bootstrap is
repeatable but resets managed passwords; it is not an every-restart operation. Do not
use `/docker-entrypoint-initdb.d` as the sole setup/upgrade mechanism: those scripts
only run for an empty data directory. Controlled setup must work with retained volumes.
Use the existing migration roles and grants from [database setup](database-setup.md).

Healthchecks and Compose dependencies assist startup ordering; they do not replace
bootstrap/migrations or provide ongoing recovery guarantees. Go liveness is `/healthz`;
readiness is `/readyz`, checking connectivity to both databases, not schema readiness.
Neither depends on Python. Check Go on its own container port, not solely through Caddy.
Keep failure/recovery possible without restarting Go. Configure the container stop
grace period beyond the configured HTTP shutdown deadline and verify graceful SIGTERM.

## 9. Persistence and optional local dumps

Use separate named volumes for identity and incident data. Ordinary stop, restart,
rebuild and container recreation must preserve them. Clearly distinguish stopping
services from explicit destructive volume removal; never hide `down -v` in a normal
cleanup command or recommend deleting volumes to fix setup failures.

Backup infrastructure is **not** part of this increment. An optional small follow-up
may provide manual `pg_dump` commands for both databases, using one-off tools and
appropriately scoped credentials. Write dumps to a protected, Git-ignored local
directory outside build contexts. Dumps include sensitive hashes/session data; never
commit or bundle them into images. Test a documented restore against fresh disposable
databases, bootstrapping roles first. A dump does not include PostgreSQL cluster roles.
Do not add a scheduler, backup container, cloud storage, retention service or disaster
recovery system now. Local dumps and retained volumes are not off-host protection.

## 10. Acceptance and evidence

- [ ] A clean checkout can build the Go image and run the four-service local stack
      using documented prerequisites, secret creation and commands.
- [ ] Both host-development and containerized workflows use the correct addresses.
- [ ] Fresh databases bootstrap/migrate; repeat migrations are no-ops; optional repeat
      seed preserves existing account IDs, passwords and blocked state.
- [ ] API/migrator/seed secret grants are separate; rendered configuration and routine
      logs do not disclose credential values. Secret-file error/ambiguity tests pass.
- [ ] Go runtime database grants remain restricted; bootstrap credentials are not used
      as application credentials. Non-root runtime users can read only intended files.
- [ ] Requests through Caddy exercise login/logout/current-user and incident create,
      list and detail; employee isolation and reporter/professional projections remain.
- [ ] Allowed frontend origins work; hostile unsafe origins fail. Session cookie rules
      are unchanged. Spoofed forwarded headers cannot bypass the login limiter.
- [ ] Internal route namespaces are denied through the gateway. Missing API/frontend/AI
      paths do not masquerade as a working SPA or AI service.
- [ ] Stopping either database makes Go readiness fail while liveness remains healthy;
      restarting the database recovers readiness without restarting Go.
- [ ] Accounts, sessions and incidents survive container recreation with volumes retained.
- [ ] SIGTERM reaches Go and permits graceful shutdown. Python is not a dependency.
- [ ] If manual dumps are supplied, restore is tested only against disposable databases.

Run `make go-check` and `make go-build` for the Go configuration changes. Also run
`make go-test-integration` with all four dedicated test URLs from database setup;
never point integration tests at ordinary development volumes. Add Compose validation
and actual lifecycle/browser smoke checks. Report what was exercised and any blocked
checks; a successful image build alone is not acceptance of the environment.

## 11. Future deployment boundary — record, do not implement now

The intended Azure layout is:

```text
Application VM                         AI VM
  Caddy + compiled frontend             Python API / worker / inference
  Go API                                (packaging to be confirmed)
  Identity PostgreSQL
  Incident PostgreSQL <--- restricted private AI/job access
```

The split protects the application from CPU/memory contention during inference.
Keep upstream URLs configurable: Compose service DNS and bridge networks do not span
VMs. Later deployment needs separate host configurations, private connectivity,
firewall allowlists and verified TLS for cross-host connections. Identity PostgreSQL
must remain inaccessible to Python. Incident access from the AI VM requires a narrowly
restricted private connection, never a publicly exposed database.

Public deployment will add Caddy HTTPS, certificate-state persistence, production
Secure cookies, exact proxy trust and public routing. An Azure-provided DNS name is a
possible hostname; no domain has been selected. Student credit is provisionally about
**$100**, not a validated budget. Region, quotas and VM sizes remain undecided; AI CPU
benchmarks must inform sizing. None of these decisions blocks local containerization.

Full AI integration still requires the shared job schema, atomic incident/job creation,
Go authorization/context APIs and a fake-worker demonstration from the
[engineer handoff](software-engineer-handoff.md). Keep this separate from container
acceptance. Revisit cloud backups, monitoring, cost controls and release automation
when deployment work actually begins.
