# CyberGuard — Integration and E2E Test Report

**Date:** 2026-11-24
**Branch:** `fix/ai-assessment-compatibility`
**Environment:** Local Docker Compose, WSL Linux, Chromium
**Python test environment:** Python 3.14; 58 tests passed with one dependency deprecation warning.

## 1. Overview

This document summarizes the integration and end-to-end testing
performed for the CyberGuard AI and frontend changes.

The main goal was to verify that employee reporting, AI assessment,
analyst workflows, and reporter-facing status history work together.

All manually submitted test incidents used synthetic information.

These results describe local development testing, not production
deployment approval.

## 2. Automated tests

| Test | Result | Details |
|---|---|---|
| Frontend TypeScript | PASS | No type errors |
| Frontend unit tests | PASS | 161 tests |
| Frontend production build | PASS | Build completed |
| Frontend lint | PASS | No errors |
| Frontend formatting | PASS | Prettier check |
| Playwright Chromium | PASS | 88 tests |
| Python backend tests | PASS | 58 tests |
| AI VM smoke test | PASS | Linux ext4 |
| Playwright Firefox | NOT VERIFIED | Missing system dependencies |

The AI VM smoke test validates infrastructure behavior with synthetic
fixtures. It does not replace real-model inference testing.

Automated tests should be rerun before the final merge.

## 3. Employee registration

**Result: PASS**

Verified:
- Registration through `/signup`.
- Creation of an employee account.
- Successful login after registration.
- Access to the employee incident queue.
- Direct navigation to `/signup` through Caddy.
- Registration controlled by `SIGNUP_ENABLED`.

Registration was enabled temporarily for local testing.

It must remain disabled unless the deployment owner explicitly
approves public self-registration.

## 4. Incident submission

**Result: PASS**

An employee submitted a synthetic phishing incident.

Verified:
- Incident creation through the frontend.
- Persistence in PostgreSQL.
- Correct initial status: Submitted.
- Initial reporter-facing status history.
- Access to the submitted incident through My incidents.

## 5. Staff AI Assessment Status — Issue #17

**Result: PASS**

Verified:
- Staff incident queue loads successfully.
- AI Assessment column appears for staff.
- Completed and Failed statuses are displayed.
- Employee queue does not expose the staff-only AI status column.

### Problem discovered

Initially, the staff queue returned:

HTTP 503 Service Unavailable

The incident database was using Goose migration version 9.

The `go_incidents` database role lacked permission to read the
required assessment-job status data.

### Resolution

Applied the pending incident migration:

`db/migrations/incidents/00010_staff_assessment_status.sql`

After migration 10 was applied, the staff queue loaded correctly.

### Lesson for deployment

Updating the Go API image does not guarantee that PostgreSQL
migrations have been applied.

Always validate the migration state when deploying API changes.

## 6. AI assessment with Kev and Qwen

**Result: PASS for the new synthetic incident**

Test incident:

`59311932-bc17-4e40-abcf-efc0f78714ed`

Title:

`E2E Test - Suspicious phishing email`

The updated AI Worker successfully processed the new incident.

Verified:
- AI Assessment reached Completed.
- Kev generated eight category scores.
- The eight-indicator assessment was displayed.
- Qwen generated an AI draft brief.
- The staff frontend displayed the AI results.
- AI suggestions did not automatically update confirmed fields.

### Kev classification

The highest-scoring category was:

Phishing / social engineering — approximately 56.5%.

The configured recommendation threshold was 60%.

The interface correctly displayed No recommendation.

A high-scoring category is not necessarily an accepted
recommendation or a human-confirmed incident category.

### AI indicators

Selected scores observed during testing:

| Indicator | Score |
|---|---:|
| Link clicked | 82.4% |
| Password or authentication code entered | 85.5% |
| Attachment opened or file executed | 36.4% |
| Payment or money transfer made | 12.6% |

All eight indicators were displayed in the interface.

These scores are AI model outputs and must not be treated as
verified facts or calibrated probabilities of harm.

### Previous inference failure

An earlier synthetic assessment failed with:

`llm_unavailable_or_invalid`

The Python AI image was rebuilt, and the AI API and Worker
containers were recreated using the updated image.

A newly submitted synthetic incident then completed successfully.

This confirms successful inference for the new incident.
It does not establish the definitive root cause of the earlier failure.

## 7. Analyst workflow

**Result: PASS**

Verified:
- Analyst claimed the incident.
- Category was confirmed as Phishing / social engineering.
- Severity was confirmed as Medium.
- Confirmed fields were saved.
- Investigation was started successfully.
- Incident status changed to Investigating.

### Workflow prerequisite

The initial status transition returned HTTP 409 Conflict.

The application required Category and Severity to be confirmed
before starting the investigation.

After these fields were saved, the transition succeeded.

The internal audit history recorded:

- submitted
- claimed
- fields updated
- status changed

This verifies the tested workflow and its prerequisite validation.

## 8. Reporter Status History — Issue #16

**Result: PASS**

After the analyst updated the incident, the employee signed in
and reopened the same report.

The reporter-facing status history contained:

1. Submitted
2. Triage
3. Investigating

The newest status was displayed first.

Verified:
- Status history reflects analyst workflow transitions.
- Reporter incident list shows the updated status.
- Internal audit details are not exposed in the reporter timeline.
- Staff-only AI Assessment status is absent from the employee list.

This confirms the tested role-specific behavior.
It is not a complete authorization security audit.

## 9. Docker deployment observations

### Application images

Rebuilding an image does not necessarily recreate running containers.

During testing, the AI Worker initially continued using the old image.

The old AI API and Worker containers were stopped and removed.
The full local setup was resumed afterward.

The running AI API and Worker image IDs were then verified
against the newly built image.

### Model availability

The local Kev and Qwen containers reported Healthy.

The successful synthetic assessment additionally confirmed that
the running AI pipeline could complete real-model inference.

### PostgreSQL

Existing data and volumes were preserved while applying
the missing migration and recreating AI containers.

Do not delete database volumes when updating the application.

## 10. Commands for reproducing automated tests

Run frontend tests from the `frontend` directory:

    npm run typecheck
    npm test -- --run
    npm run build
    npm run lint
    npm run format:check
    npx playwright test --project=chromium

Run Python backend tests from `backend/python`:

    .venv/bin/python -m pytest tests -q

Run the AI VM smoke test from the repository root:

    make ai-vm-smoke

The smoke test requires a filesystem that supports Unix
permissions. The Windows-mounted DrvFs workspace could not
provide the required secret-directory permissions.

The smoke test passed from a Linux ext4 working copy.

Check Git whitespace errors from the repository root:

    git diff --check

## 11. Known limitations

- The 60% Kev recommendation threshold is provisional.
- Indicator scores need further evaluation and calibration.
- LLM-generated summaries and unknowns require human review.
- Firefox E2E tests were not successfully completed locally.
- Production Azure deployment was not validated.
- Accessibility and mobile verification remain to be completed.
- The previous LLM failure requires further investigation.
- Historical four-indicator compatibility should be verified
  again against the final build.
- Registration configuration should be checked in the running
  Go API before deployment.

## 12. Pre-merge checklist

- Review the complete Git diff.
- Re-run automated tests on the final changeset.
- Verify required PostgreSQL migrations.
- Confirm the staged frontend-before-writer rollout order.
- Ensure no secrets or generated files are staged.
- Verify the running Docker images after deployment.
- Review role-specific API and frontend visibility.
- Confirm registration policy for the target environment.
- Document any outstanding issues in the Pull Request.

## 13. Conclusion

The primary local integration flow was successfully tested:

Employee
-> Go API
-> PostgreSQL
-> AI Worker
-> Kev and Qwen
-> Staff AI Assessment
-> Analyst investigation
-> Reporter Status History

The tested scenarios for Issues #16 and #17 passed.

The result demonstrates successful local integration,
not production readiness or model-quality certification.
