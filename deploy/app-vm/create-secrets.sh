#!/usr/bin/env bash
# Atomic initial credential publication; never rotate an existing installation.
set -euo pipefail
umask 077
# shellcheck source=deploy/app-vm/common.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/common.sh"
[[ $# == 2 && $1 == --config ]] || fail 'Usage: create-secrets.sh --config FILE'
load_config "$2"
safe_directory "$APP_SECRETS_DIR"
[[ ! -e $APP_SECRETS_DIR/credentials && ! -L $APP_SECRETS_DIR/credentials ]] || fail 'Credentials already exist; retain them with the matching volumes. Rotation is a separate operation.'
temporary=$(mktemp -d "$APP_SECRETS_DIR/.credentials.XXXXXX")
trap 'rm -rf -- "$temporary"' EXIT
mkdir -m 0700 "$temporary/runtime" "$temporary/provisioning"
for database in identity incidents; do
    for role in admin migration runtime; do
        openssl rand -hex 32 > "$temporary/provisioning/${database}_${role}_password"
    done
    if [[ $database == identity ]]; then host=identity-db; else host=incident-db; fi
    printf 'postgres://go_%s:%s@%s:5432/cyberguard_%s?sslmode=verify-full&sslrootcert=/run/certs/ca.crt\n' \
        "$database" "$(cat "$temporary/provisioning/${database}_runtime_password")" "$host" "$database" > "$temporary/runtime/${database}_runtime_url"
    printf 'postgres://%s_migrator:%s@%s:5432/cyberguard_%s?sslmode=verify-full&sslrootcert=/run/certs/ca.crt\n' \
        "$database" "$(cat "$temporary/provisioning/${database}_migration_password")" "$host" "$database" > "$temporary/provisioning/${database}_migration_url"
done
openssl rand -hex 32 > "$temporary/provisioning/ai_runtime_password"
openssl rand -hex 32 > "$temporary/runtime/ai_internal_token"
printf 'postgres://python_incidents:%s@%s:%s/cyberguard_incidents?sslmode=verify-full&sslrootcert=/run/certs/ca.crt\n' \
    "$(cat "$temporary/provisioning/ai_runtime_password")" "$APP_DB_TLS_NAME" "$APP_DB_PORT" > "$temporary/runtime/ai_runtime_url"
# Generated demo credentials are inert until seed-demo is explicitly invoked.
for name in employee_a employee_b analyst admin; do openssl rand -hex 32 > "$temporary/provisioning/dev_${name}_password"; done
chgrp -R "$APP_SECRET_GID" "$temporary"
chmod 0640 "$temporary/runtime/"* "$temporary/provisioning/"*
mv -T --no-clobber -- "$temporary" "$APP_SECRETS_DIR/credentials"
[[ ! -d $temporary ]] || fail 'Another credential directory appeared; nothing was overwritten.'
printf '%s\n' 'Initial credentials created. No database roles were changed; no secret values printed.'
