#!/usr/bin/env bash
# Owns a fresh, disposable Compose project. Never targets ordinary development data.
set -euo pipefail
umask 077
for tool in docker curl jq openssl; do
    command -v "$tool" > /dev/null || { echo "Missing smoke-test prerequisite: $tool" >&2; exit 1; }
done
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"
# Parent make command-line variables must not redirect nested targets to an ordinary
# project/secrets directory or enable tooling profiles during runtime startup.
unset MAKEFLAGS MFLAGS MAKEOVERRIDES
export COMPOSE_PROFILES=
export COMPOSE_PROJECT_NAME="cyberguard-smoke-$$"
state="$root/.local/$COMPOSE_PROJECT_NAME"
mkdir -p "$root/.local"
mkdir -m 700 "$state"
export DOCKER_SECRETS_DIR="$state/secrets" DOCKER_SECRET_GID="$(id -g)"
export DOCKER_IDENTITY_PORT="${DOCKER_TEST_IDENTITY_PORT:-15433}"
export DOCKER_INCIDENT_PORT="${DOCKER_TEST_INCIDENT_PORT:-15434}"
export DOCKER_GO_PORT="${DOCKER_TEST_GO_PORT:-18080}"
export DOCKER_WEB_PORT="${DOCKER_TEST_WEB_PORT:-18088}"
base="http://127.0.0.1:$DOCKER_WEB_PORT"
compose_files=(-f compose.yaml -f compose.dev.yaml)
compose_command='docker compose -f compose.yaml -f compose.dev.yaml'
if [[ ${DOCKER_TEST_AI:-0} == 1 ]]; then
    compose_files+=(-f compose.ai.yaml)
    compose_command+=' -f compose.ai.yaml'
    export AI_MODE=fake
fi
compose() { docker compose "${compose_files[@]}" "$@"; }
run_make() { make "COMPOSE=$compose_command" "$@"; }
cleanup() {
    result=$?
    trap - EXIT
    set +e
    if [[ ${DOCKER_KEEP_TEST_ENV:-0} == 1 ]]; then
        printf 'Kept test project %s and sensitive state %s. Stop/remove it explicitly when finished.\n' "$COMPOSE_PROJECT_NAME" "$state"
    else
        # --volumes is intentionally confined to the fresh project created above.
        if ! compose down --volumes > /dev/null 2>&1; then
            echo 'Could not remove the disposable test project; inspect it before deleting secret files.' >&2
            result=1
        fi
        if [[ $result == 0 ]]; then
            rm -f -- "$state/secrets/"* "$state/"*.cookies "$state/body" "$state/headers" \
                "$state/payload" "$state/config.json" "$state/service.log"
            rmdir -- "$state/secrets" "$state"
        else
            printf 'Smoke test failed; sensitive diagnostic files remain in %s.\n' "$state" >&2
        fi
    fi
    exit "$result"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

request() {
    local expected=$1 actual
    shift
    actual=$(curl --silent --show-error --max-time 15 --output "$state/body" \
        --dump-header "$state/headers" --write-out '%{http_code}' "$@")
    if [[ $actual != "$expected" ]]; then
        printf 'Expected HTTP %s, got %s; response kept in the test state directory.\n' "$expected" "$actual" >&2
        return 1
    fi
}
cookie_free() { ! grep -qi '^Set-Cookie:' "$state/headers"; }
wait_ready() {
    curl --fail --silent --show-error --max-time 5 --retry 30 --retry-max-time 45 \
        --retry-all-errors --retry-delay 1 "$base/api/readyz" > "$state/body"
}

run_make docker-secrets
if [[ ${DOCKER_TEST_AI:-0} == 1 ]]; then
    sh docker/ai/create-ai-secrets.sh
    compose build ai-api ai-worker
fi
run_make docker-config-check docker-caddy-check docker-test-readiness docker-test-web
run_make docker-db-up docker-bootstrap docker-migrate docker-seed-demo
if [[ ${DOCKER_TEST_AI:-0} == 1 ]]; then
    compose --profile tools run --rm bootstrap-ai
fi
run_make docker-migrate docker-seed-demo
run_make docker-up
expected_services=4
if [[ ${DOCKER_TEST_AI:-0} == 1 ]]; then expected_services=6; fi
[[ $(compose ps --status running --services | wc -l) == "$expected_services" ]]
request 200 "$base/__gateway_health"
request 200 "$base/api/readyz"
bash docker/test-web.sh "$base" "${DOCKER_TEST_AI:-0}"
request 401 "$base/api/incidents"
cookie_free

for username in employee_a employee_b analyst admin; do
    jq -n --arg username "$username" \
        --rawfile password "$DOCKER_SECRETS_DIR/dev_${username}_password" \
        '{username:$username,password:($password|rtrimstr("\n"))}' > "$state/payload"
    request 200 -H 'Content-Type: application/json' --data-binary "@$state/payload" \
        -c "$state/$username.cookies" "$base/api/auth/login"
    grep -qi '^Set-Cookie: cyberguard_session=' "$state/headers"
    grep -qi 'HttpOnly' "$state/headers"
    jq -e --arg username "$username" '.user.username == $username and (.user | has("password_hash") | not)' "$state/body" > /dev/null
done

jq -n '{title:"Docker smoke report",description:("界"*10000),affected_system:"Work email"}' > "$state/payload"
request 201 -b "$state/employee_a.cookies" -H 'Content-Type: application/json' \
    -H 'Origin: http://127.0.0.1:5173' -H 'Sec-Fetch-Site: cross-site' \
    --data-binary "@$state/payload" "$base/api/incidents"
cookie_free
incident=$(jq -er '.incident.id' "$state/body")
grep -qi "^Location: /api/incidents/$incident" "$state/headers"
jq -e '.incident.status == "submitted" and (.incident | has("severity") | not) and (.incident.description | length) == 10000 and (has("job_id") | not)' "$state/body" > /dev/null

request 200 -b "$state/employee_a.cookies" "$base/api/incidents/$incident"
cookie_free
request 404 -b "$state/employee_b.cookies" "$base/api/incidents/$incident"
cookie_free
request 200 -b "$state/employee_b.cookies" "$base/api/incidents"
jq -e '.incidents == []' "$state/body" > /dev/null
for username in analyst admin; do
    request 200 -b "$state/$username.cookies" "$base/api/incidents/$incident"
    jq -e '.incident | has("severity") and .severity == null' "$state/body" > /dev/null
    cookie_free
done
request 403 -b "$state/employee_a.cookies" -H 'Content-Type: application/json' \
    -H 'Origin: https://hostile.example' -H 'Sec-Fetch-Site: cross-site' \
    --data-binary "@$state/payload" "$base/api/incidents"
cookie_free
request 200 -X OPTIONS -H 'Origin: http://127.0.0.1:5173' \
    -H 'Access-Control-Request-Method: POST' -H 'Access-Control-Request-Headers: Content-Type' \
    "$base/api/incidents"
grep -qi '^Access-Control-Allow-Origin: http://127.0.0.1:5173' "$state/headers"
grep -qi '^Access-Control-Allow-Credentials: true' "$state/headers"

for path in /missing /api /api/not-found /internal /api/internal/auth/authorize \
    /api/%69nternal/auth/authorize /api//internal/auth/authorize /ai/internal/test; do
    request 404 --path-as-is "$base$path"
done
if [[ ${DOCKER_TEST_AI:-0} == 1 ]]; then
    request 401 "$base/ai/incidents/$incident/assessment"
    for username in employee_a employee_b; do
        request 403 -b "$state/$username.cookies" "$base/ai/incidents/$incident/assessment"
        cookie_free
    done
    for attempt in {1..30}; do
        request 200 -b "$state/analyst.cookies" "$base/ai/incidents/$incident/assessment"
        if jq -e '.job.status == "completed"' "$state/body" > /dev/null; then break; fi
        if jq -e '.job.status == "failed"' "$state/body" > /dev/null; then
            echo 'AI fixture worker failed; sensitive diagnostics remain private.' >&2
            exit 1
        fi
        sleep 1
    done
    jq -e --arg id "$incident" '.job.incident_id == $id and .job.status == "completed" and .result.source_kind == "fake" and .result.assessment.is_fake == true' "$state/body" > /dev/null
    for username in analyst admin; do
        request 200 -b "$state/$username.cookies" "$base/ai/incidents/$incident/assessment"
        cookie_free
        jq -n '{title:"Staff own fixture report",description:"Synthetic authorization regression."}' > "$state/payload"
        request 201 -b "$state/$username.cookies" -H 'Content-Type: application/json' \
            --data-binary "@$state/payload" "$base/api/incidents"
        own_incident=$(jq -er '.incident.id' "$state/body")
        request 200 -b "$state/$username.cookies" "$base/ai/incidents/$own_incident/assessment"
        jq -e --arg id "$own_incident" '.job.incident_id == $id' "$state/body" > /dev/null
        cookie_free
    done
else
    request 503 "$base/ai/incidents/assessment"
    jq -e '.error.code == "ai_unavailable"' "$state/body" > /dev/null
fi
cookie_free

# Four successful attempts above, one failed attempt, then a spoofed sixth attempt.
request 401 -H 'Content-Type: application/json' -b "$state/employee_a.cookies" \
    --data '{"username":"employee_a","password":"incorrect-password"}' "$base/api/auth/login"
cookie_free
request 429 -H 'Content-Type: application/json' -H 'X-Forwarded-For: 203.0.113.10' \
    --data '{"username":"employee_a","password":"incorrect-password"}' "$base/api/auth/login"
cookie_free

compose exec -T go-api sh -c 'test "$(id -u)" = 10001 && test -r /run/secrets/identity_runtime_url && test ! -e /run/secrets/identity_migration_url && test ! -e /run/secrets/identity_admin_password'
compose exec -T web sh -c 'test "$(id -u)" = 10001 && test ! -e /run/secrets/identity_runtime_url && test -r /srv/index.html && test ! -e /srv/src && test ! -e /srv/.env && test -z "$(find /srv -name "*.map")" && ! command -v node && ! command -v npm'

if [[ ${DOCKER_TEST_GO_INTEGRATION:-0} == 1 ]]; then
    export TEST_IDENTITY_DATABASE_URL="$(cat "$DOCKER_SECRETS_DIR/identity_runtime_host_url")"
    export TEST_INCIDENT_DATABASE_URL="$(cat "$DOCKER_SECRETS_DIR/incidents_runtime_host_url")"
    export TEST_IDENTITY_MIGRATION_DATABASE_URL="$(cat "$DOCKER_SECRETS_DIR/identity_migration_host_url")"
    export TEST_INCIDENT_MIGRATION_DATABASE_URL="$(cat "$DOCKER_SECRETS_DIR/incidents_migration_host_url")"
    run_make go-test-integration
    unset TEST_IDENTITY_DATABASE_URL TEST_INCIDENT_DATABASE_URL \
        TEST_IDENTITY_MIGRATION_DATABASE_URL TEST_INCIDENT_MIGRATION_DATABASE_URL
fi

# Recreate every container/network without deleting data volumes or rerunning setup.
compose down
run_make docker-up
request 200 -b "$state/employee_a.cookies" "$base/api/auth/me"
cookie_free
request 200 -b "$state/employee_a.cookies" "$base/api/incidents/$incident"
cookie_free

for database in identity-db incident-db; do
    compose stop "$database"
    request 503 "$base/api/readyz"
    request 200 "$base/api/healthz"
    if [[ $database == identity-db ]]; then
        request 503 -b "$state/employee_a.cookies" "$base/api/auth/me"
    else
        request 503 -b "$state/employee_a.cookies" "$base/api/incidents"
    fi
    cookie_free
    compose start "$database"
    wait_ready
    request 200 -b "$state/employee_a.cookies" "$base/api/incidents/$incident"
done

compose stop go-api
request 200 "$base/login"
request 200 "$base/__gateway_health"
request 502 "$base/api/readyz"
container=$(compose ps -aq go-api)
[[ $(docker inspect --format '{{.State.ExitCode}}' "$container") == 0 ]]
compose logs --no-color go-api > "$state/service.log"
grep -q 'API stopped gracefully' "$state/service.log"
compose start go-api
wait_ready
request 204 -b "$state/employee_a.cookies" -X POST "$base/api/auth/logout"
grep -qi '^Set-Cookie: cyberguard_session=' "$state/headers"
request 401 -b "$state/employee_a.cookies" "$base/api/auth/me"
cookie_free

if [[ ${DOCKER_TEST_FRONTEND:-0} == 1 ]]; then
    # Real credentials stay in standalone browser processes, never test reports.
    export E2E_PACKAGED_ORIGIN="$base" E2E_INCIDENT_SECRETS_DIR="$DOCKER_SECRETS_DIR"
    export E2E_TEST_ASSESSMENTS="${DOCKER_TEST_AI:-0}"
    run_make frontend-test-e2e frontend-test-packaged
    unset E2E_PACKAGED_ORIGIN E2E_INCIDENT_SECRETS_DIR E2E_TEST_ASSESSMENTS
fi

compose config --format json > "$state/config.json"
compose logs --no-color > "$state/service.log"
if grep -Fq -f <(cat "$DOCKER_SECRETS_DIR/"*) "$state/config.json" "$state/service.log"; then
    echo 'A secret value was exposed in rendered configuration or service logs' >&2
    exit 1
fi
if grep -Fq -f <(awk '$6 == "cyberguard_session" { print $7 }' "$state/"*.cookies) "$state/service.log"; then
    echo 'A session cookie was exposed in service logs' >&2
    exit 1
fi
printf '%s\n' 'Docker smoke passed: packaged SPA/cache/routing, setup, gateway/auth/access, large Unicode report, persistence, outages, graceful shutdown and secret isolation.'
