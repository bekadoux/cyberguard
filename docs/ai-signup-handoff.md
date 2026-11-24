# CyberGuard — AI and frontend handoff

## Overview

The integration of PRs #14 and #18 brings together the AI assessment changes,
the employee registration UI, and the incident management frontend updates.

This document describes the implementation and records prior local verification,
not production release approval.

## 1. AI assessment

### Python backend

Updated the AI assessment models and schemas to use the eight indicators
defined by the current incident rubric.

The indicators cover:
- Link clicked
- Password or authentication code entered
- Attachment opened or file executed
- Payment or money transfer made
- Data sent outside the company
- Unusual device behavior
- Multiple employees or systems affected
- Activity still ongoing

Existing assessment results are immutable. The frontend supports both
the historical four-indicator rubric and the new eight-indicator rubric.

The historical meaning of `credentials_entered` must not be interpreted
as equivalent to the broader new authentication indicator.

### Frontend

The staff assessment view now provides:
- Explicit Kev recommendation or abstention
- Collapsible ranked category scores, with the top score separate from advice
- Eight current indicator slots and separately labelled legacy scores
- An expandable AI draft brief and separately sourced, approved playbook steps
- Human review and feedback: Accept shows the suggested category as text,
  Correct offers a category picker (labelled “Choose a category” after
  abstention), and Reject leaves the category unchanged
- A distinction between AI suggestions and human-confirmed decisions

Category scores and indicator scores are model outputs, not verified facts
or calibrated probabilities of harm.

AI results do not automatically change the incident category, severity,
or workflow status.

## 2. Incident management

### Reporter status history — Issue #16

The incident detail page includes a status timeline for the reporter,
including staff viewing their own reports.

The backend endpoint used by the timeline is:

`GET /api/incidents/{id}/timeline?limit=20`

The reporter-facing timeline shows paginated workflow status changes without
exposing the internal investigation audit trail.

### Staff AI assessment status — Issue #17

The staff incident queue includes an AI Assessment status column.

The incident list response provides `ai_assessment_status`, including
queued, running, completed, failed, or null where applicable.

The staff-only column is not shown in the employee incident queue.

The corresponding PostgreSQL permissions are supplied through
`db/migrations/incidents/00010_staff_assessment_status.sql`.

The Go endpoints and migration are already in `main`. Apply the migration and
deploy the updated Go API in the target environment before relying on these
frontend views.

## 3. Employee registration

Added the `/signup` frontend route and registration form.

The form uses the existing `POST /api/auth/signup` endpoint.

New accounts receive the employee role and must sign in separately
after registration.

The UI handles disabled registration, duplicate accounts, validation
errors, rate limits, and request failures.

The Caddy SPA route allowlist includes `/signup`, allowing direct navigation
and browser refreshes.

Registration remains controlled by `SIGNUP_ENABLED`. Keep registration
disabled unless explicitly approved for the target environment.

## 4. Local verification reported in PR #18 — 2026-11-24

The PR reported these checks on its development snapshot, not on the merge commit:

- Frontend TypeScript typecheck
- Frontend unit tests: 161 passed
- Frontend production build
- Frontend lint and formatting checks
- Chromium Playwright E2E tests: 88 passed
- Python backend tests: 58 passed
- Local AI VM smoke test on a Linux ext4 filesystem
- End-to-end employee incident submission
- End-to-end AI assessment with local Kev and Qwen inference
- Eight-indicator assessment rendering
- Staff incident queue with AI Assessment status
- Analyst claim, field update, and status transition
- Reporter-facing status history after analyst actions

Firefox E2E validation was not completed because the local system lacked
required Firefox browser dependencies.

The local AI VM smoke test validates infrastructure behavior but is not
a substitute for real-model inference testing. A separate synthetic incident
was processed successfully using the running Kev and Qwen services.

## 5. Important deployment notes

### Database migrations

Apply all required PostgreSQL migrations before validating the updated Go API.

During local integration testing, the incident database was still at
Goose migration version 9. The staff incident queue returned HTTP 503
because `go_incidents` lacked the required assessment-job read permission.

After applying migration 10, the staff incident queue loaded correctly.

Do not assume that a Docker resume operation applies new migrations.

### Docker images

Rebuilding an image does not necessarily recreate an existing container.

Verify that running AI API and Worker container image IDs match the
newly built AI image.

During local testing, the existing AI containers had to be recreated
before they used the updated Python implementation.

Preserve PostgreSQL volumes, model caches, and secrets during updates.

### Safe AI rollout order

Deploy the backward-compatible frontend reader before enabling the
eight-indicator Python writer in production.

Existing open browser tabs may still run older frontend JavaScript.
Plan for client reloads and use a staged rollout where necessary.

See `docs/frontend-handoff/ai-assessment-compatibility.md`.

## 6. Known limitations and follow-up work

- The Kev category suggestion threshold of 0.60 is provisional.
- Indicator scores need evaluation and calibration before stronger
  interpretations or automated decisions can be considered.
- AI-generated unknowns and suggested actions require human review.
- The local inference test does not establish Azure production readiness.
- A previously failed local synthetic assessment was left unchanged;
  a new assessment completed successfully after updating the AI runtime.
- Firefox, mobile accessibility, and cross-browser verification remain
  outside the completed local E2E scope.
- Confirm that `SIGNUP_ENABLED=false` is effective in the running Go
  configuration before leaving a shared or public environment running.

## 7. Handoff checklist

Before merge:
- Review the full Git diff and confirm the intended scope.
- Re-run the relevant automated tests on the final commit.
- Check that no local secrets or generated artifacts are staged.
- Verify migration 10 in the target environment.
- Review the staged rollout plan with the Go, frontend, and AI owners.
- Confirm that employee-facing views do not expose staff-only data.
- Record any outstanding issues in the Pull Request description.

This handoff records verified local behavior. It is not a production
deployment sign-off or a completed model-quality evaluation.
