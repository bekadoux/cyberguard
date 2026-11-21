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
BEGIN;
SET LOCAL search_path = pg_catalog;
SELECT pg_advisory_xact_lock(918274002);
DO $$
DECLARE
  worker pg_roles%ROWTYPE;
BEGIN
  IF current_database() <> 'cyberguard_incidents' THEN
    RAISE EXCEPTION 'AI bootstrap requires cyberguard_incidents';
  END IF;
  SELECT * INTO worker FROM pg_roles WHERE rolname = 'python_incidents';
  IF NOT FOUND THEN
    CREATE ROLE python_incidents LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE
      NOREPLICATION NOBYPASSRLS NOINHERIT;
    RETURN;
  END IF;

  IF NOT worker.rolcanlogin OR worker.rolsuper OR worker.rolcreatedb OR worker.rolcreaterole
     OR worker.rolreplication OR worker.rolbypassrls OR worker.rolinherit
     OR worker.rolconnlimit = 0 OR worker.rolvaliduntil <= now()
     OR EXISTS (SELECT FROM pg_db_role_setting WHERE setrole = worker.oid)
     OR EXISTS (SELECT FROM pg_auth_members WHERE worker.oid IN (member, roleid)) THEN
    RAISE EXCEPTION 'unsuitable existing python_incidents attributes, settings or memberships';
  END IF;

  -- pg_shdepend records ownership and direct ACLs, including column grants,
  -- default privileges and objects in *other* databases. Allow only references
  -- to these exact objects/columns and inspect the actual ACL: sharing the
  -- right object does not make WRITE or WITH GRANT OPTION acceptable.
  IF EXISTS (
    WITH here AS (SELECT oid FROM pg_database WHERE datname = current_database()),
    approved(dbid, classid, objid, subid, privilege, acl) AS (
      SELECT 0::oid, 'pg_database'::regclass, oid, 0, 'CONNECT', datacl
        FROM pg_database WHERE datname = current_database()
      UNION ALL
      SELECT here.oid, 'pg_namespace'::regclass, n.oid, 0, 'USAGE', n.nspacl
        FROM here, pg_namespace n WHERE n.nspname = 'public'
      UNION ALL
      SELECT here.oid, 'pg_proc'::regclass, p.oid, 0, 'EXECUTE', p.proacl
        FROM here, pg_proc p WHERE p.oid IN (
          'public.ai_claim_job(integer,integer)'::regprocedure,
          'public.ai_renew_claim(uuid,uuid,integer)'::regprocedure,
          'public.ai_finish_job(uuid,uuid,jsonb,text)'::regprocedure,
          'public.ai_fail_job(uuid,uuid,text)'::regprocedure,
          'public.ai_worker_ready()'::regprocedure)
      UNION ALL
      SELECT here.oid, 'pg_class'::regclass, a.attrelid, a.attnum, 'SELECT', a.attacl
        FROM here, pg_attribute a WHERE
          (a.attrelid = 'public.assessment_jobs'::regclass AND a.attname IN
            ('id','incident_id','kind','status','incident_revision','attempt_count',
             'created_at','updated_at','finished_at','error_code'))
          OR (a.attrelid = 'public.assessment_results'::regclass AND a.attname IN
            ('job_id','incident_id','assessment','source_kind','created_at'))
    )
    SELECT FROM pg_shdepend d LEFT JOIN approved a ON
      (a.dbid, a.classid, a.objid, a.subid) = (d.dbid, d.classid, d.objid, d.objsubid)
    WHERE d.refclassid = 'pg_authid'::regclass AND d.refobjid = worker.oid
      AND (d.deptype <> 'a' OR a.objid IS NULL OR EXISTS (
        SELECT FROM aclexplode(a.acl) x WHERE x.grantor = worker.oid OR
          (x.grantee = worker.oid AND (x.privilege_type <> a.privilege OR x.is_grantable))))
  ) THEN
    RAISE EXCEPTION 'unsuitable existing python_incidents ownership or grants';
  END IF;
END $$;
SET LOCAL password_encryption = 'scram-sha-256';
ALTER ROLE python_incidents PASSWORD :'ai_password';
GRANT CONNECT ON DATABASE cyberguard_incidents TO python_incidents;
GRANT USAGE ON SCHEMA public TO python_incidents;
GRANT SELECT (id, incident_id, kind, status, incident_revision, attempt_count,
              created_at, updated_at, finished_at, error_code)
  ON public.assessment_jobs TO python_incidents;
GRANT SELECT (job_id, incident_id, assessment, source_kind, created_at)
  ON public.assessment_results TO python_incidents;
GRANT EXECUTE ON FUNCTION public.ai_claim_job(INTEGER, INTEGER),
  public.ai_renew_claim(UUID, UUID, INTEGER), public.ai_finish_job(UUID, UUID, JSONB, TEXT),
  public.ai_fail_job(UUID, UUID, TEXT), public.ai_worker_ready() TO python_incidents;
COMMIT;
