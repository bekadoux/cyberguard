#!/usr/bin/env bash
# Fast non-privileged safety tests; no Docker/network/real secrets required.
set -euo pipefail
root=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
for script in "$root/deploy/app-vm/"*.sh; do bash -n "$script"; done
shellcheck -x -P "$root" "$root/deploy/app-vm/"*.sh "$root/deploy/app-vm/postgres/"*.sh "$root/deploy/app-vm/tests/"*.sh
mkdir -p "$root/.local"
temporary=$(mktemp -d "$root/.local/app-vm-test.XXXXXX")
trap 'rm -rf -- "$temporary"' EXIT
mkdir -m 0700 "$temporary/secrets" "$temporary/state"
config=$temporary/config.env
printf 'APP_SECRETS_DIR=%s\nAPP_STATE_DIR=%s\nAPP_SECRET_GID=%s\n' "$temporary/secrets" "$temporary/state" "$(id -g)" > "$config"
load() { bash -c 'source "$1/deploy/app-vm/common.sh"; load_config "$2"' bash "$root" "$1"; }
reject() { if "$@" > "$temporary/error" 2>&1; then echo 'Expected rejection' >&2; exit 1; fi; }
APP_BIND_IP=0.0.0.0 load "$config"
for value in 'APP_BIND_IP=0.0.0.0' 'APP_AI_IP=8.8.8.8' 'APP_PUBLIC_NAME=app;bad' 'APP_HTTP_PORT=0' 'APP_STATE_DIR=/var/../etc' 'UNKNOWN=yes' 'APP_SECRET_GID=bad' 'APP_EDGE_SUBNET=172.28.40.1/28'; do
    cat "$config" > "$temporary/bad.env"
    printf '%s\n' "$value" >> "$temporary/bad.env"
    reject load "$temporary/bad.env"
done
# Deliberately malicious literal input, never evaluated.
# shellcheck disable=SC2016
printf 'APP_PUBLIC_NAME=$(touch %s)\n' "$temporary/executed" > "$temporary/bad.env"
reject load "$temporary/bad.env"
[[ ! -e $temporary/executed ]]
# Unknown keys must not execute array-subscript or shell input either.
# shellcheck disable=SC2016
printf '$(touch %s)=value\n' "$temporary/executed" > "$temporary/bad.env"
reject load "$temporary/bad.env"
[[ ! -e $temporary/executed ]]
printf 'APP_HTTP_PORT=80\nAPP_HTTP_PORT=81\n' > "$temporary/bad.env"
reject load "$temporary/bad.env"
bash "$root/deploy/app-vm/create-secrets.sh" --config "$config"
before=$(find "$temporary/secrets/credentials" -type f -exec sha256sum {} + | sort)
reject bash "$root/deploy/app-vm/create-secrets.sh" --config "$config"
[[ $before == "$(find "$temporary/secrets/credentials" -type f -exec sha256sum {} + | sort)" ]]
grep -q 'sslmode=verify-full' "$temporary/secrets/credentials/runtime/ai_runtime_url"
[[ $(stat -c %a "$temporary/secrets/credentials/runtime/ai_internal_token") == 640 ]]
[[ $(cat "$temporary/secrets/credentials/provisioning/identity_runtime_password") != "$(cat "$temporary/secrets/credentials/provisioning/incidents_runtime_password")" ]]
ln -s "$temporary/secrets" "$temporary/linked"
printf 'APP_SECRETS_DIR=%s\n' "$temporary/linked" > "$temporary/bad.env"
reject load "$temporary/bad.env"
mkdir "$temporary/bin"
cat > "$temporary/bin/docker" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$APP_TEST_COMMANDS"
printf '%s\n' "$DOCKER_GO_IMAGE" >> "$APP_TEST_IMAGES"
if [[ ${APP_TEST_PS_FAIL:-no} == yes && " $* " == *' ps --status running --services '* ]]; then exit 7; fi
SH
chmod 0755 "$temporary/bin/docker"
export APP_TEST_COMMANDS=$temporary/commands APP_TEST_IMAGES=$temporary/images
COMPOSE_FILE=unrelated.yaml COMPOSE_PROFILES=tools COMPOSE_PROJECT_NAME=ordinary \
    APP_PROJECT_NAME=ordinary DOCKER_GO_IMAGE=unrelated \
    PATH="$temporary/bin:$PATH" bash "$root/deploy/app-vm/manage.sh" --config "$config" config >/dev/null
grep -Fq -- '--env-file /dev/null' "$APP_TEST_COMMANDS"
grep -Fq -- '--project-name cyberguard-app-vm' "$APP_TEST_COMMANDS"
grep -Fq 'cyberguard-go:azure' "$APP_TEST_IMAGES"
[[ $(wc -l < "$APP_TEST_COMMANDS") == 1 ]]
chmod 0666 "$temporary/secrets/credentials/runtime/identity_runtime_url"
reject env PATH="$temporary/bin:$PATH" bash "$root/deploy/app-vm/manage.sh" --config "$config" up
[[ $(wc -l < "$APP_TEST_COMMANDS") == 1 ]]
chmod 0640 "$temporary/secrets/credentials/runtime/identity_runtime_url"
PATH="$temporary/bin:$PATH" bash "$root/deploy/app-vm/manage.sh" --config "$config" quiesce >/dev/null
[[ -f $temporary/state/maintenance/public/enabled && -f $temporary/state/maintenance/public/upgrade-blocked ]]
grep -Fq 'stop private-go go-api' "$APP_TEST_COMMANDS"
if grep -Eq '(down|--volumes|prune)' "$APP_TEST_COMMANDS"; then echo 'Destructive management action' >&2; exit 1; fi
[[ $before == "$(find "$temporary/secrets/credentials" -type f -exec sha256sum {} + | sort)" ]]
# Only this negative state-inspection test mocks certificate tooling. Real certificate
# verification, hostname mismatches and HBA behavior are covered by app-vm-smoke.
mkdir -m 0700 "$temporary/secrets/tls"
for name in ca.crt identity.crt identity.key incident.crt incident.key go.crt go.key; do
    printf 'synthetic input; not a certificate\n' > "$temporary/secrets/tls/$name"
    chmod 0640 "$temporary/secrets/tls/$name"
done
cat > "$temporary/bin/openssl" <<'SH'
#!/usr/bin/env bash
exit 0
SH
chmod 0755 "$temporary/bin/openssl"
original_url=$(cat "$temporary/secrets/credentials/runtime/identity_runtime_url")
printf '%s\n' "${original_url/sslmode=verify-full/sslmode=disable}" > "$temporary/secrets/credentials/runtime/identity_runtime_url"
reject env PATH="$temporary/bin:$PATH" bash "$root/deploy/app-vm/manage.sh" --config "$config" up
grep -Fq 'Credential URL must use its scoped role/endpoint and verified TLS' "$temporary/error"
printf '%s\n' "$original_url" > "$temporary/secrets/credentials/runtime/identity_runtime_url"
reject env APP_TEST_PS_FAIL=yes PATH="$temporary/bin:$PATH" bash "$root/deploy/app-vm/manage.sh" --config "$config" migrate
grep -Fq 'Could not inspect running services' "$temporary/error"
if grep -q ' run ' "$APP_TEST_COMMANDS"; then echo 'Database tool ran after state inspection failed' >&2; exit 1; fi
[[ $before == "$(find "$temporary/secrets/credentials" -type f -exec sha256sum {} + | sort)" ]]
printf '%s\n' 'Application VM literal config, path safety and non-overwriting TLS credential generation passed.'
