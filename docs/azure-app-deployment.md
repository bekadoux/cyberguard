# Azure application VM — Ubuntu 26.04 post-install setup

This is post-install automation for an **existing Ubuntu 26.04 amd64 VM**, not Azure
provisioning or production acceptance. The selected Standard D2as v4 has 2 vCPUs and
8 GiB RAM. The initial environment is a synthetic-data demonstration with an
Azure-provided public DNS hostname. Disk size has not been supplied; 128 GiB is a
comfortable recommendation, not a provisioned resource or cost guarantee.

Read [the AI VM guide](azure-ai-deployment.md), [Go handoff](handoff-implementation.md)
and [local Docker guide](docker-setup.md). The local HTTP override is **not** deployment
configuration. No real Azure install, public ACME renewal, NSG isolation, cold VM reboot
or real model inference is established by disposable local tests.

## Architecture and boundaries

| Container | Exposure and responsibility |
| --- | --- |
| `web` | Public HTTP redirect/ACME and HTTPS; compiled frontend, `/api`, verified remote `/ai` |
| `go-api` | No published port; identity, incidents, workflow, admin, notifications and maintenance |
| `identity-db` | Separate PostgreSQL instance; TLS; internal Docker network; never published |
| `incident-db` | Separate PostgreSQL instance; TLS; private published port for the restricted AI role |
| `private-go` | Private HTTPS, read-only allowlist for Python authorization/context |

No Node, Go toolchain, Python, models, Redis or separate scheduler is installed on the
host. Build toolchains stay in images. Go owns hourly incident auto-close, also run at
startup: restarting Go can close eligible incidents. Frontend workflow/admin screens
remain outside this increment, even though their authenticated API routes exist.

The manager explicitly selects root `compose.yaml` plus `deploy/app-vm/compose.yaml`;
provided-certificate mode adds only `provided.compose.yaml`. It never selects
`compose.dev.yaml`, the single-host AI overlay, root `.env`, or inherited Compose
projects/profiles. Compose **2.24.4+** is required for `!override`. Ordinary startup
selects runtime services by name, never the `tools` profile wholesale.

## 1. Prepare the host

Transfer a reviewed committed checkout with the README's archive-over-SSH pattern,
using a new stable directory, not by copying workstation secrets or `.local/`.
On the application VM, as its existing sudo-capable operator:

```sh
sudo bash deploy/app-vm/bootstrap.sh --owner "$(id -un)"
sudoedit /etc/cyberguard/app/config.env
```

Bootstrap requires at least 20 GiB free under `/var` for initial images/builds, **not**
as a database capacity guarantee. It installs official Docker Engine, Compose/Buildx
and basic utilities, verifies Docker's signing-key fingerprint, and enables Docker.
It does not upgrade the whole OS, reboot, edit SSH/firewall policy, configure Azure,
grant Docker-group access, generate credentials or start the application.

| Path | Purpose |
| --- | --- |
| `/etc/cyberguard/app/config.env` | Root-owned non-secret literal configuration |
| `/etc/cyberguard/app/secrets/` | Operator-owned 0700 secret parent |
| `secrets/credentials/runtime/` | Runtime URLs and shared internal token |
| `secrets/credentials/provisioning/` | Admin/migrator/role/demo credentials, one-off tools only |
| `secrets/tls/` | Server certificates/keys and approved CA bundle |
| `/var/lib/cyberguard-app/` | Protected lifecycle/maintenance state, not database files |

Database data and Caddy certificate state use separate project-scoped Docker volumes.
Bootstrap reruns retain files, credentials and installed Docker versions. Conflicting
packages/sources, symlinks and unexpected ownership/modes are rejected for review,
never automatically removed or repaired. Plan OS/Docker security updates separately.
Use sudo for Docker; Docker-group membership is effectively root access.
If overriding state/secret paths, provision the corresponding real directories with
the same owner/group/modes first; bootstrap creates only the documented defaults.

## 2. Configure and build independently

```sh
sudo bash deploy/app-vm/manage.sh --config /etc/cyberguard/app/config.env config
sudo bash deploy/app-vm/manage.sh --config /etc/cyberguard/app/config.env build
```

Configuration is literal `KEY=value`: no quotes, shell substitutions, whitespace,
inline comments, duplicate/unknown keys or secret contents. Builds run sequentially
for the 2-vCPU host, require outbound package/image access, and start no services.
Use unique reviewed image tags rather than reusing `:azure` for actual releases.

Set the real Azure public DNS hostname in `APP_PUBLIC_NAME`; its DNS must resolve to
the intended public IP. Default `APP_PUBLIC_TLS=acme` uses public Caddy certificate
issuance/renewal. `provided` is an alternative requiring `public.crt`/`public.key`
trusted by the configured CA bundle; disposable tests use this instead of public ACME.
It is not a browser certificate-verification bypass.

Keep `APP_BIND_IP=127.0.0.1` and `APP_NETWORK_APPROVED=no` during preparation. Later,
set the assigned application private NIC address and approve networking explicitly.
All application publishing uses that address, including public ports behind Azure DNAT.
Default ports are 80/443 public, 8443 private Go and 5432 incident PostgreSQL.

Set `APP_AI_IP` to the stable AI VM private IP, `APP_AI_TLS_NAME` to its certificate DNS
SAN, and `APP_GO_TLS_NAME`/`APP_DB_TLS_NAME` to the application's private endpoint names.
`APP_AI_IP` is both the allowed database source and the gateway destination. This
increment assumes direct VM-to-VM connectivity, without a source-translating intermediary.

Review all three `/28` Docker subnets against your VNet, VPN and existing Docker networks.
Their defaults are examples, not discovered free address space. The public gateway has
a fixed Docker IP and Go trusts **only that `/32`** for forwarded client addresses.
Neither the private gateway nor entire private/VNet ranges are trusted proxies.

## 3. Networking and certificates are explicit prerequisites

Provision/review these outside bootstrap:

| Source | Destination | TCP |
| --- | --- | --- |
| Demo users / approved public clients | Application public IP | 443 |
| Public ACME HTTP validation / redirects | Application public IP | 80 |
| AI VM `/32` | Application private IP | 8443 and 5432 |
| Application VM `/32` | AI private IP | 8443 |
| Approved administrative source | Required VM | 22 |

Custom NSG denies for other private-endpoint sources must outrank Azure's broad default
`AllowVNetInBound`. Preserve SSH access. A private bind is **not** network isolation:
public-IP DNAT can reach the NIC, and Docker publishing can bypass UFW. Review NSGs
and Docker-aware host policy; the acknowledgement flag does not inspect or prove them.
No direct Go HTTP, identity SQL, FastAPI or model port may be public.

Private Go and DB names must resolve **inside AI containers**. Use approved private DNS,
not just host `/etc/hosts`; Docker service DNS does not span VMs. App-to-AI routing uses
the explicit private IP while verifying `APP_AI_TLS_NAME` as TLS SNI/hostname.

Install these externally issued certificate/full-chain and unencrypted key files in
`secrets/tls/`, with directory mode 0700, files **0640**, group `APP_SECRET_GID`:

| Files | Required certificate SANs |
| --- | --- |
| `ca.crt` | Approved trust bundle for app/AI/private database endpoints |
| `identity.crt`, `identity.key` | `DNS:identity-db` |
| `incident.crt`, `incident.key` | `DNS:incident-db`, `DNS:APP_DB_TLS_NAME` |
| `go.crt`, `go.key` | `DNS:APP_GO_TLS_NAME` |
| `public.crt`, `public.key` | `DNS:APP_PUBLIC_NAME`, **provided mode only** |

Keep the **CA private key off both runtime VMs**. Issue AI gateway certificates as in
the AI guide. Certificates must pass strict OpenSSL server verification, match their
keys, and have at least 24 hours remaining. Renewal/rotation of private certificates
requires deliberate replacement and container recreation; files alone do not reload
clients or rotate roles. Never make keys world-readable or disable TLS verification.

PostgreSQL copies only its own server key/cert into a postgres-owned private tmpfs.
Managed HBA rules allow `python_incidents` on `cyberguard_incidents` over TLS from the
AI source `/32`, then reject **all other** connections from that source before the
internal-network allows. Internal SQL also requires TLS/SCRAM. There is no broad image
default host allow. Container-local socket trust is retained for official image
initialization and explicitly authorized Docker-admin operations, not host publishing.

## 4. Explicit fresh database setup

As the secret-directory owner, **without sudo**, create credentials once:

```sh
bash deploy/app-vm/create-secrets.sh --config /etc/cyberguard/app/config.env
```

Generation publishes a complete new credential directory, never overwriting an existing
one or changing database roles. URLs use `verify-full` and `/run/certs/ca.crt`; independent
random hex passwords need no URL escaping. Do not regenerate after creating databases.
Management rejects connection files that change the expected role, host, database or
verified-TLS options. Deliberate password rotation must keep encoded URLs and actual
database role passwords consistent; editing a file alone does not rotate a role.
Admin/migration/demo files never enter the API, public web image or AI services.

After certificates/configuration/network review, run with sudo:

```sh
sudo bash deploy/app-vm/manage.sh --config /etc/cyberguard/app/config.env db-up
sudo bash deploy/app-vm/manage.sh --config /etc/cyberguard/app/config.env bootstrap
sudo bash deploy/app-vm/manage.sh --config /etc/cyberguard/app/config.env migrate
sudo bash deploy/app-vm/manage.sh --config /etc/cyberguard/app/config.env migration-status
sudo bash deploy/app-vm/manage.sh --config /etc/cyberguard/app/config.env bootstrap-ai
```

`db-up` enables the persistent public-write pause. Bootstrap is repeatable but **reapplies
managed role passwords**, including AI bootstrap: never routine startup or a grant-repair
shortcut. Migration uses separate scoped migrators, checks both current and image target
versions against the checkout, and exposes no automatic downgrade. Currently identity
must reach 00003 and incidents 00005. API and worker never migrate themselves.
Status uses separate file-backed migrator connections with server-side
`default_transaction_read_only=on`. This is deliberate: Goose can lazily create its
version table even in status mode. Startup/check must reject an unmigrated database
without creating that table. Only explicit `migrate` uses the writable migration URLs.

For this synthetic demo only, set `APP_ALLOW_DEMO_SEED=yes`, then explicitly run
`manage.sh --config ... seed-demo`. Passwords for `employee_a`, `employee_b`, `analyst`
and `admin` are in protected provisioning files. Read privately; never put passwords in
command arguments, browser configuration, screenshots or test reports. Repeating seed
preserves accounts and passwords. Do not seed these accounts into a real-user deployment.

## 5. Connect the AI VM and start

Securely transfer **only** `credentials/runtime/ai_runtime_url`,
`credentials/runtime/ai_internal_token`, the approved CA bundle and separately issued
AI gateway certificate/key into the corresponding AI secret files. Do not copy the
application secret directory or provisioning credentials. The exported URL captures
the configured DB hostname/port; changing those settings later requires reviewing the
URL without changing its password. Go/Python must receive the same internal token.

```sh
sudo bash deploy/app-vm/manage.sh --config /etc/cyberguard/app/config.env up
sudo bash deploy/app-vm/manage.sh --config /etc/cyberguard/app/config.env check
# Complete the AI VM's own up/check procedure, then:
sudo bash deploy/app-vm/manage.sh --config /etc/cyberguard/app/config.env check-ai
sudo bash deploy/app-vm/manage.sh --config /etc/cyberguard/app/config.env maintenance-off
```

`up` uses prepared images (`--no-build --pull never`), starts databases, verifies schema,
initializes only empty Caddy certificate-volume ownership, and starts/checks the app.
Public ACME needs working DNS and reachable validation ports; initial checks may fail
until issuance completes. Fix the cause and rerun `check`, never use insecure curl.
Persistent `/data` retains account/certificate state for renewals and recreation.
Listeners use HTTP/1.1 and HTTP/2 over TCP; HTTP/3 is not advertised on internal port 8443.

Application checks do **not** depend on AI being online. Unreachable AI produces an
assessment-only gateway error; login and incident operations remain usable. `check-ai`
verifies remote AI gateway TLS/readiness plus the local restricted AI role's
`ai_worker_ready()` bit. It does not prove the AI host can reach Go/SQL or run models:
run the AI VM's `check` and a new synthetic incident for that evidence.

Go `/readyz` remains connectivity-only; the management migration check is separate.
Checks never create incidents or claim/requeue jobs. Existing workers and Go's normal
maintenance tasks continue independently. Staff, including staff reporters, may read
AI; employees cannot. The private gateway permits only GET readiness/current-user,
single-incident detail and authenticated job context, never login/admin/workflow writes.

## 6. Maintenance, upgrades and protected backups

For an existing installation follow the [AI upgrade contract](azure-ai-deployment.md#upgrading-an-existing-deployment).
Do not run the fresh credential/bootstrap sequence over retained data.

1. Record revisions, tags/configuration and migration versions. Run `maintenance-on`:
   public unsafe methods receive JSON 503/Retry-After; reads/private worker context remain.
   Already-authorized in-flight writes are not canceled: wait for them to finish.
2. Drain jobs using the compatible old Go/worker pair where possible, **before** snapshot
   migrations. Stop **every** worker on both VMs. No script here SSHes to another host
   or verifies that remote workers are stopped; this is an explicit operator prerequisite.
3. Run `quiesce`. It pauses public writes, records a durable startup guard, and explicitly
   stops Go/private gateway. `unless-stopped` keeps those containers stopped after reboot.
   Do not bypass the manager using raw Compose startup during maintenance.
4. Take protected dumps of both instances and retain matching credential/configuration
   records off-host. As a trusted Docker administrator, in a root Bash shell:

   ```sh
   umask 077
   source deploy/app-vm/common.sh
   load_config /etc/cyberguard/app/config.env
   backup_dir=$(mktemp -d /secure/operator-selected-backups/app.XXXXXX)
   compose exec -T identity-db pg_dump -U bootstrap_admin -d cyberguard_identity -Fc > "$backup_dir/identity.dump"
   compose exec -T incident-db pg_dump -U bootstrap_admin -d cyberguard_incidents -Fc > "$backup_dir/incidents.dump"
   ```

   Create/protect the operator-selected backup parent first; it is not installed by
   bootstrap. These local socket commands expose no passwords in arguments. Dumps
   contain sessions/password hashes and incident text. They include object owners/ACLs,
   not cluster roles/passwords. Test `pg_restore` only against fresh disposable instances,
   bootstrapping matching roles first. Two live dumps are not a cross-database atomic
   backup; stopping all writers matters. A local dump/volume is not off-host protection.
5. Review the new checkout and build its images explicitly, then run `migrate`.
   Go must be stopped and maintenance enabled. Successful migration/version checks
   remove the startup guard, but leave public writes paused. Failure leaves it in place.
6. Migration 00005 grants an existing AI role its added readiness permission without
   resetting its password. Pending legacy jobs without snapshots block worker readiness;
   no automatic backfill, retirement, retry, reset or mass-failure is supplied. Keep
   processing unavailable until an explicit reviewed decision. Historical results remain.
7. Start updated Go with `up`, then updated AI API/worker using the AI guide. Verify a new
   synthetic submission, original immutable context after an affected-system edit, staff
   own-report access and employee denial. Resume public writes using `maintenance-off`
   only after the required checks/operator decisions.

Migration 00005 is forward-only. Retain old images/configuration, but do not assume old
code is compatible with the upgraded schema. Recovery is a reviewed compatibility or
protected-backup restoration decision, with writers/workers paused, not automatic down.

`status`/`logs` provide service status/bounded logs. `stop` preserves data, certificates,
credentials and maintenance markers. No normal command prunes Docker, removes volumes,
upgrades PostgreSQL majors or updates releases. Scheduled backups, cloud storage,
production account provisioning and distributed admin abuse controls are not supplied.

## Resource limits and acceptance

Initial caps: PostgreSQL 1 GiB each, Go 768 MiB, public Caddy 256 MiB and private Caddy
128 MiB. Each database starts with 40 connections, 128 MiB shared buffers and 4 MiB
work memory. CPU caps are shared ceilings, not reserved cores; there are only 2 vCPUs.
These are **unmeasured starting values**, not a throughput guarantee. Measure CPU/RAM,
login/admin password hashing, warm responsiveness, disk/WAL/audit/snapshot growth and
certificate renewal on the actual VM. Do not run inference here or enable swap silently.

```sh
make app-vm-config APP_VM_CONFIG=deploy/app-vm/config.env.example
make app-vm-test                 # host ShellCheck required
make app-vm-test-bootstrap       # disposable Ubuntu; apt/systemd/network mocked
make app-vm-smoke                # real app/TLS/SQL, disposable synthetic data
make docker-test-secrets
make docker-smoke
DOCKER_TEST_AI=1 DOCKER_TEST_FRONTEND=1 make docker-smoke
make ai-vm-test
make ai-vm-smoke
```

The smoke owns only `cyberguard-app-vm-smoke-*` resources and protected `.local/app-vm-smoke.*`
state; it removes only its own disposable volumes. Failures retain protected diagnostics
which may contain synthetic credentials. Never reuse them for deployment.
The default smoke additionally creates a separate AI fixture project and a disposable
routed Docker transit network, preserving an exact source `/32` for HBA rejection tests.
It requires a Linux Docker host supporting routed bridge mode (Docker 28+ recommended),
host `ip`, and the pinned Python image build, but downloads no models. `APP_TEST_AI=0`
runs only application/AI-offline checks; do not describe that as AI integration acceptance.
Default host test ports are 18089/18446/18447/15435; explicit `APP_TEST_HTTP_PORT`,
`APP_TEST_HTTPS_PORT`, `APP_TEST_PRIVATE_PORT`, `APP_TEST_DB_PORT` overrides avoid conflicts.
Keep local fixture acceptance distinct from real Azure NSGs/ACME/reboot, model inference
and production security/capacity acceptance. Deallocate VMs in Azure when unused;
stopping containers is not deallocation, and retained resources may still cost money.

Before a real demonstration, separately verify the public certificate/redirect and
renewal state, browser Secure/HttpOnly/host-only session behavior, cold VM reboot and
session/data persistence, source-specific private connectivity and denial from public
and unauthorized VNet sources. Exercise a new synthetic incident end-to-end with the
actual AI VM, including immutable context and staff/employee visibility. Record measured
D2as v4 CPU/RAM/disk behavior; a healthy container or fixture test is not that evidence.

### Local validation — 10 November 2026

Validated on Linux with Docker Engine 29.9.3 / Compose 5.7.0, Go 1.27.3,
Node 24.22.2 / npm 12.2.2 and the pinned images:

- `app-vm-config` and `app-vm-build` passed with the non-secret example, without
  runtime secrets or a running application/AI VM. The host used the legacy builder
  fallback; bootstrap installs Buildx, but its real installation is not tested here.
- `app-vm-test` (ShellCheck supplied by an explicit `uv tool run --from shellcheck-py`)
  and `app-vm-test-bootstrap` passed. Host apt/systemd/network actions were mocked.
- `make app-vm-smoke` passed using the actual deployment gateway, separate AI fixture
  project and routed transit network. It verified public/private/SQL TLS, original
  immutable context, stored fake output, staff own-report access, employee denial,
  wrong-hostname rejection, exact AI source IP, role/database/plaintext/other-source
  HBA rejection, and snapshot-less job readiness blocking without modifying that job.
- The same smoke verified Secure cookies, CSRF/admin boundaries, forwarded-header
  spoof rejection, maintenance/migration guards, **no version-table creation during
  startup status checks**, retained data/sessions/Caddy state and both SQL outages.
  Both dumps restored into fresh disposable databases on the test instances; restored
  row counts and migration versions matched. This is not an off-host disaster-recovery test.
- The explicit `APP_TEST_AI=0` application/AI-offline variant passed separately.
- `make docker-test-secrets`, `make docker-smoke`, `make ai-vm-test`,
  `make ai-vm-test-bootstrap` and `make ai-vm-smoke` passed.
- `DOCKER_TEST_AI=1 DOCKER_TEST_FRONTEND=1 make docker-smoke` passed all **148** packaged
  Firefox/Chromium regressions and **4** live packaged browser flows. Those are the
  existing local HTTP browser checks, not a deployed HTTPS browser acceptance claim.
- A separate `DOCKER_TEST_AI=1 DOCKER_TEST_GO_INTEGRATION=1 make docker-smoke`, using
  alternate disposable ports, passed the full Go/PostgreSQL race integration suite.
  Go/Python/SQL application source was unchanged; no new schema or authentication
  implementation was introduced by this deployment work.

Only disposable test containers/networks/volumes were removed. Ordinary application
data and secrets were not selected. Protected failed-iteration diagnostics may remain
under `.local/`. No Azure credentials or VM access were supplied, no weights downloaded,
and no real inference, public ACME issuance/renewal, NSG isolation, VM reboot or deployed
resource/cost measurements were performed.
