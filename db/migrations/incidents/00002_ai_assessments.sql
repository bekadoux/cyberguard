-- +goose Up
-- MVP decision: incident creation enqueues through a database trigger in the SAME
-- transaction as Go's existing INSERT. Coordinate any later direct Go enqueueing.
ALTER TABLE incidents ADD COLUMN context_revision BIGINT NOT NULL DEFAULT 1
    CHECK (context_revision > 0);

-- +goose StatementBegin
CREATE FUNCTION ai_bump_incident_revision() RETURNS TRIGGER
LANGUAGE plpgsql AS $$
BEGIN
    IF (NEW.title, NEW.description, NEW.affected_system)
       IS DISTINCT FROM (OLD.title, OLD.description, OLD.affected_system) THEN
        NEW.context_revision := OLD.context_revision + 1;
    ELSE
        NEW.context_revision := OLD.context_revision;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER ai_incident_revision BEFORE UPDATE ON incidents
    FOR EACH ROW EXECUTE FUNCTION ai_bump_incident_revision();

CREATE TABLE assessment_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    incident_id UUID NOT NULL REFERENCES incidents(id),
    kind TEXT NOT NULL DEFAULT 'initial_assessment'
        CHECK (kind IN ('initial_assessment', 'regenerated_assessment')),
    status TEXT NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'running', 'completed', 'failed')),
    incident_revision BIGINT NOT NULL CHECK (incident_revision > 0),
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    error_code TEXT,
    claim_token UUID,
    lease_expires_at TIMESTAMPTZ,
    UNIQUE (id, incident_id),
    CONSTRAINT ai_running_claim CHECK ((status = 'running') =
        (claim_token IS NOT NULL AND lease_expires_at IS NOT NULL))
);
CREATE UNIQUE INDEX assessment_jobs_one_initial_per_incident
    ON assessment_jobs (incident_id) WHERE kind = 'initial_assessment';
CREATE INDEX assessment_jobs_queue_idx ON assessment_jobs (status, created_at, id);
CREATE INDEX assessment_jobs_incident_idx ON assessment_jobs (incident_id, created_at DESC);

CREATE TABLE assessment_results (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id UUID NOT NULL UNIQUE,
    incident_id UUID NOT NULL,
    assessment JSONB NOT NULL CHECK (jsonb_typeof(assessment) = 'object'),
    source_kind TEXT NOT NULL CHECK (source_kind IN ('kev_llm', 'fake')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (job_id, incident_id)
        REFERENCES assessment_jobs (id, incident_id)
);

-- SECURITY DEFINER functions let the worker call narrow transitions without
-- direct UPDATE/INSERT privileges. Function owner is incidents_migrator.
-- +goose StatementBegin
CREATE FUNCTION ai_enqueue_initial() RETURNS TRIGGER
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
BEGIN
    INSERT INTO public.assessment_jobs (incident_id, incident_revision)
    VALUES (NEW.id, NEW.context_revision);
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER ai_incident_enqueue AFTER INSERT ON incidents
    FOR EACH ROW EXECUTE FUNCTION ai_enqueue_initial();

-- +goose StatementBegin
CREATE FUNCTION ai_claim_job(p_max_attempts INTEGER, p_lease_seconds INTEGER)
RETURNS TABLE(job_id UUID, incident_id UUID, incident_revision BIGINT, claim_token UUID)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
BEGIN
    IF p_max_attempts NOT BETWEEN 1 AND 10 OR p_lease_seconds NOT BETWEEN 30 AND 3600 THEN
        RAISE EXCEPTION 'invalid worker configuration';
    END IF;
    -- Expired, exhausted claims become terminal; no permanent running zombies.
    UPDATE public.assessment_jobs AS j
       SET status='failed', error_code='attempts_exhausted',
           claim_token=NULL, lease_expires_at=NULL, finished_at=now(), updated_at=now()
     WHERE j.status='running' AND j.lease_expires_at < now()
       AND j.attempt_count >= p_max_attempts;
    RETURN QUERY
    WITH next_job AS (
        SELECT j.id FROM public.assessment_jobs AS j
        WHERE (j.status='queued' OR
               (j.status='running' AND j.lease_expires_at < now()))
          AND j.attempt_count < p_max_attempts
        ORDER BY j.created_at, j.id LIMIT 1 FOR UPDATE SKIP LOCKED
    )
    UPDATE public.assessment_jobs AS j SET
        status='running', attempt_count=j.attempt_count + 1,
        claim_token=gen_random_uuid(),
        lease_expires_at=now()+make_interval(secs=>p_lease_seconds),
        started_at=now(), finished_at=NULL, error_code=NULL, updated_at=now()
    FROM next_job AS n WHERE j.id=n.id
    RETURNING j.id, j.incident_id, j.incident_revision, j.claim_token;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION ai_renew_claim(p_job UUID, p_token UUID, p_lease_seconds INTEGER)
RETURNS BOOLEAN LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
BEGIN
    IF p_lease_seconds NOT BETWEEN 30 AND 3600 THEN RETURN FALSE; END IF;
    UPDATE public.assessment_jobs AS j
    SET lease_expires_at=now()+make_interval(secs=>p_lease_seconds), updated_at=now()
    WHERE j.id=p_job AND j.status='running' AND j.claim_token=p_token
      AND j.lease_expires_at > now();
    RETURN FOUND;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION ai_finish_job(p_job UUID, p_token UUID, p_assessment JSONB, p_source TEXT)
RETURNS BOOLEAN LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE v_incident UUID;
BEGIN
    IF p_source NOT IN ('kev_llm', 'fake') OR
       jsonb_typeof(p_assessment) IS DISTINCT FROM 'object' THEN
        RETURN FALSE;
    END IF;
    SELECT j.incident_id INTO v_incident FROM public.assessment_jobs AS j
    WHERE j.id=p_job AND j.status='running' AND j.claim_token=p_token
      AND j.lease_expires_at > now() FOR UPDATE;
    IF NOT FOUND THEN RETURN FALSE; END IF;
    INSERT INTO public.assessment_results(job_id, incident_id, assessment, source_kind)
    VALUES (p_job, v_incident, p_assessment, p_source);
    UPDATE public.assessment_jobs AS j SET status='completed', claim_token=NULL,
        lease_expires_at=NULL, finished_at=now(), updated_at=now()
    WHERE j.id=p_job;
    RETURN TRUE;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION ai_fail_job(p_job UUID, p_token UUID, p_code TEXT)
RETURNS BOOLEAN LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
BEGIN
    IF p_code IS NULL OR p_code !~ '^[a-z_]{1,50}$' THEN RETURN FALSE; END IF;
    UPDATE public.assessment_jobs AS j SET status='failed', error_code=p_code,
        claim_token=NULL, lease_expires_at=NULL, finished_at=now(), updated_at=now()
    WHERE j.id=p_job AND j.status='running' AND j.claim_token=p_token
      AND j.lease_expires_at > now();
    RETURN FOUND;
END;
$$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION ai_enqueue_initial() FROM PUBLIC;
REVOKE ALL ON FUNCTION ai_claim_job(INTEGER, INTEGER) FROM PUBLIC;
REVOKE ALL ON FUNCTION ai_renew_claim(UUID, UUID, INTEGER) FROM PUBLIC;
REVOKE ALL ON FUNCTION ai_finish_job(UUID, UUID, JSONB, TEXT) FROM PUBLIC;
REVOKE ALL ON FUNCTION ai_fail_job(UUID, UUID, TEXT) FROM PUBLIC;
-- Go needs only job metadata for its protected internal context lookup.
GRANT SELECT (id, incident_id, incident_revision) ON assessment_jobs TO go_incidents;
-- Worker privileges are granted after its role is provisioned by bootstrap-ai.

-- +goose Down
DROP TRIGGER ai_incident_enqueue ON incidents;
DROP FUNCTION ai_enqueue_initial();
DROP FUNCTION ai_fail_job(UUID, UUID, TEXT);
DROP FUNCTION ai_finish_job(UUID, UUID, JSONB, TEXT);
DROP FUNCTION ai_renew_claim(UUID, UUID, INTEGER);
DROP FUNCTION ai_claim_job(INTEGER, INTEGER);
DROP TABLE assessment_results;
DROP TABLE assessment_jobs;
DROP TRIGGER ai_incident_revision ON incidents;
DROP FUNCTION ai_bump_incident_revision();
ALTER TABLE incidents DROP COLUMN context_revision;
