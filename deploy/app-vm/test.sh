#!/usr/bin/env bash
# Fast non-privileged safety tests; no Docker/network/real secrets required.
set -euo pipefail
root=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
for script in "$root/deploy/app-vm/"*.sh; do bash -n "$script"; done
shellcheck -x -P "$root" "$root/deploy/app-vm/"*.sh "$root/deploy/app-vm/postgres/"*.sh "$root/deploy/app-vm/tests/"*.sh
temporary=$(mktemp -d "${TMPDIR:-/tmp}/cyberguard-app-vm-test.XXXXXX")
trap 'rm -rf -- "$temporary"' EXIT
mkdir -m 0700 "$temporary/secrets" "$temporary/state"
config=$temporary/config.env
printf 'APP_SECRETS_DIR=%s\nAPP_STATE_DIR=%s\nAPP_SECRET_GID=%s\n' "$temporary/secrets" "$temporary/state" "$(id -g)" > "$config"
load() { bash -c 'source "$1/deploy/app-vm/common.sh"; load_config "$2"' bash "$root" "$1"; }
reject() { if "$@" > "$temporary/error" 2>&1; then echo 'Expected rejection' >&2; exit 1; fi; }
APP_BIND_IP=0.0.0.0 load "$config"
for value in 'APP_BIND_IP=0.0.0.0' 'APP_AI_IP=8.8.8.8' 'APP_PUBLIC_NAME=app;bad' 'APP_HTTP_PORT=0' 'APP_STATE_DIR=/var/../etc' 'UNKNOWN=yes' 'APP_SECRET_GID=bad' 'APP_EDGE_SUBNET=172.28.40.1/28' 'SIGNUP_ENABLED=yes'; do
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
case "$*" in
    *' ps --all --quiet go-api')
        [[ ${APP_TEST_PS_FAIL:-no} == no ]] || exit 7
        if [[ -n ${APP_TEST_GO_STATE:-} ]]; then echo go-id; fi ;;
    *' ps --all --quiet web')
        if [[ -n ${APP_TEST_WEB_MOUNT:-} ]]; then echo web-id; fi ;;
    'inspect --format {{.State.Status}} go-id') echo "$APP_TEST_GO_STATE" ;;
    'inspect --format {{.State.Status}} web-id') echo running ;;
    'inspect --format '*web-id) echo "bind:false:$APP_TEST_WEB_MOUNT" ;;
    'exec web-id '*)
        stat -c '%d:%i' "$APP_TEST_WEB_MOUNT"
        if [[ -f $APP_TEST_WEB_MOUNT/enabled ]]; then echo "${APP_TEST_WEB_MARKER:-enabled}"; fi ;;
    'image inspect '*) echo "$APP_TEST_IMAGE_ID" ;;
    'run '*)
        [[ " $* " == *" --entrypoint /bin/sh $APP_TEST_IMAGE_ID "* ]] || exit 2
        cd "$APP_TEST_MIGRATIONS" || exit 1
        export LC_ALL=C
        sha256sum identity/*.sql incidents/*.sql ;;
    *' --interactive=false status-identity'|*' --interactive=false status-incidents')
        if [[ -n ${APP_TEST_STDIN:-} ]] && IFS= read -r unexpected; then
            echo 'Migration status inherited the owner password stream.' >&2; exit 1
        fi
        database=${!#}; database=${database#status-}
        files=("$APP_TEST_MIGRATIONS/$database/"*.sql)
        version=${files[-1]##*/}; version=$((10#${version%%_*}))
        printf '{"msg":"migration versions","database":"%s","current":%d,"target":%d}\n' "$database" "$version" "$version" ;;
    *' migrate-identity /app/seed '*' -create-owner')
        cat > "$APP_TEST_STDIN" ;;
esac
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
# These management guard tests mock Docker and certificate tooling.
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
grep -Fq 'Could not inspect Go service' "$temporary/error"
if grep -q ' run ' "$APP_TEST_COMMANDS"; then echo 'Database tool ran after state inspection failed' >&2; exit 1; fi
manage() { PATH="$temporary/bin:$PATH" bash "$root/deploy/app-vm/manage.sh" --config "$config" "$@"; }
for state in running restarting paused dead; do
    APP_TEST_GO_STATE=$state reject manage migrate
    grep -Fq "state: $state" "$temporary/error"
done
rm "$temporary/state/maintenance/public/enabled"
APP_TEST_WEB_MOUNT=$temporary/state reject manage maintenance-on
grep -Fq 'does not match the web gateway bind mount' "$temporary/error"
[[ ! -e $temporary/state/maintenance/public/enabled ]]
export APP_TEST_WEB_MOUNT=$temporary/state/maintenance/public
manage maintenance-on >/dev/null
APP_TEST_WEB_MARKER=absent reject manage quiesce
grep -Fq 'Live web gateway does not see' "$temporary/error"
if grep -q ' run ' "$APP_TEST_COMMANDS"; then echo 'Database tool ran before state guards passed' >&2; exit 1; fi
export APP_TEST_MIGRATIONS=$temporary/migrations APP_TEST_IMAGE_ID
APP_TEST_IMAGE_ID="sha256:$(printf '%064d' 1)"
cp -r "$root/db/migrations" "$APP_TEST_MIGRATIONS"
for database in identity incidents; do
    # Same version number, different bytes. Neither database may be touched.
    files=("$APP_TEST_MIGRATIONS/$database/"*.sql)
    printf '\n-- test drift\n' >> "${files[0]}"
    reject manage migrate
    grep -Fq 'do not match this checkout' "$temporary/error"
    [[ -f $APP_TEST_WEB_MOUNT/upgrade-blocked ]]
    if grep -Eq ' migrate-(identity|incidents)$' "$APP_TEST_COMMANDS"; then echo 'Migration ran before verifying both directories' >&2; exit 1; fi
    cp "$root/db/migrations/$database/${files[0]##*/}" "${files[0]}"
done
APP_TEST_GO_STATE=exited manage migrate >/dev/null
[[ ! -e $APP_TEST_WEB_MOUNT/upgrade-blocked && -f $APP_TEST_WEB_MOUNT/enabled ]]
[[ $(tail -4 "$APP_TEST_IMAGES" | sort -u) == "$APP_TEST_IMAGE_ID" ]]
[[ $(awk '/ (migrate|status)-(identity|incidents)$/ {print $NF}' "$APP_TEST_COMMANDS") == $'migrate-identity\nmigrate-incidents\nstatus-identity\nstatus-incidents' ]]
export APP_TEST_STDIN=$temporary/owner-stdin
APP_TEST_GO_STATE=running reject manage init-owner first_admin
grep -Fq 'state: running' "$temporary/error"
reject manage init-owner 'not a username'
reject manage init-owner first_admin --unknown
printf '%s\n' 'synthetic-owner-password' | APP_TEST_GO_STATE=exited manage init-owner first_admin --create >/dev/null
grep -Fq -- '--interactive=false status-identity' "$APP_TEST_COMMANDS"
grep -Fq -- '--interactive=false status-incidents' "$APP_TEST_COMMANDS"
grep -Fq 'migrate-identity /app/seed -owner first_admin -create-owner' "$APP_TEST_COMMANDS"
grep -Fxq 'synthetic-owner-password' "$APP_TEST_STDIN"
if grep -Fq 'synthetic-owner-password' "$APP_TEST_COMMANDS"; then echo 'Password entered command arguments' >&2; exit 1; fi
APP_TEST_GO_STATE=exited manage init-owner existing_admin >/dev/null
grep -Fq 'migrate-identity /app/seed -owner existing_admin' "$APP_TEST_COMMANDS"
[[ $before == "$(find "$temporary/secrets/credentials" -type f -exec sha256sum {} + | sort)" ]]
printf '%s\n' 'Application VM literal config, path safety and non-overwriting TLS credential generation passed.'
