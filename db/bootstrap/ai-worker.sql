-- Run against cyberguard_incidents using bootstrap_admin only after migrations.
-- ai_password comes from an environment value in the one-off process.
\set ON_ERROR_STOP on
\getenv ai_password AI_RUNTIME_PASSWORD
SELECT length(:'ai_password') >= 16 AS valid_password \gset
\if :valid_password
\else
  \echo 'Password must be at least 16 characters'
  DO $$ BEGIN RAISE EXCEPTION 'AI password too short'; END $$;
\endif
DO $$ BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'python_incidents') THEN
    CREATE ROLE python_incidents LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE
      NOREPLICATION NOBYPASSRLS NOINHERIT;
  END IF;
END $$;
SET password_encryption = 'scram-sha-256';
ALTER ROLE python_incidents PASSWORD :'ai_password';
GRANT CONNECT ON DATABASE cyberguard_incidents TO python_incidents;
GRANT USAGE ON SCHEMA public TO python_incidents;
GRANT SELECT (id, incident_id, kind, status, incident_revision, attempt_count,
              created_at, updated_at, finished_at, error_code)
  ON assessment_jobs TO python_incidents;
GRANT SELECT (job_id, incident_id, assessment, source_kind, created_at)
  ON assessment_results TO python_incidents;
GRANT EXECUTE ON FUNCTION ai_claim_job(INTEGER, INTEGER),
  ai_renew_claim(UUID, UUID, INTEGER), ai_finish_job(UUID, UUID, JSONB, TEXT),
  ai_fail_job(UUID, UUID, TEXT), ai_worker_ready() TO python_incidents;
