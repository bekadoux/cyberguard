# Frontend handoff: reporter-safe incident status timeline

**Status:** Implemented in Go. No frontend changes yet.

## API contract

Add `GET /incidents/{id}/timeline` to Go (`GET /api/incidents/{id}/timeline`
through the gateway). A current authenticated account may read the timeline
**only for a non-archived incident it reported**, regardless of its role.
Non-owners, archived incidents, invalid IDs and missing incidents get the same
`404`; missing or invalid sessions get `401`. Staff keep their separate
professional `/history` endpoint.

Return status and time only, newest first. Use UTC RFC 3339 timestamps.
Pagination: `limit` defaults to `20`, range `1`–`100`; `cursor` is an
opaque continuation token scoped to the incident and does not grant access.
Order is stable even when timestamps tie. Invalid, duplicate and unknown query
parameters return `400`.

```json
{
  "events": [
    {"status": "investigating", "occurred_at": "2026-11-23T13:20:00Z"},
    {"status": "triage", "occurred_at": "2026-11-23T13:15:00Z"},
    {"status": "submitted", "occurred_at": "2026-11-23T13:00:00Z"}
  ],
  "next_cursor": null
}
```

### Visible events

- Show submission at `incidents.created_at`, even for older incidents with no
  submission audit entry. Do not show a second submission from audit.
- Show actual transitions to `triage`, `investigating`, `awaiting_reporter`,
  `resolved`, `closed`, and `false_positive`. A public reply, reopen, or automatic
  closure appears only as the resulting status, not as a separate event.
- An initial claim records `triage`. The API now records the transition
  explicitly when an admin first assigns a `submitted` report. Older
  assignment rows do not say whether the status changed, so the API does not
  infer a historical transition. A reassignment does not create a status event.
- Do not return archive actions, audit rows or identifiers, actors, reasons,
  comments, internal notes, classification edits, or AI data. Do not invent
  missing transitions from the current status.

Ownership and archive checks apply on every page. See
[workflow OpenAPI](../../api/go-handoff.openapi.yaml).

## Draft frontend issue

**Title:** Show an incident status timeline to the reporter

**Depends on:** deploying the Go timeline endpoint above.

- Show **Status history** on an incident the signed-in user reported, including
  staff on their own reports. Keep the staff action history separate.
- Render the statuses and timestamps in the order returned. Offer **Load more**
  when a cursor exists. Handle loading, errors and retry within the history
  section without hiding an already loaded incident.
- Do not call `/history` for this UI or fill gaps in legacy histories. Cover
  owner-only display, staff on their own vs. others’ reports, loading/error
  states, and pagination using synthetic responses.
