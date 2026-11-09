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
the documented setup and `make docker-up`. Investigation actions, AI assessment and
production HTTPS deployment remain future work.

## AI Backend (proposed integration)

Python/FastAPI, assessment jobs, a CPU-only Kev-compatible decision adapter and
llama.cpp/Qwen brief generation are in `backend/python/`, with optional Docker
services in `compose.ai.yaml`. **AI services are not enabled in the base Compose
deployment** and are **not yet integration-tested on Azure**. Applying the AI migration
enqueues jobs for new incidents even when no AI worker is running; it does not backfill
existing incidents. AI/frontend integration remains deferred.

See [`docs/ai-backend.md`](docs/ai-backend.md) for the schema migration, separate worker
database role, explicit fake fixture, model setup, and approvals required before merging.
