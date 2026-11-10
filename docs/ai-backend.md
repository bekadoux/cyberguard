# CyberGuard AI Backend — Developer Setup and Integration Notes

**Status:** implementation candidate, not yet production-validated. The upstream `docs/assessment-job-contract.md` is a *proposal*, and its unresolved decisions require team/architect review. The files in this increment implement one particular MVP resolution; do not merge schema/interface decisions without that review.

## Delivered components

- `backend/python/cyberguard_ai/api.py`: FastAPI read-only endpoint `GET /ai/incidents/{uuid}/assessment` through the AI Caddy overlay. Response has `job` (queued/running/completed/failed) and `result` (null until completed). `GET /ai/healthz` and `/ai/readyz` are health endpoints.
- `backend/python/cyberguard_ai/worker.py`: single-claim polling process; no Redis or message broker; claims, renewable lease, fencing by random claim token, bounded attempts for expired claims, terminal failures. Running a second worker is safe but not recommended on CPU.
- `backend/python/cyberguard_ai/models.py`: Kev decision adapter (`choice`, `score`, `noul`); Qwen GGUF LLM via llama.cpp OpenAI-compatible API; safe fallback is **failure**, never fabricated output. `AI_MODE=fake` explicitly enables a non-AI integration fixture.
- `db/migrations/incidents/00002_ai_assessments.sql`: new `assessment_jobs`, `assessment_results`, revision tracking and fenced security-definer SQL functions. Insert trigger enqueues `initial_assessment` in **the same transaction** as the existing Go `CreateIncident` INSERT, so the current Go sqlc-generated code can keep its API contract. Do not also enqueue directly in Go without revisiting this trigger.
- `backend/go/internal/httpapi/ai_context.go`: Go's internal-only `GET /internal/ai/jobs/{job_id}/context`, with service-secret authentication; after the 00003 handoff migration checks persisted job-to-snapshot pairing and archived state. Caddy hides any `/internal` segments, including `/api/internal`.
- `compose.ai.yaml`: optional AI services and proxy. Original Compose files and their existing smoke-test expectations remain unchanged when overlay is not used.

**Handoff follow-up:** After applying `00003_context_and_investigations.sql`, each new incident has an immutable `incident_contexts` snapshot linked to its job, and the internal Go endpoint reads this persisted snapshot (rather than comparing against mutable incident fields). Historical jobs are not backfilled. See [`handoff-implementation.md`](handoff-implementation.md).

Apply incident migrations through `00005_snapshot_worker_safety.sql` and identity
through `00003_user_admin_audit.sql` before restarting the updated services. Migration
00004 supplies Go's snapshot-link read grant. Migration 00005 preserves snapshot-less
jobs (including expired claims), and adds the restricted boolean `ai_worker_ready()`
probe. Pending historical jobs block worker readiness without being consumed or failed.
See the [upgrade procedure](azure-ai-deployment.md#upgrading-an-existing-deployment)
before changing an installation that already has jobs. New jobs use immutable original
context; an `affected_system` correction does not rewrite their input or enqueue again.

On `feat/frontend`, the overlay selects a baked AI proxy snippet via `AI_HANDLER`
instead of mounting a replacement Caddyfile. The shared gateway retains static files,
SPA reloads, cache policy and internal-path denial in both modes. Rebuild the web image
after changing either Caddy snippet; no host Caddyfile mount is required.

## CPU models and the Jev decision

Original TypeSafe **Jev** is a hosted, proprietary decision model. There is no official self-hosted Jev weight download as of 9 Nov 2026. Using Jev's API would send incident descriptions outside this deployment and would not satisfy the project's self-hosting preference.

Instead the optional `ai-kev` container runs **Kev-0.8B**, a Jev-style *different* open-weight model, with `--device cpu`. It is NOT Jev. Kev serves a compatible `/v1/systemone` API. The adapter sends 1 choice (8 categories), 1 score (impact) and 4 nouls (explicitly reported indicators). Output is always advisory; `0.60` is a provisional uncertainty threshold, **not** measured/calibrated on CyberGuard data.

The optional `ai-llm` container runs **Qwen2.5-1.5B-Instruct Q4_K_M GGUF** with `llama.cpp` and a single CPU thread. The LLM produces a brief (summary, facts, unknowns, questions). `suggested_steps` are taken **only from** `docs/playbooks.md` (which is currently a draft awaiting domain-expert approval); the model cannot choose arbitrary playbook steps or run tools. `AI_MODE=models` is the default. A failed model call makes the job `failed`, never `completed` with a fake result.

**No model weights are bundled**: models download at a separate explicit setup stage.
Check licensing, provenance, SHA hashes and CPU performance. Use `AI_MODE=fake` only
against disposable integration data, then enable real models. The Kev Dockerfile now
pins upstream source, uv and hashed CPU-only dependencies. Model revisions, tokenizer/
configuration hashes and Kev's separate Qwen base are pinned in
`deploy/ai-vm/models.json`. Neither an image build nor metadata pins validate inference.
The [AI VM deployment guide](azure-ai-deployment.md) describes the standalone Ubuntu
26.04/two-VM candidate; the commands below remain the **single-host local** workflow.

## Local setup (Linux/WSL2, Docker Desktop)

Execute from the repository root; do **not** paste any passwords into Git or commit `.local/` files. Use the original `docs/docker-setup.md` first to provision Go and PostgreSQL. On a fresh setup its sequence is:

```bash
export DOCKER_SECRET_GID="$(id -g)"
make docker-secrets
make docker-build
make docker-db-up
make docker-bootstrap
make docker-migrate
# Optional: make docker-seed-demo
```

On an **existing** setup with DB volumes, first follow the
[upgrade procedure](azure-ai-deployment.md#upgrading-an-existing-deployment), including
pausing submissions, draining compatible old jobs where possible, stopping all workers
and reviewing pending snapshot-less jobs. Do **not** regenerate secrets, bootstrap a
fresh DB or remove volumes. During that maintenance window, rebuild the migration image
(it embeds SQL), then apply both instances' pending migrations with existing credentials:

```bash
make docker-build
make docker-migrate
```

For a **first AI installation only**, create additional AI secrets once and provision
the restricted database role *after all incident migrations*. Do not rerun this
credential-provisioning step for an existing AI role merely to add the readiness grant:

```bash
sh docker/ai/create-ai-secrets.sh
export DOCKER_SECRET_GID="$(id -g)"
docker compose -f compose.yaml -f compose.dev.yaml -f compose.ai.yaml \
  --profile tools run --rm bootstrap-ai
```

Run API + worker in **explicit fixture mode**, without downloading any model weights:

```bash
export AI_MODE=fake
docker compose -f compose.yaml -f compose.dev.yaml -f compose.ai.yaml \
  build go-api ai-api ai-worker
docker compose -f compose.yaml -f compose.dev.yaml -f compose.ai.yaml \
  up -d identity-db incident-db go-api ai-api ai-worker web
```

This starts Caddy at `http://127.0.0.1:8088`. `GET /ai/healthz` should return `{"status":"ok"}`, and `/ai/readyz` requires the restricted PostgreSQL role. Creating an incident through **Go authenticated API** automatically adds one queued job. Poll `GET /ai/incidents/<uuid>/assessment` using the *same browser session cookie* as Go; the user must have the `analyst` or `admin` role and permission to view that incident. An employee cannot retrieve AI analysis via this endpoint (conservative permission until team approves requirements).

**Existing incidents:** the new trigger applies to *new INSERTs only*. Old incidents are not automatically backfilled. An architect-reviewed explicit backfill migration/operation would be needed; this increment intentionally does not modify old user data.

## Enabling the actual models

1. Stop the worker to avoid marking jobs `failed` while models are still loading: `docker compose ... stop ai-worker`.
2. Review the model cards and `deploy/ai-vm/models.json`. Create a non-secret local
   config from `deploy/ai-vm/config.env.example` only if your selected config does not
   exist, and set `AI_MODEL_DIR` to the **absolute** path of `.local/models/`. Create that
   directory, then run `bash deploy/ai-vm/prepare-models.sh --config YOUR_LOCAL_CONFIG`.
   This populates `gguf/` and the exact-revision `hf/hub/` cache under `.local/models/`,
   verifying hashes for Qwen GGUF, Kev and Kev's Qwen base. Requires Bash, curl, jq and
   standard Linux tools, not host Python. Downloads total roughly 3 GB.
3. Build the optional Kev service (this installs substantial CPU-only Torch packages,
   **not weights**). Both models mount the prepared directory read-only. Kev starts
   offline, so an empty cache cannot be repaired by restarting it. The former Docker
   `kev-cache` volume is no longer used; existing volumes/files are not deleted or
   automatically migrated. Retain them until reviewed manually. Peak RAM remains to
   be measured; local 4 GiB Kev limits may need adjustment after real-model tests.
4. Start models with `docker compose -f compose.yaml -f compose.dev.yaml -f compose.ai.yaml --profile ai-models up -d ai-kev ai-llm`. Wait for actual health/readiness; do not rely on a running container alone.
5. Set `AI_MODE=models`, then recreate the AI worker (`... up -d --force-recreate ai-worker`). **Failed jobs are terminal** in this version: create a new test incident to exercise the real workflow. Never silently reset production jobs.

Both model services are on the private `ai-inference` Docker network, **not exposed on host/public ports**. Python worker uses local URLs inside Docker. Thread and CPU limits are conservative starting values, **not proof** that your Azure VM remains responsive; benchmark CPU, RAM, warm latency and API responsiveness on the actual VM (especially with 8 GB RAM). Consider running Kev and LLM sequentially or choosing a smaller decision checkpoint if needed.

The overlay enables non-mutating Go/model and snapshot-queue readiness probes before
each worker claim, including after restarts. Missing safety migration or pending legacy
jobs keep the queue untouched. This cannot prevent a dependency failing during inference.
Failed jobs remain terminal; no automatic retry/backfill/retirement is introduced.

## Authorization, secrets and database boundaries

- Go remains the only login authority. Python checks `/auth/me` and `/incidents/{id}` on **every** assessment read; authenticated analyst/admin only, with a current incident-access check. It does not trust the incoming `incident_id`, role or cookie without Go verification. Browser only receives stored results. No write API is exposed.
- Analysts/admins retain AI access on their own reports, as on another person's report.
  Employees receive 403 before AI-table reads. Actor/incident/reporter identifiers are
  validated from Go, never trusted from the browser. Missing/malformed Go identity or
  incident data fails closed; session expiry during the second lookup remains 401.
- The worker uses `AI_INTERNAL_TOKEN` (or `_FILE`) to call Go's internal context endpoint. **Never** expose direct Go port publicly with this route enabled, or publish `AI_INTERNAL_TOKEN`; use an internal Docker network/private VM interface. This token is not a user session cookie.
- Python's `python_incidents` role has only limited `SELECT` on AI tables and `EXECUTE` on controlled DB functions. No SELECT on `incidents` or `users`, no UPDATE of analyst fields, no schema write permissions. Password is in a Compose secret.
- The migration runs using the existing incident **migrator**, not via API/worker startup. `bootstrap-ai` is a separate one-off provisioning step; never use it for every startup or as part of ordinary `up`.
- Failed inference is visible in `job.status=failed` with a *safe error identifier*, not a stack trace or incident text. Process logs intentionally exclude user-submitted incident text, headers, responses and credentials. The `AI` status result is still sensitive and should not be cached.
- PostgreSQL lease functions reject stale claim tokens and expired leases. `ai_finish_job` saves JSON and marks the job completed **inside one DB transaction**. Workers never modify incident business fields.

## Test plan and honest limitations

The original candidate checklist below predates the frontend integration follow-up.
The optional disposable fixture smoke now exercises the real PostgreSQL/Go/Python/
Caddy happy path with `AI_MODE=fake`; it does not establish real inference, concurrent
claim/lease-expiry/rollback coverage or production acceptance.

```bash
# In a Python 3.13 virtual environment, from the repo root:
python -m pip install -r backend/python/requirements-dev.txt
PYTHONPATH=backend/python python -m pytest -q backend/python/tests
```

The unit tests mock external HTTP model/Go services and verify classification JSON,
approved playbooks, fake provenance, access checks, context validation, worker persistence
and failure paths. The original generation environment had no Docker/PostgreSQL runtime
or model weights. Later validation of regenerated sqlc output, Go 1.27.3, real PostgreSQL/
TLS and Docker fixtures is recorded in the
[deployment guide](azure-ai-deployment.md#main-integration-validation--10-november-2026).
Concurrent AI job claims, full AI lease recovery, real Kev/Qwen inference and measured
resource usage on Azure remain separate checks, not established by the fixture smoke.

Historical authorization follow-up (10 November 2026, superseded own-report policy):
all 23 Python tests passed with Python
3.13.18, including analyst/admin own-report denial before storage access and malformed
Go-response rejection. Scoped Ruff formatting/lint and Pyright checks passed for the
changed authorization/tests. The suite emits an upstream Starlette/AnyIO deprecation
warning. These mocked authorization checks do not establish real-model or PostgreSQL
integration acceptance; the outstanding runtime checks above remain separate.

Frontend integration follow-up (10 November 2026): the optional combined smoke now
provisions a fresh, separate project and verifies the real DB trigger, worker claim,
Go service context, stored fixture result and staff browser read path:

```sh
DOCKER_TEST_AI=1 DOCKER_TEST_FRONTEND=1 make docker-smoke
```

The current fixture verifies employee denial and analyst/admin own-report access with
native cookies, preserves the packaged SPA/cache/internal-route boundaries, and runs browser
checks with no credential-bearing diagnostics in reports. It forces `AI_MODE=fake`
and never enables model profiles or downloads weights. See
[frontend evidence](frontend-setup.md#ai-integration-follow-up--10-november-2026).
Successful happy-path fixture validation does not replace the remaining full AI claim/
lease-expiry, real-model and production review items above. See the
deployment guide for the current integration's validation rather than this historical run.

## Team decisions for review

The original proposal's open context choice is resolved here as immutable job-linked
snapshots, not rejection against the current mutable incident revision. The implementation
retains trigger enqueueing, one result per job, terminal model errors, a single background
worker and analyst/admin-only reads (including own reports). Schema/service acceptance,
production HTTPS/proxy policy, rotation, backups, reviewed retry/backfill policies and
real-model evaluation/resource measurements remain separate team acceptance tasks.

## Remaining Go handoff implementation (new increment)

See [`handoff-implementation.md`](handoff-implementation.md) for the immutable context
contract, assignment/workflow/comment/admin endpoints and notification/audit behavior.
The older test evidence above is historical, not validation of the current increment.
Apply all required forward migrations and rebuild Go/Python images before startup.
