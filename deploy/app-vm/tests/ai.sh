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
    chmod 0640 "$test_dir/ai_runtime_url"
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
    [[ $(curl --fail --silent --noproxy '*' --cacert "$APP_TLS_DIR/ca.crt" \
        --resolve "$APP_GO_TLS_NAME:$APP_PRIVATE_PORT:$APP_BIND_IP" --header "@$test_dir/service-header" \
        "https://$APP_GO_TLS_NAME:$APP_PRIVATE_PORT/internal/ai/jobs/$job/context" | jq -er '.affected_system') == 'original fixture system' ]]
    ai_compose up -d --no-build --pull never ai-worker
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
restore_check() {
    local database service owner table count
    for database in identity incidents; do
        if [[ $database == identity ]]; then service='identity-db'; else service='incident-db'; fi
        owner=${database}_migrator
        if [[ $database == identity ]]; then table=users; else table=incidents; fi
        count=$(compose exec -T "$service" psql -X -U bootstrap_admin -d "cyberguard_$database" -Atc "SELECT count(*) FROM $table")
        compose exec -T "$service" pg_dump -U bootstrap_admin -d "cyberguard_$database" -Fc > "$test_dir/$database.dump"
        compose exec -T "$service" createdb -U bootstrap_admin -O "$owner" disposable_restore
        compose exec -T "$service" pg_restore -U bootstrap_admin --exit-on-error -d disposable_restore < "$test_dir/$database.dump"
        [[ $(compose exec -T "$service" psql -X -U bootstrap_admin -d disposable_restore -Atc 'SELECT max(version_id) FROM goose_db_version WHERE is_applied') == "$(expected_restore_version "$database")" ]]
        [[ $(compose exec -T "$service" psql -X -U bootstrap_admin -d disposable_restore -Atc "SELECT count(*) FROM $table") == "$count" ]]
        compose exec -T "$service" dropdb -U bootstrap_admin disposable_restore
    done
}
expected_restore_version() {
    if [[ $1 == identity ]]; then printf 3; else printf 5; fi
}
