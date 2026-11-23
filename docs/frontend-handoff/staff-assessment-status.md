# Frontend handoff: initial AI assessment status in the incident queue

**Status:** Implemented in Go. Deploy the updated API and incident migration
`00010` before enabling this UI.

## API contract

Extend the existing Go `GET /incidents` response (`GET /api/incidents` through
the gateway). Each row returned to an `analyst` or `admin` has one additional
field, including when the report belongs to that staff member:

```json
{
  "ai_assessment_status": "running"
}
```

| Value | Meaning | Suggested UI label |
| --- | --- | --- |
| `queued` | The initial assessment job is waiting to be processed | Queued |
| `running` | The initial job is being processed | In progress |
| `completed` | The job finished; this says nothing about its recommendation or human review | Complete |
| `failed` | The job failed | Failed |
| `null` | No initial assessment job exists for this incident | No assessment |

For `employee`, omit the key entirely. Do not send an AI result, job ID, review,
error detail, or model data. Use the persisted **initial** job, not the most
recent job of any kind. A queued job is not `null` just because the worker is
offline. Preserve current incident visibility, filters, ordering and cursor
semantics. This proposal adds neither an AI filter nor a sort option and does
not change the detail response.

Go reads the status from incident PostgreSQL in the list query, without
per-row queries or a call to Python. See
[incident OpenAPI](../../api/go-incidents.openapi.yaml).

## Draft frontend issue

**Title:** Add an AI assessment status column to the staff incident queue

**Depends on:** deploying the Go incident-list contract above.

- Show **AI assessment** for `analyst` and `admin` only, separate from the
  incident workflow status. Use the labels in the contract, including **No
  assessment** for `null`. Never show the column to employees.
- Use the value supplied by the list API. Keep the existing manual refresh
  and pagination; do not poll or fetch assessments per row.
- Cover the five display values, staff viewing their own report, employee
  visibility, and refresh without changing filters or losing the list state
  unnecessarily. Use synthetic responses in browser tests.
