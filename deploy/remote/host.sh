#!/usr/bin/env bash
# Privileged per-VM installer. The workstation calls this via sudo on the target.
set +x
set -euo pipefail
umask 077

fail() { printf '%s\n' "$*" >&2; exit 1; }
present() { [[ -e $1 || -L $1 ]]; }
done_step() { [[ -f $state/$1 && ! -L $state/$1 ]]; }

mark() { install -m 0600 /dev/null "$state/$1"; }

once() {
    local receipt=$1
    shift
    if ! done_step "$receipt"; then
        printf '%s VM: %s\n' "$role" "$receipt"
        "$@"
        mark "$receipt"
    fi
}

assert_own_state() {
    [[ -d $state && ! -L $state && $(stat -c '%u:%a' "$state") == 0:700 &&
       -f $state/deployment && ! -L $state/deployment ]] ||
        fail "No managed remote setup state for the $role VM. Run install with the original settings."
    [[ $(cat "$state/deployment") == "$deployment:$operator" ]] ||
        fail "The $role VM belongs to a different revision, operator, or configuration. Upgrade/adoption is a separate operation."
}

volume_names() {
    # Name and label checks avoid adopting an unrelated or manually-created volume.
    docker volume ls --format '{{.Name}}'
}

assert_unused() {
    local resources volumes available
    if present "$etc" || present "$data"; then
        fail "Existing $role VM configuration or state found. This installer does not adopt manual deployments."
    fi
    if command -v docker >/dev/null; then
        resources=$(docker ps -aq --filter "label=com.docker.compose.project=$project") ||
            fail 'Cannot inspect the local Docker daemon; not assuming this host is empty.'
        [[ -z $resources ]] || fail "Docker containers already exist for $project."
        resources=$(docker network ls -q --filter "label=com.docker.compose.project=$project")
        [[ -z $resources ]] || fail "Docker networks already exist for $project."
        resources=$(docker volume ls -q --filter "label=com.docker.compose.project=$project")
        [[ -z $resources ]] || fail "Docker volumes already exist for $project."
        volumes=$(volume_names)
        if grep -Eq "^${project}_(identity-data|incident-data|web-data)$" <<< "$volumes"; then
            fail "Docker volume names reserved for $project already exist."
        fi
    fi
    available=$(df -Pk /var | awk 'NR==2 {print $4}')
    (( available >= 20 * 1024 * 1024 )) || fail 'At least 20 GiB free under /var is required on each VM.'
}

inspect() {
    if present "$state"; then assert_own_state; else assert_unused; fi
}

claim_fresh_state() {
    local staging
    if ! present "$state"; then
        assert_unused
        staging=$(mktemp -d /var/lib/.cyberguard-remote.XXXXXXXX)
        printf '%s:%s\n' "$deployment" "$operator" > "$staging/deployment"
        # Atomic; another installer cannot replace a nonempty state directory.
        mv -T --no-clobber -- "$staging" "$state"
        if [[ -d $staging ]]; then
            rm -f -- "$staging/deployment"
            rmdir -- "$staging"
        fi
    fi
    assert_own_state
}

manager() { bash "$repo/deploy/$role-vm/manage.sh" --config "$config" "$@"; }

check_config_directory() {
    if [[ ! -d $etc || -L $etc || $(realpath -- "$etc") != "$etc" ]]; then
        fail "Refusing a missing or relocated $role configuration directory."
    fi
}

require_configured() {
    check_config_directory
    done_step configured && [[ -f $config && ! -L $config ]] ||
        fail "The $role VM has no installed configuration. Rerun install."
    # Detect manual edits instead of starting or provisioning with different settings.
    [[ $(sha256sum < "$config") == "$(<"$state/configured")" ]] ||
        fail "The $role VM configuration differs from the installed settings; review it manually."
}

verify_transfer() {
    local file=$1 expected=$2 actual
    [[ $expected =~ ^[a-f0-9]{64}$ ]] || fail 'Expected a SHA-256 transfer checksum.'
    actual=$(sha256sum < "$file")
    [[ ${actual%% *} == "$expected" ]] || fail 'Transferred file checksum mismatch. Retry the upload.'
}

configure_vm() (
    local expected=$1 file example receipt key
    done_step bootstrapped || fail 'Host preparation must succeed before configuration.'
    check_config_directory
    [[ -f $config && ! -L $config ]] || fail 'Expected the regular configuration file created by bootstrap.'
    file=$(mktemp "$etc/.remote-config.XXXXXXXX")
    example=$(mktemp "$etc/.remote-default.XXXXXXXX")
    receipt=$(mktemp "$state/.configured.XXXXXXXX")
    trap 'rm -f -- "$file" "$example" "$receipt"' EXIT
    cat > "$file"
    verify_transfer "$file" "$expected"
    if [[ $role == app ]]; then key=APP_SECRET_GID; else key=AI_SECRET_GID; fi
    printf '%s=%s\n' "$key" "$gid" >> "$file"
    # Validate with the same allowlist and defaults as the VM manager.
    (
        # shellcheck disable=SC1090
        source "$repo/deploy/$role-vm/common.sh"
        load_config "$file"
    )
    if done_step configured; then require_configured; fi
    if ! cmp -s "$file" "$config"; then
        sed "s/^$key=.*/$key=$gid/" "$repo/deploy/$role-vm/config.env.example" > "$example"
        if done_step configured || ! cmp -s "$example" "$config"; then
            fail "Refusing to overwrite existing $role configuration."
        fi
        chmod 0644 "$file"
        mv -T -- "$file" "$config"
    fi
    sha256sum < "$config" > "$receipt"
    mv -T -- "$receipt" "$state/configured"
)

receive_file() (
    local name=$1 expected=${2:-} dest temporary limit=131072 value
    require_configured
    case "$role:$name" in
        app:ca.crt|app:identity.crt|app:identity.key|app:incident.crt|app:incident.key|app:go.crt|app:go.key|app:public.crt|app:public.key)
            dest=$etc/secrets/tls ;;
        ai:ca.crt|ai:api.crt|ai:api.key) dest=$etc/secrets ;;
        ai:ai_runtime_url|ai:ai_internal_token) dest=$etc/secrets; limit=16384 ;;
        *) fail 'File is not allowed for this VM.' ;;
    esac
    [[ -d $dest && $(realpath -- "$dest") == "$dest" &&
       $(stat -c '%U:%g:%a' "$dest") == "$operator:$gid:700" ]] ||
        fail 'Secret destination must be owned by the operator with mode 0700.'
    temporary=$(mktemp "$dest/.receive.XXXXXXXX")
    trap 'rm -f -- "$temporary"' EXIT
    head -c "$((limit + 1))" > "$temporary"
    [[ -s $temporary && $(stat -c %s "$temporary") -le $limit ]] || fail "Empty or oversized input: $name"
    case "$name" in
        ai_internal_token)
            value=$(<"$temporary")
            [[ $value =~ ^[a-f0-9]{64}$ && $(stat -c %s "$temporary") == 65 ]] ||
                fail 'Incomplete or invalid worker token.' ;;
        ai_runtime_url)
            value=$(<"$temporary")
            [[ $value =~ ^postgres://python_incidents:[a-f0-9]{64}@[a-z0-9.-]+:5432/cyberguard_incidents\?sslmode=verify-full\&sslrootcert=/run/certs/ca\.crt$ &&
               $(stat -c %s "$temporary") == "$(( ${#value} + 1 ))" ]] ||
                fail 'Incomplete or invalid scoped AI database URL.' ;;
        *) verify_transfer "$temporary" "$expected" ;;
    esac
    if present "$dest/$name"; then
        [[ -f $dest/$name && ! -L $dest/$name && $(stat -c '%U:%g:%a' "$dest/$name") == "$operator:$gid:640" ]] &&
            cmp -s "$temporary" "$dest/$name" ||
            fail "Existing file differs or has unsafe permissions: $name. Rotation is separate."
    else
        chown -- "$operator:$gid" "$temporary"
        chmod 0640 "$temporary"
        mv -T -- "$temporary" "$dest/$name"
    fi
)

export_ai() {
    local name=$1 file
    require_prepared
    case "$name" in ai_runtime_url|ai_internal_token) ;; *) fail 'Refusing to export a non-AI credential.' ;; esac
    file=$etc/secrets/credentials/runtime/$name
    [[ -f $file && ! -L $file && -s $file && $(stat -c '%U:%g:%a' "$file") == "$operator:$gid:640" &&
       $(realpath -- "$file") == "$file" ]] || fail "Invalid AI runtime file: $name"
    cat -- "$file"
}

check_database_volumes() {
    local volume labels
    for volume in identity-data incident-data; do
        labels=$(docker volume inspect --format '{{index .Labels "com.docker.compose.project"}}:{{index .Labels "com.docker.compose.volume"}}' \
            "${project}_$volume") || fail 'A managed database volume is missing. Restore it; no replacement was created.'
        [[ $labels == "$project:$volume" ]] || fail 'A database volume does not belong to this deployment.'
    done
}

make_app_credentials() {
    local directory=$etc/secrets/credentials
    # Credential generation publishes the directory atomically: it may have completed
    # just before an interrupted SSH call. Never regenerate a published directory.
    if ! present "$directory"; then
        sudo -n -u "$operator" -- bash "$repo/deploy/app-vm/create-secrets.sh" --config "$config"
    fi
    [[ -d $directory && ! -L $directory ]] || fail 'Expected the original application credential directory.'
}

prepare() {
    local file
    require_configured
    if done_step prepared; then require_prepared; return; fi
    once images manager build
    if [[ $role == app ]]; then
        once credentials make_app_credentials
        [[ -d $etc/secrets/credentials && ! -L $etc/secrets/credentials ]] ||
            fail 'Restore the original application credentials; they will not be regenerated.'
        once databases manager db-up
        check_database_volumes
        once database-roles manager bootstrap
        once migrations manager migrate
        once ai-role manager bootstrap-ai
        if grep -Fxq 'APP_ALLOW_DEMO_SEED=yes' "$config"; then once demo-accounts manager seed-demo; fi
    else
        for file in ai_runtime_url ai_internal_token api.crt api.key ca.crt; do
            [[ -s $etc/secrets/$file && ! -L $etc/secrets/$file ]] || fail "Missing AI file: $file"
        done
        once models sudo -n -u "$operator" -- bash "$repo/deploy/ai-vm/prepare-models.sh" --config "$config"
    fi
    mark prepared
}

require_prepared() {
    require_configured
    done_step prepared || fail "The $role VM is not fully provisioned. Rerun install."
    if [[ $role == app ]]; then check_database_volumes; fi
}

require_installed() {
    require_prepared
    done_step installed || fail "The $role VM did not complete initial cutover. Rerun install."
}

main() {
    [[ $# -ge 3 && $EUID == 0 ]] || fail 'Run this VM helper through sudo via remote-setup.sh.'
    role=$1 deployment=$2
    local action=$3
    shift 3
    [[ $role == app || $role == ai ]] || fail 'Invalid VM role.'
    [[ $deployment =~ ^[a-f0-9]{64}$ ]] || fail 'Invalid deployment ID.'
    operator=${SUDO_USER:-}
    [[ -n $operator && $operator != root ]] || fail 'Use sudo from the non-root VM account.'
    gid=$(id -g "$operator")
    readonly role deployment operator gid
    state="/var/lib/cyberguard-remote-$role"
    etc="/etc/cyberguard/$role"
    config="$etc/config.env"
    data="/var/lib/cyberguard-$role"
    project="cyberguard-$role-vm"
    unset DOCKER_HOST DOCKER_CONTEXT COMPOSE_FILE COMPOSE_PROJECT_NAME COMPOSE_PROFILES COMPOSE_ENV_FILES

    if [[ $action == inspect ]]; then inspect; return; fi
    repo=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
    if [[ $action == initialize ]]; then claim_fresh_state; else assert_own_state; fi
    exec 8<"$state"
    flock -n 8 || fail "Another remote install operation is active on the $role VM."

    case "$action:$#" in
        initialize:0)
            once bootstrapped bash "$repo/deploy/$role-vm/bootstrap.sh" --owner "$operator" ;;
        configure:1) configure_vm "$1" ;;
        receive:1|receive:2) receive_file "$@" ;;
        export-ai:1) [[ $role == app ]] || fail 'Only the app VM exports AI credentials.'; export_ai "$1" ;;
        prepare:0) prepare ;;
        require-state:0) ;;
        require-installed:0) require_installed ;;
        up:0|check:0) require_prepared; manager "$action" ;;
        check-ai:0) [[ $role == app ]] || fail 'Run check-ai on the app VM.'; require_prepared; manager check-ai ;;
        status:0|stop:0)
            if done_step configured; then require_configured; manager "$action"
            else printf '%s\n' 'Configuration not installed; no managed services to report or stop.'; fi ;;
        installed:0)
            require_prepared
            if ! done_step installed; then
                if [[ $role == app ]]; then manager maintenance-off; fi
                mark installed
            fi ;;
        *) fail 'Unknown or invalid remote install action.' ;;
    esac
}

if [[ -z ${BASH_SOURCE[0]:-} || ${BASH_SOURCE[0]} == "$0" ]]; then main "$@"; fi
