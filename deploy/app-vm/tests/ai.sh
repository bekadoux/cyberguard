#!/usr/bin/env bash
# Sourced only by smoke.sh: all writes/SQL alterations target its disposable fixture.
# shellcheck disable=SC2154
set -euo pipefail
ai_compose() {
    docker compose --env-file /dev/null --project-directory "$APP_REPO_ROOT" \
        --project-name "$project-ai" -f "$APP_DEPLOY_DIR/tests/ai.compose.yaml" "$@"
}
ai_start() {
    # A numeric private endpoint avoids depending on cross-project Docker DNS. The
    # disposable certificates include its IP SAN, as well as the production DNS SANs.
    sed "s/@$APP_DB_TLS_NAME:/@$APP_BIND_IP:/" "$APP_RUNTIME_DIR/ai_runtime_url" > "$test_dir/ai_runtime_url"
    # Start with a mismatched AI-side token. Never change Go's credential.
    printf 'disposable-mismatched-worker-token\n' > "$test_dir/worker-token"
    chmod 0640 "$test_dir/ai_runtime_url" "$test_dir/worker-token"
    ai_compose up -d --no-build --pull never --wait --wait-timeout 60 ai-api ai-gateway
}
sql() { compose exec -T incident-db psql -X -v ON_ERROR_STOP=1 -U bootstrap_admin -d cyberguard_incidents -qAtc "$1"; }
ai_check() {
    local incident=$1 job legacy
    request 200 -b "$test_dir/analyst.cookies" -H 'Content-Type: application/json' --data '{}' "$base/api/incidents/$incident/claim"
    request 200 -b "$test_dir/analyst.cookies" -H 'Content-Type: application/json' -X PATCH \
        --data '{"affected_system":"revised fixture system"}' "$base/api/incidents/$incident/fields"
    job=$(sql "SELECT id FROM assessment_jobs WHERE incident_id='$incident'")
    printf 'Authorization: Bearer %s\n' "$(cat "$APP_RUNTIME_DIR/ai_internal_token")" > "$test_dir/service-header"
    request 404 --header "@$test_dir/service-header" "$base/api/internal/ai/auth-check"
    local probe="https://$APP_GO_TLS_NAME:$APP_PRIVATE_PORT/internal/ai/auth-check"
    local -a private=(curl --silent --show-error --noproxy '*' --cacert "$APP_TLS_DIR/ca.crt"
        --resolve "$APP_GO_TLS_NAME:$APP_PRIVATE_PORT:$APP_BIND_IP" -o /dev/null -w '%{http_code}')
    [[ $("${private[@]}" "$probe") == 401 ]]
    [[ $("${private[@]}" --header "@$test_dir/service-header" "$probe") == 204 ]]
    [[ $("${private[@]}" --header "@$test_dir/service-header" -X POST "$probe") == 404 ]]
    [[ $(curl --fail --silent --noproxy '*' --cacert "$APP_TLS_DIR/ca.crt" \
        --resolve "$APP_GO_TLS_NAME:$APP_PRIVATE_PORT:$APP_BIND_IP" --header "@$test_dir/service-header" \
        "https://$APP_GO_TLS_NAME:$APP_PRIVATE_PORT/internal/ai/jobs/$job/context" | jq -er '.affected_system') == 'original fixture system' ]]
    ai_compose up -d --no-build --pull never ai-worker
    if ai_compose run --rm --no-deps ai-worker python -m cyberguard_ai.preflight; then
        fail 'AI preflight accepted a mismatched worker token.'
    fi
    sleep 3 # Allow several one-second claim polls with the bad token.
    [[ $(sql "SELECT status || attempt_count FROM assessment_jobs WHERE id='$job'") == queued0 ]] || fail 'Worker token mismatch consumed a job.'
    cat "$APP_RUNTIME_DIR/ai_internal_token" > "$test_dir/worker-token"
    ai_compose restart ai-worker
    for _ in {1..40}; do
        [[ $(sql "SELECT status FROM assessment_jobs WHERE id='$job'") == completed ]] && break
        sleep 1
    done
    [[ $(sql "SELECT status FROM assessment_jobs WHERE id='$job'") == completed ]] || fail 'Synthetic cross-project job did not complete.'
    request 200 -b "$test_dir/analyst.cookies" "$base/ai/incidents/$incident/assessment"
    jq -e '.job.status == "completed" and .result.assessment.is_fake == true and .result.source_kind == "fake"' "$test_dir/body" >/dev/null
    request 403 -b "$test_dir/employee_a.cookies" "$base/ai/incidents/$incident/assessment"
    # The actual app proxy must reject a correctly chained but wrong upstream name.
    cp "$config" "$test_dir/wrong-ai.env"
    printf 'APP_AI_TLS_NAME=wrong-ai.test\n' >> "$test_dir/wrong-ai.env"
    bash "$APP_DEPLOY_DIR/manage.sh" --config "$test_dir/wrong-ai.env" up
    request 502 -b "$test_dir/analyst.cookies" "$base/ai/incidents/$incident/assessment"
    if bash "$APP_DEPLOY_DIR/manage.sh" --config "$test_dir/wrong-ai.env" check-ai; then fail 'AI TLS accepted the wrong hostname.'; fi
    manage up
    request 200 -b "$test_dir/analyst.cookies" "$base/ai/incidents/$incident/assessment"
    # Staff own-report access is deliberately allowed under the merged contract.
    request 201 -b "$test_dir/analyst.cookies" -H 'Content-Type: application/json' \
        --data '{"title":"DISPOSABLE staff own report","description":"Synthetic ownership fixture."}' "$base/api/incidents"
    local own
    own=$(jq -er '.incident.id' "$test_dir/body")
    request 200 -b "$test_dir/analyst.cookies" "$base/ai/incidents/$own/assessment"
    # Preserve a pending legacy job and fail readiness, without claiming/resetting it.
    ai_compose stop ai-worker
    legacy=$(sql "INSERT INTO incidents(reporter_id,title,description) VALUES('11111111-1111-4111-8111-111111111111','DISPOSABLE legacy job','Synthetic readiness fixture') RETURNING id")
    sql "UPDATE assessment_jobs SET context_id=NULL WHERE incident_id='$legacy'" >/dev/null
    if manage check-ai; then fail 'Legacy pending job did not block readiness.'; fi
    [[ $(sql "SELECT status || attempt_count FROM assessment_jobs WHERE incident_id='$legacy'") == queued0 ]]
    # No retirement is needed: this complete fixture is about to be destroyed.
}
hba_client() {
    local role=$1 database=$2 mode=$3 query=$4
    # network=container emulates the exact AI source; the password stays in a file.
    docker run --rm -i --network "container:$(ai_compose ps -q ai-api)" \
        --volume "$APP_PROVISION_DIR/ai_runtime_password:/run/password:ro" \
        --volume "$APP_TLS_DIR/ca.crt:/run/ca.crt:ro" \
        -e PGHOST="$APP_BIND_IP" -e PGPORT="$APP_DB_PORT" -e PGUSER="$role" \
        -e PGDATABASE="$database" -e PGSSLMODE="$mode" -e PGSSLROOTCERT=/run/ca.crt \
        -e PGCONNECT_TIMEOUT=3 \
        postgres:17.11-bookworm@sha256:665dc7b37ac52a17e298511414c21d23ffa4e6a6ef6225b3e5d59c7afb35e994 \
        /bin/sh -ec 'export PGPASSWORD="$(cat /run/password)"; exec psql -X --no-password -At' <<< "$query"
}
hba_check() {
    [[ $(hba_client python_incidents cyberguard_incidents verify-full 'SELECT inet_client_addr();') == "$APP_AI_IP" ]] || fail 'Fixture did not preserve the AI source address.'
    local role database mode
    for role in python_incidents go_incidents; do
        for database in cyberguard_incidents postgres; do
            for mode in verify-full disable; do
                [[ $role != python_incidents || $database != cyberguard_incidents || $mode != verify-full ]] || continue
                if hba_client "$role" "$database" "$mode" 'SELECT 1;' > "$test_dir/hba-error" 2>&1; then fail 'AI source bypassed SQL HBA restrictions.'; fi
                grep -q 'pg_hba.conf rejects connection' "$test_dir/hba-error" || fail 'Connection failed for a reason other than the explicit HBA reject.'
            done
        done
    done
    # A different emulated VNet host has no rule even with the correct AI password.
    if docker run --rm --network "$APP_TEST_TRANSIT" --ip "172.29.$octet.3" \
        --volume "$APP_PROVISION_DIR/ai_runtime_password:/run/password:ro" \
        --volume "$APP_TLS_DIR/ca.crt:/run/ca.crt:ro" \
        -e PGHOST="$APP_BIND_IP" -e PGPORT="$APP_DB_PORT" -e PGUSER=python_incidents \
        -e PGDATABASE=cyberguard_incidents -e PGSSLMODE=verify-full -e PGSSLROOTCERT=/run/ca.crt -e PGCONNECT_TIMEOUT=3 \
        postgres:17.11-bookworm@sha256:665dc7b37ac52a17e298511414c21d23ffa4e6a6ef6225b3e5d59c7afb35e994 \
        /bin/sh -ec 'export PGPASSWORD="$(cat /run/password)"; exec psql -X --no-password -Atc "SELECT 1"' > "$test_dir/outsider-error" 2>&1; then
        fail 'Another VNet source bypassed HBA isolation.'
    fi
    grep -q 'no pg_hba.conf entry' "$test_dir/outsider-error" || fail 'Outsider failure did not exercise SQL HBA isolation.'
}
restore_compose() {
    DOCKER_SECRETS_DIR=$APP_PROVISION_DIR docker compose --env-file /dev/null \
        --project-directory "$APP_REPO_ROOT" --project-name "$project-restore" \
        -f "$APP_REPO_ROOT/compose.yaml" -f "$APP_DEPLOY_DIR/tests/restore.compose.yaml" "$@"
}
restore_runtime_query() {
    local client=$1 password=$2 service=$3 role=$4 database=$5 query=$6
    # shellcheck disable=SC2016 # Positional parameters belong to the client shell.
    restore_compose run --rm -T --no-deps --pull never --entrypoint sh "$client" -ec '
        PGPASSWORD=$(cat "/run/secrets/$1")
        export PGPASSWORD
        exec psql -X -w -h "$2" -U "$3" -d "$4" -v ON_ERROR_STOP=1 -v VERBOSITY=verbose -Atc "$5"
    ' sh "$password" "$service" "$role" "cyberguard_$database" "$query"
}
restore_check() (
    # Separate clusters and volumes; no published SQL ports or shared DB network.
    [[ $project =~ ^cyberguard-app-vm-smoke-[a-f0-9]{12}$ ]] || fail 'Invalid restore fixture project.'
    local label="label=com.docker.compose.project=$project-restore"
    local resources
    resources=$(docker volume ls -q --filter "$label" && docker ps -aq --filter "$label" &&
        docker network ls -q --filter "$label")
    [[ -z $resources ]] || fail 'Restore project already exists; refusing to reuse or clean it.'
    trap 'restore_compose down --volumes >> "$test_dir/restore-cleanup.log" 2>&1 || exit 1' EXIT
    trap 'exit 1' HUP INT TERM
    restore_compose up -d --no-build --pull never --wait --wait-timeout 90 identity-db incident-db
    # shellcheck source=deploy/app-vm/restore-roles.sh
    source "$APP_DEPLOY_DIR/restore-roles.sh"
    prepare_restore_roles restore_compose
    if restore_compose exec -T incident-db psql -X -U bootstrap_admin -d postgres -v target=incidents -f - \
        < "$APP_REPO_ROOT/db/restore/prepare-roles.sql" > "$test_dir/restore-role-refusal.log" 2>&1; then
        fail 'Restore preparation reused existing incident roles.'
    fi
    grep -q 'requires a fresh recovery instance' "$test_dir/restore-role-refusal.log" || fail 'Expected fresh-instance guard.'
    local database service table count metadata source_meta target_meta restored_count
    for database in identity incidents; do
        if [[ $database == identity ]]; then
            service='identity-db'; table=users
        else
            service='incident-db'; table=incidents
        fi
        local -a admin=(exec -T "$service" psql -X -U bootstrap_admin -d "cyberguard_$database" -v ON_ERROR_STOP=1 -Atc)
        count=$(compose "${admin[@]}" "SELECT count(*) FROM $table")
        metadata="SELECT max(version_id) FROM goose_db_version WHERE is_applied;
            SELECT pg_get_userbyid(datdba), ARRAY(SELECT a::text FROM unnest(datacl) a ORDER BY 1)
                FROM pg_database WHERE datname=current_database();
            SELECT pg_get_userbyid(relowner) FROM pg_class WHERE oid='public.$table'::regclass;"
        source_meta=$(compose "${admin[@]}" "$metadata")
        compose exec -T "$service" pg_dump -U bootstrap_admin -d "cyberguard_$database" -Fc > "$test_dir/$database.dump"
        restore_compose exec -T "$service" pg_restore -U bootstrap_admin --exit-on-error \
            --create -d postgres < "$test_dir/$database.dump"
        target_meta=$(restore_compose "${admin[@]}" "$metadata")
        [[ $source_meta == "$target_meta" ]] || fail "Restore metadata/owner/ACL mismatch: $database"

        # The ordinary bootstrap sets the Go/migrator passwords and enables LOGIN.
        # It does not supply application table grants. Do not run migrations or seed.
        restore_compose run --rm -T --no-deps --pull never "bootstrap-$database"
        restored_count=$(restore_runtime_query "bootstrap-$database" "${database}_runtime_password" \
            "$service" "go_$database" "$database" "SELECT count(*) FROM $table")
        [[ $count == "$restored_count" ]] || fail "Restored runtime row count mismatch: $database"
    done
    # No bootstrap-ai grants were applied on recovery. Test both allowed and
    # forbidden access with real SCRAM credentials (not SET ROLE as an admin).
    local -a ai=(bootstrap-ai ai_runtime_password incident-db python_incidents incidents)
    local query='SELECT ai_worker_ready(), (SELECT count(id) FROM assessment_jobs), (SELECT count(job_id) FROM assessment_results)'
    local expected actual
    expected=$(sql "$query")
    actual=$(restore_runtime_query "${ai[@]}" "$query")
    [[ $actual == "$expected" ]] || fail 'Restored AI runtime readiness or row counts differ.'
    if restore_runtime_query "${ai[@]}" 'SELECT claim_token FROM assessment_jobs LIMIT 0' > "$test_dir/restore-denied.log" 2>&1; then
        fail 'Restored AI runtime could read a private job column.'
    fi
    grep -q '42501: permission denied' "$test_dir/restore-denied.log" || fail 'Expected restored SQL privilege denial.'
)
