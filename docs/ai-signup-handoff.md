# AI and frontend change handoff (working snapshot)

This branch-ready working copy is based on the `cyberguard-for-review.zip`
snapshot supplied on 2026-11-23. It is not a model evaluation or a release sign-off.

## Implemented

- Kev prompts now ask the eight explicit-report indicators in
  `domain-specification.md`. Updated category wording keeps ransomware in
  `malware` and accidental external disclosures in `data_exposure`.
  No indicators change the human-confirmed severity.
- AI OpenAPI, documentation and frontend parsing support the rubric. Legacy
  signal codes remain readable for existing, immutable assessment rows.
- Staff AI view shows always-visible Kev advice or explicit abstention, the
  highest-scoring category **without** implying a recommendation, ranked
  collapsible category bars, eight indicator score cells, a collapsible AI brief,
  and separately sourced approved playbook steps.
- Human review shows the category as text for Accept, a picker for Correct,
  "Choose a category" when Kev abstains, and preserves Reject semantics.
- `/signup` uses the existing `POST /api/auth/signup` endpoint. The user is
  created as `employee` and must sign in separately. Disabled registration
  (HTTP 404), duplicates, rate limits, and network failures have safe messages.
  The Caddy SPA allowlist includes `/signup`.
- Login/registration and staff incident list received restrained visual
  improvements without changing authorization or incident API contracts.

## Explicitly not complete

- **No new Go endpoints** for reporter-safe timeline or the AI job status in
  the staff incident queue. These require an agreed Go/frontend API contract.
- **No production inference calibration**: category threshold `0.60` remains
  provisional; there is no evaluated, reliable indicator yes/no threshold.
  The interface shows model scores, not asserted facts or severity updates.
- **No concept-image matching**: the referenced sign-in/sign-up concept images
  were not included in the supplied archive.
- **No change to registration policy**: `SIGNUP_ENABLED` defaults to disabled
  according to the Go configuration. Do not enable it on public deployments
  without team approval.
- No integration acceptance on real Azure/real Kev/Qwen or browser screenshots.

## Verification commands

From repository root with Python 3.13 and `requirements-dev.txt` installed:

```bash
PYTHONPATH=backend/python python -m pytest -q backend/python/tests
```

From `frontend/` using the **project's pinned Node 24.22.2 and npm 12.2.2**:

```bash
npm ci
npm run check
npx playwright install firefox chromium
npm run test:e2e -- --grep 'assessment|employee|registration'
```

For browser development, use the local gateway described in
`docs/docker-setup.md` and `make frontend-dev`. Signup creates no session.

Before merging: review new UI copy and styling, run full tests and accessibility
checks on both desktop and mobile, and have the Go/frontend owners agree on
separate timeline and queue contracts. Evaluate bilingual model scores before
signing off on indicator thresholds.
