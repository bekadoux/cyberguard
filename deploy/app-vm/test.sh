#!/usr/bin/env bash
# Fast non-privileged safety tests; no Docker/network/real secrets required.
set -euo pipefail
root=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
for script in "$root/deploy/app-vm/"*.sh; do bash -n "$script"; done
shellcheck -x -P "$root" "$root/deploy/app-vm/"*.sh
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
printf '%s\n' 'Application VM literal config, path safety and non-overwriting TLS credential generation passed.'
