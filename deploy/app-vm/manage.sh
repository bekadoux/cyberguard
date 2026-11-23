#!/usr/bin/env bash
set -euo pipefail
umask 077
# shellcheck source=deploy/app-vm/common.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/common.sh"
[[ $# -ge 3 && $1 == --config ]] || fail 'Usage: manage.sh --config FILE ACTION (see docs/azure-app-deployment.md)'
if [[ $3 == init-owner ]]; then
    [[ $# == 4 || ( $# == 5 && $5 == --create ) ]] || fail 'Usage: manage.sh --config FILE init-owner USERNAME [--create]'
    [[ $4 =~ ^[a-z0-9][a-z0-9_.-]{2,63}$ ]] || fail 'Provide a normalized owner username.'
else
    [[ $# == 3 ]] || fail 'Unexpected arguments.'
fi
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
credential_url() {
    local file=$1 user=$2 host=$3 port=$4 database=$5 value pattern
    [[ $(stat -c %s "$file") -le 16384 ]] || fail 'Credential URL exceeds the size limit.'
    value=$(cat "$file")
    pattern="^postgres://$user:[^[:space:]@/?#]+@$host:$port/$database"
    pattern+='\?sslmode=verify-full&sslrootcert=/run/certs/ca\.crt'
    if [[ ${6:-no} == yes ]]; then pattern+='&default_transaction_read_only=on'; fi
    pattern+='$'
    [[ ${#value} -le 16384 && $value =~ $pattern ]] || fail "Credential URL must use its scoped role/endpoint and verified TLS: ${file##*/}"
}
local_check() {
    local file
    safe_directory "$APP_SECRETS_DIR"
    safe_directory "$APP_SECRETS_DIR/credentials"
    safe_directory "$APP_RUNTIME_DIR"
    safe_directory "$APP_PROVISION_DIR"
    safe_directory "$APP_TLS_DIR"
    for file in identity_runtime_url incidents_runtime_url ai_internal_token ai_runtime_url; do secret_file "$APP_RUNTIME_DIR/$file"; done
    [[ $(stat -c %s "$APP_RUNTIME_DIR/ai_internal_token") -le 16384 ]] || fail 'Internal token exceeds the size limit.'
    for file in "$APP_PROVISION_DIR/"*; do secret_file "$file"; done
    credential_url "$APP_RUNTIME_DIR/identity_runtime_url" go_identity identity-db 5432 cyberguard_identity
    credential_url "$APP_RUNTIME_DIR/incidents_runtime_url" go_incidents incident-db 5432 cyberguard_incidents
    credential_url "$APP_PROVISION_DIR/identity_migration_url" identity_migrator identity-db 5432 cyberguard_identity
    credential_url "$APP_PROVISION_DIR/incidents_migration_url" incidents_migrator incident-db 5432 cyberguard_incidents
    credential_url "$APP_PROVISION_DIR/identity_status_url" identity_migrator identity-db 5432 cyberguard_identity yes
    credential_url "$APP_PROVISION_DIR/incidents_status_url" incidents_migrator incident-db 5432 cyberguard_incidents yes
    credential_url "$APP_RUNTIME_DIR/ai_runtime_url" python_incidents "${APP_DB_TLS_NAME//./\\.}" "$APP_DB_PORT" cyberguard_incidents
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
        [[ ! -e $APP_STATE_DIR/maintenance/public/$file || -f $APP_STATE_DIR/maintenance/public/$file ]] || fail 'Expected a regular maintenance marker.'
    done
}
verify_web_bind() {
    local dir=$APP_STATE_DIR/maintenance/public ids id state mount expected observed
    ids=$(compose ps --all --quiet web) || fail 'Could not inspect the web gateway.'
    for id in $ids; do
        mount=$(docker inspect --format '{{range .Mounts}}{{if eq .Destination "/run/maintenance"}}{{.Type}}:{{.RW}}:{{.Source}}{{end}}{{end}}' "$id")
        [[ $mount == "bind:false:$dir" ]] || fail 'Configured maintenance directory does not match the web gateway bind mount.'
        state=$(docker inspect --format '{{.State.Status}}' "$id")
        case "$state" in
            created|exited) continue ;;
            running) ;;
            *) fail 'Cannot verify the maintenance marker on the web gateway in its current state.' ;;
        esac
        # A replaced host directory can have the same path as a detached live bind.
        expected=$(stat -c '%d:%i' "$dir")
        if [[ -f $dir/enabled ]]; then expected+=$'\nenabled'; fi
        observed=$(docker exec "$id" sh -ec 'stat -c "%d:%i" /run/maintenance; if [ -f /run/maintenance/enabled ]; then echo enabled; fi')
        [[ $observed == "$expected" ]] || fail 'Live web gateway does not see the configured maintenance directory/marker.'
    done
}
pause_writes() {
    verify_web_bind
    if [[ ! -f $APP_STATE_DIR/maintenance/public/enabled ]]; then
        install -m 0644 /dev/null "$APP_STATE_DIR/maintenance/public/enabled"
    fi
    verify_web_bind
}
require_stopped() {
    local ids id state
    ids=$(compose ps --all --quiet go-api) || fail 'Could not inspect Go service; refusing database changes.'
    for id in $ids; do
        state=$(docker inspect --format '{{.State.Status}}' "$id")
        [[ $state == exited ]] || fail "Stop Go using quiesce before changing databases/roles (state: $state)."
    done
}
tool() { compose run --rm -T --no-deps --pull never "$@"; }
verify_migration_image() {
    local image expected actual
    image=$(docker image inspect --format '{{.Id}}' "$APP_GO_IMAGE")
    [[ $image =~ ^sha256:[a-f0-9]{64}$ ]] || fail 'Cannot identify the local migration image.'
    expected=$(cd "$APP_REPO_ROOT/db/migrations" && export LC_ALL=C && sha256sum identity/*.sql incidents/*.sql)
    actual=$(docker run --rm --pull never --network none --read-only --cap-drop ALL \
        --security-opt no-new-privileges --entrypoint /bin/sh "$image" -ec \
        'cd /app/migrations; export LC_ALL=C; sha256sum identity/*.sql incidents/*.sql')
    [[ $expected == "$actual" ]] || fail 'Migration files in the image do not match this checkout; rebuild before migrating.'
    # Both migration containers and their post-write status checks use these same bytes.
    export APP_GO_IMAGE=$image DOCKER_GO_IMAGE=$image
}
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
        result=$(tool --interactive=false "status-$database" </dev/null)
        if ! jq -es --arg database "$database" --argjson expected "$target" \
            'map(select(.msg == "migration versions")) | last | .database == $database and .current == $expected and .target == $expected' <<< "$result" >/dev/null; then
            fail "Migration versions do not match this checkout/image: $database"
        fi
    done
}
gateway_check() {
    local -a trust=() startup_wait=()
    [[ $APP_PUBLIC_TLS != provided ]] || trust=(--cacert "$APP_TLS_DIR/ca.crt")
    if [[ $action == up && $APP_PUBLIC_TLS == acme ]]; then
        # Caddy's container health check is HTTP-only. The first TLS handshake
        # can fail while ACME is in progress. Leave time for the final attempt.
        printf '%s\n' 'Checking public HTTPS (allowing up to two minutes for Caddy/ACME).'
        startup_wait=(--retry 40 --retry-all-errors --retry-delay 3 --retry-max-time 110)
    fi
    curl --fail --silent --show-error --noproxy '*' --connect-timeout 3 --max-time 10 \
        "${trust[@]}" "${startup_wait[@]}" --resolve "$APP_PUBLIC_NAME:$APP_HTTPS_PORT:$APP_BIND_IP" \
        "https://$APP_PUBLIC_AUTHORITY/api/readyz" >/dev/null ||
        fail 'Public HTTPS readiness failed. Check the app VM web (Caddy) logs for certificate issuance, DNS, or challenge errors.'
    curl --fail --silent --show-error --noproxy '*' --connect-timeout 3 --max-time 10 \
        --cacert "$APP_TLS_DIR/ca.crt" --resolve "$APP_GO_TLS_NAME:$APP_PRIVATE_PORT:$APP_BIND_IP" \
        "https://$APP_GO_TLS_NAME:$APP_PRIVATE_PORT/readyz" >/dev/null ||
        fail 'Private Go HTTPS readiness failed. Check the app VM private-go and go-api logs.'
}

case "$action" in
    config) compose --profile tools config --quiet; printf '%s\n' 'Compose configuration valid; not a readiness check.'; exit ;;
    build)
        # Sequential builds suit the D2as v4; no startup/setup or runtime credentials.
        docker build -f "$APP_REPO_ROOT/backend/go/Dockerfile" -t "$APP_GO_IMAGE" "$APP_REPO_ROOT"
        docker build -f "$APP_REPO_ROOT/frontend/Dockerfile" -t "$APP_WEB_IMAGE" "$APP_REPO_ROOT"
        compose pull --policy missing identity-db incident-db private-go prepare-web
        exit ;;
    status) compose ps; exit ;;
    logs) compose logs --tail 100; exit ;;
    create-secrets) exec bash "$APP_DEPLOY_DIR/create-secrets.sh" --config "$2" ;;
    db-up|bootstrap|migrate|migration-status|bootstrap-ai|seed-demo|init-owner|up|check|check-ai|maintenance-on|maintenance-off|quiesce|stop) ;;
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
        local_check </dev/null
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
                verify_web_bind
                install -m 0644 /dev/null "$APP_STATE_DIR/maintenance/public/upgrade-blocked"
                verify_migration_image
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
            init-owner)
                # Only seed may read the supplied password, never a Compose
                # child (including the read-only migration status containers).
                {
                    require_stopped
                    verify_migration_image
                    migration_check
                } </dev/null
                if [[ ${5:-} == --create ]]; then
                    tool migrate-identity /app/seed -owner "$4" -create-owner
                else
                    tool migrate-identity /app/seed -owner "$4" </dev/null
                fi ;;
            up|check|maintenance-off)
                verify_web_bind
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
                if [[ $action == maintenance-off ]]; then
                    rm -f -- "$APP_STATE_DIR/maintenance/public/enabled"
                    verify_web_bind
                fi
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
