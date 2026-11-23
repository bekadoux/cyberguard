# React frontend

The React/TypeScript single-page app lives in [`frontend/`](../frontend). Vite builds
static files for Caddy; Node is not a deployed service. React Router defines login,
account, incident list/create/detail, notifications, and admin screens in
[`src/app/App.tsx`](../frontend/src/app/App.tsx), including a public `/signup`
page for the existing `POST /auth/signup` endpoint. Signup is feature-gated on
Go via `SIGNUP_ENABLED`, always creates an employee, and never starts a session.
A new browser route also needs the [gateway's SPA allowlist](deployment.md#gateway-routing).

| Code | Responsibility |
| --- | --- |
| `src/features/auth/` | Login, current-user state, route protection, and logout |
| `src/features/incidents/` | Incident views and workflows, comments/history, admin and notifications, AI read/review |
| `src/shared/api/` | Credentialed no-store JSON requests, cancellation, bounded requests, and safe errors |
| `src/shared/theme/`, `src/styles/` | System/light/dark preference and semantic style tokens |

## Session and authorization

- Requests use relative `/api` and `/ai` paths with the existing HttpOnly cookie.
  JavaScript never reads the cookie or stores the identity or token in browser storage.
  The theme preference is stored separately.
- The client restores and rechecks identity through `/auth/me` on app startup and
  on focus/visibility changes. It has no background keepalive. Login/logout
  are serialized within one app instance, and generation checks discard stale
  responses. Ordering across independent tabs is not guaranteed.
- A current 401 clears the session. Unavailable session checks gate protected content
  without claiming server-side logout. An incident or assessment outage is an error
  for that feature, not a logout. Role and ownership gates in the UI are not authority;
  each API enforces its own checks.

## Incident and assessment views

The list uses cursor pagination with status and category filters. The detail page
separates the original report, confirmed fields, discussion, and staff-only action
history. Workflow forms keep local drafts during refresh and send only edited
fields. A submission with an uncertain network outcome is not retried automatically.

Analysts and admins, including staff reporters, can see the stored AI job status
and assessment. Employees do not mount or fetch the panel. Refresh is manual.
Fixture output is labelled and cannot be reviewed as a real assessment. For real
output, the panel shows a saved human decision independently of whether the viewer
can submit one. Suggestions and the brief are rendered as text, separate from
confirmed incident fields. Staff can see ranked category scores, the eight
rubric indicators, an expandable AI draft brief, and approved playbook steps.
No indicator automatically changes severity. Historic signals may be shown
separately as legacy output.

## Development and checks

Use [local Docker](docker-setup.md) for the gateway and backends. For hot reload, use
Node `24.22.2` and npm `12.2.2`, then `make frontend-install` and `make frontend-dev`.
Open `http://127.0.0.1:5173/login`. Vite proxies `/api` and `/ai` to the local gateway
on `127.0.0.1:8088`; keep the exact loopback origin and do not put credentials in
frontend configuration.

`make frontend-check` runs static checks, unit/component tests, and a production
build. For browser checks install managed Firefox and Chromium from `frontend/` with
`npx playwright install firefox chromium`, then run `make frontend-test-e2e` from the
root. The default tests use synthetic API responses. Browser checks against real
packaged assets and services are described in the [local runbook](docker-setup.md#checks).
