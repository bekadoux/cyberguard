-- +goose Up
-- The authenticated internal Go AI-context endpoint joins assessment_jobs to
-- immutable incident_contexts via context_id. Grant only this new column;
-- previous migrations already grant id, incident_id, incident_revision.
-- No write permissions or job-processing metadata are exposed.
GRANT SELECT (context_id) ON assessment_jobs TO go_incidents;

-- +goose Down
REVOKE SELECT (context_id) ON assessment_jobs FROM go_incidents;
