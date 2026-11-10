# Frontend investigation and AI review handoff

This candidate extends the React workspace in `feature/frontend-handoff` and the
Go handoff API. It is not production-accepted until the checks below pass in the
recipient's environment.

## Functional coverage

| Feature | UI | Go API |
| --- | --- | --- |
| Claim / assign / edit fields | Incident detail → Investigation actions | Existing `/claim`, `/assign`, `PATCH /fields` |
| Investigation lifecycle | Incident detail → Change status / Archive | Existing `/transition`, `/archive` |
| Public/internal comments and reporter replies | Incident detail → Comments | Existing comments endpoints |
| Incident history | Incident detail → Incident history (staff) | Existing `/history` |
| Notifications read/unread | Navigation → Notifications | Existing `/read`, new `/unread` |
| Account administration and audit | Navigation → Admin (admin only) | Existing `/admin/users`, `/admin/audit` |
| Human AI review | Incident detail → AI assessment → Human review | New `POST /incidents/{id}/ai-feedback` |

All browser calls use `request` with `credentials: include`; no passwords or
session tokens are stored in localStorage. UI role/ownership gates are hints:
Go still performs authoritative permission checks on every operation. API
responses are sanitized and explicit errors shown without server diagnostics.

## AI Feedback persistence

Migration `db/migrations/incidents/00006_ai_feedback.sql` grants the Go runtime
read access to only the assessment-result columns needed to validate a review,
and adds one unique feedback event per assessment job/incident. The new Go
endpoint verifies: the caller is an assigned analyst or admin; the incident is
not terminal; the job and result belong to the incident; the result is **not**
a synthetic fixture; the decision and confirmed category are consistent; and
reason requirements. Acceptance/correction updates the incident category and
records an `ai_feedback_submitted` event in **one transaction**. Rejection
records a reason and leaves the confirmed category unchanged.

AI output does **not** automatically change severity or status. There is no
model retraining endpoint; feedback is currently persisted as an auditable human
review event, suitable for future controlled evaluation/export.

## Local verification checklist

1. Apply the patch to the clean `feature/frontend-handoff` branch.
2. Run `npm ci` in `frontend/` with Node 24.22.2 and npm 12.2.2.
3. Run `npm run typecheck`, `npm run lint`, `npm run format:check`, `npm test`,
   and `npm run build`. Format changed files with the pinned Prettier if needed.
4. With Go 1.27.3 run `go test ./internal/httpapi` from `backend/go/`, then the
   project-level Go checks and dedicated PostgreSQL integration suite.
5. Apply `00006_ai_feedback.sql` through the project's normal migrations tool,
   **after** all previously merged migrations, to a disposable database first.
6. Validate reporter, assigned/unassigned analyst and admin flows against
   two disposable PostgreSQL instances. Verify AI feedback rejects duplicate
   submissions and synthetic assessments; verify notifications can be marked
   read/unread only by their recipient.
7. Run relevant Playwright and Docker smoke suites before opening a PR.

Do not run migrations against a production database before review. Do not edit
sqlc-generated code; this feature uses the existing handoff persistence layer.
