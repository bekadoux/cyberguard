#!/bin/sh
# Restricted, non-mutating local role/schema probe; not a cross-VM firewall test.
set -eu
export PGHOST=incident-db PGPORT=5432 PGUSER=python_incidents PGDATABASE=cyberguard_incidents PGCONNECT_TIMEOUT=5
PGPASSWORD=$(cat /run/secrets/ai_runtime_password)
export PGPASSWORD
[ "$(psql -X --no-password -Atc 'SELECT ai_worker_ready()')" = t ] || {
    echo 'AI worker not ready; review migrations/grants and pending snapshot-less jobs.' >&2
    exit 1
}
