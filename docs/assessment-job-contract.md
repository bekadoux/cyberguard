# CyberGuard — PostgreSQL Assessment Job Contract

**Status:** Proposal for discussion with the Go developer and software architect  
**Scope:** Saving an incident and scheduling its first AI assessment in PostgreSQL

This document describes how the Go backend and Python AI service will share assessment jobs. The goal is to agree on the database structure and responsibilities before implementing job creation. It does not define a complete worker or select an AI model.

## 1. Job lifecycle

When an employee reports an incident, Go saves the incident and creates an assessment job **in the same PostgreSQL transaction**. The job starts in `queued` state. Go returns a successful response only after both records have been committed.

Python picks up the job later, obtains the incident details from Go, runs the assessment, and saves the result. Incident submission must not depend on Python being online. The job must still exist after an API restart or after the employee closes their browser.

Assessment jobs have their own state, independent of the incident's business status:

`queued` → `running` → `completed` or `failed`

AI-generated content is a suggestion for the analyst. It must not overwrite confirmed category, severity, status, comments, or resolution information.

## 2. Database schema

The existing `incidents` table is defined in `db/migrations/incidents/00001_create_incidents.sql`. It already has UUID identifiers and the text fields needed for an initial assessment (`title`, `description`, and optional `affected_system`). The AI tables should be added in a **new migration**, rather than changing that existing migration after it has been applied.

### Assessment jobs (`assessment_jobs`)

| Column | Type | Null? | Notes |
| --- | --- | --- | --- |
| `id` | `uuid` | No | Primary key. |
| `incident_id` | `uuid` | No | Foreign key to `incidents.id`. |
| `kind` | `text` | No | `initial_assessment` for the first job; `regenerated_assessment` is a possible future value. |
| `status` | `text` | No | `queued`, `running`, `completed`, or `failed`; defaults to `queued`. |
| `incident_revision` | `bigint` | No | Identifies the version of incident content intended for analysis. See the open decision below. |
| `attempt_count` | `integer` | No | Starts at `0`; increments when execution is attempted. |
| `created_at` | `timestamptz` | No | Set when the job is inserted. |
| `updated_at` | `timestamptz` | No | Updated when job execution metadata changes. |
| `started_at` | `timestamptz` | Yes | Empty until execution starts. |
| `finished_at` | `timestamptz` | Yes | Empty until the job finishes or fails. |
| `error_code` | `text` | Yes | A safe error identifier, never credentials or raw incident text. |

For a new job, Go sets `kind = 'initial_assessment'`, `status = 'queued'`, and `attempt_count = 0`. The execution timestamps and error code are initially `NULL`. Timestamps are stored in PostgreSQL as `timestamptz`; API timestamps use UTC/RFC 3339.

**One initial job per incident.** The database should enforce this, not just application code. A proposed index is:

```sql
CREATE UNIQUE INDEX assessment_jobs_one_initial_per_incident
    ON assessment_jobs (incident_id)
    WHERE kind = 'initial_assessment';
```

If a job needs another execution attempt, it keeps the same `job_id`. A new assessment after an incident edit would use a separate job ID and a different kind. How regeneration is requested and deduplicated can be decided later. We should also index `(status, created_at, id)` for queue lookups and `(incident_id, created_at DESC)` for job history.

**Incident version is still an open decision.** The existing `incidents` table has `updated_at`, but no revision number. My preference is to add `context_revision bigint NOT NULL DEFAULT 1` and increment it whenever Go changes AI-relevant incident content. The job would record that revision at creation. We still need to decide what happens if the incident changes before Python fetches its context: reject the stale request or keep an immutable snapshot of the original content. A revision number alone does not let us reconstruct an older version.

### Assessment results (`assessment_results`)

I suggest keeping results separate from jobs and from human-edited incident fields:

| Column | Type | Null? | Notes |
| --- | --- | --- | --- |
| `id` | `uuid` | No | Primary key. |
| `job_id` | `uuid` | No | Unique foreign key to `assessment_jobs.id`. |
| `incident_id` | `uuid` | No | Incident reference; must agree with the job's `incident_id`. |
| `assessment` | `jsonb` | No | Generated assessment; exact JSON format will be agreed separately. |
| `source_kind` | `text` | No | Marks the source, e.g. `fake` for the integration fixture. |
| `created_at` | `timestamptz` | No | Time the result was saved. |

There should be **at most one final stored result per job**. Inserting the result and marking the job `completed` must happen in a single transaction. We also need a database-backed way to guarantee that `assessment_results.incident_id` matches the incident on its job (for example, a composite foreign key). The later worker design must prevent an outdated execution attempt from writing a result after another attempt has taken over.

## 3. Service responsibilities and database access

| Action | Go backend | Python AI service |
| --- | --- | --- |
| Create an incident | Yes | No |
| Create the initial queued job | Yes | No |
| Read job metadata | As needed | Yes |
| Claim a job and update execution fields | No, in the normal API flow | Yes |
| Save an assessment result | No, in the normal API flow | Yes |
| Update analyst-confirmed incident decisions | Through authorized Go workflows | No |
| Change database schema | No runtime permission | No runtime permission |

Go continues using `go_incidents`. Python should have a separate, restricted incident-database login (proposed name: `python_incidents`). It needs access to job metadata, narrowly scoped updates to execution fields, and inserts/reads for results. It must not have direct read access to incident descriptions or access to the identity database.

Python gets the text to assess through a protected internal Go endpoint using service credentials. **Jobs must not store browser cookies or rely on a user's active session.** Checking a browser user's permission to view saved results is a separate step and still goes through Go.

Column-level PostgreSQL grants help limit what Python can update, but do not by themselves enforce valid state transitions or ensure that only the worker holding a claim can complete a job. Those rules need explicit SQL checks and tests when we implement claiming.

## 4. Database migrations

We should use the existing goose migration chain in `db/migrations/incidents/`. The Go developer coordinates migration files and corresponding sqlc changes; I can review and contribute the AI-specific schema and queries. The architect reviews the final schema and permissions.

Migrations are applied in a controlled step using `incidents_migrator`, not automatically when Go or Python starts. Python must not use Alembic or ORM `create_all()` to create or modify the shared tables. If the initial incident migration is already applied, we add a new forward migration instead of editing its history.

The separate Python runtime role and its grants also need to be included in the agreed database bootstrap/migration setup.

## 5. Incident context for the worker

The minimum context for an initial assessment is:

- `job_id` and `incident_id`;
- the incident revision or agreed snapshot reference;
- `title` and `description`;
- optional `affected_system`.

Python will request this context from Go using service-to-service authentication. It should not query the `incidents` table directly. Go must check that the job and incident belong together and apply the agreed revision/snapshot rule.

Exact internal endpoint paths, request/response JSON, service authentication and error responses are **not finalized in this document**. They need to be agreed before connecting the fake worker. The same applies to retry limits, leases, crash recovery, competing workers, and the final result/status API.

## 6. Test fixture

We can use the following made-up incident for development and integration tests. A real fixture must also set a `reporter_id` belonging to a seeded test employee, along with any timestamps required by the test setup.

**Incident** (illustrative; `context_revision` assumes the proposed schema change):

```json
{
  "id": "11111111-1111-4111-8111-111111111111",
  "title": "Suspicious password reset email",
  "description": "An employee received an unexpected password reset email asking them to open a link. The employee reports that they did not click it.",
  "affected_system": "Corporate email",
  "status": "submitted",
  "category": null,
  "severity": null,
  "context_revision": 1
}
```

**Queued job:**

```json
{
  "id": "22222222-2222-4222-8222-222222222222",
  "incident_id": "11111111-1111-4111-8111-111111111111",
  "kind": "initial_assessment",
  "status": "queued",
  "incident_revision": 1,
  "attempt_count": 0,
  "started_at": null,
  "finished_at": null,
  "error_code": null
}
```

**Fake assessment — test output only, not an AI prediction or finalized API format:**

```json
{
  "is_fake": true,
  "summary": "FAKE ASSESSMENT: The employee reported an unexpected password reset email.",
  "reported_facts": [
    "An unexpected password reset email was reported",
    "The employee says they did not click the link"
  ],
  "unknowns": [
    "Whether the email sender was spoofed",
    "Whether similar emails reached other employees"
  ],
  "follow_up_questions": [
    "Can the employee provide the email headers?"
  ],
  "suggested_steps": [
    "Review the email metadata using the approved investigation playbook"
  ],
  "disclaimer": "Synthetic test output. Not a confirmed finding."
}
```

The UUID values above are for test data only, not fixed application identifiers.

## 7. Decisions and next steps

For this issue, the main outcome is a shared understanding of the database contract. Before Go starts creating assessment jobs, we should agree on:

1. **Tables and initial values:** final column names, types, nullable fields, job kinds, and the initial `queued` record.
2. **Job identity:** the one-initial-job-per-incident constraint, and the distinction between retrying a job and requesting a new assessment.
3. **Incident context:** whether to use a revision or an immutable snapshot, and what the worker should do if the incident has changed.
4. **Ownership and permissions:** which statements each runtime role may execute, and how Python is prevented from changing analyst decisions.
5. **Migration workflow:** who owns the shared goose migration, how changes are reviewed, and how the Python role is provisioned.
6. **Test fixture:** one agreed synthetic incident, queued job, and clearly labelled fake result.

We can consider this contract ready once the Go developer and AI specialist agree on these points, the architect has reviewed them, and the agreed English document is committed under `docs/`. That does **not** mean the worker is implemented.

Before **fake-worker integration**, we will need a separate, more detailed agreement covering claim ownership and recovery, retries, internal Go endpoints and service authentication, assessment JSON/provenance, and authorized status/result access. Those items should not be assumed to be solved by this initial schema proposal.
