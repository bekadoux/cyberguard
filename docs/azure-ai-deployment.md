# Azure AI VM runbook

This runbook targets an **existing Ubuntu 26.04 amd64 VM**. It does not create Azure
resources or databases. The AI VM uses standalone Compose; see
[deployment architecture](deployment.md) and the [AI service](ai-backend.md).
Set up the [application VM](azure-app-deployment.md) before connecting runtime
services.

For a fresh two-VM installation from a workstation, see the
[SSH installer](remote-setup.md). This runbook remains the manual and upgrade
reference.

## 1. Prepare the host independently

Copy a reviewed checkout onto the VM (SCP/archive, or Git if already installed). Keep
the repository at a stable path: its private Caddy configuration is mounted read-only.
Run from the repository root with an existing administrative account:

```sh
sudo bash deploy/ai-vm/bootstrap.sh --owner YOUR_AZURE_USER
```

This validates Ubuntu/architecture and 20 GiB free under `/var`, installs Docker Engine
from its official apt repository, Compose/Buildx and basic utilities, verifies Docker's
signing-key fingerprint, and enables Docker at boot. It creates:

| Path | Ownership/purpose |
| --- | --- |
| `/etc/cyberguard/ai/config.env` | Root-owned, non-secret configuration template |
| `/etc/cyberguard/ai/secrets/` | Operator-owned, 0700; only runtime credentials/certificates |
| `/var/lib/cyberguard-ai/models/` | Operator-owned, persistent model artifacts |

Reruns retain existing files, credentials and Docker versions. Existing conflicting
packages, different Docker apt sources/keys or unexpected directory ownership/modes
are rejected for manual review; nothing is uninstalled to resolve conflicts. Normal
utility package updates may occur. The script neither performs an unattended full OS
upgrade nor reboots. Plan OS/Docker security updates deliberately before public use.

It does not edit SSH, host firewall policy, Docker daemon settings or group membership.
Starting Docker creates Docker's normal networking/firewall rules.
Use sudo for Docker commands unless Docker access has separately been authorized;
membership in the Docker group is effectively root access. Do not run this bootstrap
on the application VM or on an unrelated Docker host.

**Host prepared is not application ready.** You can finish bootstrap, image builds and
model preparation before the second VM exists.

## 2. Review configuration and build images

```sh
sudoedit /etc/cyberguard/ai/config.env
sudo bash deploy/ai-vm/manage.sh --config /etc/cyberguard/ai/config.env config
sudo bash deploy/ai-vm/manage.sh --config /etc/cyberguard/ai/config.env build
```

The config is literal `KEY=value`: no quotes, interpolation, inline comments, spaces,
or secret values. Unknown/duplicate keys are rejected. The scripts never source it as
shell code and explicitly bypass root `.env`/parent Compose project overrides.

- Keep `AI_BIND_IP=127.0.0.1` and `AI_NETWORK_APPROVED=no` until network approval.
- Set `AI_SECRET_GID` to the group owning secret source files, normally `id -g` for
  the selected operator. File-backed Compose secrets retain host ownership/modes.
- Set `AI_GO_URL` to the private HTTPS **origin**, without `/api` or another path.
- `AI_TLS_NAME` must match a DNS SAN on the AI server certificate. The diagnostic
  client uses `curl --resolve`; a private DNS zone is not required for that local check.
- For releases, use unique `AI_IMAGE`/`AI_KEV_IMAGE` tags and retain the previous images
  and checkout for rollback. The `:azure` defaults are convenient local build tags,
  not immutable release identifiers.

`build` builds the Python/Kev images and pulls the pinned Caddy/llama.cpp images. It
does **not** download weights or start services. Do not use the local `compose.dev.yaml`
or combine this standalone Compose file with the application files on the AI VM.

## 3. Explicit model preparation

Review the official model cards/licenses and `deploy/ai-vm/models.json` first:

- [Qwen2.5-1.5B-Instruct-GGUF](https://huggingface.co/Qwen/Qwen2.5-1.5B-Instruct-GGUF)
- [Kev-0.8B](https://huggingface.co/jaredpalmer/kev-0.8b)
- [Qwen3.5-0.8B-Base](https://huggingface.co/Qwen/Qwen3.5-0.8B-Base)

As the model-directory owner (normally **without sudo**):

```sh
bash deploy/ai-vm/prepare-models.sh --config /etc/cyberguard/ai/config.env
bash deploy/ai-vm/prepare-models.sh --config /etc/cyberguard/ai/config.env --check
```

Approximately 3 GB of artifacts are downloaded, in addition to container/build caches.
The manifest records immutable Hub revisions and SHA-256 hashes for weights,
configuration and tokenizer files. Non-LFS hashes were computed from the pinned file
contents; LFS hashes came from Hub metadata. These are provenance/integrity pins, not
independent model auditing or CyberGuard quality measurements.

The downloader checks size/hash before promoting a temporary file, serializes checks/
downloads, skips valid existing files, and refuses to overwrite mismatched artifacts.
An interrupted/failed download is not treated as complete. Investigate corruption
manually; do not delete all models as routine cleanup. Preparation never loads a model
or consumes a job. Stop the worker before changing model pins or preparing a release.

Kev's **adapter and base checkpoint** are populated into an exact-revision Hugging Face
cache. Inference mounts the directory read-only with offline flags and telemetry
disabled. An empty cache must be prepared online first; startup never downloads weights.
Changing manifest revisions also requires reviewing the matching Compose `--run` pin.

## 4. Application VM and network prerequisites

Follow the application runbook for
[private networking and certificates](azure-app-deployment.md#3-networking-and-certificates-are-explicit-prerequisites),
both migration chains, and the restricted `python_incidents` role. For an existing
installation, use the [upgrade procedure](#upgrading-an-existing-deployment).
Go and PostgreSQL names must resolve from inside the Python containers. Keep the
CA private key off both VMs and never expose SQL, worker context, or model ports
to the public network.
`AI_NETWORK_APPROVED=yes` acknowledges operator review; it does not validate the NSGs.

Setup needs outbound HTTPS for package/image/model downloads. Runtime inference
networks have no external routing; Python still needs private Go and incident SQL.

## 5. Install scoped secrets and certificates

Securely transfer only these files into `/etc/cyberguard/ai/secrets/`:

| File | Contents |
| --- | --- |
| `ai_runtime_url` | URL for **python_incidents**, never administrator/migrator/Go credentials |
| `ai_internal_token` | Same server-only secret as Go; not a browser cookie |
| `ca.crt` | Approved CA trust bundle for Go, PostgreSQL and AI gateway diagnostics |
| `api.crt` | AI endpoint server certificate/full chain |
| `api.key` | Matching unencrypted server key; mounted only into private Caddy |

Use an editor/secure file-transfer mechanism, not shell history or command-line password
arguments. The URL must contain a percent-encoded password and verified PostgreSQL TLS:

```text
postgres://python_incidents:URL_ENCODED_PASSWORD@INCIDENT_PRIVATE_NAME:5432/cyberguard_incidents?sslmode=verify-full&sslrootcert=/run/certs/ca.crt
```

Set each file to **0640**, with group matching `AI_SECRET_GID`; retain directory mode
0700. Do not make them world-readable to fix mounts. Python mounts only its runtime URL,
token and CA; Caddy mounts only its own certificate/key. The AI host must not receive
the application administrator/migrator/demo secret directory. Do not run the local
`make docker-secrets` or `create-ai-secrets.sh` here: they create single-host dev URLs
and/or unnecessary credentials. Provision shared credentials deliberately on the app
side and transfer their AI-scoped subset.

HTTP clients explicitly load `AI_GO_CA_FILE` while retaining `trust_env=False`; proxy
and CA environment variables are not implicitly trusted. `AI_REQUIRE_TLS=true` rejects
HTTP Go origins, unexpected database/role names, and DB connections without
`verify-full`/a CA file. Certificates must satisfy Python 3.13/OpenSSL strict validation,
including CA key-usage extensions. Never disable
certificate verification to get past a setup failure.

## 6. Start and diagnose

After fulfilling the prerequisites, set the real private bind address and
`AI_NETWORK_APPROVED=yes` in the non-secret config. The script also checks that the
selected private IP is assigned to this host.

```sh
sudo bash deploy/ai-vm/manage.sh --config /etc/cyberguard/ai/config.env up
sudo bash deploy/ai-vm/manage.sh --config /etc/cyberguard/ai/config.env check
sudo bash deploy/ai-vm/manage.sh --config /etc/cyberguard/ai/config.env status
sudo bash deploy/ai-vm/manage.sh --config /etc/cyberguard/ai/config.env logs
```

`up` verifies local permissions/certificates/models, starts models/API/TLS gateway
without rebuilding/pulling, waits for model health (up to ten minutes), probes storage
and remote Go over TLS, checks the local TLS gateway, then starts the worker. The API
certificate must have at least 24 hours remaining. `check` executes the safe probes
inside the persistent worker, so a stopped worker fails the deployment check. The
probes themselves do not claim jobs.

The configured worker independently probes Go/models and `ai_worker_ready()` **before
every claim**, including after Docker/VM restart; Compose ordering alone is not a reboot
guarantee. A missing safety migration/grant or pending historical job without a snapshot
blocks readiness without consuming jobs. SQL claims independently exclude snapshot-less
jobs, even if a worker bypasses dependency probes. Readiness is not an atomic guarantee
against a model failing mid-job. Terminal failures remain terminal. No requeue/backfill/
reset is automated. A real-model failure never produces a completed fake assessment.

Roll out the Go auth-check route and private gateway allowlist before the worker.
Preflight and every claim require a 204 from this authenticated GET, even with optional
worker probes disabled. A 401/403 on the subsequent context read leaves the claim under
its existing lease; the worker backs off and checks its credential again. After fixing
a token mismatch, recreate services holding stale credentials. Expired claims remain
subject to normal attempt limits.

If checks fail, distinguish the stages:

- **Host prepared:** Docker and directories exist; remote endpoints may be absent.
- **Models prepared:** all pinned files verify; no real inference is established.
- **Application ready:** TLS, storage and service probes pass; schema/grants/context and
  real inference must still be exercised with a new synthetic incident.

Diagnose missing DNS/SANs, cert expiry, group IDs, URL encoding, PostgreSQL TLS/HBA,
Go's shared token, cold models, migration/grant state and pending snapshot-less jobs.
Never print secret contents or
incident/model input to diagnostics. Logs are bounded; llama.cpp logs and Kev prefix
caching are disabled. Review upstream model-server logging before using sensitive data.

## 7. Stop and restart

```sh
sudo bash deploy/ai-vm/manage.sh --config /etc/cyberguard/ai/config.env stop
# Models, secrets and configuration are retained. Restart explicitly with up.
```

Restart policies resume previously running containers after Docker/VM reboot; explicitly
stopped containers remain stopped. There is no separate application systemd unit or
automatic release update. For updates, use the procedure below. Retain old release
images/config/model directory for reviewed recovery; database compatibility must be
checked before rolling code back. Do not prune unrelated Docker state or remove volumes.
Credential/certificate rotation needs deliberate changes on both hosts and container
recreation; editing files alone does not rotate a DB role or reload the Python client.

Configured limits are two threads per model, one worker, and one LLM slot;
the 8 GiB Kev and 4 GiB llama.cpp caps are not measured peak usage.

Deallocate the VM manually when unused; stopping containers or shutting down the guest
is not Azure deallocation. Managed disks and other retained resources can still cost
credit. Nothing in these scripts deletes Azure resources or automates backups.

## Upgrading an existing deployment

The new Go context endpoint serves **immutable job-linked snapshots**, with the same
worker JSON shape as before. Incident migration 00003 intentionally does not backfill
historical jobs. Migration 00004 grants Go access to the job's snapshot link. Migration
00005 preserves snapshot-less queued/running jobs (including expired exhausted claims),
excludes them from SQL claims, and exposes a restricted readiness bit. Stored historical
results remain readable; no job or result is deleted by this upgrade.

1. Follow [application maintenance and protected backups](azure-app-deployment.md#6-maintenance-upgrades-and-protected-backups).
   Pause writes, drain compatible old jobs where possible, then stop **all** workers
   and quiesce the application before migration. Use the `stop` command above on this
   VM. Never copy database backups or administrator/migrator secrets here.
2. After both migration chains reach their heads, an existing `python_incidents` role
   has the `ai_worker_ready()` grant from migration 00005; do not reapply bootstrap just
   to add it. If the role does not exist, provision it explicitly after migrating.
3. Have the authorized application-side database operator review a metadata-only
   inventory in the incident database:

   ```sql
   SELECT status, count(*)
   FROM assessment_jobs
   WHERE context_id IS NULL
   GROUP BY status;
   ```

   Any `queued`/`running` count requires an **explicit operator/team decision**. Keep
   workers stopped until that decision is implemented and reviewed, or leave AI
   processing unavailable. An approved retirement or separately designed historical
   backfill is not supplied by these scripts. Never reconstruct an alleged original
   snapshot from current mutable fields, silently requeue, or mass-fail historical jobs.
   Completed/failed snapshot-less jobs do not block readiness. The runtime-role query
   `SELECT ai_worker_ready();` must return true before worker startup; a missing function
   or permission error is a deployment failure, not permission to disable probes.
4. Start the updated Go service and private gateway first. Build and start the AI
   release explicitly with `build`, `up`, and `check`. Reuse verified artifacts if
   the pins are unchanged. Follow the application runbook for cutover and the
   controlled end-to-end submission; readiness probes do not claim a job.

Migration 00005 is deliberately **forward-only**: restoring the old claim function could
consume snapshot-less jobs. Do not run an automatic schema downgrade or assume the old
Go/worker pair is a safe rollback. Recovery needs a reviewed code/schema compatibility
plan (and a deliberate protected-backup restore if selected), with submissions/workers
paused. This document does not authorize changing historical production jobs.

## Disposable checks

`make ai-vm-test` needs ShellCheck; `make ai-vm-test-bootstrap` mocks host setup.
`make ai-vm-smoke` creates a separate project and exercises TLS, real PostgreSQL,
Go, and Python with synthetic model responses. It downloads no weights and
cleans up only its own disposable volumes. Protect any retained diagnostics.
See the [backlog](backlog.md#validation-and-release-work) for real-host and model
acceptance, which these fixtures do not establish.
