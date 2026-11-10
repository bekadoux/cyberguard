# Azure AI VM — Ubuntu 26.04 post-install setup

Status: post-install automation and a standalone AI deployment candidate. No Azure
resources are created. **Real model inference and an actual Azure VM installation/
reboot remain separate acceptance checks**, not established by fixture tests.

The supplied infrastructure describes an existing E4as_v4 in Belgium Central with
4 vCPUs, 32 GiB RAM and a 128 GiB OS disk. Those are operator-provided facts, not an
Azure inventory or a validated capacity/cost guarantee. This deployment targets Ubuntu
26.04 amd64. Read [AI integration notes](ai-backend.md) for pending model/schema approvals.

## Architecture and scope

| AI VM service | Responsibility | Host publishing |
| --- | --- | --- |
| `ai-gateway` | Small Caddy TLS proxy, no frontend | Loopback 8443 initially; later the AI private IP |
| `ai-api` | Read stored results, authorize every read through Go | None |
| `ai-worker` | One job at a time, restricted incident-DB functions | None |
| `ai-kev` | Kev-0.8B on CPU, including its Qwen3.5 base | None |
| `ai-llm` | llama.cpp/Qwen2.5-1.5B GGUF on CPU | None |

The fifth container is intentional: it terminates private TLS without exposing FastAPI
or changing the existing public/frontend Caddy image. Both inference services are on an
internal Docker network. Python and the gateway share a separate bridge; Python can
reach the application VM. Docker bridge DNS does **not** span Azure VMs.

Go, the compiled frontend/public gateway, and **both separate PostgreSQL instances**
remain on the application VM. No databases, Redis, GPU runtime, system Python packages,
Node, or native llama.cpp compiler toolchain are installed on the AI host.

Python dependencies remain `backend/python/requirements.txt` and
`requirements-dev.txt`; no application uv migration is introduced. Python 3.13.18 and
llama.cpp images are digest-pinned. Kev source is commit-pinned; its separate CPU-only
serving environment uses pinned uv and `docker/ai/requirements-kev.lock` with hashes.
The image build asserts Torch has neither CUDA nor HIP support. OS apt packages and
the application's transitive pip dependencies are not fully locked: these controls do
not imply bit-for-bit reproducible builds or an inference-quality/security approval.

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

## 4. Application-VM and network prerequisites

These are **not implemented by the AI bootstrap**:

1. Manually place both VMs in the intended VNet and choose stable private addresses.
   Use private DNS names or private-IP certificate SANs. Go and DB names must resolve
   from inside the Python containers, not only from the host's `/etc/hosts`.
2. Apply the agreed incident migration using the existing **migrator**, then provision
   the restricted `python_incidents` role through `db/bootstrap/ai-worker.sql` using a
   scoped one-off administrator process. Confirm schema/grant decisions with the team.
   Neither API nor worker runs bootstrap/migrations/seed. No identity access is granted.
3. Mount the same independently generated `AI_INTERNAL_TOKEN_FILE` into Go and Python.
   Provide a private HTTPS Go listener supporting `/readyz`, `/auth/me`, incident reads
   and `/internal/ai/jobs/{job_id}/context`. The current public Caddy deliberately denies
   internal paths: do not remove that denial or route worker context through it.
4. Enable PostgreSQL server TLS on the incident instance, publish/bind it only as
   required on the application private interface, and restrict `pg_hba.conf` to the
   incident DB/`python_incidents`/AI VM source. Put a specific `hostssl` SCRAM allow and
   a following AI-source reject **before** broad image/default host rules. Reject
   non-TLS and other database/role access from that source. Keep identity PostgreSQL
   unpublished/inaccessible to the AI VM.
5. Issue server certificates from a trusted private CA for the AI endpoint, Go private
   listener and incident DB, with the corresponding DNS/IP SANs. Keep the **CA private
   key off both runtime VMs**. The trust bundle may include multiple approved CAs.
6. Configure the application's public HTTPS gateway to strip `/ai`, proxy to the AI
   VM's HTTPS endpoint with CA/hostname verification, and preserve same-origin cookie
   behavior. Keep Secure cookies and the existing public internal-route denial.
   Public HTTPS/certificate persistence and proxy trust remain application deployment work.

Suggested cross-VM flow matrix (final ports can differ):

| Source | Destination | TCP | Purpose |
| --- | --- | --- | --- |
| Application VM private IP | AI VM private IP | 8443 | Stored assessment reads via gateway |
| AI VM private IP | Application VM private IP | 8443 | Private Go authorization/context HTTPS |
| AI VM private IP | Application VM private IP | 5432 | Incident DB TLS only |
| Authorized administration source | Required VM | 22 | Key-based SSH |

Apply source `/32` allowlists and deny other sources for these private endpoints. Azure's
default `AllowVNetInBound` is broader than this matrix: custom denies must outrank that
default. Preserve administrative access while reviewing rules. A private-IP Docker bind
is **not proof of isolation**: a public-IP DNAT can still reach that NIC, and Docker
published ports can bypass UFW. Review NSGs and Docker-aware host firewall rules;
`AI_NETWORK_APPROVED=yes` is an operator acknowledgement, not automatic verification.
No model port (8009/8081), direct Go HTTP port, or database should be public.

Setup needs outbound HTTPS for package/image/model downloads. Runtime model networks
have no external routing. Python's network still needs the application endpoints;
there is no fallback to an external AI provider.

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
certificate must have at least 24 hours remaining. `check` repeats safe probes without
claiming jobs; existing running workers continue independently.

The worker independently probes dependencies **before every claim**, including after
Docker/VM restart; Compose dependency order alone is not a reboot guarantee. Readiness
is not an atomic guarantee against a model failing mid-job. Existing terminal failures
remain terminal. No requeue/backfill/reset is automated. A real-model failure never
produces a completed fake assessment.

If checks fail, distinguish the stages:

- **Host prepared:** Docker and directories exist; remote endpoints may be absent.
- **Models prepared:** all pinned files verify; no real inference is established.
- **Application ready:** TLS, storage and service probes pass; schema/grants/context and
  real inference must still be exercised with a new synthetic incident.

Diagnose missing DNS/SANs, cert expiry, group IDs, URL encoding, PostgreSQL TLS/HBA,
Go's shared token, cold models and resource limits. Never print secret contents or
incident/model input to diagnostics. Logs are bounded; llama.cpp logs and Kev prefix
caching are disabled. Review upstream model-server logging before using sensitive data.

## 7. Lifecycle and acceptance

```sh
sudo bash deploy/ai-vm/manage.sh --config /etc/cyberguard/ai/config.env stop
# Models, secrets and configuration are retained. Restart explicitly with up.
```

Restart policies resume previously running containers after Docker/VM reboot; explicitly
stopped containers remain stopped. There is no separate application systemd unit or
automatic release update. For updates: stop, review the checkout/pins, build, prepare
models if changed, then up/check. Retain old release images/config/model directory for
deliberate rollback. Do not prune unrelated Docker state or remove database volumes.
Credential/certificate rotation needs deliberate changes on both hosts and container
recreation; editing files alone does not rotate a DB role or reload the Python client.

On the E4as_v4 the initial model thread limits are two each, one worker and one LLM
slot. Kev and generation are sequential within a job. CPU limits are caps, not
reservations; 8 GiB Kev/4 GiB llama.cpp limits are unmeasured starting values, not
guaranteed peak usage or justification for larger models. Measure startup/warm latency,
peak RAM, disk growth and responsiveness on the actual VM. Use synthetic incidents and
record model/source revisions. Validate cold reboot, Go/DB/model outages and recovery,
staff access, reporter denial and no public inference/database/internal-route access.

Deallocate the VM manually when unused; stopping containers or shutting down the guest
is not Azure deallocation. Managed disks and other retained resources can still cost
credit. Nothing in these scripts deletes Azure resources or automates backups.

### Development checks

```sh
# Host ShellCheck required for this target (e.g. an explicitly installed shellcheck-py).
make ai-vm-test
# Disposable Ubuntu 26.04 container; apt/systemd/network are MOCKED, no host installs:
make ai-vm-test-bootstrap
# Builds Go/Python, real TLS/DB/worker/API but synthetic model HTTP responses:
make ai-vm-smoke
make docker-test-secrets
make docker-smoke
DOCKER_TEST_AI=1 make docker-smoke
```

`ai-vm-smoke` creates only a fresh `cyberguard-ai-vm-smoke-*` project, separate secrets,
certificates and DB volumes. It tests real TLS to Go/PostgreSQL and into the AI gateway,
actual job completion/staff reads, employee denial, cold-model no-claim behavior,
hostname-mismatch rejection and container restart persistence. It does not download
weights or claim real inference. The test-only app TLS listener is not a production
gateway configuration. Its own disposable volumes are removed on exit; failure
diagnostics remain in protected `.local/ai-vm-smoke.*` directories and may contain test
secrets. Default loopback port is 18443; override explicitly with `AI_TEST_PORT` if busy.
Ordinary development services, secrets and models are never selected.

Python regression command (in a Python 3.13 environment with the existing dev requirements):

```sh
PYTHONPATH=backend/python python -m pytest -q backend/python/tests
```

Mocked/bootstrap/fixture checks are not acceptance of real Ubuntu package installation,
systemd/Docker reboot behavior, Azure NSGs, concurrent DB claims/lease recovery, real
Kev/Qwen inference, model quality or production resource use. Report those checks
separately when the actual infrastructure is available.

### Local validation evidence — 10 November 2026

- `make ai-vm-config` and `make ai-vm-build`, using the checked-in non-secret example,
  passed without an application VM or runtime secrets. Both Python and CPU Kev images
  built; the pinned Kev server's `--help` import and llama.cpp CLI ran without weights.
- `make ai-vm-test` and `make ai-vm-test-bootstrap` passed, including literal config
  parsing, inherited Compose isolation, download hash/overwrite/symlink safeguards,
  credential permission rejection and mocked bootstrap repeatability/failure paths.
- `make ai-vm-smoke` passed with real Go/PostgreSQL/Python/Caddy, verified private-CA
  HTTPS/PostgreSQL TLS, cold-model gating, stored synthetic output, employee denial,
  the management `check` path and restart persistence. Its own test containers/volumes
  were removed; synthetic model servers were used, not Kev/Qwen inference.
- `make docker-test-secrets`, `make docker-smoke` and
  `DOCKER_TEST_AI=1 make docker-smoke` passed. The latter remains an explicitly labelled
  fixture, with no model profile or weights. No browser suite or full Go race suite
  was rerun for this deployment/Python change; Go and SQL source were unchanged.
- All **36 Python tests** passed on Python 3.13.18. Scoped Ruff formatting/lint and
  Pyright passed for the changed Python modules and new deployment fixtures/tests.
  The existing upstream Starlette/AnyIO deprecation warning remains.

Docker used the local legacy-builder fallback (Buildx was absent on the development
host); the VM bootstrap installs Buildx explicitly. No Azure credentials/VM access were
provided, so real host installation, private NSG connectivity, cold VM reboot and real
model downloads/inference/resource measurements were **not run**.
