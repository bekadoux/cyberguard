# Frontend — Authentication Increment

Implemented: React + TypeScript + Vite + React Router, responsive login, session
restoration, an authenticated account-confirmation page, logout, and System/Light/Dark
themes. Firefox is the primary test browser; the same browser suite also runs on Chromium.

This increment does **not** implement incident screens, account administration, password
recovery, notifications or AI. The account page explicitly describes that boundary.
The existing Go incident API remains available independently. No assessment job is
created by the current backend; see [incident handoff](incidents-handoff.md).

## Prerequisites and startup

- Node **24.22.2** (recorded in `frontend/.node-version`) and npm **12.2.2**. Package
  engines restrict development to the supported Node 24/npm 12 release lines.
- Existing Caddy/Go/two-PostgreSQL stack. Follow the explicit fresh setup sequence in
  [Docker setup](docker-setup.md#fresh-local-environment), including migrations and,
  for a local demonstration, the optional four-account seed. Neither frontend nor API
  startup runs bootstrap, migrations or seeding.
- A modern browser. Playwright uses its own matching Firefox/Chromium builds for tests.

From the repository root:

```sh
make frontend-install
make frontend-dev
```

Open **http://127.0.0.1:5173/login**. Vite binds loopback and requires port 5173 instead
of silently selecting another port: Go trusts that exact frontend origin. Use
`127.0.0.1` consistently, not a mixture of IP and localhost names.

The browser uses relative `/api/...` URLs. Vite proxies that prefix unchanged to Caddy
at `http://127.0.0.1:8088`; Caddy removes `/api` before forwarding to Go. Browser
credentials, Host/Origin policy and root-path HttpOnly cookies are preserved. No
frontend environment variables, database credentials or service tokens are needed.

The explicit demo usernames are `employee_a`, `employee_b`, `analyst` and `admin`.
With generated Docker secrets their respective passwords are in protected files
`.local/docker-secrets/dev_employee_a_password`, `dev_employee_b_password`,
`dev_analyst_password` and `dev_admin_password`. Read them privately when needed;
never commit them, paste them into frontend configuration or log them. Repeating the
seed does not reset existing passwords. Do not seed these accounts in production.

This increment is **host-run Vite development**, not public deployment. Caddy still
returns 404 for frontend paths: `http://127.0.0.1:8088/login` is not the UI yet.
`npm run build` produces static `frontend/dist/`. The later packaging step must copy
those files into a separate Caddy runtime image and add SPA fallback after API/internal
path handling. Do not serve the frontend through Go or add a permanent Node backend;
see [containerization handoff](containerization-handoff.md#caddy-and-the-future-frontend).
`npm run preview` is a local asset preview, not the documented API-integrated server.

## Structure and extension points

```text
frontend/
  src/app/                 Router and application-wide providers
  src/features/auth/       Auth API DTOs, session store, login and account screens
  src/shared/api/          Credentialed, bounded JSON requests and safe API errors
  src/shared/theme/        System/light/dark preference and theme provider
  src/shared/ui/           Shared brand and theme control
  src/styles/              Semantic color tokens and shared UI primitives
  tests/e2e/               Browser UI/failure tests and separate real-backend auth test
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

Future protected feature requests must handle their own 401 by refreshing/invalidating
the session, without letting an obsolete response clear a newer identity. Do not
assume a successful `/auth/me` authorizes any particular incident.

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

`frontend-check` includes type checking, ESLint, Prettier, unit/component tests and a
production build. Browser tests are separate; they are not implied by a passing build.

The lockfile pins dependencies. A temporary `minimatch` 11.0.5 override addresses
GHSA-g3hv-75wg-m249 in the ESLint/TypeScript lint tooling's transitive 10.x dependency.
Remove the override once those packages select a patched version themselves. The
lint/type/test/build checks exercise this combination; `npm audit` checks all dependencies.

The default browser suite intercepts auth endpoints **in tests only** to deterministically
exercise form validation, success/failures, duplicate-submit prevention, cooldowns,
session restoration/expiry/outages, failed logout, themes and narrow layouts. Axe scans
both themes; automated accessibility checks are not a substitute for manual review.
Unit tests additionally exercise response-ordering races and API/validation boundaries.

On the current Arch host, Playwright downloads Ubuntu fallback browser builds. Firefox
159 crashes inside axe after changing a native select's selected option. Accessibility
tests therefore exercise both palettes via the system color-scheme preference, without
changing the select before scanning. Separate browser tests exercise the theme control,
persistence and tab synchronization. No accessibility rules or browser optimizers are
disabled. Recheck this scanner limitation when upgrading Playwright/browser builds.

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

### Validation evidence — 9 November 2026

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

This is local development/browser evidence, not production HTTPS/Caddy-static, AI,
incident-screen or manual accessibility acceptance. No Go/SQL behavior was changed;
the separate full Go/PostgreSQL integration suite was not rerun for this UI increment.
