-- Run on the separate incident PostgreSQL instance, never on the identity instance.
\set ON_ERROR_STOP on
SET statement_timeout = '30s';
SET lock_timeout = '5s';
\getenv migration_password INCIDENT_MIGRATION_PASSWORD
\getenv runtime_password INCIDENT_RUNTIME_PASSWORD
\if :{?migration_password}
\else
  \echo 'INCIDENT_MIGRATION_PASSWORD is required'
  DO $$ BEGIN RAISE EXCEPTION 'bootstrap configuration is incomplete'; END $$;
\endif
\if :{?runtime_password}
\else
  \echo 'INCIDENT_RUNTIME_PASSWORD is required'
  DO $$ BEGIN RAISE EXCEPTION 'bootstrap configuration is incomplete'; END $$;
\endif

SELECT length(:'migration_password') >= 16
   AND length(:'runtime_password') >= 16
   AND left(:'migration_password', 8) <> 'REPLACE_'
   AND left(:'runtime_password', 8) <> 'REPLACE_'
   AND :'migration_password' <> :'runtime_password' AS passwords_ok \gset
\if :passwords_ok
\else
  \echo 'Replace placeholders with distinct passwords of at least 16 characters'
  DO $$ BEGIN RAISE EXCEPTION 'unsafe bootstrap passwords'; END $$;
\endif

SELECT pg_advisory_lock(918274002);
SELECT NOT EXISTS (
    SELECT 1 FROM pg_database
    WHERE datname = 'cyberguard_incidents'
      AND pg_get_userbyid(datdba) <> 'incidents_migrator'
) AND NOT EXISTS (
    SELECT 1 FROM pg_auth_members
    WHERE member IN (SELECT oid FROM pg_roles WHERE rolname IN ('incidents_migrator', 'go_incidents'))
) AS setup_safe \gset
\if :setup_safe
\else
  \echo 'Refusing an existing database with another owner or roles with memberships'
  DO $$ BEGIN RAISE EXCEPTION 'unsafe existing database or roles'; END $$;
\endif

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'incidents_migrator') THEN
        CREATE ROLE incidents_migrator;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'go_incidents') THEN
        CREATE ROLE go_incidents;
    END IF;
END $$;

SET password_encryption = 'scram-sha-256';
ALTER ROLE incidents_migrator WITH LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE
    NOREPLICATION NOBYPASSRLS NOINHERIT PASSWORD :'migration_password';
ALTER ROLE go_incidents WITH LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE
    NOREPLICATION NOBYPASSRLS NOINHERIT PASSWORD :'runtime_password';

SELECT 'CREATE DATABASE cyberguard_incidents OWNER incidents_migrator'
WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = 'cyberguard_incidents') \gexec

REVOKE ALL ON DATABASE cyberguard_incidents FROM PUBLIC;
REVOKE ALL ON DATABASE cyberguard_incidents FROM go_incidents;
GRANT CONNECT ON DATABASE cyberguard_incidents TO go_incidents;

\connect cyberguard_incidents
SET statement_timeout = '30s';
SET lock_timeout = '5s';
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
REVOKE ALL ON SCHEMA public FROM go_incidents;
GRANT USAGE ON SCHEMA public TO go_incidents;
