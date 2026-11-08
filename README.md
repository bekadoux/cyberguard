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
is available for the later frontend integration.
