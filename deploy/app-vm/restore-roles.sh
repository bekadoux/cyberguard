#!/usr/bin/env bash
# Source after common.sh/load_config. The compose function must select the isolated
# recovery project; the SQL refuses a previously bootstrapped instance.
prepare_restore_roles() {
    local recovery_compose=$1
    "$recovery_compose" exec -T identity-db psql -X -U bootstrap_admin -d postgres \
        -v target=identity -f - < "$APP_REPO_ROOT/db/restore/prepare-roles.sql" || return 1
    # Reuse the scoped bootstrap client (including its TLS settings), not its
    # post-migration SQL. Credential contents stay out of argv and Docker config.
    # shellcheck disable=SC2016 # Expand only inside the one-off client.
    "$recovery_compose" run --rm -T --no-deps --pull never --entrypoint sh bootstrap-ai -ec '
        PGPASSWORD=$(cat /run/secrets/incidents_admin_password)
        AI_RUNTIME_PASSWORD=$(cat /run/secrets/ai_runtime_password)
        export PGPASSWORD AI_RUNTIME_PASSWORD
        exec psql -X -w -h incident-db -U bootstrap_admin -d postgres -v target=incidents -f -
    ' < "$APP_REPO_ROOT/db/restore/prepare-roles.sql"
}
