# Azure application VM runbook

This runbook targets an **existing Ubuntu 26.04 amd64 VM**. Bootstrap does not
create Azure resources. See [deployment architecture](deployment.md) for service
boundaries and the [AI VM runbook](azure-ai-deployment.md) for the optional second
host. Do not use the local HTTP override on this VM.

The manager explicitly selects root `compose.yaml` plus `deploy/app-vm/compose.yaml`;
provided-certificate mode adds only `provided.compose.yaml`. It never selects
`compose.dev.yaml`, the single-host AI overlay, root `.env`, or inherited Compose
projects/profiles. Compose **2.24.4+** is required for `!override`. Ordinary startup
selects runtime services by name, never the `tools` profile wholesale.

## 1. Prepare the host

Transfer a reviewed committed checkout using Git or an archive of tracked files,
in a stable directory. Do not copy workstation secrets or `.local/`.
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

Install the CA bundle, externally issued server certificates/full chains, and matching
unencrypted keys in `secrets/tls/`, with directory mode 0700, files **0640**, group
`APP_SECRET_GID`:

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
# If connecting an AI VM, provision its restricted role before starting Go:
sudo bash deploy/app-vm/manage.sh --config /etc/cyberguard/app/config.env bootstrap-ai
```

`db-up` enables the persistent public-write pause. Bootstrap is repeatable but **reapplies
managed role passwords**, including AI bootstrap: never routine startup or a grant-repair
shortcut. Before either migrator writes, the manager compares the SQL files for **both**
databases in the selected image to the checkout and pins that image ID for the run.
It accepts Go only if absent or exited, then checks current and image target versions
after migrating. There is no automatic downgrade. Currently identity
must reach 00004 and incidents 00007. API and worker never migrate themselves.
Status uses separate file-backed migrator connections with server-side
`default_transaction_read_only=on`. This is deliberate: Goose can lazily create its
version table even in status mode. Startup/check must reject an unmigrated database
without creating that table. Only explicit `migrate` uses the writable migration URLs.

For this synthetic demo only, set `APP_ALLOW_DEMO_SEED=yes`, then explicitly run
`manage.sh --config ... seed-demo`. Passwords for `employee_a`, `employee_b`, `analyst`
and `admin` are in protected provisioning files. Read privately; never put passwords in
command arguments, browser configuration, screenshots or test reports. Repeating seed
preserves accounts and passwords. Do not seed these accounts into a real-user deployment.

### Initialize the application owner

After migrating and before starting Go, initialize the owner once. On an
existing installation, do this during the upgrade maintenance window, with Go
stopped. The command refuses to replace an already-initialized owner.

For a fresh installation, supply a protected file with the initial password:

```bash
sudo bash deploy/app-vm/manage.sh --config /etc/cyberguard/app/config.env \
  init-owner first_admin --create < /secure/path/owner-password
```

Alternatively, explicitly select an existing active admin, including on a
legacy demo installation. This does not change their password:

```bash
sudo bash deploy/app-vm/manage.sh --config /etc/cyberguard/app/config.env \
  init-owner existing_admin
```

Only then, if you want people to self-register as `employee`, set
`SIGNUP_ENABLED=true` in `/etc/cyberguard/app/config.env`. The default is false.
Enable it only for an audience allowed to create active accounts; the API
does not ask for an invite or approval. Do not use the demo seed to create
the initial admin for a real-user deployment.

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
# When connecting an AI VM, complete its up/check procedure, then:
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
single-incident detail, authenticated job context and `GET /internal/ai/auth-check`.
The last route checks the same worker bearer credential without touching storage or
sessions (204 on success, 401 on rejection). The public gateway denies both internal
routes. Roll out Go and its private allowlist before starting an upgraded worker.

## 6. Maintenance, upgrades and protected backups

For an existing installation follow the [AI upgrade contract](azure-ai-deployment.md#upgrading-an-existing-deployment).
Do not run the fresh credential/bootstrap sequence over retained data.

1. Record revisions, tags/configuration and migration versions. Retain the configured
   state directory: the manager verifies the gateway's actual maintenance bind mount
   and marker before reporting a pause. Run `maintenance-on`:
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
   not cluster roles/passwords. Use the [fresh-instance restore procedure](#restore-both-archives-on-fresh-instances)
   to rehearse recovery. Two live dumps are not a cross-database atomic backup;
   stopping all writers matters. A local dump/volume is not off-host protection.
5. Review the new checkout and build its images explicitly, then run `migrate`.
   Go must be stopped and maintenance enabled. Successful migration/version checks
   remove the startup guard, but leave public writes paused. Failure leaves it in place.
   Deploy the rebuilt Go and frontend images only after both databases reach their
   migration heads. If 00007 refuses historical review data, keep writes paused for
   [audit review](#feedback-migration-conflicts).
6. Migration 00005 grants an existing AI role its added readiness permission without
   resetting its password. Pending legacy jobs without snapshots block worker readiness;
   no automatic backfill, retirement, retry, reset or mass-failure is supplied. Keep
   processing unavailable until an explicit reviewed decision. Historical results remain.
7. Start updated Go with `up`, then the AI stack if enabled. Run the read-only
   `check` and, when applicable, `check-ai`. Once checks and required operator
   decisions have passed, use `maintenance-off` to resume writes. The pause also
   blocks synthetic browser submissions: run write verification in a disposable
   fixture first, or on the deployment only in an explicitly authorized test window.
   Verify original context after an affected-system edit, staff own-report access,
   and employee denial. Re-enable maintenance if verification fails.

Migration 00005 is forward-only. Retain old images/configuration, but do not assume old
code is compatible with the upgraded schema. Recovery is a reviewed compatibility or
protected-backup restoration decision, with writers/workers paused, not automatic down.

`status`/`logs` provide service status/bounded logs. `stop` preserves data, certificates,
credentials and maintenance markers. No normal command prunes Docker, removes volumes,
upgrades PostgreSQL majors or updates releases. Deallocate unused VMs in Azure;
stopping containers does not deallocate a VM or stop charges for retained resources.

### Feedback migration conflicts

Incident migration 00007 enforces one decision per normalized job UUID without
rewriting historical actions. If it rejects duplicate, missing, or invalid UUIDs,
keep writes paused and retain the backup. For valid UUIDs the migrator can group
duplicates without disclosing reason text:

```sql
SELECT (details->>'job_id')::uuid AS job_id, count(*) AS reviews
FROM incident_actions
WHERE action = 'ai_feedback_submitted'
GROUP BY (details->>'job_id')::uuid
HAVING count(*) > 1;
```

If invalid UUIDs are reported, do not run the cast query until the audit owner
has identified the invalid records. Reconciliation requires explicit approval;
the migration does not select or delete any decision for you.

### Restore both archives on fresh instances

On an **isolated recovery host** use two empty PostgreSQL 17 instances, a distinct
recovery config/project with new volumes, and the matching checkout, images, protected
credentials and TLS material. Use trusted archives from the same quiesced backup.
Keep Go, workers and public traffic off the recovery databases throughout. Confirm
the Docker context, project and volume destinations before running these commands.
Never point them at the source installation. In a root Bash shell on that host:

```bash
set -euo pipefail
umask 077
recovery_config=/etc/cyberguard/app/recovery.env
backup_dir=/secure/operator-selected-backups/REPLACE_WITH_BACKUP
source deploy/app-vm/common.sh
load_config "$recovery_config"
bash deploy/app-vm/manage.sh --config "$recovery_config" db-up
source deploy/app-vm/restore-roles.sh
prepare_restore_roles compose

compose exec -T identity-db pg_restore -U bootstrap_admin --exit-on-error --create \
    -d postgres < "$backup_dir/identity.dump"
compose exec -T incident-db pg_restore -U bootstrap_admin --exit-on-error --create \
    -d postgres < "$backup_dir/incidents.dump"

bash deploy/app-vm/manage.sh --config "$recovery_config" bootstrap
bash deploy/app-vm/manage.sh --config "$recovery_config" migration-status
```

Role preparation refuses existing application databases or roles. It creates only
the names needed by the archives: non-login Go/migrator placeholders on their
respective instances and a restricted `python_incidents` login using the retained
AI password on the incident instance. It grants no object privileges. Do **not**
run normal bootstrap, migrations, seed or `bootstrap-ai` before restore: the archives
must create the databases, and the normal AI grants require functions/tables inside
them. `--create -d postgres` restores each original database name, owner and database
ACL as well as its schema, rows and object ACLs. Do not skip ACLs or owners, or clean
an existing database. On a partial failure stop and investigate; retry on new empty
instances. Normal bootstrap **after** restore activates Go/migrator logins with the
retained passwords. For an AI-provisioned backup, the archive already grants the AI
role its access; reapplying grants would mask a recovery error.

Compare both migration versions, representative row counts, owners and grants with
the backup record. Authenticate using the actual Go credentials to read `users` and
`incidents` on their respective instances. Using the AI credential, read the
allowed job/result columns and call `ai_worker_ready()`; `claim_token` must remain
unreadable. A false readiness bit requires the same legacy-job review as on the
source. Verify deployment TLS/HBA, sessions, Go and AI separately before cutover.

## Disposable checks

`make app-vm-test` needs ShellCheck; `make app-vm-test-bootstrap` mocks host setup.
`make app-vm-smoke` exercises actual application containers, TLS, SQL, backup/restore,
and synthetic AI on a disposable routed Linux Docker network. It needs supported
routed bridge networking and host `ip`. `APP_TEST_AI=0` selects the app-only variant.
Protect retained diagnostics. See the [backlog](backlog.md#validation-and-release-work)
for separate real-host checks; smoke does not validate public ACME, Azure networking,
reboot behavior, or model inference.
