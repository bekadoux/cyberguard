#!/bin/sh
set -eu
export PGPASSWORD="$(cat /run/secrets/incidents_admin_password)"
export AI_RUNTIME_PASSWORD="$(cat /run/secrets/ai_runtime_password)"
[ "${#AI_RUNTIME_PASSWORD}" -ge 16 ] || { echo 'AI password is too short' >&2; exit 1; }
psql -X -v ON_ERROR_STOP=1 -h incident-db -U bootstrap_admin -d cyberguard_incidents \
   -f /setup/ai-worker.sql
