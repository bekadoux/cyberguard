#!/usr/bin/env bash
# Interactive, isolated, full-stack development on the local Docker engine.
# Account passwords must only appear in the intended terminal display.
set +x
set -Eeuo pipefail
umask 077

usage() {
    cat <<'EOF'
Usage: docker/local-setup.sh [up|resume|stop|status|logs|check|prepare-models]
       docker/local-setup.sh destroy [--purge-models]

With no arguments, set up the full development stack or resume it.
First setup and destruction require an interactive terminal.
EOF
}

die() { printf '%s\n' "$*" >&2; exit 1; }
step() { stage=$*; printf '\n==> %s\n' "$stage"; }

action=${1:-up}
purge=no
case "$action:$#" in
    -h:1|--help:1) usage; exit ;;
    destroy:2) [[ $2 == --purge-models ]] || { usage >&2; exit 2; }; purge=yes ;;
    up:0|up:1|resume:1|stop:1|status:1|logs:1|check:1|prepare-models:1|destroy:1) ;;
    *) usage >&2; exit 2 ;;
esac

for tool in docker sha256sum flock; do
    command -v "$tool" >/dev/null || die "Missing prerequisite: $tool"
done
root=$(CDPATH='' cd -P -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$root"
# shellcheck source=docker/ai/inference-cpus.sh
source "$root/docker/ai/inference-cpus.sh"
digest=$(printf '%s' "$root" | sha256sum)
project="cyberguard-full-${digest:0:16}"
local_dir="$root/.local"
state="$local_dir/full-stack"
models="$local_dir/full-stack-models"
settings="$state/settings"
secrets="$state/secrets"

# Do not use a remote Docker daemon or ambient Compose configuration.
unset COMPOSE_FILE COMPOSE_PROFILES COMPOSE_PROJECT_NAME COMPOSE_ENV_FILES \
    COMPOSE_REMOVE_ORPHANS COMPOSE_IGNORE_ORPHANS AI_INFERENCE_CPUS AI_INFERENCE_PIDS
context=$(docker context show) || die 'Could not resolve the Docker context.'
endpoint=${DOCKER_HOST:-}
if [[ -n ${DOCKER_CONTEXT:-} || -z $endpoint ]]; then
    endpoint=$(docker context inspect "$context" --format '{{.Endpoints.docker.Host}}')
fi
[[ $endpoint == unix:///* ]] || die 'Select a local Docker context backed by a Unix socket.'
engine=$(docker info --format '{{.OSType}}/{{.Architecture}}') ||
    die 'Docker is not ready or this user cannot access it.'
[[ $engine == linux/x86_64 || $engine == linux/amd64 ]] ||
    die 'Full local setup currently supports a Linux amd64 Docker engine.'
docker compose version >/dev/null || die 'The Docker Compose plugin is required.'

stage='Check local state'
temporary=''
cleanup() {
    if [[ -n $temporary ]]; then rm -rf -- "$temporary"; fi
}
trap cleanup EXIT
trap 'printf "Failed: %s. Run docker/local-setup.sh status or logs, then retry after fixing the error.\n" "$stage" >&2' ERR
trap 'exit 130' INT
trap 'exit 143' TERM

for dir in "$local_dir" "$state" "$models" "$secrets"; do
    [[ ! -L $dir && ( ! -e $dir || -d $dir ) ]] || die "Expected a directory, not a link or file: $dir"
done
if [[ $action == up ]]; then mkdir -p -- "$local_dir"; fi
[[ -d $local_dir ]] || die 'No full local setup. Run docker/local-setup.sh first.'
exec 9<"$local_dir"
flock -n 9 || die 'Another full local setup command is running for this checkout.'

export DOCKER_SECRETS_DIR="$secrets" DOCKER_SECRET_GID LOCAL_OPERATOR_UID
DOCKER_SECRET_GID=$(id -g)
LOCAL_OPERATOR_UID=$(id -u)
export LOCAL_MODEL_DIR="$models"
export DOCKER_GO_IMAGE="cyberguard-go:$project" DOCKER_WEB_IMAGE="cyberguard-web:$project"
export LOCAL_AI_IMAGE="cyberguard-ai:$project" LOCAL_KEV_IMAGE="cyberguard-kev:$project"
export LOCAL_PREPARE_IMAGE="cyberguard-prepare-models:$project"
export AI_MODE=models

compose() {
    docker compose --env-file /dev/null --project-directory "$root" --project-name "$project" \
        -f compose.yaml -f compose.ai.yaml -f compose.full.yaml --profile ai-models "$@"
}

open_terminal() {
    [[ -n ${terminal:-} ]] && return 0
    { exec {terminal}<>/dev/tty; } 2>/dev/null ||
        die 'This action requires an interactive terminal.'
}

ask() {
    printf '%s' "$1" >&"$terminal"
    IFS= read -r answer <&"$terminal" || die 'Setup cancelled; no answer received.'
}

local_ipv4() {
    local a b c d octet
    [[ $1 =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]] || return 1
    IFS=. read -r a b c d <<< "$1"
    for octet in "$a" "$b" "$c" "$d"; do
        [[ $octet == 0 || $octet != 0* ]] && ((octet <= 255)) || return 1
    done
    [[ $1 == 127.0.0.1 || $a == 10 || ($a == 172 && $b -ge 16 && $b -le 31) ||
        ($a == 192 && $b == 168) ]]
}

valid_port() { [[ $1 =~ ^[1-9][0-9]{3,4}$ ]] && (( $1 >= 1024 && $1 <= 65535 )); }

read_addresses() {
    command -v ip >/dev/null || die 'Missing prerequisite: ip (iproute2).'
    addresses=$(ip -o -4 addr show up scope global)
}

address_on_host() {
    [[ $1 == 127.0.0.1 || $addresses == *" inet $1/"* ]]
}

verify_bind_address() {
    if [[ $LOCAL_BIND_IP != 127.0.0.1 ]]; then
        read_addresses
        address_on_host "$LOCAL_BIND_IP" ||
            die "The saved LAN address $LOCAL_BIND_IP is not assigned to an active interface."
    fi
}

project_volumes() {
    docker volume ls --filter "label=com.docker.compose.project=$project" --format '{{.Name}}'
}

assert_no_project_resources() {
    local volumes containers networks
    volumes=$(project_volumes)
    containers=$(docker ps -aq --filter "label=com.docker.compose.project=$project")
    networks=$(docker network ls -q --filter "label=com.docker.compose.project=$project")
    [[ -z $volumes && -z $containers && -z $networks ]] ||
        die "$1"
}

configure() {
    local suggestion= interface address busy
    assert_no_project_resources "Docker resources already exist for $project without saved settings; refusing to adopt them."
    command -v openssl >/dev/null || die 'Missing prerequisite: openssl.'
    command -v ss >/dev/null || die 'Missing prerequisite: ss (iproute2).'
    read_addresses
    open_terminal
    while read -r _ interface _ address _; do
        address=${address%%/*}
        if [[ $interface != docker* && $interface != br-* ]] && local_ipv4 "$address"; then
            suggestion=$address
            break
        fi
    done <<< "$addresses"
    if [[ -n $suggestion ]]; then
        printf 'Detected LAN address: %s (enter "lan" to use it).\n' "$suggestion" >&"$terminal"
    fi
    while :; do
        ask 'Bind address: loopback, lan, or a local private IPv4 address [loopback]: '
        case "$answer" in
            ''|loopback) LOCAL_BIND_IP=127.0.0.1 ;;
            lan) LOCAL_BIND_IP=$suggestion ;;
            *) LOCAL_BIND_IP=$answer ;;
        esac
        if local_ipv4 "$LOCAL_BIND_IP" && address_on_host "$LOCAL_BIND_IP"; then break; fi
        printf 'Choose loopback or a private IPv4 address assigned to this machine.\n' >&"$terminal"
    done
    if [[ $LOCAL_BIND_IP != 127.0.0.1 ]]; then
        printf 'Only Caddy will be published. HTTP does not protect credentials or incident data.\n' >&"$terminal"
        ask "Expose CyberGuard at $LOCAL_BIND_IP on a trusted LAN for synthetic data only? [y/N] "
        [[ $answer == [yY] || $answer == [yY][eE][sS] ]] || die 'LAN exposure not accepted; nothing was deployed.'
    fi
    while :; do
        ask 'Web port [8088]: '
        LOCAL_WEB_PORT=${answer:-8088}
        if valid_port "$LOCAL_WEB_PORT"; then
            busy=$(ss -H -ltn "sport = :$LOCAL_WEB_PORT")
            if [[ -z $busy ]]; then break; fi
        fi
        printf 'Choose an unused TCP port from 1024 to 65535.\n' >&"$terminal"
    done
    while :; do
        ask 'Create the four development accounts? (Otherwise there is no initial login.) [Y/n] '
        case "$answer" in
            ''|[yY]|[yY][eE][sS]) seed=yes; break ;;
            [nN]|[nN][oO]) seed=no; break ;;
            *) printf 'Enter yes or no.\n' >&"$terminal" ;;
        esac
    done
    DEMO_INCIDENTS_ENABLED=false
    if [[ $seed == yes ]]; then
        while :; do
            ask 'Enable the admin button to add synthetic demo incidents? [y/N] '
            case "$answer" in
                [yY]|[yY][eE][sS]) DEMO_INCIDENTS_ENABLED=true; break ;;
                ''|[nN]|[nN][oO]) break ;;
                *) printf 'Enter yes or no.\n' >&"$terminal" ;;
            esac
        done
    fi
    printf '%s\n' 'Using all CPUs for inference can make the app or host less responsive under load.' >&"$terminal"
    while :; do
        ask 'AI inference threads per model (positive integer to limit) [all]: '
        inference_threads=${answer:-all}
        if valid_inference_threads "$inference_threads"; then break; fi
        printf 'Enter all or a positive whole number with up to 9 digits.\n' >&"$terminal"
    done
    printf '%s\n' \
        'Real inference uses roughly 3 GB of pinned model artifacts, plus Docker images and caches.' \
        'Review the model licenses: https://huggingface.co/jaredpalmer/kev-0.8b' \
        '  https://huggingface.co/Qwen/Qwen3.5-0.8B-Base' \
        '  https://huggingface.co/Qwen/Qwen2.5-1.5B-Instruct-GGUF' >&"$terminal"
    ask 'Prepare the pinned models (downloading missing artifacts) and deploy? [y/N] '
    [[ $answer == [yY] || $answer == [yY][eE][sS] ]] || die 'Model preparation not accepted; nothing was deployed.'
    mkdir -m 700 -p -- "$state"
    temporary=$(mktemp "$state/.settings.XXXXXX")
    printf '%s\n' "$LOCAL_BIND_IP" "$LOCAL_WEB_PORT" "$seed" "$DEMO_INCIDENTS_ENABLED" \
        "$inference_threads" > "$temporary"
    mv -T -- "$temporary" "$settings"
    temporary=''
}

read_settings() {
    local -a saved
    [[ -f $settings && ! -L $settings ]] || die "Expected a regular settings file: $settings"
    mapfile -t saved < "$settings"
    (( ${#saved[@]} >= 3 && ${#saved[@]} <= 5 )) &&
        local_ipv4 "${saved[0]}" && valid_port "${saved[1]}" &&
        [[ ${saved[2]} == yes || ${saved[2]} == no ]] || die "Invalid settings: $settings"
    LOCAL_BIND_IP=${saved[0]} LOCAL_WEB_PORT=${saved[1]} seed=${saved[2]}
    DEMO_INCIDENTS_ENABLED=${saved[3]-false}
    [[ $DEMO_INCIDENTS_ENABLED == false || ( $DEMO_INCIDENTS_ENABLED == true && $seed == yes ) ]] ||
        die "Invalid demo incident setting: $settings"
    inference_threads=${saved[4]-all}
    valid_inference_threads "$inference_threads" || die "Invalid AI inference thread setting: $settings"
}

ensure_secrets() {
    local volumes name
    volumes=$(project_volumes)
    if [[ ! -d $secrets ]]; then
        [[ -z $volumes && ! -e $state/databases.done ]] ||
            die 'Database state exists without its secrets. Restore the original secret files; they will not be regenerated.'
        step 'Generate scoped runtime and development credentials'
        temporary=$(mktemp -d "$state/.secrets.XXXXXX")
        unset DOCKER_IDENTITY_PORT DOCKER_INCIDENT_PORT
        DOCKER_SECRETS_DIR="$temporary/secrets" sh docker/create-secrets.sh >/dev/null
        DOCKER_SECRETS_DIR="$temporary/secrets" sh docker/ai/create-ai-secrets.sh >/dev/null
        mv -T -- "$temporary/secrets" "$secrets"
        rmdir -- "$temporary"
        temporary=''
    fi
    for name in identity_admin_password identity_migration_password identity_runtime_password \
        incidents_admin_password incidents_migration_password incidents_runtime_password \
        identity_runtime_url identity_migration_url incidents_runtime_url incidents_migration_url \
        ai_runtime_url ai_runtime_password ai_internal_token \
        dev_employee_a_password dev_employee_b_password dev_analyst_password dev_admin_password; do
        [[ -f $secrets/$name && ! -L $secrets/$name && -s $secrets/$name ]] ||
            die "Missing or invalid secret file: $secrets/$name. Restore it; setup will not overwrite it."
    done
    if [[ -e $state/databases.done ]]; then
        [[ $'\n'$volumes$'\n' == *$'\n'"${project}_identity-data"$'\n'* &&
           $'\n'$volumes$'\n' == *$'\n'"${project}_incident-data"$'\n'* ]] ||
            die 'A provisioned database volume is missing; refusing to create an empty replacement.'
    fi
}

ensure_image() {
    local image=$1 file=$2 marker="$state/${1%%:*}.built"
    if [[ ! -f $marker ]] || ! docker image inspect "$image" >/dev/null 2>&1; then
        docker build --tag "$image" --file "$file" .
        touch -- "$marker"
    fi
}

ensure_prepare_image() {
    local file=docker/ai/Dockerfile.prepare-models
    local marker="$state/${LOCAL_PREPARE_IMAGE%%:*}.built" inputs stamp
    # Match the files copied by the Dockerfile. Old empty receipts force a
    # rebuild instead of reusing a tool image that may lack a sourced helper.
    inputs=$(sha256sum -- "$file" docker/ai/inference-cpus.sh \
        deploy/ai-vm/{common.sh,prepare-models.sh,models.json})
    stamp=$(printf '%s\n' "$inputs" | sha256sum)
    stamp=${stamp%% *}
    if [[ ! -f $marker || -L $marker || $(<"$marker") != "$stamp" ]] ||
       ! docker image inspect "$LOCAL_PREPARE_IMAGE" >/dev/null 2>&1; then
        docker build --tag "$LOCAL_PREPARE_IMAGE" --file "$file" .
        temporary=$(mktemp "$state/.prepare-image.XXXXXX")
        printf '%s\n' "$stamp" > "$temporary"
        mv -T -- "$temporary" "$marker"
        temporary=''
    fi
}

up() { compose up --wait --wait-timeout "$1" --detach --no-build --pull never "${@:2}"; }
run_tool() { compose run --rm --no-deps --pull never --interactive=false -T "$@"; }

database_setup() {
    local service
    step 'Start both PostgreSQL instances'
    up 90 identity-db incident-db
    touch -- "$state/databases.done"
    for service in bootstrap-identity bootstrap-incidents migrate-identity migrate-incidents bootstrap-ai; do
        if [[ ! -e $state/$service.done ]]; then
            step "Provision database: $service"
            run_tool "$service"
            touch -- "$state/$service.done"
        fi
    done
}

password_hint() {
    printf 'Development passwords: %s/dev_<username>_password\n' "$secrets"
    printf 'For example: cat -- %q\n' "$secrets/dev_admin_password"
    printf 'These are the initial passwords, not any replacements changed in the app.\n'
}

seed_accounts() {
    [[ $seed == yes && ! -e $state/seed-demo.done ]] || return 0
    local username password
    open_terminal
    step 'Seed development accounts'
    run_tool seed-demo
    touch -- "$state/seed-demo.done"
    printf '\nInitial development passwords (shown only once; keep them private):\n' >&"$terminal"
    for username in employee_a employee_b analyst admin; do
        password=$(<"$secrets/dev_${username}_password")
        printf '  %-10s %s\n' "$username" "$password" >&"$terminal"
    done
    password_hint >&"$terminal"
}

prepare_models() {
    local running
    running=$(compose ps --status running --services ai-worker ai-kev ai-llm)
    [[ -z $running ]] || die 'Stop the full stack before preparing or repairing model files.'
    step 'Download and verify the pinned Kev and Qwen model artifacts'
    mkdir -m 755 -p -- "$models"
    ensure_prepare_image
    run_tool prepare-models
    touch -- "$state/models.done"
}

deploy() {
    verify_bind_address
    configure_inference_resources "$inference_threads" 256
    ensure_secrets
    step 'Validate the full local Compose configuration'
    compose --profile tools --profile local-setup config --quiet
    step 'Build application images and obtain PostgreSQL'
    ensure_image "$DOCKER_GO_IMAGE" backend/go/Dockerfile
    ensure_image "$DOCKER_WEB_IMAGE" frontend/Dockerfile
    compose pull --policy missing identity-db
    database_setup
    seed_accounts
    step 'Start the core application and HTTP gateway'
    up 90 go-api web
    printf 'Core application: http://%s:%s/login\n' "$LOCAL_BIND_IP" "$LOCAL_WEB_PORT"
    if [[ $DEMO_INCIDENTS_ENABLED == true ]]; then
        printf '%s\n' 'Sign in as admin and open Admin → Demo data to add unused demo incidents.'
    fi

    if [[ ! -e $state/models.done ]]; then
        prepare_models
    else
        step 'Verify the cached model files (no downloads)'
        [[ -d $models ]] || die 'Model directory missing. Stop the stack and run docker/local-setup.sh prepare-models.'
        ensure_prepare_image
        run_tool prepare-models --check ||
            die 'Model verification failed. Investigate the artifact; stop the stack before running prepare-models.'
    fi
    step 'Build the AI runtime images and obtain llama.cpp'
    ensure_image "$LOCAL_AI_IMAGE" backend/python/Dockerfile
    ensure_image "$LOCAL_KEV_IMAGE" docker/ai/Dockerfile.kev
    compose pull --policy missing ai-llm
    step 'Start the AI assessment API'
    up 90 ai-api
    step "Load Kev and Qwen ($AI_INFERENCE_CPUS inference threads per model)"
    up 600 ai-kev ai-llm
    step 'Start the worker and wait for authenticated dependency checks'
    up 150 ai-worker
    printf '\nFull CyberGuard stack is ready at http://%s:%s/login\n' "$LOCAL_BIND_IP" "$LOCAL_WEB_PORT"
    printf 'Worker readiness passed. To validate real inference, submit a new synthetic report and check its assessment as staff.\n'
    if [[ $seed == yes ]]; then password_hint; fi
}

destroy() {
    open_terminal
    printf 'This will permanently delete the containers, database volumes, credentials and settings for %s only.\n' \
        "$project" >&"$terminal"
    if [[ $purge == yes ]]; then
        printf 'It will also delete the model cache at %s.\n' "$models" >&"$terminal"
    else
        printf 'The pinned model cache will be kept at %s.\n' "$models" >&"$terminal"
    fi
    ask 'Type "destroy" to continue: '
    [[ $answer == destroy ]] || die 'Cancelled; nothing was deleted.'
    if [[ -f $settings ]]; then
        step 'Destroy full local Docker state'
        compose --profile tools --profile local-setup down --volumes --remove-orphans
        assert_no_project_resources "Some Docker resources for $project remain. Secrets were kept; inspect the project before retrying."
    fi
    rm -rf --one-file-system -- "$state"
    if [[ $purge == yes ]]; then rm -rf --one-file-system -- "$models"; fi
    printf 'Destroyed the full local deployment. Docker images and build caches were left in place.\n'
}

export LOCAL_BIND_IP LOCAL_WEB_PORT DEMO_INCIDENTS_ENABLED
if [[ -e $settings || -L $settings ]]; then
    read_settings
else
    case "$action" in
        up) configure ;;
        destroy) assert_no_project_resources "Docker resources already exist for $project without saved settings; refusing to delete them." ;;
        *) die 'No full local setup. Run docker/local-setup.sh first.' ;;
    esac
fi

case "$action" in
    up|resume) deploy ;;
    stop)
        step 'Stop the gateway and worker before the remaining services'
        compose stop web ai-worker
        compose stop
        ;;
    status) compose ps --all ;;
    logs) compose logs --tail 100 ;;
    check) compose exec -T ai-worker python -m cyberguard_ai.preflight ;;
    prepare-models) prepare_models ;;
    destroy) destroy ;;
esac
