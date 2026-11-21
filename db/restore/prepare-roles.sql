-- For pg_restore --create on a *new* cluster, before any database bootstrap.
-- The archive supplies database/object ownership and ACLs, but cannot create roles.
\set ON_ERROR_STOP on
BEGIN;
SET LOCAL search_path = pg_catalog;
DO $$
BEGIN
    IF current_database() <> 'postgres' OR current_user <> 'bootstrap_admin'
        OR EXISTS (SELECT FROM pg_database WHERE NOT datistemplate AND datname <> 'postgres')
        OR EXISTS (SELECT FROM pg_roles WHERE rolname !~ '^pg_' AND rolname <> current_user)
    THEN
        RAISE EXCEPTION 'restore role preparation requires a fresh recovery instance';
    END IF;
END $$;

SELECT :'target' = 'identity' AS identity, :'target' = 'incidents' AS incidents \gset
\if :identity
    CREATE ROLE identity_migrator NOLOGIN;
    CREATE ROLE go_identity NOLOGIN;
\elif :incidents
    CREATE ROLE incidents_migrator NOLOGIN;
    CREATE ROLE go_incidents NOLOGIN;
    -- No table/function grants here. Never adopt or rotate an existing AI role.
    CREATE ROLE python_incidents LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE
        NOREPLICATION NOBYPASSRLS NOINHERIT;
    \getenv ai_password AI_RUNTIME_PASSWORD
    SELECT length(:'ai_password') >= 16 AND left(:'ai_password', 8) <> 'REPLACE_' AS password_ok \gset
    \if :password_ok
        SET LOCAL password_encryption = 'scram-sha-256';
        ALTER ROLE python_incidents PASSWORD :'ai_password';
    \else
        DO $$ BEGIN RAISE EXCEPTION 'missing or unsafe AI recovery password'; END $$;
    \endif
\else
    DO $$ BEGIN RAISE EXCEPTION 'unknown restore target'; END $$;
\endif
COMMIT;
