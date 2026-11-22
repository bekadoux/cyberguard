# PostgreSQL architecture and setup

CyberGuard uses **two separately running PostgreSQL instances**, not two databases on
one server. Go holds independent pgx pools. User identities referenced by incident
records are UUIDs; there are no cross-instance foreign keys or transactions.

| Database | Stored data | Owner / migrator | Runtime access |
| --- | --- | --- | --- |
| `cyberguard_identity` | Users, password hashes, SCS sessions, account audit | `identity_migrator` | `go_identity` |
| `cyberguard_incidents` | Incidents, revisions, resolution timers, comments, actions, notifications, job snapshots, jobs and results | `incidents_migrator` | `go_incidents`; restricted `python_incidents` |

A separate bootstrap administrator provisions each instance. Neither runtime nor
migration roles are server admins. Go runtime grants are explicit and scoped to the
required data and operations. Python has no identity credentials or direct read
access to incident descriptions. It reads approved job/result fields and uses
controlled SQL functions to claim, renew, and finish jobs. It gets incident content
from Go. Deployment uses verified TLS; see [deployment architecture](deployment.md).

## Schema and migrations

- [`db/bootstrap/`](../db/bootstrap) provisions databases and roles.
  [`db/migrations/`](../db/migrations) owns the two Goose migration chains.
  Apply new numbered migrations rather than editing shared/applied migrations.
  Add explicit grants for new tables. Python does not run its own migrations.
- Current heads are identity `00004` and incidents `00007`. The incident insert
  trigger writes the snapshot, initial job, and submitted event in the incident
  transaction. One initial job per incident is enforced in PostgreSQL.
- Human AI review is an incident action, unique by normalized job UUID. Accepting or
  correcting a category and recording the decision commit together. `resolved_at`
  lives in `incident_resolutions`, not in the incident row.
- Edit handwritten queries in `backend/go/sql/{identity,incidents}/`, regenerate
  with `make go-sqlc`, and commit the result. `make go-sqlc-check` verifies generation;
  never edit the generated `backend/go/internal/db/` packages by hand.

## Provisioning and credentials

Use the [local Docker runbook](docker-setup.md#fresh-local-environment) or
[application VM runbook](azure-app-deployment.md) for their managed environments.
For separately managed local instances, start both servers, then export the trusted
settings from [`db/.env.example`](../db/.env.example) into scoped administrative
processes. `psql` 15+ is required for bootstrap. Use administrator connections to
`postgres` for bootstrap and the exact migration-role connections for migrations:

```sh
make db-bootstrap-identity db-bootstrap-incidents
make db-migrate-identity db-migrate-incidents
make db-status-identity db-status-incidents
```

Bootstrap requires independent passwords and may reapply role passwords on rerun;
never use it for normal startup. `make db-seed-demo` is a separate local/demo step
that creates four accounts and preserves existing credentials on repeat. It does
not seed incidents. Migration is explicit, with a bounded lock and an up/status
interface; the API and worker never run it.

Start Go in a clean runtime-only environment. Neither Go nor Make automatically reads
`.env`. Use one source per setting: direct credential or `_FILE`, never both even
when one is empty. Go reads regular secret files (at most 16 KiB) at startup and
removes one final LF/CRLF only. Percent-encode passwords in connection URLs. Editing
a file neither rotates its database role nor reloads a process.

## Initialize the application owner

After applying the identity migrations, run the one-off `seed` binary with the
`identity_migrator` connection. Ownership is initially unset and can be initialized
only once. This does not run during application startup or the legacy demo seed.

For a fresh installation, create one new active admin as the owner. Supply a
protected password file via standard input, with no password in command arguments:

```bash
backend/go/bin/seed -owner first_admin -create-owner < /secure/path/owner-password
```

On an existing installation, explicitly select an active admin without changing
their password:

```bash
backend/go/bin/seed -owner existing_admin
```

The commands use `IDENTITY_MIGRATION_DATABASE_URL` or its `_FILE` alternative;
do not expose migration credentials to Go API processes. For managed Docker/VM
installations use the corresponding [local](docker-setup.md#demo-with-self-registration)
or [server](azure-app-deployment.md#initialize-the-application-owner) procedure.
Keep the owner's password in an approved password manager: other admins cannot
reset it and there is no in-app recovery override.

## Integration tests

Use **two disposable, separately bootstrapped PostgreSQL instances**, never ordinary
development or deployed data. Export these four variables with the corresponding
fixture-role URLs, then run `make go-test-integration`:

| Variable | Fixture role |
| --- | --- |
| `TEST_IDENTITY_DATABASE_URL` | `go_identity` |
| `TEST_INCIDENT_DATABASE_URL` | `go_incidents` |
| `TEST_IDENTITY_MIGRATION_DATABASE_URL` | `identity_migrator` |
| `TEST_INCIDENT_MIGRATION_DATABASE_URL` | `incidents_migrator` |

Persistence tests migrate into random schemas and clean up only those schemas. The
[`docker-smoke` integration flag](docker-setup.md#checks) can provision both disposable
instances and configure these URLs.
