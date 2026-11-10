# Cyberguard

A university team project for reporting, investigating, and resolving cybersecurity
incidents, with AI-assisted triage and investigation support.

See the [project overview and current plan](docs/project-plan.md) for the agreed
direction, scope, architecture, team responsibilities, and open decisions.

The [software engineer handoff](docs/software-engineer-handoff.md) details the initial
Go backend milestone, chosen tooling, implementation order, and integration checks.

The [Go backend foundation guide](docs/go-backend.md) documents the implemented
configuration, health/readiness endpoints, local startup, and test commands.

The [SQL setup guide](docs/database-setup.md) covers database/role bootstrap, identity
migrations, generated queries, and development accounts. The
[architect handoff](docs/architect-handoff.md) records the Docker integration contract
and the remaining joint acceptance checks.

The [authentication handoff](docs/authentication-handoff.md) covers the implemented
login/logout/current-user API, sessions and browser security, with separate architect,
AI specialist and domain expert review tasks. Its [OpenAPI contract](api/go-authentication.openapi.yaml)
is used by the frontend login/session UI.

The [incident core handoff](docs/incidents-handoff.md) and
[OpenAPI contract](api/go-incidents.openapi.yaml) cover incident create/list/detail,
validation and access-controlled views based on the
[domain specification](docs/domain-specification.md). The AI candidate extends this
core with transactional initial-job enqueueing when migration
`db/migrations/incidents/00002_ai_assessments.sql` is applied. This still requires
contract approval and runtime validation; it is not acceptance of the complete first
vertical slice. See the AI backend notes below.

The [local Docker setup](docs/docker-setup.md) provides runnable commands for Caddy,
Go, two PostgreSQL instances, secret-file delivery and disposable lifecycle tests.
Start with `make docker-secrets` and follow that guide's explicit setup sequence.
The [containerization handoff](docs/containerization-handoff.md) records ownership,
acceptance criteria and the later frontend/AI and two-VM deployment boundaries.

The [frontend setup guide](docs/frontend-setup.md) covers the implemented login/session
UI, light/dark themes, protected incident submission/list/detail, Vite development,
and static frontend packaging in a separate Caddy image. `make docker-build` builds
both application images; the packaged UI is at `http://127.0.0.1:8088/login` after
the documented setup and `make docker-up`. Eligible staff can view stored AI assessment
states/results with advisory and fixture labels; reporter views exclude AI output.
Investigation actions and production HTTPS deployment remain future work.

## AI Backend (proposed integration)

Python/FastAPI, assessment jobs, a CPU-only Kev-compatible decision adapter and
llama.cpp/Qwen brief generation are in `backend/python/`, with optional Docker
services in `compose.ai.yaml`. **AI services are not enabled in the base Compose
deployment** and are **not yet integration-tested on Azure**. Applying the AI migration
enqueues jobs for new incidents even when no AI worker is running; it does not backfill
existing incidents. The optional AI overlay preserves frontend serving and enables
read-only staff assessment display. Real-model and Azure acceptance remain separate.

See [`docs/ai-backend.md`](docs/ai-backend.md) for the schema migration, separate worker
database role, explicit fake fixture, model setup, and approvals required before merging.

## Set up a newly created AI VM

Use **Ubuntu 26.04 amd64**, an existing sudo-capable account, at least **20 GiB free
under `/var`**, and outbound access for packages, container images and model downloads.
The AI VM runs Python API/worker, Kev and llama.cpp behind a private TLS gateway;
Go, the frontend and both PostgreSQL instances belong on the application VM.
No host Python, CUDA or Node installation is needed.

See the [full deployment guide](docs/azure-ai-deployment.md) for certificate/network
details, troubleshooting and validation limits. The scripts configure an existing VM;
they do not create Azure resources or provision the application VM/databases.

### 1. Copy the project to the VM

From your **local repository root**, transfer a reviewed, committed revision. This
works even if the new VM does not yet have Git installed. Replace `YOUR_AZURE_USER`
and `AI_VM` with your SSH account and reachable hostname/address (or use your approved
VPN/jump-host route for a private-only VM):

```sh
git archive --format=tar HEAD | ssh YOUR_AZURE_USER@AI_VM \
  'mkdir "$HOME/cyberguard" && tar -x -C "$HOME/cyberguard"'
ssh YOUR_AZURE_USER@AI_VM
```

The transfer includes committed files only, not local secrets, models or uncommitted
changes. If `~/cyberguard` already exists, stop and inspect it rather than overwriting it.
Keep the deployed project at a stable path because its private gateway config is mounted
from the checkout.

### 2. Prepare the host, images and models

Run these commands **on the AI VM as your selected non-root operator**:

```sh
cd "$HOME/cyberguard"
sudo bash deploy/ai-vm/bootstrap.sh --owner "$(id -un)"
sudoedit /etc/cyberguard/ai/config.env
sudo bash deploy/ai-vm/manage.sh --config /etc/cyberguard/ai/config.env config
sudo bash deploy/ai-vm/manage.sh --config /etc/cyberguard/ai/config.env build
```

Bootstrap installs Docker Engine, Compose/Buildx and utilities, enables Docker, and
creates protected configuration/secret directories plus `/var/lib/cyberguard-ai/models/`.
It retains existing configuration and credentials; it does not change SSH policy or
grant Docker-group access. Continue using sudo for Docker management.

The non-secret config uses literal `KEY=value` settings, without quotes or substitutions.
Keep **`AI_BIND_IP=127.0.0.1` and `AI_NETWORK_APPROVED=no`** until private networking is
approved. Bootstrap sets `AI_SECRET_GID` to your operator's group. Do not put passwords
or tokens in this config.

Review the [model cards/licenses](docs/azure-ai-deployment.md#3-explicit-model-preparation)
and `deploy/ai-vm/models.json`, then download the pinned artifacts as the model-directory
owner, **without sudo**:

```sh
bash deploy/ai-vm/prepare-models.sh --config /etc/cyberguard/ai/config.env
bash deploy/ai-vm/prepare-models.sh --config /etc/cyberguard/ai/config.env --check
```

This downloads roughly **3 GB** for Qwen GGUF, Kev and Kev's base checkpoint, and verifies
their hashes. Valid existing downloads are reused; mismatches are not overwritten.
**You can complete steps 1–2 before the application VM exists.** Building/preparing
models does not start the application or establish real inference readiness.

### 3. Connect to the application VM and start services

Before startup, complete the guide's
[application/network prerequisites](docs/azure-ai-deployment.md#4-application-vm-and-network-prerequisites):

- Prepare the application-side incident schema and restricted `python_incidents` role.
  Go and Python must share the same server-only internal token.
- Provide verified private HTTPS to Go and verified TLS to incident PostgreSQL, plus
  application-gateway routing to the AI VM. Keep identity PostgreSQL inaccessible to AI.
- Restrict cross-VM connections with NSGs/Docker-aware firewall rules. Default ports
  are application → AI **8443**, AI → Go **8443**, and AI → incident DB **5432**.
  Never expose model ports **8009/8081**, direct Go HTTP or database ports publicly.
- Securely install `ai_runtime_url`, `ai_internal_token`, `ca.crt`, `api.crt` and `api.key`
  in `/etc/cyberguard/ai/secrets/`. Files must be **0640**, with group matching
  `AI_SECRET_GID`; the directory remains **0700**. The database URL must use the
  restricted role, `sslmode=verify-full` and the mounted CA path. Follow the guide's
  [secret/certificate contract](docs/azure-ai-deployment.md#5-install-scoped-secrets-and-certificates).
  Never copy application administrator/migrator credentials or the CA private key here.
- Update `/etc/cyberguard/ai/config.env`: set `AI_GO_URL` to Go's private HTTPS origin
  (no `/api` suffix), match `AI_TLS_NAME` to the AI certificate's DNS SAN, and set
  `AI_BIND_IP` to the AI VM's assigned private IPv4 address. Set
  `AI_NETWORK_APPROVED=yes` **only after** reviewing the network allowlists.

Then, from the project root on the AI VM:

```sh
sudo bash deploy/ai-vm/manage.sh --config /etc/cyberguard/ai/config.env up
sudo bash deploy/ai-vm/manage.sh --config /etc/cyberguard/ai/config.env check
sudo bash deploy/ai-vm/manage.sh --config /etc/cyberguard/ai/config.env status
sudo bash deploy/ai-vm/manage.sh --config /etc/cyberguard/ai/config.env logs
```

Startup uses prepared images/models, waits for readiness, then starts the worker. It
never builds, downloads weights, migrates or seeds. Do **not** use the local
`make docker-secrets`/`compose.dev.yaml` workflow on this VM.

To stop services while retaining models, configuration and secrets:

```sh
sudo bash deploy/ai-vm/manage.sh --config /etc/cyberguard/ai/config.env stop
```

Actual Azure installation/reboot and real Kev/Qwen inference/resource measurements
remain unverified. Validate a new synthetic incident end-to-end before a demonstration.
When unused, **deallocate the VM in Azure**; stopping containers does not stop compute
billing, and retained disks can still incur charges.
