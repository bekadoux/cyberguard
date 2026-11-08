# SQL Foundation and Identity Persistence

Implemented: PostgreSQL bootstrap SQL, versioned identity migrations, generated sqlc
queries, and explicit development-account seeding. HTTP authentication, incident tables,
assessment tables, and Python database access are not implemented yet.

## Ownership and versions

| Item | Source | Owner |
| --- | --- | --- |
| PostgreSQL processes, networks, volumes | Architect's Compose | Architect |
| Database and SQL-role creation | `db/bootstrap/*.sql` | Engineer |
| Identity schema and grants | `db/migrations/identity/*.sql` | Engineer, architect review |
| Handwritten queries | `backend/go/sql/identity/users.sql` | Engineer |
| Generated queries | `backend/go/internal/db/identity/` | sqlc; never hand-edit |
| Demo accounts | `backend/go/cmd/seed`, `internal/demoseed` | Engineer |

Use two separate PostgreSQL instances. Validation used PostgreSQL 17.6 and psql 17.9;
this is not a recommendation to deploy an old patch. Pin a supported, patched image
with the architect. Bootstrap requires psql 15+ for `\getenv`. Goose is pinned in
`go.mod` to **v3.29.0**, sqlc in the Makefile to **v1.31.1**.

## Credentials and privileges

| Database | Owner / migration login | API runtime login |
| --- | --- | --- |
| `cyberguard_identity` | `identity_migrator` | `go_identity` |
| `cyberguard_incidents` | `incidents_migrator` | `go_incidents` |

A separate bootstrap administrator creates roles/databases. Neither migration nor
runtime roles are superusers or have CREATEDB, CREATEROLE, replication, RLS-bypass
privileges, or role memberships. Migration roles own their database. Runtime roles
receive database CONNECT and schema USAGE, not database/schema CREATE or database TEMP.

After migration, `go_identity` has SELECT/INSERT/UPDATE on `users` (**no DELETE**), and
SELECT/INSERT/UPDATE/DELETE on `sessions`, as SCS requires. It cannot read the goose
version table. New tables receive no automatic runtime grants. `go_incidents` has no
business-table permissions yet because incident tables do not exist. Python receives
no identity credentials; its restricted role waits for the agreed AI schema.

### Separate environments

- Root `.env.example`: API-only runtime URLs.
- `db/.env.example`: one-off administration, migration URLs and demo passwords.
- Copy to `.env` and `db/.env` only if those local files do not exist. Both are ignored
  by Git. Replace every placeholder; neither the API nor Makefile automatically reads them.
- Generate independent secrets. Bootstrap migration/runtime passwords within an
  instance must differ, contain at least 16 characters and not start with `REPLACE_`.
- Bootstrap password variables hold raw passwords. URL passwords must be percent-encoded
  and must represent the same secrets used in bootstrap.
- `sslmode=disable` is for trusted local development only, not a deployment TLS policy.

**Never inject `db/.env` into the API/Python container or launch the API from a shell
still exporting administrator/migration secrets.** Use a clean shell or an explicitly
scoped process environment. Ignoring privileged variables in Go does not remove them
from the process environment.

## Bootstrap: databases and roles

Start the architect's two PostgreSQL instances first. In a dedicated administrative
shell, load your trusted edited settings and run from the repository root:

```sh
set -a
. ./db/.env
set +a
make db-bootstrap-identity
make db-bootstrap-incidents
```

These targets use psql with ON_ERROR_STOP and no interactive password prompts. Admin
URLs connect to each instance's `postgres` maintenance database. The scripts reconnect
to the project database to secure its public schema. Set `PSQL` to override the client
executable. The architect can also execute the scripts using psql inside the existing
PostgreSQL containers, with appropriately scoped environment variables and mounts.

Bootstrap properties:

- Missing, short, placeholder, or duplicate migration/runtime passwords fail nonzero
  before role/database changes.
- Repeat bootstrap preserves data but reapplies grants and resets managed-role
  passwords to supplied values. It is not a routine API-start operation.
- An existing database with another owner or managed roles with memberships is rejected.
- Advisory locks serialize bootstrap; statements are bounded. Scripts never drop data.
- CREATE DATABASE cannot run in a transaction. After a partial failure, investigate
  and rerun; do not discard volumes to work around errors.
- Secure admin terminals and PostgreSQL audit logs. Application log redaction does
  not control database-server logging of privileged commands.

## Identity migrations

Set `IDENTITY_MIGRATION_DATABASE_URL` to the `identity_migrator` connection:

```sh
make db-status-identity
make db-migrate-identity
make db-status-identity
```

The command requires the migration role, has a one-minute context deadline and a
bounded PostgreSQL session advisory lock. Goose tracks versions; repeating up is a
no-op once current. Connection strings are not logged. The normal command exposes
up/status, not destructive down. Do not edit already-shared/applied migrations; add
the next numbered SQL file. Python must not run a competing migration system.

### Schema

**`users`:**

| Column | Meaning |
| --- | --- |
| `id` | UUID primary key, PostgreSQL-generated UUID v4 |
| `username` | Unique lowercase ASCII, 3–64 characters: letters, digits, `_`, `.`, `-`; first character alphanumeric |
| `password_hash` | Argon2id PHC string; SQL rejects plaintext-looking values; library verifies the actual encoding |
| `role` | Explicit `employee`, `analyst`, `admin`; no role default |
| `is_blocked` | Initially false |
| `auth_version` | Positive bigint, initially 1 |
| `created_at`, `updated_at` | Timezone-aware instants; future API DTOs serialize UTC/RFC 3339 |

Primary-key and unique-username constraints provide lookup indexes. sqlc queries update
`updated_at` explicitly and increment `auth_version` on password, blocked-state and
role changes. Unblocking also increments it, preventing restoration of old session
versions. Future middleware must compare the session's version with the current user;
these SQL queries alone do **not** implement HTTP authentication or revocation.

**`sessions`:** documented SCS/pgxstore layout: `token TEXT PRIMARY KEY`, `data BYTEA
NOT NULL`, `expiry TIMESTAMPTZ NOT NULL`, and an expiry index. SCS owns serialization
and storage operations, not sqlc. The pinned pgxstore requires its context-aware API
(`CommitCtx`, `FindCtx`, `DeleteCtx`); SCS selects this interface automatically.

## Explicit demo accounts

Supply four distinct non-placeholder passwords of 12–128 bytes:

| Variable | Username | Role |
| --- | --- | --- |
| `DEV_EMPLOYEE_A_PASSWORD` | `employee_a` | employee |
| `DEV_EMPLOYEE_B_PASSWORD` | `employee_b` | employee |
| `DEV_ANALYST_PASSWORD` | `analyst` | analyst |
| `DEV_ADMIN_PASSWORD` | `admin` | admin |

Against an intended **development/demo database only**:

```sh
make db-seed-demo
```

The binary requires explicit `-demo`, uses the migration connection for this controlled
one-off operation, hashes using Argon2id library defaults, and commits all accounts
together. It is never called by API startup.

Re-running preserves existing IDs, passwords, blocked state and auth versions. It is
not a password-reset mechanism. A conflicting existing role fails and rolls back new
accounts. Concurrent seeds preserve username uniqueness. Logs contain counts, not
passwords/hashes. Never seed these predictable accounts in production; use a separately
reviewed administrative setup there.

## Build and generated queries

```sh
make go-sqlc
make go-check
make go-build
```

`go-sqlc` regenerates `internal/db/identity` from migrations and handwritten queries.
`go-sqlc-check` uses the non-mutating sqlc `diff` command and is included in `go-check`.
Commit generated output, but edit only SQL inputs/config. There is no empty incident
sqlc package: it will be added with the real incident schema.

Generated `identity.User` contains the password hash and is persistence-only. Future
HTTP handlers must return safe API DTOs, never serialize this struct directly.

`go-build` produces `backend/go/bin/api`, `bin/migrate` and `bin/seed`. Outside Make:

```sh
backend/go/bin/migrate -dir db/migrations/identity
backend/go/bin/migrate -dir db/migrations/identity -status
```

The architect can include all binaries in one Go application image and run migrate as
a one-off command. Copy/mount the shared SQL directory and supply its actual path.

## Integration tests

Prepare bootstrap roles on **dedicated disposable instances**, then set:

```sh
export TEST_IDENTITY_DATABASE_URL='postgres://go_identity:URL_ENCODED_TEST_PASSWORD@127.0.0.1:15433/cyberguard_identity?sslmode=disable'
export TEST_INCIDENT_DATABASE_URL='postgres://go_incidents:URL_ENCODED_TEST_PASSWORD@127.0.0.1:15434/cyberguard_incidents?sslmode=disable'
export TEST_IDENTITY_MIGRATION_DATABASE_URL='postgres://identity_migrator:URL_ENCODED_TEST_PASSWORD@127.0.0.1:15433/cyberguard_identity?sslmode=disable'
make go-test-integration
```

Replace all placeholders. Never use deployed or ordinary development data. Persistence
tests create a random schema, apply real migrations using the migration role, test
through the runtime connection, and remove only that newly created schema. Down/reapply
does not target public application tables, and the project database is never dropped.

Tests cover constraints, UUID lookups, auth-version updates, forbidden runtime DDL/user
deletion, SCS session round trips/expiry, repeatable/concurrent seeding, rollback on
conflict, and migration down/reapply. The pool suite checks both instances and roles.
Bootstrap, repeated migration/seed commands and integration tests were exercised on
two real user-space PostgreSQL instances, not only mocks. Compose lifecycle and volume
persistence still need the architect's joint acceptance check.

At validation govulncheck found no affected called symbols or imported packages. It
also reported module-only advisory **GO-2026-5932** for the deprecated, unimported
`golang.org/x/crypto/openpgp` package in latest stable x/crypto. We use `argon2`, not
`openpgp`; do not import the unsafe deprecated package. Monitor advisories and update
to an appropriate patched stable dependency when available.
