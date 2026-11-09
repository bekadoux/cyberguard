#!/bin/sh
# Explicit local-only provisioning. Never regenerate over an existing directory.
set -eu
umask 077

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
directory=${DOCKER_SECRETS_DIR:-$root/.local/docker-secrets}
case "$directory" in
    /*) ;;
    *) directory=$root/$directory ;;
esac
command -v openssl >/dev/null 2>&1 || { echo 'openssl is required to generate secrets' >&2; exit 1; }
identity_port=${DOCKER_IDENTITY_PORT:-5433}
incident_port=${DOCKER_INCIDENT_PORT:-5434}
for port in "$identity_port" "$incident_port"; do
    case "$port" in
        ''|*[!0-9]*) echo 'Database ports must be integers from 1 to 65535' >&2; exit 1 ;;
    esac
    if [ "${#port}" -gt 5 ] || [ "$port" -lt 1 ] || [ "$port" -gt 65535 ]; then
        echo 'Database ports must be integers from 1 to 65535' >&2
        exit 1
    fi
done
mkdir -p -- "$(dirname -- "$directory")"
if ! mkdir -- "$directory"; then
    echo 'Refusing to overwrite an existing secrets directory; retain it with its database volumes.' >&2
    exit 1
fi

for name in identity_admin_password identity_migration_password identity_runtime_password \
    incidents_admin_password incidents_migration_password incidents_runtime_password \
    dev_employee_a_password dev_employee_b_password dev_analyst_password dev_admin_password; do
    openssl rand -hex 32 > "$directory/$name"
done

# Generated hex passwords need no URL escaping. User-supplied passwords do.
printf 'postgres://go_identity:%s@identity-db:5432/cyberguard_identity?sslmode=disable\n' \
    "$(cat "$directory/identity_runtime_password")" > "$directory/identity_runtime_url"
printf 'postgres://identity_migrator:%s@identity-db:5432/cyberguard_identity?sslmode=disable\n' \
    "$(cat "$directory/identity_migration_password")" > "$directory/identity_migration_url"
printf 'postgres://go_incidents:%s@incident-db:5432/cyberguard_incidents?sslmode=disable\n' \
    "$(cat "$directory/incidents_runtime_password")" > "$directory/incidents_runtime_url"
printf 'postgres://incidents_migrator:%s@incident-db:5432/cyberguard_incidents?sslmode=disable\n' \
    "$(cat "$directory/incidents_migration_password")" > "$directory/incidents_migration_url"

for database in identity incidents; do
    case "$database" in
        identity) runtime=go_identity; migrator=identity_migrator; port=$identity_port ;;
        incidents) runtime=go_incidents; migrator=incidents_migrator; port=$incident_port ;;
    esac
    printf 'postgres://%s:%s@127.0.0.1:%s/cyberguard_%s?sslmode=disable\n' \
        "$runtime" "$(cat "$directory/${database}_runtime_password")" "$port" "$database" \
        > "$directory/${database}_runtime_host_url"
    printf 'postgres://%s:%s@127.0.0.1:%s/cyberguard_%s?sslmode=disable\n' \
        "$migrator" "$(cat "$directory/${database}_migration_password")" "$port" "$database" \
        > "$directory/${database}_migration_host_url"
done

# Compose file secrets are bind mounts: source ownership/mode, not YAML uid/mode,
# controls access. Go joins this group; the containing directory remains mode 0700.
chgrp "$(id -g)" "$directory"/*
chmod 0640 "$directory"/*
printf 'Created local secrets in %s. Set DOCKER_SECRET_GID=%s for non-root Go access.\n' "$directory" "$(id -g)"
