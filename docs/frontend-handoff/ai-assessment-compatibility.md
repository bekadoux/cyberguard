# AI assessment rubric compatibility and rollout

The persisted assessment schema remains `1.0` for both the original four-indicator
rubric and the eight-indicator domain rubric. The reader distinguishes the rubrics
by their exclusive indicator keys and rejects mixed rubrics. If only
`credentials_entered` is present, provenance is ambiguous; it is interpreted as
legacy rather than being presented with a potentially false new meaning.

The historical `credentials_entered` question concerns credentials entered into a
**suspicious form**. The new question concerns **any password or authentication
code entered**. Old scores must remain under the historical label; the new
question is shown as **Not scored** on legacy assessments. Stored outputs remain
advisory and are never merged into staff-confirmed incident fields.

## Deploy safely

1. Deploy the backwards-compatible frontend reader first and verify archived and
   eight-indicator fixture results. Rebuild/redeploy the packaged web image. Allow
   old open browser tabs to reload; an existing old JavaScript bundle does *not*
   become compatible merely because the server deploys a new bundle.
2. Only after all clients use a compatible reader, enable/deploy the Python writer
   with the eight domain-rubric indicators and the updated AI-VM fixture. Rollback
   the writer first if old clients cannot be guaranteed compatible.
3. The incident list/status timeline UI requires the Go endpoints and incident
   migration `00010_staff_assessment_status.sql` already deployed.

The combined source patch contains both steps for development and testing; it
**does not imply** they are safe to deploy simultaneously in production.

## Integration verification (2026-11-24)

The combined frontend and Python changes were tested in the local
CyberGuard full-stack Docker environment.

### Assessment compatibility

- The frontend supports legacy four-indicator assessments.
- The updated Python writer produces the eight-indicator rubric.
- A new synthetic incident completed real Kev and Qwen inference.
- All eight indicators were rendered successfully.
- The AI-generated draft brief was displayed.
- AI results did not overwrite human-confirmed incident fields.

Legacy compatibility is covered by automated tests. The manual
real-model test exercised the new eight-indicator format.

### Staff and reporter integration

- Issue #16: Reporter status history passed local E2E testing.
- Issue #17: Staff AI Assessment status passed local E2E testing.
- The employee incident queue did not expose the staff-only AI column.
- The reporter timeline did not expose the full staff audit history.

### Database migration requirement

The staff incident queue initially returned HTTP 503 because the
incident database was still at migration version 9.

Applying `00010_staff_assessment_status.sql` resolved the problem.

Deployments must apply the required database migrations before
validating the updated Go API.

### Docker deployment requirement

Rebuilding the Python image did not automatically replace the
running AI API and Worker containers.

The containers were recreated, and their image IDs were verified
against the newly built Python image.

A new synthetic incident subsequently completed AI Assessment.

An earlier `llm_unavailable_or_invalid` failure remains unexplained.
Successful processing of the new incident does not prove that the
earlier inference problem has been permanently resolved.

### Release considerations

The production rollout order remains:

1. Deploy the backward-compatible frontend reader.
2. Verify old and new assessment rendering.
3. Ensure existing browser sessions load the compatible frontend.
4. Deploy the eight-indicator Python writer.
5. Validate real inference and monitor processing failures.

The local E2E results do not replace production deployment checks
or model-quality evaluation.

See `docs/e2e-test-report.md` for the complete local test report.
