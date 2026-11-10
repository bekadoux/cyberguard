-- +goose Up
-- Forward-only increment: do not rewrite applied 00001/00002 migrations.
-- Historical assessment jobs are deliberately not backfilled; snapshots are
-- created for new submissions, within the same INSERT transaction as the job.
CREATE TABLE incident_contexts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    incident_id UUID NOT NULL REFERENCES incidents(id),
    revision BIGINT NOT NULL CHECK (revision > 0),
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    affected_system TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (incident_id, revision),
    UNIQUE (id, incident_id, revision)
);
ALTER TABLE assessment_jobs ADD COLUMN context_id UUID;
ALTER TABLE assessment_jobs ADD CONSTRAINT assessment_jobs_snapshot_link
    FOREIGN KEY (context_id, incident_id, incident_revision)
    REFERENCES incident_contexts(id, incident_id, revision);

-- +goose StatementBegin
CREATE FUNCTION immutable_incident_context() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'incident contexts are immutable';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER immutable_incident_context_update BEFORE UPDATE OR DELETE
    ON incident_contexts FOR EACH ROW EXECUTE FUNCTION immutable_incident_context();

-- Replace the original enqueueing trigger function in-place (not its migration).
-- A failure of either INSERT rolls back the original incident INSERT atomically.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ai_enqueue_initial() RETURNS TRIGGER
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE snapshot_id UUID;
BEGIN
    EXECUTE format('INSERT INTO %I.incident_contexts(incident_id, revision, title, description, affected_system) VALUES ($1,$2,$3,$4,$5) RETURNING id', TG_TABLE_SCHEMA)
        INTO snapshot_id USING NEW.id, NEW.context_revision, NEW.title, NEW.description, NEW.affected_system;
    EXECUTE format('INSERT INTO %I.assessment_jobs (incident_id, incident_revision, context_id) VALUES ($1,$2,$3)', TG_TABLE_SCHEMA)
        USING NEW.id, NEW.context_revision, snapshot_id;
    EXECUTE format('INSERT INTO %I.incident_actions(incident_id,actor_id,action) VALUES ($1,$2,$3)', TG_TABLE_SCHEMA)
        USING NEW.id, NEW.reporter_id, 'submitted';
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- Changed affected-system context is persisted for review but does not silently
-- enqueue another initial job. The title and description remain write-protected.
-- +goose StatementBegin
CREATE FUNCTION ai_snapshot_revised_context() RETURNS TRIGGER
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
BEGIN
    IF NEW.context_revision <> OLD.context_revision THEN
        EXECUTE format('INSERT INTO %I.incident_contexts(incident_id, revision, title, description, affected_system) VALUES ($1,$2,$3,$4,$5)', TG_TABLE_SCHEMA)
            USING NEW.id, NEW.context_revision, NEW.title, NEW.description, NEW.affected_system;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER ai_snapshot_revised_context AFTER UPDATE ON incidents
    FOR EACH ROW EXECUTE FUNCTION ai_snapshot_revised_context();

-- Separate resolution timestamp avoids changing sqlc-generated incident row
-- shape (the existing generated code uses RETURNING * / SELECT *).
CREATE TABLE incident_resolutions (
    incident_id UUID PRIMARY KEY REFERENCES incidents(id),
    resolved_at TIMESTAMPTZ
);

CREATE TABLE incident_comments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    incident_id UUID NOT NULL REFERENCES incidents(id),
    author_id UUID NOT NULL,
    visibility TEXT NOT NULL CHECK (visibility IN ('public','internal')),
    body TEXT NOT NULL CHECK (char_length(body) BETWEEN 1 AND 5000 AND btrim(body) <> ''),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX incident_comments_by_incident ON incident_comments(incident_id, created_at, id);

CREATE TABLE incident_actions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    incident_id UUID NOT NULL REFERENCES incidents(id),
    actor_id UUID,
    action TEXT NOT NULL,
    reason TEXT,
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (jsonb_typeof(details) = 'object')
);
CREATE INDEX incident_actions_by_incident ON incident_actions(incident_id, created_at, id);

CREATE TABLE incident_notifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    recipient_id UUID NOT NULL,
    incident_id UUID REFERENCES incidents(id),
    event_type TEXT NOT NULL,
    message TEXT NOT NULL CHECK (char_length(message) <= 500),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    read_at TIMESTAMPTZ
);
CREATE INDEX incident_notifications_unread ON incident_notifications(recipient_id, created_at DESC)
    WHERE read_at IS NULL;
CREATE INDEX incident_notifications_history ON incident_notifications(recipient_id, created_at DESC);

-- Only the columns used by workflow actions are mutable through Go.
-- No grants to change reporter_id, original title or original description.
GRANT SELECT ON incident_contexts TO go_incidents;
GRANT SELECT, INSERT, UPDATE ON incident_resolutions TO go_incidents;
GRANT SELECT, INSERT ON incident_comments, incident_actions TO go_incidents;
GRANT SELECT, INSERT ON incident_notifications TO go_incidents;
GRANT UPDATE (read_at) ON incident_notifications TO go_incidents;
GRANT UPDATE (assigned_analyst_id, status, category, severity,
              affected_system, resolution_note,
              archived_at, archive_reason, updated_at) ON incidents TO go_incidents;
REVOKE ALL ON FUNCTION immutable_incident_context() FROM PUBLIC;
REVOKE ALL ON FUNCTION ai_snapshot_revised_context() FROM PUBLIC;

-- +goose Down
-- Refuse to destroy existing history, snapshots or resolution data.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM incident_contexts) OR EXISTS (SELECT 1 FROM incident_comments)
       OR EXISTS (SELECT 1 FROM incident_actions) OR EXISTS (SELECT 1 FROM incident_notifications)
       OR EXISTS (SELECT 1 FROM incident_resolutions) THEN
        RAISE EXCEPTION 'handoff data exists; refusing destructive downgrade';
    END IF;
END;
$$;
-- +goose StatementEnd
DROP TRIGGER ai_snapshot_revised_context ON incidents;
DROP FUNCTION ai_snapshot_revised_context();
DROP TRIGGER immutable_incident_context_update ON incident_contexts;
DROP FUNCTION immutable_incident_context();
-- Restore original enqueue function for installations which genuinely have no
-- handoff data, without altering the existing 00002 migration file.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ai_enqueue_initial() RETURNS TRIGGER
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
BEGIN
    EXECUTE format('INSERT INTO %I.assessment_jobs(incident_id,incident_revision) VALUES($1,$2)',TG_TABLE_SCHEMA)
        USING NEW.id, NEW.context_revision;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
ALTER TABLE assessment_jobs DROP CONSTRAINT assessment_jobs_snapshot_link;
ALTER TABLE assessment_jobs DROP COLUMN context_id;
DROP TABLE incident_notifications;
DROP TABLE incident_actions;
DROP TABLE incident_comments;
DROP TABLE incident_resolutions;
DROP TABLE incident_contexts;
