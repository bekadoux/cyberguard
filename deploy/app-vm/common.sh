#!/usr/bin/env bash
# Literal configuration and safety checks; sourcing performs no host changes.
# APP_* inputs are assigned from the allowlisted template using printf -v.
# shellcheck disable=SC2153
set -euo pipefail
APP_DEPLOY_DIR=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
APP_REPO_ROOT=$(CDPATH='' cd -- "$APP_DEPLOY_DIR/../.." && pwd)
fail() { printf '%s\n' "$*" >&2; exit 1; }

ipv4() {
    local value=$1 octet
    [[ $value =~ ^([0-9]+\.){3}[0-9]+$ ]] || return 1
    local -a octets
    IFS=. read -r -a octets <<< "$value"
    for octet in "${octets[@]}"; do
        [[ $octet =~ ^(0|[1-9][0-9]{0,2})$ ]] && ((10#$octet <= 255)) || return 1
    done
}
private_ipv4() {
    ipv4 "$1" || return 1
    local a b rest
    IFS=. read -r a b rest <<< "$1"
    [[ $a == 10 || ($a == 172 && $b -ge 16 && $b -le 31) || ($a == 192 && $b == 168) ]]
}
safe_directory() {
    local directory=$1
    [[ -d $directory && ! -L $directory && $(stat -c %a "$directory") == 700 && $(stat -c %g "$directory") == "$APP_SECRET_GID" ]] || fail "Expected a real 0700 directory with the configured group: $directory"
}
secret_file() {
    local file=$1
    [[ -f $file && ! -L $file && -s $file && $(stat -c %a "$file") == 640 && $(stat -c %g "$file") == "$APP_SECRET_GID" ]] || fail "Expected a nonempty regular 0640 credential/certificate with the configured group: ${file##*/}"
}
load_config() {
    local file=$1 line key value subnet base last
    [[ -f $file && ! -L $file ]] || fail 'A regular configuration file is required.'
    unset COMPOSE_FILE COMPOSE_PROFILES COMPOSE_PROJECT_NAME COMPOSE_ENV_FILES
    # All allowed values start from checked-in defaults, never inherited APP_* values.
    local defaults=$APP_DEPLOY_DIR/config.env.example
    local -A allowed=() seen=()
    while IFS= read -r line || [[ -n $line ]]; do
        [[ -z $line || $line == \#* ]] && continue
        key=${line%%=*}; value=${line#*=}
        allowed[$key]=1
        printf -v "$key" '%s' "$value"
    done < "$defaults"
    while IFS= read -r line || [[ -n $line ]]; do
        [[ -z $line || $line == \#* ]] && continue
        [[ $line == *=* ]] || fail 'Expected literal KEY=value configuration.'
        key=${line%%=*}; value=${line#*=}
        [[ ${allowed[$key]+exists} ]] || fail 'Unknown configuration key.'
        [[ ! ${seen[$key]+exists} ]] || fail "Duplicate configuration key: $key"
        [[ -n $value && $value != *[[:space:]\'\"\`\$\\]* ]] || fail "Invalid literal value: $key"
        seen[$key]=1
        printf -v "$key" '%s' "$value"
    done < "$file"
    [[ $APP_PROJECT_NAME =~ ^cyberguard-app-vm(-[a-z0-9-]+)?$ ]] || fail 'Use a dedicated cyberguard-app-vm project name.'
    for value in "$APP_STATE_DIR" "$APP_SECRETS_DIR"; do
        [[ $value =~ ^/[a-zA-Z0-9/_.-]+$ && $value != / && $value != */../* && $value != */.. && $value != */./* && $value != */. && $value != */ ]] || fail 'Use normalized absolute state paths.'
        if [[ $value == "$APP_REPO_ROOT" || $value == "$APP_REPO_ROOT/"* ]]; then
            [[ $value == "$APP_REPO_ROOT/.local/"* ]] || fail 'Keep state outside the checkout or inside ignored .local.'
        fi
        [[ $(realpath -m -- "$value") == "$value" ]] || fail 'State paths must not traverse symlinks.'
    done
    [[ $APP_STATE_DIR != "$APP_SECRETS_DIR" && $APP_STATE_DIR != "$APP_SECRETS_DIR/"* && $APP_SECRETS_DIR != "$APP_STATE_DIR/"* ]] || fail 'State and secret directories must be separate.'
    [[ $APP_SECRET_GID =~ ^(0|[1-9][0-9]{0,9})$ ]] || fail 'Invalid secret group ID.'
    for key in APP_HTTP_PORT APP_HTTPS_PORT APP_PRIVATE_PORT APP_DB_PORT APP_AI_PORT; do
        value=${!key}
        if [[ ! $value =~ ^[1-9][0-9]{0,4}$ ]] || ((value > 65535)); then fail "Invalid port: $key"; fi
    done
    [[ $(printf '%s\n' "$APP_HTTP_PORT" "$APP_HTTPS_PORT" "$APP_PRIVATE_PORT" "$APP_DB_PORT" | sort -u | wc -l) == 4 ]] || fail 'Application published ports must differ.'
    [[ $APP_BIND_IP == 127.0.0.1 ]] || private_ipv4 "$APP_BIND_IP" || fail 'Bind only to loopback or an RFC1918 IPv4 address.'
    private_ipv4 "$APP_AI_IP" || fail 'AI requires an RFC1918 IPv4 address.'
    [[ $APP_AI_IP != "$APP_BIND_IP" ]] || fail 'Application and AI VM addresses must differ.'
    [[ $APP_NETWORK_APPROVED == yes || $APP_NETWORK_APPROVED == no ]] || fail 'Invalid network acknowledgement.'
    [[ $APP_ALLOW_DEMO_SEED == yes || $APP_ALLOW_DEMO_SEED == no ]] || fail 'Invalid demo acknowledgement.'
    [[ $APP_PUBLIC_TLS == acme || $APP_PUBLIC_TLS == provided ]] || fail 'Public TLS must be acme or provided.'
    for key in APP_PUBLIC_NAME APP_GO_TLS_NAME APP_DB_TLS_NAME APP_AI_TLS_NAME; do
        [[ ${!key} =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ && ${!key} == *.* && ${!key} != *..* && ! ${!key} =~ ^[0-9.]+$ ]] || fail "Invalid TLS DNS name: $key"
    done
    for value in "$APP_GO_IMAGE" "$APP_WEB_IMAGE"; do
        [[ $value =~ ^[a-zA-Z0-9][a-zA-Z0-9._/:@-]+$ ]] || fail 'Invalid image reference.'
    done
    for subnet in "$APP_EDGE_SUBNET" "$APP_IDENTITY_SUBNET" "$APP_INCIDENT_SUBNET"; do
        [[ $subnet == */28 ]] || fail 'Use /28 deployment Docker subnets.'
        base=${subnet%/*}; private_ipv4 "$base" || fail 'Use private Docker subnets.'
        last=${base##*.}; ((last % 16 == 0 && last <= 240)) || fail 'Docker subnet is not a /28 network address.'
        [[ $APP_AI_IP != "${base%.*}."* && $APP_BIND_IP != "${base%.*}."* ]] || fail 'VM addresses must not overlap deployment Docker subnet /24s.'
    done
    [[ $APP_EDGE_SUBNET != "$APP_IDENTITY_SUBNET" && $APP_EDGE_SUBNET != "$APP_INCIDENT_SUBNET" && $APP_IDENTITY_SUBNET != "$APP_INCIDENT_SUBNET" ]] || fail 'Docker subnets must differ.'
    base=${APP_EDGE_SUBNET%/*}; last=${base##*.}
    APP_WEB_IP=${base%.*}.$((last + 2))
    APP_GO_IP=${base%.*}.$((last + 3))
    APP_PRIVATE_IP=${base%.*}.$((last + 4))
    APP_RUNTIME_DIR=$APP_SECRETS_DIR/credentials/runtime
    APP_PROVISION_DIR=$APP_SECRETS_DIR/credentials/provisioning
    APP_TLS_DIR=$APP_SECRETS_DIR/tls
    APP_TLS_HANDLER=Caddyfile.acme
    [[ $APP_PUBLIC_TLS != provided ]] || APP_TLS_HANDLER=Caddyfile.provided
    APP_PUBLIC_AUTHORITY=$APP_PUBLIC_NAME
    [[ $APP_HTTPS_PORT == 443 ]] || APP_PUBLIC_AUTHORITY=$APP_PUBLIC_NAME:$APP_HTTPS_PORT
    for key in "${!allowed[@]}"; do export "${key?}"; done
    export APP_WEB_IP APP_GO_IP APP_PRIVATE_IP APP_RUNTIME_DIR APP_PROVISION_DIR APP_TLS_DIR APP_TLS_HANDLER APP_PUBLIC_AUTHORITY
    export DOCKER_GO_IMAGE=$APP_GO_IMAGE DOCKER_WEB_IMAGE=$APP_WEB_IMAGE DOCKER_SECRET_GID=$APP_SECRET_GID
    export DOCKER_SECRETS_DIR=$APP_RUNTIME_DIR
}
compose() {
    local -a extra=()
    [[ $APP_PUBLIC_TLS != provided ]] || extra=(-f "$APP_DEPLOY_DIR/provided.compose.yaml")
    docker compose --env-file /dev/null --project-directory "$APP_REPO_ROOT" \
        --project-name "$APP_PROJECT_NAME" -f "$APP_REPO_ROOT/compose.yaml" \
        -f "$APP_DEPLOY_DIR/compose.yaml" "${extra[@]}" "$@"
}
