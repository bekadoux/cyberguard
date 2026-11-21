-- +goose Up
-- A job UUID has one identity regardless of spelling and belongs to exactly
-- one incident. Keep the check and the index replacement in one transaction.
LOCK TABLE incident_actions IN SHARE ROW EXCLUSIVE MODE;
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM incident_actions WHERE action = 'ai_feedback_submitted'
        GROUP BY (details->>'job_id')::uuid HAVING count(*) > 1
    ) THEN
        RAISE EXCEPTION 'duplicate normalized AI feedback job IDs; migration refused'
            USING HINT = 'Review conflicting audit decisions before retrying; no decision has been removed.';
    END IF;
END;
$$;
-- +goose StatementEnd

ALTER TABLE incident_actions ADD CONSTRAINT incident_actions_ai_feedback_job_required
    CHECK (action <> 'ai_feedback_submitted' OR (details->>'job_id') IS NOT NULL);
-- Index the UUID value without rewriting the spelling in historical evidence.
DROP INDEX incident_actions_one_ai_feedback_per_job;
CREATE UNIQUE INDEX incident_actions_one_ai_feedback_per_job
    ON incident_actions (((details->>'job_id')::uuid))
    WHERE action = 'ai_feedback_submitted';

-- +goose Down
DROP INDEX incident_actions_one_ai_feedback_per_job;
CREATE UNIQUE INDEX incident_actions_one_ai_feedback_per_job
    ON incident_actions (incident_id, ((details->>'job_id')))
    WHERE action = 'ai_feedback_submitted';
ALTER TABLE incident_actions DROP CONSTRAINT incident_actions_ai_feedback_job_required;
