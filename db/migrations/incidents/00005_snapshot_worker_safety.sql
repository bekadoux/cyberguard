-- +goose Up
-- Historical jobs were deliberately left without snapshots by 00003. Preserve
-- pending ones for explicit operator review, rather than claiming and failing them.
-- A forward migration is required: never rewrite the shared 00002/00003 files.

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ai_claim_job(p_max_attempts INTEGER, p_lease_seconds INTEGER)
RETURNS TABLE(job_id UUID, incident_id UUID, incident_revision BIGINT, claim_token UUID)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
BEGIN
    IF p_max_attempts NOT BETWEEN 1 AND 10 OR p_lease_seconds NOT BETWEEN 30 AND 3600 THEN
        RAISE EXCEPTION 'invalid worker configuration';
    END IF;
    UPDATE public.assessment_jobs AS j
       SET status='failed', error_code='attempts_exhausted',
           claim_token=NULL, lease_expires_at=NULL, finished_at=now(), updated_at=now()
     WHERE j.context_id IS NOT NULL AND j.status='running'
       AND j.lease_expires_at < now() AND j.attempt_count >= p_max_attempts;
    RETURN QUERY
    WITH next_job AS (
        SELECT j.id FROM public.assessment_jobs AS j
        WHERE j.context_id IS NOT NULL
          AND (j.status='queued' OR
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

-- Return a single readiness bit, not incident contents or broader table access.
-- Its existence also establishes that the snapshot-safety migration was applied.
-- +goose StatementBegin
CREATE FUNCTION ai_worker_ready() RETURNS BOOLEAN
LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path = pg_catalog, public AS $$
BEGIN
    RETURN NOT EXISTS (
        SELECT 1 FROM public.assessment_jobs
        WHERE context_id IS NULL AND status IN ('queued', 'running')
    );
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION ai_worker_ready() FROM PUBLIC;

-- Upgrade an already-provisioned AI role without resetting its password. Fresh
-- installations receive the same explicit EXECUTE grant from bootstrap-ai later.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='python_incidents') THEN
        GRANT EXECUTE ON FUNCTION ai_worker_ready() TO python_incidents;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- Restoring the old claim function could silently consume unsnapshotted jobs.
-- +goose StatementBegin
DO $$
BEGIN
    RAISE EXCEPTION 'snapshot worker safety is forward-only; review rollback explicitly';
END;
$$;
-- +goose StatementEnd
