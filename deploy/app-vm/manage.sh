#!/usr/bin/env bash
set -euo pipefail
umask 077
# shellcheck source=deploy/app-vm/common.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/common.sh"
[[ $# == 3 && $1 == --config ]] || fail 'Usage: manage.sh --config FILE ACTION (see docs/azure-app-deployment.md)'
load_config "$2"
action=$3

network_check() {
    if [[ $APP_BIND_IP != 127.0.0.1 ]]; then
        [[ $APP_NETWORK_APPROVED == yes ]] || fail 'Review NSGs and Docker-aware firewall rules before acknowledging APP_NETWORK_APPROVED=yes.'
        ip -o -4 address show | awk '{split($4,a,"/"); print a[1]}' | grep -Fxq "$APP_BIND_IP" || fail 'APP_BIND_IP is not assigned to this host.'
    fi
}
certificate_check() {
    local name=$1 hostname=$2 cert_public key_public
    secret_file "$APP_TLS_DIR/$name.crt"
    secret_file "$APP_TLS_DIR/$name.key"
    openssl verify -x509_strict -purpose sslserver -CAfile "$APP_TLS_DIR/ca.crt" \
        -untrusted "$APP_TLS_DIR/$name.crt" -verify_hostname "$hostname" "$APP_TLS_DIR/$name.crt" >/dev/null 2>&1 || fail "Certificate verification failed: $name"
    openssl x509 -in "$APP_TLS_DIR/$name.crt" -noout -checkend 86400 >/dev/null 2>&1 || fail "Certificate expires within 24 hours: $name"
    cert_public=$(openssl x509 -in "$APP_TLS_DIR/$name.crt" -pubkey -noout | openssl pkey -pubin -outform DER 2>/dev/null | sha256sum)
    key_public=$(openssl pkey -in "$APP_TLS_DIR/$name.key" -passin pass: -pubout -outform DER 2>/dev/null | sha256sum) || fail "Unreadable or encrypted server key: $name"
    [[ $cert_public == "$key_public" ]] || fail "Certificate/key mismatch: $name"
}
local_check() {
    local file
    safe_directory "$APP_SECRETS_DIR"
    safe_directory "$APP_SECRETS_DIR/credentials"
    safe_directory "$APP_RUNTIME_DIR"
    safe_directory "$APP_PROVISION_DIR"
    safe_directory "$APP_TLS_DIR"
    for file in identity_runtime_url incidents_runtime_url ai_internal_token ai_runtime_url; do secret_file "$APP_RUNTIME_DIR/$file"; done
    for file in identity_admin_password incidents_admin_password identity_migration_url incidents_migration_url; do secret_file "$APP_PROVISION_DIR/$file"; done
    secret_file "$APP_TLS_DIR/ca.crt"
    certificate_check identity identity-db
    certificate_check incident incident-db
    # The incident certificate must also authenticate the cross-VM endpoint name.
    openssl verify -x509_strict -CAfile "$APP_TLS_DIR/ca.crt" -untrusted "$APP_TLS_DIR/incident.crt" \
        -verify_hostname "$APP_DB_TLS_NAME" "$APP_TLS_DIR/incident.crt" >/dev/null 2>&1 || fail 'Incident certificate lacks the cross-VM DNS name.'
    certificate_check go "$APP_GO_TLS_NAME"
    if [[ $APP_PUBLIC_TLS == provided ]]; then certificate_check public "$APP_PUBLIC_NAME"; fi
    network_check
}
state_check() {
    safe_directory "$APP_STATE_DIR"
    [[ ! -L $APP_STATE_DIR/manage.lock ]] || fail 'Management lock must not be a symlink.'
    exec 9> "$APP_STATE_DIR/manage.lock"
    flock -n 9 || fail 'Another application management command is active.'
    if [[ ! -e $APP_STATE_DIR/maintenance ]]; then install -d -m 0700 -g "$APP_SECRET_GID" "$APP_STATE_DIR/maintenance"; fi
    safe_directory "$APP_STATE_DIR/maintenance"
    if [[ ! -e $APP_STATE_DIR/maintenance/public ]]; then install -d -m 0755 -g "$APP_SECRET_GID" "$APP_STATE_DIR/maintenance/public"; fi
    [[ -d $APP_STATE_DIR/maintenance/public && ! -L $APP_STATE_DIR/maintenance/public && $(stat -c %a "$APP_STATE_DIR/maintenance/public") == 755 ]] || fail 'Maintenance mount must be a real 0755 directory.'
    for file in enabled upgrade-blocked; do
        [[ ! -L $APP_STATE_DIR/maintenance/public/$file ]] || fail 'Maintenance markers must not be symlinks.'
    done
}
pause_writes() {
    install -m 0644 /dev/null "$APP_STATE_DIR/maintenance/public/enabled"
}
require_stopped() {
    if compose ps --status running --services | grep -Fxq go-api; then fail 'Stop Go using quiesce before changing databases/roles.'; fi
}
tool() { compose run --rm -T --no-deps --pull never "$@"; }
expected_version() {
    local file number maximum=0
    for file in "$APP_REPO_ROOT/db/migrations/$1/"*.sql; do
        number=${file##*/}; number=${number%%_*}; number=$((10#$number))
        if ((number > maximum)); then maximum=$number; fi
    done
    printf '%s' "$maximum"
}
migration_check() {
    local database result target
    for database in identity incidents; do
        target=$(expected_version "$database")
        result=$(tool "migrate-$database" /app/migrate -database "$database" -dir "/app/migrations/$database" -status)
        if ! jq -es --argjson expected "$target" \
            'map(select(.msg == "migration versions")) | last | .current == $expected and .target == $expected' <<< "$result" >/dev/null; then
            fail "Migration versions do not match this checkout/image: $database"
        fi
    done
}
gateway_check() {
    local -a trust=()
    [[ $APP_PUBLIC_TLS != provided ]] || trust=(--cacert "$APP_TLS_DIR/ca.crt")
    curl --fail --silent --show-error --noproxy '*' --connect-timeout 3 --max-time 10 \
        "${trust[@]}" --resolve "$APP_PUBLIC_NAME:$APP_HTTPS_PORT:$APP_BIND_IP" \
        "https://$APP_PUBLIC_AUTHORITY/api/readyz" >/dev/null
    curl --fail --silent --show-error --noproxy '*' --connect-timeout 3 --max-time 10 \
        --cacert "$APP_TLS_DIR/ca.crt" --resolve "$APP_GO_TLS_NAME:$APP_PRIVATE_PORT:$APP_BIND_IP" \
        "https://$APP_GO_TLS_NAME:$APP_PRIVATE_PORT/readyz" >/dev/null
}

case "$action" in
    config) compose --profile tools config --quiet; printf '%s\n' 'Configuration valid; not a readiness check.'; exit ;;
    build)
        # Sequential builds suit the D2as v4; no startup/setup or runtime credentials.
        docker build -f "$APP_REPO_ROOT/backend/go/Dockerfile" -t "$APP_GO_IMAGE" "$APP_REPO_ROOT"
        docker build -f "$APP_REPO_ROOT/frontend/Dockerfile" -t "$APP_WEB_IMAGE" "$APP_REPO_ROOT"
        compose pull identity-db incident-db private-go prepare-web
        exit ;;
    status) compose ps; exit ;;
    logs) compose logs --tail 100; exit ;;
    create-secrets) exec bash "$APP_DEPLOY_DIR/create-secrets.sh" --config "$2" ;;
    db-up|bootstrap|migrate|migration-status|bootstrap-ai|seed-demo|up|check|check-ai|maintenance-on|maintenance-off|quiesce|stop) ;;
    *) fail 'Unknown action. See docs/azure-app-deployment.md.' ;;
esac
state_check
case "$action" in
    maintenance-on) pause_writes; printf '%s\n' 'Public writes paused; private reads and existing workers remain active.' ;;
    quiesce)
        pause_writes
        install -m 0644 /dev/null "$APP_STATE_DIR/maintenance/public/upgrade-blocked"
        compose stop private-go go-api
        printf '%s\n' 'Local Go stopped; startup blocked until successful migrate. Stop ALL remote/local AI workers separately before migrating.' ;;
    stop) compose stop ;;
    *)
        local_check
        case "$action" in
            db-up)
                pause_writes
                compose up -d --no-build --pull never --wait --wait-timeout 90 identity-db incident-db ;;
            bootstrap)
                require_stopped
                tool bootstrap-identity
                tool bootstrap-incidents ;;
            migrate)
                require_stopped
                [[ -f $APP_STATE_DIR/maintenance/public/enabled ]] || fail 'Enable maintenance before migrations.'
                install -m 0644 /dev/null "$APP_STATE_DIR/maintenance/public/upgrade-blocked"
                tool migrate-identity
                tool migrate-incidents
                migration_check
                rm -f -- "$APP_STATE_DIR/maintenance/public/upgrade-blocked" ;;
            migration-status) migration_check; printf '%s\n' 'Both databases match the checkout and migration image.' ;;
            bootstrap-ai)
                require_stopped
                migration_check
                secret_file "$APP_PROVISION_DIR/ai_runtime_password"
                tool bootstrap-ai ;;
            seed-demo)
                [[ $APP_ALLOW_DEMO_SEED == yes ]] || fail 'Demo seed requires APP_ALLOW_DEMO_SEED=yes and synthetic/demo data only.'
                require_stopped
                migration_check
                tool seed-demo ;;
            up|check|maintenance-off)
                [[ ! -e $APP_STATE_DIR/maintenance/public/upgrade-blocked ]] || fail 'Upgrade incomplete; successful explicit migrate is required before startup/resume.'
                if [[ $action == up ]]; then
                    compose up -d --no-build --pull never --wait --wait-timeout 90 identity-db incident-db
                fi
                migration_check
                if [[ $action == up ]]; then
                    [[ $APP_PUBLIC_TLS != acme || $APP_PUBLIC_NAME != *.invalid ]] || fail 'Configure your real public DNS hostname before ACME startup.'
                    tool prepare-web
                    compose up -d --no-build --pull never --wait --wait-timeout 120 identity-db incident-db go-api web private-go
                fi
                gateway_check
                if [[ $action == maintenance-off ]]; then rm -f -- "$APP_STATE_DIR/maintenance/public/enabled"; fi
                printf '%s\n' 'Application TLS/connectivity/schema checks passed; AI and browser acceptance are separate.' ;;
            check-ai)
                migration_check
                tool probe-ai
                curl --fail --silent --show-error --noproxy '*' --connect-timeout 3 --max-time 10 \
                    --cacert "$APP_TLS_DIR/ca.crt" --resolve "$APP_AI_TLS_NAME:$APP_AI_PORT:$APP_AI_IP" \
                    "https://$APP_AI_TLS_NAME:$APP_AI_PORT/readyz" >/dev/null
                printf '%s\n' 'AI gateway TLS and local restricted-role snapshot-safety checks passed. Run the AI VM check for remote DB/Go/model readiness.' ;;
        esac ;;
esac
