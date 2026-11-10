#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=deploy/ai-vm/common.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/common.sh"
[[ $# == 3 && $1 == --config ]] || fail 'Usage: manage.sh --config FILE {config|build|up|check|status|logs|stop}'
load_config "$2"

local_check() {
    local file config=$1
    [[ -d $AI_SECRETS_DIR && ! -L $AI_SECRETS_DIR && $(stat -c %a "$AI_SECRETS_DIR") == 700 ]] || fail 'Secrets directory must be a real directory with mode 0700.'
    for file in ai_runtime_url ai_internal_token api.crt api.key ca.crt; do
        [[ -f $AI_SECRETS_DIR/$file && ! -L $AI_SECRETS_DIR/$file && -s $AI_SECRETS_DIR/$file ]] || fail "Missing regular secret/certificate file: $file"
        [[ $(stat -c %a "$AI_SECRETS_DIR/$file") == 640 && $(stat -c %g "$AI_SECRETS_DIR/$file") == "$AI_SECRET_GID" ]] || fail "Expected mode 0640 and configured group on: $file"
    done
    [[ $(stat -c %s "$AI_SECRETS_DIR/ai_runtime_url") -le 16384 && $(stat -c %s "$AI_SECRETS_DIR/ai_internal_token") -le 16384 ]] || fail 'Runtime credentials exceed the size limit.'
    openssl verify -x509_strict -CAfile "$AI_SECRETS_DIR/ca.crt" \
        -untrusted "$AI_SECRETS_DIR/api.crt" -verify_hostname "$AI_TLS_NAME" \
        "$AI_SECRETS_DIR/api.crt" >/dev/null 2>&1 || fail 'API certificate verification failed.'
    openssl x509 -in "$AI_SECRETS_DIR/api.crt" -noout -checkend 86400 >/dev/null 2>&1 || fail 'API certificate expires within 24 hours.'
    local cert_public key_public
    cert_public=$(openssl x509 -in "$AI_SECRETS_DIR/api.crt" -pubkey -noout | openssl pkey -pubin -outform DER 2>/dev/null | sha256sum)
    key_public=$(openssl pkey -in "$AI_SECRETS_DIR/api.key" -passin pass: -pubout -outform DER 2>/dev/null | sha256sum) || fail 'API key must be readable and unencrypted.'
    [[ $cert_public == "$key_public" ]] || fail 'API certificate and key do not match.'
    if [[ $AI_BIND_IP != 127.0.0.1 ]]; then
        [[ $AI_NETWORK_APPROVED == yes ]] || fail 'Configure the NSG/firewall allowlist, then set AI_NETWORK_APPROVED=yes.'
        ip -o -4 address show | awk '{split($4,a,"/"); print a[1]}' | grep -Fxq "$AI_BIND_IP" || fail 'AI_BIND_IP is not assigned to this host.'
    fi
    bash "$AI_DEPLOY_DIR/prepare-models.sh" --config "$config" --check
}

gateway_check() {
    curl --fail --silent --show-error --noproxy '*' --retry 5 --retry-connrefused \
        --retry-delay 1 --retry-max-time 20 --connect-timeout 3 --max-time 5 \
        --cacert "$AI_SECRETS_DIR/ca.crt" \
        --resolve "$AI_TLS_NAME:$AI_PORT:$AI_BIND_IP" \
        "https://$AI_TLS_NAME:$AI_PORT/readyz" >/dev/null
}

# local_check takes the original config explicitly, avoiding shell config evaluation.
config_file=$2
case "$3" in
    config) compose config --quiet; printf '%s\n' 'Compose configuration valid (not a readiness check).' ;;
    build)
        docker build -f "$AI_REPO_ROOT/backend/python/Dockerfile" -t "$AI_IMAGE" "$AI_REPO_ROOT"
        docker build -f "$AI_REPO_ROOT/docker/ai/Dockerfile.kev" -t "$AI_KEV_IMAGE" "$AI_REPO_ROOT"
        compose pull ai-gateway ai-llm
        ;;
    up|check)
        local_check "$config_file"
        compose config --quiet
        if [[ $3 == up ]]; then
            compose up -d --no-build --pull never --wait --wait-timeout 600 \
                ai-kev ai-llm ai-api ai-gateway
        fi
        compose run --rm --no-deps --pull never ai-worker python -m cyberguard_ai.preflight
        gateway_check
        if [[ $3 == up ]]; then compose up -d --no-build --pull never ai-worker; fi
        printf '%s\n' 'Application ready: TLS, storage and dependency probes passed; real inference is a separate acceptance check.'
        ;;
    status) compose ps ;;
    logs) compose logs --tail 100 ;;
    stop) compose stop ;;
    *) fail 'Unknown action. Use config, build, up, check, status, logs or stop.' ;;
esac
