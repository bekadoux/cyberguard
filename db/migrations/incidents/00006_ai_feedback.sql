-- +goose Up
-- The browser Go role may verify a result belongs to the incident and
-- inspect only the suggested category and source label. No worker writes.
GRANT SELECT (job_id, incident_id, source_kind, assessment) ON assessment_results TO go_incidents;
-- Prevent multiple decisions for the same assessment job on one incident.
CREATE UNIQUE INDEX incident_actions_one_ai_feedback_per_job
    ON incident_actions (incident_id, ((details->>'job_id')))
    WHERE action='ai_feedback_submitted';

-- +goose Down
DROP INDEX incident_actions_one_ai_feedback_per_job;
REVOKE SELECT (job_id, incident_id, source_kind, assessment) ON assessment_results FROM go_incidents;
