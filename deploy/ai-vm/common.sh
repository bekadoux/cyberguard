#!/usr/bin/env bash
# Shared literal configuration reader. This file performs no privileged actions.
set -euo pipefail

AI_DEPLOY_DIR=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
AI_REPO_ROOT=$(CDPATH='' cd -- "$AI_DEPLOY_DIR/../.." && pwd)

fail() { printf '%s\n' "$*" >&2; exit 1; }

load_config() {
    local file=$1 key value line
    [[ -f $file && ! -L $file ]] || fail 'A regular configuration file is required.'
    # Explicit config wins over inherited Compose/application settings.
    unset COMPOSE_FILE COMPOSE_PROFILES COMPOSE_PROJECT_NAME COMPOSE_ENV_FILES
    AI_PROJECT_NAME=cyberguard-ai-vm
    AI_MODEL_DIR=/var/lib/cyberguard-ai/models
    AI_SECRETS_DIR=/etc/cyberguard/ai/secrets
    AI_SECRET_GID=1000
    AI_BIND_IP=127.0.0.1
    AI_PORT=8443
    AI_NETWORK_APPROVED=no
    AI_TLS_NAME=ai.internal
    AI_GO_URL=https://app.internal:8443
    AI_IMAGE=cyberguard-ai:azure
    AI_KEV_IMAGE=cyberguard-kev:azure
    local -A seen=()
    while IFS= read -r line || [[ -n $line ]]; do
        [[ -z $line || $line == \#* ]] && continue
        [[ $line == *=* ]] || fail 'Expected literal KEY=value configuration.'
        key=${line%%=*}; value=${line#*=}
        case "$key" in
            AI_PROJECT_NAME|AI_MODEL_DIR|AI_SECRETS_DIR|AI_SECRET_GID|AI_BIND_IP|AI_PORT|AI_NETWORK_APPROVED|AI_TLS_NAME|AI_GO_URL|AI_IMAGE|AI_KEV_IMAGE) ;;
            *) fail 'Unknown configuration key.' ;;
        esac
        [[ ! ${seen[$key]+exists} ]] || fail "Duplicate configuration key: $key"
        [[ -n $value && $value != *[[:space:]\'\"\`\$\\]* ]] || fail "Invalid literal value: $key"
        seen[$key]=1
        printf -v "$key" '%s' "$value"
    done < "$file"
    [[ $AI_PROJECT_NAME =~ ^cyberguard-ai-vm(-[a-z0-9-]+)?$ ]] || fail 'Use a dedicated cyberguard-ai-vm project name.'
    [[ $AI_MODEL_DIR == /* && $AI_SECRETS_DIR == /* ]] || fail 'State paths must be absolute.'
    [[ $AI_MODEL_DIR != / && $AI_SECRETS_DIR != / ]] || fail 'Root is not a state directory.'
    for value in "$AI_MODEL_DIR" "$AI_SECRETS_DIR"; do
        [[ $value =~ ^/[a-zA-Z0-9/_.-]+$ && $value != */../* && $value != */.. && $value != */./* && $value != */. ]] || fail 'Use normalized absolute state paths.'
        if [[ $value == "$AI_REPO_ROOT" || $value == "$AI_REPO_ROOT/"* ]]; then
            [[ $value == "$AI_REPO_ROOT/.local/"* ]] || fail 'Keep runtime state outside the checkout or under its ignored .local directory.'
        fi
    done
    [[ $AI_SECRET_GID =~ ^[0-9]{1,10}$ ]] || fail 'Invalid secret group ID.'
    if [[ ! $AI_PORT =~ ^[0-9]{1,5}$ ]] || ((10#$AI_PORT < 1024 || 10#$AI_PORT > 65535)); then
        fail 'AI_PORT must be 1024–65535.'
    fi
    [[ $AI_NETWORK_APPROVED == yes || $AI_NETWORK_APPROVED == no ]] || fail 'Invalid network acknowledgement.'
    [[ $AI_TLS_NAME =~ ^[a-zA-Z0-9][a-zA-Z0-9.-]*$ ]] || fail 'Invalid TLS DNS name.'
    [[ $AI_GO_URL =~ ^https://[a-zA-Z0-9.-]+(:[0-9]{1,5})?$ ]] || fail 'Go requires an HTTPS origin without credentials or path.'
    local a b c d octet
    IFS=. read -r a b c d <<< "$AI_BIND_IP"
    for octet in "$a" "$b" "$c" "$d"; do
        if [[ ! $octet =~ ^(0|[1-9][0-9]{0,2})$ ]] || ((octet > 255)); then fail 'Invalid bind IPv4 address.'; fi
    done
    [[ $AI_BIND_IP == 127.0.0.1 || $a == 10 || ($a == 172 && $b -ge 16 && $b -le 31) || ($a == 192 && $b == 168) ]] || fail 'Bind only to loopback or an RFC1918 IPv4 address.'
    [[ $AI_IMAGE =~ ^[a-zA-Z0-9][a-zA-Z0-9._/:@-]+$ && $AI_KEV_IMAGE =~ ^[a-zA-Z0-9][a-zA-Z0-9._/:@-]+$ ]] || fail 'Invalid image reference.'
    export AI_PROJECT_NAME AI_MODEL_DIR AI_SECRETS_DIR AI_SECRET_GID AI_BIND_IP AI_PORT
    export AI_NETWORK_APPROVED AI_TLS_NAME AI_GO_URL AI_IMAGE AI_KEV_IMAGE
}

compose() {
    # /dev/null prevents the repository's host-oriented .env from being loaded.
    docker compose --env-file /dev/null --project-directory "$AI_REPO_ROOT" \
        --project-name "$AI_PROJECT_NAME" -f "$AI_DEPLOY_DIR/compose.yaml" "$@"
}
