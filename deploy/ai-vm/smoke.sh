#!/usr/bin/env bash
# Explicit disposable fixture. No models/downloads/Azure or ordinary dev state used.
set -euo pipefail
# shellcheck source=deploy/ai-vm/common.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/common.sh"
mkdir -p "$AI_REPO_ROOT/.local"
AI_TEST_DIR=$(mktemp -d "$AI_REPO_ROOT/.local/ai-vm-smoke.XXXXXX")
chmod 0700 "$AI_TEST_DIR"
project=cyberguard-ai-vm-smoke-$(openssl rand -hex 6)
export AI_TEST_DIR AI_PROJECT_NAME=$project
export AI_IMAGE=cyberguard-ai:azure AI_KEV_IMAGE=cyberguard-ai:azure
export AI_SECRETS_DIR=$AI_TEST_DIR/secrets DOCKER_SECRETS_DIR=$AI_TEST_DIR/secrets
export AI_SECRET_GID DOCKER_SECRET_GID
AI_SECRET_GID=$(id -g); DOCKER_SECRET_GID=$AI_SECRET_GID
export AI_MODEL_DIR=$AI_TEST_DIR/models AI_BIND_IP=127.0.0.1 AI_PORT=${AI_TEST_PORT:-18443}
export AI_GO_URL=https://app-tls:8443 DOCKER_GO_IMAGE=cyberguard-go:local
unset COMPOSE_FILE COMPOSE_PROFILES COMPOSE_PROJECT_NAME COMPOSE_ENV_FILES
dc() {
    docker compose --env-file /dev/null --project-directory "$AI_REPO_ROOT" \
        --project-name "$project" -f "$AI_REPO_ROOT/compose.yaml" \
        -f "$AI_DEPLOY_DIR/compose.yaml" -f "$AI_DEPLOY_DIR/tests/smoke.compose.yaml" "$@"
}
cleanup() {
    local result=$?
    trap - EXIT
    if [[ $result != 0 ]]; then dc logs --no-color > "$AI_TEST_DIR/runtime.log" 2>&1 || true; fi
    dc down --volumes --remove-orphans > "$AI_TEST_DIR/cleanup.log" 2>&1 || result=1
    if [[ $result == 0 ]]; then
        rm -rf -- "$AI_TEST_DIR"
    else
        printf 'Disposable deployment check failed; protected diagnostics retained in %s\n' "$AI_TEST_DIR" >&2
    fi
    exit "$result"
}
trap cleanup EXIT
sh "$AI_REPO_ROOT/docker/create-secrets.sh" >/dev/null
sh "$AI_REPO_ROOT/docker/ai/create-ai-secrets.sh" >/dev/null
mkdir -m 0755 "$AI_TEST_DIR/certs" "$AI_MODEL_DIR" "$AI_TEST_DIR/control"
openssl req -x509 -newkey rsa:2048 -nodes -days 2 -subj /CN=Disposable-AI-smoke-CA \
    -addext 'keyUsage=critical,keyCertSign,cRLSign' \
    -keyout "$AI_TEST_DIR/certs/ca.key" -out "$AI_TEST_DIR/secrets/ca.crt" >/dev/null 2>&1
issue_certificate() {
    local name=$1 sans=$2
    openssl req -newkey rsa:2048 -nodes -subj "/CN=$name" -keyout "$AI_TEST_DIR/certs/$name.key" \
        -out "$AI_TEST_DIR/certs/$name.csr" >/dev/null 2>&1
    printf 'subjectAltName=%s\nextendedKeyUsage=serverAuth\n' "$sans" > "$AI_TEST_DIR/certs/$name.ext"
    openssl x509 -req -days 2 -in "$AI_TEST_DIR/certs/$name.csr" \
        -CA "$AI_TEST_DIR/secrets/ca.crt" -CAkey "$AI_TEST_DIR/certs/ca.key" -CAcreateserial \
        -extfile "$AI_TEST_DIR/certs/$name.ext" -out "$AI_TEST_DIR/certs/$name.crt" >/dev/null 2>&1
}
issue_certificate api DNS:ai.internal,DNS:ai-gateway
issue_certificate app DNS:app-tls
issue_certificate db DNS:incident-db
cp "$AI_TEST_DIR/certs/api."{crt,key} "$AI_TEST_DIR/secrets/"
chmod 0640 "$AI_TEST_DIR/secrets/"*
cat > "$AI_TEST_DIR/AppCaddyfile" <<'CADDY'
{
    admin off
    auto_https off
}
:8443 {
    tls /test-certs/app.crt /test-certs/app.key
    reverse_proxy go-api:8080
}
CADDY
printf 'postgres://python_incidents:%s@incident-db:5432/cyberguard_incidents?sslmode=verify-full&sslrootcert=/run/certs/ca.crt\n' \
    "$(cat "$AI_SECRETS_DIR/ai_runtime_password")" > "$AI_SECRETS_DIR/ai_runtime_url"
dc config --quiet
dc up -d --no-build --wait --wait-timeout 90 identity-db incident-db > "$AI_TEST_DIR/start.log" 2>&1
for tool in bootstrap-identity bootstrap-incidents migrate-identity migrate-incidents bootstrap-ai seed-demo; do
    dc run --rm "$tool" >> "$AI_TEST_DIR/setup.log" 2>&1
done
{
    dc up -d --no-build --wait --wait-timeout 60 go-api ai-api app-tls ai-gateway
    # Simulate daemon restarts: bypass dependency ordering and keep models cold.
    dc up -d --no-build --no-deps ai-kev ai-llm
    dc up -d --no-build --no-deps ai-worker
} >> "$AI_TEST_DIR/start.log" 2>&1
incident=$(dc run --rm --no-deps ai-api python /fixture/client.py create)
[[ $incident =~ ^[a-f0-9-]{36}$ ]] || fail 'Fixture incident creation failed.'
sql() { dc exec -T incident-db psql -X -U bootstrap_admin -d cyberguard_incidents -Atc "$1"; }
for _ in {1..4}; do
    [[ $(sql 'SELECT status || attempt_count FROM assessment_jobs') == queued0 ]] || fail 'A cold model consumed a queued job.'
    sleep 1
done
touch "$AI_TEST_DIR/control/kev-ready" "$AI_TEST_DIR/control/llm-ready"
for _ in {1..45}; do
    [[ $(sql 'SELECT status FROM assessment_jobs') == completed ]] && break
    sleep 1
done
[[ $(sql 'SELECT status FROM assessment_jobs') == completed ]] || fail 'Fixture job did not complete.'
dc run --rm --no-deps ai-api python /fixture/client.py read "$incident"
dc run --rm --no-deps ai-worker python -m cyberguard_ai.preflight
# Exercise the unchanged management/check path with a tiny, test-only manifest.
# The production manifest and real model directory are never rewritten or selected.
staged=$AI_TEST_DIR/staged/deploy/ai-vm
mkdir -p "$staged"
cp "$AI_DEPLOY_DIR/"{common.sh,manage.sh,prepare-models.sh,compose.yaml,Caddyfile} "$staged/"
mkdir "$AI_MODEL_DIR/fixture"
printf 'synthetic model fixture' > "$AI_MODEL_DIR/fixture/model.bin"
hash=$(sha256sum "$AI_MODEL_DIR/fixture/model.bin" | cut -d' ' -f1)
jq -n --arg hash "$hash" '{models:[{repo:"fixture/model",revision:("a"*40),directory:"fixture",files:[{name:"model.bin",size:23,sha256:$hash}]}]}' > "$staged/models.json"
printf '%s\n' "AI_PROJECT_NAME=$project" "AI_MODEL_DIR=$AI_MODEL_DIR" \
    "AI_SECRETS_DIR=$AI_SECRETS_DIR" "AI_SECRET_GID=$AI_SECRET_GID" \
    "AI_GO_URL=$AI_GO_URL" "AI_PORT=$AI_PORT" "AI_IMAGE=$AI_IMAGE" \
    "AI_KEV_IMAGE=$AI_KEV_IMAGE" > "$AI_TEST_DIR/config.env"
bash "$staged/manage.sh" --config "$AI_TEST_DIR/config.env" check
curl --fail --silent --show-error --noproxy '*' --cacert "$AI_SECRETS_DIR/ca.crt" \
    --resolve "ai.internal:$AI_PORT:127.0.0.1" "https://ai.internal:$AI_PORT/readyz" >/dev/null
if curl --fail --silent --noproxy '*' --cacert "$AI_SECRETS_DIR/ca.crt" \
    --resolve "wrong.internal:$AI_PORT:127.0.0.1" "https://wrong.internal:$AI_PORT/readyz" >/dev/null 2>&1; then
    fail 'TLS hostname mismatch was accepted.'
fi
# Only the TLS gateway may publish an AI port; models stay on an internal network.
dc config --format json | jq -e '
    .services["ai-api"].ports == null and .services["ai-worker"].ports == null and
    .services["ai-kev"].ports == null and .services["ai-llm"].ports == null and
    .networks.inference.internal == true' >/dev/null
dc restart ai-api ai-worker ai-gateway >> "$AI_TEST_DIR/start.log" 2>&1
dc up -d --no-build --wait --wait-timeout 60 ai-api ai-gateway >> "$AI_TEST_DIR/start.log" 2>&1
dc run --rm --no-deps ai-api python /fixture/client.py read "$incident"
printf '%s\n' 'Disposable deployment smoke passed: real Go/DB/worker/API, cross-service TLS, cold-model gating and restart persistence. Model responses were synthetic; no weights or real inference used.'
