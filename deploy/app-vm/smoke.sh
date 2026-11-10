#!/usr/bin/env bash
# Only fresh disposable projects, credentials, certificates and volumes are selected.
set -euo pipefail
umask 077
# shellcheck source=deploy/app-vm/common.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/common.sh"
for tool in docker curl jq openssl; do command -v "$tool" >/dev/null || fail "Missing prerequisite: $tool"; done
mkdir -p "$APP_REPO_ROOT/.local"
test_dir=$(mktemp -d "$APP_REPO_ROOT/.local/app-vm-smoke.XXXXXX")
config=$test_dir/config.env
project=cyberguard-app-vm-smoke-$(openssl rand -hex 6)
octet=$((40 + 16#$(openssl rand -hex 1) % 100))
mkdir -m 0700 "$test_dir/state" "$test_dir/secrets" "$test_dir/secrets/tls" "$test_dir/issuer"
cat > "$config" <<EOF
APP_PROJECT_NAME=$project
APP_STATE_DIR=$test_dir/state
APP_SECRETS_DIR=$test_dir/secrets
APP_SECRET_GID=$(id -g)
APP_PUBLIC_NAME=app.test
APP_GO_TLS_NAME=go.test
APP_DB_TLS_NAME=db.test
APP_PUBLIC_TLS=provided
APP_HTTP_PORT=${APP_TEST_HTTP_PORT:-18089}
APP_HTTPS_PORT=${APP_TEST_HTTPS_PORT:-18446}
APP_PRIVATE_PORT=${APP_TEST_PRIVATE_PORT:-18447}
APP_DB_PORT=${APP_TEST_DB_PORT:-15435}
APP_GO_IMAGE=cyberguard-go:local
APP_WEB_IMAGE=cyberguard-web:local
APP_ALLOW_DEMO_SEED=yes
APP_EDGE_SUBNET=172.27.$octet.0/28
APP_IDENTITY_SUBNET=172.27.$octet.16/28
APP_INCIDENT_SUBNET=172.27.$octet.32/28
EOF
load_config "$config"
manage() { bash "$APP_DEPLOY_DIR/manage.sh" --config "$config" "$@"; }
cleanup() {
    local result=$?
    trap - EXIT
    if [[ $result != 0 ]]; then compose logs --no-color > "$test_dir/runtime.log" 2>&1 || true; fi
    compose down --volumes --remove-orphans > "$test_dir/cleanup.log" 2>&1 || result=1
    if [[ $result == 0 ]]; then rm -rf -- "$test_dir"; else printf 'Protected failure diagnostics retained: %s\n' "$test_dir" >&2; fi
    exit "$result"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
bash "$APP_DEPLOY_DIR/create-secrets.sh" --config "$config" > "$test_dir/setup.log"
# A throwaway CA is generated ONLY inside this disposable test, never on runtime VMs.
openssl req -x509 -newkey rsa:2048 -nodes -days 3 -subj /CN=Disposable-App-Smoke-CA \
    -addext 'keyUsage=critical,keyCertSign,cRLSign' \
    -keyout "$test_dir/issuer/ca.key" -out "$APP_TLS_DIR/ca.crt" >/dev/null 2>&1
issue_certificate() {
    local name=$1 sans=$2
    openssl req -newkey rsa:2048 -nodes -subj "/CN=$name" -keyout "$APP_TLS_DIR/$name.key" \
        -out "$test_dir/issuer/$name.csr" >/dev/null 2>&1
    printf 'subjectAltName=%s\nextendedKeyUsage=serverAuth\n' "$sans" > "$test_dir/issuer/$name.ext"
    openssl x509 -req -days 3 -in "$test_dir/issuer/$name.csr" \
        -CA "$APP_TLS_DIR/ca.crt" -CAkey "$test_dir/issuer/ca.key" -CAcreateserial \
        -extfile "$test_dir/issuer/$name.ext" -out "$APP_TLS_DIR/$name.crt" >/dev/null 2>&1
}
issue_certificate identity DNS:identity-db
issue_certificate incident DNS:incident-db,DNS:db.test
issue_certificate go DNS:go.test
issue_certificate public DNS:app.test
chmod 0640 "$APP_TLS_DIR/"*
manage config >> "$test_dir/setup.log" 2>&1
manage db-up >> "$test_dir/setup.log" 2>&1
if manage up >> "$test_dir/expected-errors.log" 2>&1; then fail 'Unmigrated deployment was started.'; fi
{
    manage bootstrap
    manage migrate
    manage migrate
    manage bootstrap-ai
    manage seed-demo
    manage up
    manage maintenance-off
} >> "$test_dir/setup.log" 2>&1
base=https://$APP_PUBLIC_AUTHORITY
request() {
    local expected=$1 actual
    shift
    actual=$(curl --silent --show-error --noproxy '*' --max-time 15 --cacert "$APP_TLS_DIR/ca.crt" \
        --resolve "$APP_PUBLIC_NAME:$APP_HTTPS_PORT:$APP_BIND_IP" --dump-header "$test_dir/headers" \
        --output "$test_dir/body" --write-out '%{http_code}' "$@")
    [[ $actual == "$expected" ]] || fail "Expected HTTP $expected, got $actual (protected diagnostics retained)."
}
request 200 "$base/login"
request 200 "$base/incidents/new"
request 404 "$base/.env"
request 404 "$base/api/internal/ai/jobs/11111111-1111-4111-8111-111111111111/context"
request 502 "$base/ai/readyz"
for username in employee_a employee_b analyst admin; do
    jq -n --arg username "$username" --rawfile password "$APP_PROVISION_DIR/dev_${username}_password" \
        '{username:$username,password:($password|rtrimstr("\n"))}' > "$test_dir/payload"
    request 200 -H 'Content-Type: application/json' --data-binary "@$test_dir/payload" \
        -c "$test_dir/$username.cookies" "$base/api/auth/login"
    grep -qi '^Set-Cookie: __Host-cyberguard_session=' "$test_dir/headers"
    grep -qi '; Secure' "$test_dir/headers"
    grep -qi '; HttpOnly' "$test_dir/headers"
done
jq -n '{title:"DISPOSABLE Azure app fixture",description:"Synthetic deployment transport check.",affected_system:"original fixture system"}' > "$test_dir/payload"
request 201 -b "$test_dir/employee_a.cookies" -H 'Content-Type: application/json' \
    --data-binary "@$test_dir/payload" "$base/api/incidents"
incident=$(jq -er '.incident.id' "$test_dir/body")
grep -qi "^Location: /api/incidents/$incident" "$test_dir/headers"
request 404 -b "$test_dir/employee_b.cookies" "$base/api/incidents/$incident"
request 403 -b "$test_dir/employee_a.cookies" -H 'Content-Type: application/json' \
    -H 'Origin: https://hostile.example' -H 'Sec-Fetch-Site: cross-site' \
    --data-binary "@$test_dir/payload" "$base/api/incidents"
request 403 -b "$test_dir/employee_a.cookies" "$base/api/admin/users"
request 200 -b "$test_dir/admin.cookies" "$base/api/admin/users"
request 200 -b "$test_dir/employee_a.cookies" "$base/api/notifications"
manage maintenance-on >> "$test_dir/setup.log" 2>&1
request 503 -b "$test_dir/employee_a.cookies" -H 'Content-Type: application/json' \
    --data-binary "@$test_dir/payload" "$base/api/incidents"
request 200 -b "$test_dir/employee_a.cookies" "$base/api/incidents/$incident"
request 404 -X POST "$base/api/internal/ai/jobs/11111111-1111-4111-8111-111111111111/context"
if manage migrate >> "$test_dir/expected-errors.log" 2>&1; then fail 'Migration ran with Go active.'; fi
manage quiesce >> "$test_dir/setup.log" 2>&1
if manage up >> "$test_dir/expected-errors.log" 2>&1; then fail 'Upgrade startup guard was bypassed.'; fi
{
    manage migrate
    manage up
    manage maintenance-off
} >> "$test_dir/setup.log" 2>&1
request 200 -b "$test_dir/employee_a.cookies" "$base/api/incidents/$incident"
# Full recreation retains database contents, sessions and Caddy's persistent state.
compose down >> "$test_dir/setup.log" 2>&1
manage up >> "$test_dir/setup.log" 2>&1
request 200 -b "$test_dir/employee_a.cookies" "$base/api/incidents/$incident"
for database in identity-db incident-db; do
    compose stop "$database" >> "$test_dir/setup.log" 2>&1
    request 503 "$base/api/readyz"
    request 200 "$base/api/healthz"
    compose up -d --no-build --pull never --wait "$database" >> "$test_dir/setup.log" 2>&1
    manage check >> "$test_dir/setup.log" 2>&1
done
compose --profile tools config --format json | jq -e '
    .services["identity-db"].ports == null and .services["go-api"].ports == null and
    .networks.identity.internal == true and
    ([.services["go-api"].secrets[].source] | index("identity_admin_password") == null) and
    ([.services.web.volumes[] | select(.target == "/data")][0].type == "volume")' >/dev/null
printf '%s\n' 'Application deployment smoke passed: real public/private TLS, DB provisioning, Secure sessions, maintenance/migration gates, AI-offline operation and persistence. Azure/ACME/real inference not tested.'
