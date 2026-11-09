#!/bin/sh
# Privileged secrets enter only this one-off psql process, never the API.
set -eu
export PGUSER=bootstrap_admin PGDATABASE=postgres PGPORT=5432 PGCONNECT_TIMEOUT=5
case "${1:-}" in
    identity)
        export PGHOST=identity-db
        PGPASSWORD=$(cat /run/secrets/identity_admin_password)
        IDENTITY_MIGRATION_PASSWORD=$(cat /run/secrets/identity_migration_password)
        IDENTITY_RUNTIME_PASSWORD=$(cat /run/secrets/identity_runtime_password)
        export PGPASSWORD IDENTITY_MIGRATION_PASSWORD IDENTITY_RUNTIME_PASSWORD
        ;;
    incidents)
        export PGHOST=incident-db
        PGPASSWORD=$(cat /run/secrets/incidents_admin_password)
        INCIDENT_MIGRATION_PASSWORD=$(cat /run/secrets/incidents_migration_password)
        INCIDENT_RUNTIME_PASSWORD=$(cat /run/secrets/incidents_runtime_password)
        export PGPASSWORD INCIDENT_MIGRATION_PASSWORD INCIDENT_RUNTIME_PASSWORD
        ;;
    *) echo 'Usage: bootstrap.sh identity|incidents' >&2; exit 1 ;;
esac
exec psql -X --no-password --set=ON_ERROR_STOP=on --file "/bootstrap/$1.sql"
