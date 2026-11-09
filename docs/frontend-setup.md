# Frontend — Authentication and Incident Workspace

Implemented: React + TypeScript + Vite + React Router, responsive login, session
restoration, an authenticated account-confirmation page, logout, and System/Light/Dark
themes, plus a protected workspace, incident submission and role-scoped list/detail
with status/category filters and cursor pagination. Firefox is the primary test browser;
the same browser suite also runs on Chromium. Sign-in now lands on `/incidents`;
`/account` retains the account information screen.
Eligible staff can also read stored AI assessment states and results on another
person's incident when the optional AI service is enabled.

This increment does **not** implement investigation mutations, comments, account
administration, password recovery, notifications, model execution in the browser or
assessment retry mutations. The merged AI migration queues an initial job on new
incident inserts; processing requires the optional AI services. See
[AI backend setup](ai-backend.md) for candidate-contract and real-inference boundaries.

## Prerequisites and startup

- For host development/checks: Node **24.22.2** (recorded in `frontend/.node-version`)
  and npm **12.2.2**. Package engines restrict development to Node 24/npm 12.
  Docker packaging supplies its own pinned build toolchain; running the packaged UI
  does not require host Node/npm or Vite.
- Existing Caddy/Go/two-PostgreSQL stack. Follow the explicit fresh setup sequence in
  [Docker setup](docker-setup.md#fresh-local-environment), including migrations and,
  for a local demonstration, the optional four-account seed. Neither frontend nor API
  startup runs bootstrap, migrations or seeding.
- A modern browser. Playwright uses its own matching Firefox/Chromium builds for tests.

### Packaged local application

Follow [Docker setup](docker-setup.md#fresh-local-environment) for a fresh environment.
For existing provisioned databases/secrets, from the repository root:

```sh
export DOCKER_SECRET_GID=$(id -g)
make docker-build-web docker-caddy-check docker-up
```

Open **http://127.0.0.1:8088/login**. The separate `cyberguard-web:local` image contains
only the built static frontend and Caddy runtime/config. It runs non-root/read-only,
without Node, Vite, secret mounts or source/config bind mounts. Relative `/api` and
`/ai` calls are same-origin; existing cookie/CSRF policy is unchanged. Build-time inputs
exclude host `.env` files and Git-local LAN overrides. Rebuild after frontend/Caddyfile changes;
the running container does not watch source. No database setup occurs during startup.

### Vite hot-reload development

From the repository root:

```sh
make frontend-install
make frontend-dev
```

Open **http://127.0.0.1:5173/login**. Vite binds loopback and requires port 5173 instead
of silently selecting another port: Go trusts that exact frontend origin. Use
`127.0.0.1` consistently, not a mixture of IP and localhost names.

The browser uses relative `/api/...` and `/ai/...` URLs. Vite proxies both prefixes
unchanged to Caddy at `http://127.0.0.1:8088`; Caddy removes `/api` before forwarding
to Go and, with the optional AI overlay, removes `/ai` before forwarding to Python.
Browser credentials, Host/Origin policy and root-path HttpOnly cookies are preserved. No
frontend environment variables, database credentials or service tokens are needed.

The explicit demo usernames are `employee_a`, `employee_b`, `analyst` and `admin`.
With generated Docker secrets their respective passwords are in protected files
`.local/docker-secrets/dev_employee_a_password`, `dev_employee_b_password`,
`dev_analyst_password` and `dev_admin_password`. Read them privately when needed;
never commit them, paste them into frontend configuration or log them. Repeating the
seed does not reset existing passwords. Do not seed these accounts in production.

Both workflows are local HTTP development, **not public deployment**. Caddy now serves
the real build with SPA fallback for implemented client routes, after API/internal/AI
handling. Missing assets/source/config paths do not become HTML 200 responses. HTML
and unfingerprinted public files are no-store; existing fingerprinted assets are
immutable. See [gateway contract](docker-setup.md#gateway-contract). Do not serve the
frontend through Go or add a permanent Node backend.
`npm run preview` is a local asset preview, not the documented API-integrated server.

## Structure and extension points

```text
frontend/
  src/app/                 Router and application-wide providers
  src/features/auth/       Auth API DTOs, session store, login and account screens
  src/features/incidents/  Safe DTOs, submission, scoped list/detail and pagination
  src/shared/api/          Credentialed, bounded JSON requests and safe API errors
  src/shared/theme/        System/light/dark preference and theme provider
  src/shared/ui/           Shared brand and theme control
  src/styles/              Semantic color tokens and shared UI primitives
  tests/e2e/               Browser UI/failure tests and opt-in real-backend suites
```

Add later domain screens under `src/features/<feature>/` and mount protected routes
under `RequireSession` in `src/app/App.tsx`. Reuse the request helper and theme tokens;
keep incident DTOs and endpoints in that feature rather than growing the auth module.
Do not create placeholder implementations or enable actions before their backend
contracts exist. Client routing/role labels are UX only: Go always authorizes requests.

### Authentication safeguards

- All requests use `credentials: 'include'` and `cache: 'no-store'`. No token is read
  by JavaScript; user identity remains in memory, not localStorage/sessionStorage.
- `/auth/me` restores identity and revalidates it on window focus/page visibility.
  No background keepalive prevents the backend's idle timeout.
- The session store rejects overlapping login/logout operations in this application
  instance. A generation counter and abortable reads discard obsolete `/me` responses
  after an authentication change. Cross-tab mutation ordering is not promised; that
  needs a separate protocol if required. Focus revalidation discovers changes elsewhere.
- A protected 401 clears identity. A 503/network error gates protected content but
  retains identity as unconfirmed, rather than claiming logout. A failed logout shows
  a retryable warning and never claims the server session was revoked. The session-check
  outage screen still offers sign-out, even when identity could not be restored; a
  failing current-user query does not necessarily prevent session deletion. Sign-out
  remains available while rechecking a previously confirmed identity.
- Login passwords are never trimmed, logged or persisted. Browser validation enforces
  nonempty inputs and the backend's raw UTF-8 byte limits; Go alone applies username
  trimming, lowercasing and the normalized ASCII policy. JavaScript's Unicode rules
  differ (for example, Turkish dotted `İ`), so the browser submits usernames unchanged.
  Failed submissions clear the password. Only explicit user submission causes a login
  mutation: no automatic retry.
- Safe messages distinguish invalid credentials, CSRF/policy rejection, throttling,
  capacity, dependency and network errors. `Retry-After` disables resubmission until
  its deadline. Raw backend error messages are never rendered.

Protected feature requests use `runProtected` from the session store. Current-request
401 clears identity; obsolete responses/denials cannot replace a newer identity.
Identity/role changes remount the protected workspace to discard previous account data.
An incident dependency 503 remains a retryable feature error, not proof of logout.
Do not assume a successful `/auth/me` authorizes any particular incident.

### Incident list contract

Employees see their own reports; analysts/admins see the company queue. The UI uses
only `status`, `category`, fixed `limit=20` and opaque continuation cursors. Changing
filters clears old results and restarts pagination; obsolete reads are aborted/ignored.
Unknown/duplicate/empty URL filters are rejected visibly, not silently widened. Failed
pagination preserves loaded rows and permits retry. Counts describe loaded rows, not
global totals. No search, severity filtering, ordering controls or staff-only-own filter
are implied. Severity is never displayed on the reporter's own rows, including staff.

### Incident detail contract

`/incidents/:id` renders original text, affected system, reporter-facing resolution,
category, analyst identity and timestamps. Null values are explicit, not invented
assessments or assignment. Analyst accounts missing from identity retain their UUID and
an unavailable-account label. Severity is shown only to staff reading another person's
report and only when returned by Go. Missing, malformed, non-visible and archived IDs
share one concealed not-found view. No partial report is shown on dependency failure.
Descriptions/resolutions are plain React text with whitespace preserved, never HTML.
Back navigation retains validated list filters without forwarding them to the detail API.

### Incident submission contract

`/incidents/new` accepts only title, description and optional affected system. Required
text must contain a non-whitespace character using Go-compatible Unicode whitespace
rules. Limits are **200**, **10,000** and **500** Unicode code points, not UTF-16 units
or visual grapheme clusters. NUL and unpaired surrogates are rejected. Original text
is not trimmed or normalized; an empty optional field is omitted, while Go maps
whitespace-only affected systems to null.

Input stays in component memory, never browser storage, URLs or logs. Leaving/reloading
the page or losing the confirmed session discards unsaved text. A synchronous lock
prevents overlapping POSTs; input is disabled while sending. Only HTTP **201** with a
validated incident DTO confirms success and opens the saved record. Leaving the form
before completion prevents late navigation, but cannot undo a committed submission.

Definitive 400/403/413/415 rejections retain fields and show safe errors. Network,
timeout, 5xx and malformed/unexpected success results are ambiguous: no automatic
retry, and resubmission requires explicit confirmation after checking incidents in a
separate tab. The API has no idempotency-key contract, so exactly-once submission
across tabs/reloads/network loss is not promised. A current 401 expires the session;
obsolete results cannot affect a newer identity. New reports are unclassified and
unassigned. The merged AI migration queues new reports even without a worker;
submission guidance does not promise that AI processing is running.

### Stored AI assessment contract

Analyst/admin detail views of another person's report request
`/ai/incidents/:id/assessment` with the existing HttpOnly session. Employees and staff
reporters never mount/fetch this panel; Python independently enforces the same ownership
rule. The `/api` and `/ai` clients share cookie, no-store, timeout, cancellation and
session-generation safeguards, but retain distinct path prefixes.

Queued/running/completed/failed job states remain separate from incident business
status. Refresh is explicit: no background loop keeps sessions alive or starts inference.
Missing jobs (including older reports), forbidden access and disabled/unavailable AI
show assessment-only messages without hiding the incident. A current 401 expires the
session; obsolete responses cannot populate a newer identity's screen.

Completed results require validated job/incident pairing, schema, timestamps and
consistent `is_fake`/`source_kind` provenance. Fake output is prominently labelled;
all summaries, facts, unknowns, questions and steps render as literal text, never HTML
or executable instructions. Suggestions never overwrite confirmed incident fields.
Model impact/probabilities are labelled unconfirmed and not calibrated risk scores.
Terminal failures offer status refresh only, not an invented requeue mutation.

### Themes and accessibility

The theme defaults to the system preference, supports explicit light/dark, and stores
only `cyberguard.theme`. Semantic CSS variables define surfaces, text, borders and
status colors. A small external head script applies the theme before first paint.
Blocked storage falls back safely; system-theme changes and theme changes in other
tabs are observed. There are no external fonts, gradients or expensive visual effects.
Keyboard focus, form labels/error associations, password visibility, Caps Lock hints,
reduced-motion support and small-screen layout are included.

Dark surfaces use neutral black/charcoal rather than blue-tinted backgrounds; teal
remains the action/focus accent. On narrow screens the brand and theme control share
one header. Shorter viewports use tighter spacing and omit the decorative login icon,
without shrinking the form controls. Mobile inputs use 16px text to avoid iOS focus zoom.
The normal login fits the tested 1024×600 desktop, 320×568 phone and 375×548 reduced
phone viewports without either scrollbar. Very short viewports, keyboard/zoom and
validation messages can still grow the page naturally: no content is clipped or
scrolling disabled to force a fit.

## Checks

```sh
make frontend-check
# From frontend/; repeat after Playwright upgrades:
npx playwright install firefox chromium
# On supported Linux distributions, install missing libraries if requested:
# npx playwright install-deps firefox chromium
```

Then, from the repository root:

```sh
make frontend-test-e2e
# Existing explicitly seeded local demo backend must be running:
E2E_PASSWORD_FILE="$PWD/.local/docker-secrets/dev_employee_a_password" \
  make frontend-test-e2e-live
```

For a freshly provisioned packaged fixture, with no Vite process required:

```sh
DOCKER_TEST_FRONTEND=1 make docker-smoke
# Add the real AI API/worker with synthetic, explicitly labelled output:
DOCKER_TEST_AI=1 DOCKER_TEST_FRONTEND=1 make docker-smoke
```

This runs all default browser regressions against compiled assets, followed by real
same-origin packaged flows without API interception. The runner builds/provisions and
cleans up only its disposable project. Host Node/npm, locked dependencies and managed
Firefox/Chromium must already be installed for this optional browser flag.
The AI flag provisions a fresh restricted AI role and runs the actual queue/worker/API
flow in `AI_MODE=fake`; it does not download models or validate real-model accuracy.
Alternatively, keep a smoke fixture and target its exact nonstandard loopback origin:

```sh
E2E_PACKAGED_ORIGIN=http://127.0.0.1:18088 \
E2E_INCIDENT_SECRETS_DIR="$PWD/.local/PROJECT/secrets" \
  make frontend-test-packaged
```

The packaged-origin override is restricted to disposable loopback ports and disables
Playwright's Vite startup. `frontend-test-packaged` verifies the built shell and runs
credential-safe create/read/isolation checks through actual Caddy requests, including
deep-link reloads, HttpOnly cookie policy and revoked-cookie rejection. Its write
helper requires the UI and API to share exactly that origin, not a route.fetch bridge.

Incident write/isolation checks require a **separate disposable** backend; do not run
them against ordinary development data. With Vite running, prepare a fixture and use
the exact project name printed by the smoke command:

```sh
DOCKER_KEEP_TEST_ENV=1 make docker-smoke
# Replace PROJECT with the printed cyberguard-smoke-<number> name:
E2E_INCIDENT_API_ORIGIN=http://127.0.0.1:18088 \
E2E_INCIDENT_SECRETS_DIR="$PWD/.local/PROJECT/secrets" \
  make frontend-test-incidents-live
```

The helper accepts only an explicit nonstandard loopback HTTP API port and a secrets
directory under a `cyberguard-smoke-*` or `cyberguard-ui-*` fixture. It forwards only
browser `/api/` requests to that gateway; UI assets still come from Vite. Passwords
are read only inside the discarded-output subprocess. Each browser submits synthetic
Unicode text, verifies reload/list persistence, checks another employee's concealed
404 and an analyst's professional projection, then signs out. It leaves synthetic
reports in that fixture and does not modify account definitions. Three logins per
browser can hit the unchanged five-per-minute peer limit; one throttled login may
retry after `Retry-After`. Incident POSTs are never retried automatically.

Afterward, remove **only that fixture's** containers/networks/volumes, not the ordinary
`cyberguard` project. Retained secret files remain sensitive and Git-ignored:

```sh
COMPOSE_PROJECT_NAME=PROJECT \
DOCKER_SECRETS_DIR="$PWD/.local/PROJECT/secrets" DOCKER_SECRET_GID=$(id -g) \
  docker compose -f compose.yaml -f compose.dev.yaml down --volumes
```

`frontend-check` includes type checking, ESLint, Prettier, unit/component tests and a
production build. Browser tests are separate; they are not implied by a passing build.

The lockfile pins dependencies. A temporary `minimatch` 11.0.5 override addresses
GHSA-g3hv-75wg-m249 in the ESLint/TypeScript lint tooling's transitive 10.x dependency.
Remove the override once those packages select a patched version themselves. The
lint/type/test/build checks exercise this combination; `npm audit` checks all dependencies.

The default browser suite intercepts auth and incident endpoints **in tests only** to
deterministically exercise validation, success/failures, duplicate-submit prevention,
cooldowns, session restoration/expiry/outages, failed logout, themes and narrow layouts.
Axe scans both themes; automated accessibility checks are not a substitute for manual review.
Unit tests additionally exercise response-ordering races and API/validation boundaries.

On the current Arch host, Playwright downloads Ubuntu fallback browser builds. Firefox
159 crashes inside axe after changing a native select's selected option. Accessibility
tests therefore exercise both palettes via the system color-scheme preference, without
changing the select before scanning. Separate browser tests exercise the theme control,
persistence and tab synchronization. No accessibility rules or browser optimizers are
disabled. Recheck this scanner limitation when upgrading Playwright/browser builds.

Workspace axe scans additionally run in a standalone Node/browser process: Firefox's
fallback build crashes during native-widget evaluation under the main test runner's
instrumentation. The standalone scan uses the same synthetic fixtures, both palettes,
desktop/mobile viewports and all WCAG A/AA rules, without altering browser optimizers.
Normal interaction and layout assertions remain in the main Firefox/Chromium suite.
Additional standalone scans of inline validation and ambiguous-result confirmation
pass in Chromium in both palettes at 320px. Firefox scans the inline validation state,
but its fallback build still crashes during the ambiguous-result scan, where a native
confirmation checkbox is dynamically shown. That extra scan is not claimed as passing;
the confirmation controls' interaction regressions pass in both browsers.

The explicit live suite uses real Go/Caddy/PostgreSQL and verifies login, cookie flags,
reload persistence, no browser-stored identity/token and server-side logout. It requires
`E2E_PASSWORD_FILE` (read only by a standalone browser-check subprocess, never the test
reporter or Vite); `E2E_USERNAME` optionally selects the matching demo account.
It does not bootstrap or mutate account definitions.
It uses one login per browser, below the existing five-per-minute per-peer limit if
run alone. Repeated runs may require waiting for the limit to reset; never weaken it
for a test. Unit/browser fixtures do not point Go's SQL integration suite at dev data.

Playwright traces, video, screenshots and saved browser state are disabled by default.
Those switches do not disable automatic ARIA failure snapshots or action-error logs.
The live suite therefore keeps all credential/cookie-bearing browser work in a standalone
subprocess with discarded stdout/stderr; only fixed checkpoint names reach the reporter.
The normal browser suite verifies disabled-field and stalled-login failures with synthetic
passwords. Test artifacts are Git-ignored; still treat failure reports as potentially
sensitive. Prefer screenshots of the logged-out screen with empty fields when checking
appearance. Do not publish real credentials.

### Authentication increment evidence — 9 November 2026

- `make frontend-check`: type checking, lint, formatting, **47** unit/component tests
  and static production build passed.
- `make frontend-test-e2e`: **52** tests passed across managed Firefox 159 and
  Chromium 158, including both themes' full WCAG A/AA axe scans on desktop and mobile.
- Explicit real-backend suite: **4** checks passed: employee and Turkish dotted-I
  `ADMİN` sign-in in each browser. Each check replayed the pre-logout cookie and
  confirmed that Go rejects the revoked session.
- Credential-reporting follow-up: **4** disabled-field/stalled-login regression checks
  passed across Firefox/Chromium. An intentionally failing synthetic login in each
  browser produced list, JSON and HTML/error-context reports without the synthetic
  password.
- Session-outage follow-up: **6** component regressions and **2** browser regressions
  passed, covering sign-out without a confirmed identity, failed/duplicate logout,
  rechecking races and recovery after a canceled check. Protected content remains gated.
- Responsive/theme follow-up: both palettes fit **12** desktop/phone viewport sizes per
  browser without scrolling. Regressions also check neutral dark surfaces, readable
  mobile input sizes, touch targets, and reachable error/form/footer content when a very
  short viewport needs scrolling. The employee live auth check passed again in both browsers.
- `npm audit`: no known vulnerabilities reported, including development dependencies.
- Fresh local Docker setup, migration status and readiness passed; demo accounts were
  explicitly seeded. `make docker-test-secrets` and `make docker-smoke` passed after
  excluding frontend test artifacts from Docker contexts.

This section records the earlier authentication increment. The incident-screen evidence
is below. These are local development/browser checks, not production HTTPS/Caddy-static,
AI or full manual accessibility acceptance. No Go/SQL behavior was changed; the separate
full Go/PostgreSQL integration suite was not rerun for these UI increments.

### Incident workspace chunk 1 — 9 November 2026

`make frontend-check` passed (67 unit/component tests plus types, lint, formatting and
build); `make frontend-test-e2e` passed 74 Firefox/Chromium tests; the two explicit real
authentication checks passed with the new landing page. `npm audit` reported no known
vulnerabilities. Checks cover filters/cursors, obsolete responses, pagination retry,
own-report severity restrictions, identity changes, failed-logout read recovery,
mobile layout and full accessibility scans in both themes. No Go/SQL behavior changed.

### Incident detail chunk 2 — 9 November 2026

Frontend checks passed with 74 unit/component tests and the static build; all 92
Firefox/Chromium browser tests passed. Coverage includes reporter/staff projections,
unknown analyst identities, full resolution/original text, hostile literal markup,
concealed 404s, dependency recovery, stale-detail cancellation, filter-preserving back
navigation, long Unicode content, mobile reflow and both palettes' accessibility scans.

### Incident submission chunk 3 — 9 November 2026

- `make frontend-check` passed: types, lint, formatting, **92** unit/component tests
  and the production build.
- `make frontend-test-e2e` passed all **118** Firefox/Chromium tests, including the
  submission validation, repeated submit-event lock, retained rejection inputs,
  ambiguous-result review, session expiry, responsive layouts and pristine-form axe scans.
- `make frontend-test-incidents-live` passed in **both browsers** against a fresh
  disposable Caddy/Go/two-PostgreSQL project: real creation, Unicode/whitespace fidelity,
  reload/list persistence, employee isolation and staff visibility. The helper's ordinary
  development-port rejection was also verified before credential loading.
- The existing **two** real-auth checks passed again. `npm audit` found no known
  vulnerabilities; `git diff --check`, `make docker-test-secrets` and the fresh
  `make docker-smoke` passed. No Go/SQL application behavior changed.
- Synthetic desktop/mobile screenshots were reviewed in both palettes. Additional
  error-state axe results and the remaining Firefox scanner limitation are noted above.

Only the disposable project's containers, networks and volumes were removed afterward;
ordinary development services/data, ignored LAN configuration and protected secret
files were left intact. This remains incident core create/read, not acceptance of the
future atomic incident-and-assessment workflow or production deployment.

### Frontend/Caddy packaging — 9 November 2026

- `make frontend-check` passed with **92** unit/component tests, types, lint,
  formatting and production build; `npm audit` reported no known vulnerabilities.
- `DOCKER_TEST_FRONTEND=1 make docker-smoke` passed from fresh images/databases.
  All **118** Firefox/Chromium regressions passed against the compiled assets, plus
  **4** packaged-shell/real-flow tests through actual same-origin Caddy requests.
- `make frontend-test-e2e` separately passed all **118** checks using the unchanged
  Vite hot-reload workflow.
- HTTP checks passed for direct client routes and reloads, HEAD, existing asset MIME/
  immutable cache/ETag behavior, no-store HTML/bootstrap files, missing assets/source/
  config, internal namespaces, API errors and unsupported frontend methods.
- The full smoke lifecycle passed, including readiness, all-role API checks, secret
  isolation, persistence, SQL outages and a static shell that still loads while Go is
  stopped. The runtime is UID/GID 10001 and contains no Node/npm, source or source maps.
- Packaged write-helper safeguards rejected ordinary API ports and mismatched UI/API
  origins before loading credentials. Base Compose still has no published ports,
  insecure-cookie override, source mounts or web secrets.

The disposable project cleaned up its own containers/networks/volumes. Ordinary
development services/data and ignored LAN configuration were untouched. No Go/SQL
application code or auth policy changed; the separate Go race/integration suite was
not rerun for this packaging-only increment. Public HTTPS/Azure and the previously
documented extra Firefox error-state scanner limitation remain outside this acceptance.

### AI integration follow-up — 10 November 2026

The server-side reporter restriction was fixed on `main` first, then merged into
`feat/frontend`. All **23** Python tests and scoped Ruff/Pyright checks passed. Go's
unit/HTTP suite also passed after the merge; no Go/SQL files were changed in this
frontend follow-up.

`make frontend-check` passed **106** unit/component tests, types, lint, formatting and
the production build. All **146** Firefox/Chromium regressions passed in the Vite
workflow and against packaged assets. The new coverage includes real job states,
fake provenance, literal model text, unavailable/missing/forbidden assessments,
session expiry/isolation, no background polling, both palettes and mobile accessibility.

`DOCKER_TEST_AI=1 DOCKER_TEST_FRONTEND=1 make docker-smoke` passed the actual
PostgreSQL trigger/claim/worker/Go context/AI API/Caddy pipeline in fixture mode, plus
**4** packaged shell/live-browser tests. Native requests verified employee denial,
analyst/admin own-report denial, other-report staff access and completed fake provenance;
live browsers verified the explicit fixture banner and reporter restrictions. Shared
SPA/security/cache handling survived the AI overlay. A read-only Vite `/ai/healthz`
probe also reached the ordinary AI-disabled gateway as JSON 503, not Vite HTML.
The final `make docker-smoke` also passed separately with AI disabled, including the
original four-service lifecycle, unavailable AI response and shared gateway checks.

Dependency audit and secret-generation checks passed. Synthetic assessment screenshots
were reviewed at desktop/mobile widths. Ordinary services/data, protected credentials
and ignored LAN configuration were not changed; successful fixture resources were
removed by their owner script. Real model inference/evaluation, concurrent claim/lease
failure cases, the full Go race/integration/generated-query checks and production
deployment are not claimed by this follow-up. The existing upstream Python test
deprecation warning and extra Firefox error-state scanner limitation remain documented.
