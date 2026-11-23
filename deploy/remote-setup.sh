#!/usr/bin/env bash
# Workstation entrypoint for the existing two-VM Azure deployment.
set +x
set -euo pipefail
umask 077

root=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
# shellcheck source=docker/ai/inference-cpus.sh
source "$root/docker/ai/inference-cpus.sh"
work_dir=$PWD
config="$root/.local/remote-setup.env"
command=install
if [[ ${1:-} == --config && $# -ge 2 ]]; then
    config=$2
    shift 2
fi
if (( $# )); then command=$1; shift; fi
reset_mode=managed
if [[ $command == reset && ${1:-} == --legacy ]]; then reset_mode=legacy; shift; fi
usage() {
    printf '%s\n' \
        'Usage: bash deploy/remote-setup.sh [--config FILE] [install|preflight|resume|status|check|stop]' \
        '       bash deploy/remote-setup.sh [--config FILE] reset [--legacy]' \
        'Default: install. The default settings file is .local/remote-setup.env.'
}
die() { printf '%s\n' "$*" >&2; exit 1; }
(( $# == 0 )) || { usage >&2; exit 2; }
case "$command" in
    -h|--help) usage; exit 0 ;;
    install|preflight|resume|status|check|stop|reset) ;;
    *) usage >&2; exit 2 ;;
esac

for tool in ssh git sha256sum flock curl openssl getent; do
    command -v "$tool" >/dev/null || die "Missing workstation prerequisite: $tool"
done

# Explicit, literal configuration; values never get evaluated as shell code.
declare -A settings=(
    [APP_SSH]='' [APP_KEY]='' [AI_SSH]='' [AI_KEY]='' [REVISION]=''
    [APP_PUBLIC_NAME]='' [APP_PRIVATE_IP]='' [AI_PRIVATE_IP]=''
    [APP_GO_TLS_NAME]='app.internal' [APP_DB_TLS_NAME]='incidents.internal' [AI_TLS_NAME]='ai.internal'
    [TLS_DIR]='' [PUBLIC_TLS]='acme' [DEMO_SEED]='no' [DEMO_INCIDENTS]='no' [NETWORK_APPROVED]='no'
    [AI_INFERENCE_THREADS]='all'
    [APP_EDGE_SUBNET]='172.28.40.0/28' [APP_IDENTITY_SUBNET]='172.28.41.0/28'
    [APP_INCIDENT_SUBNET]='172.28.42.0/28'
)

load_settings() {
    local key line
    local -A seen=()
    [[ -f $config && ! -L $config ]] || die "Expected a regular settings file: $config"
    while IFS= read -r line || [[ -n $line ]]; do
        [[ -z $line || $line == \#* ]] && continue
        key=${line%%=*}
        [[ $line == *=* && $key =~ ^[A-Z_]+$ ]] || die 'Expected literal KEY=value settings.'
        [[ -v settings[$key] && ! -v seen[$key] ]] ||
            die "Unknown or duplicate setting: $key"
        settings[$key]=${line#*=}
        seen[$key]=1
    done < "$config"
}

prompt() {
    local key=$1 label=$2 answer
    printf '%s [%s]: ' "$label" "${settings[$key]}" >&"$terminal"
    IFS= read -r answer <&"$terminal" || die 'Setup cancelled.'
    settings[$key]=${answer:-${settings[$key]}}
}

configure() {
    { exec {terminal}<>/dev/tty; } 2>/dev/null || die 'First setup needs a terminal or --config FILE.'
    prompt APP_SSH 'Application VM SSH alias or user@host'
    prompt APP_KEY 'Application VM private key path (blank uses SSH config/agent)'
    prompt AI_SSH 'AI VM SSH alias or user@host'
    prompt AI_KEY 'AI VM private key path (blank uses SSH config/agent)'
    prompt APP_PUBLIC_NAME 'Public DNS name'
    prompt APP_PRIVATE_IP 'Application VM private IPv4'
    prompt AI_PRIVATE_IP 'AI VM private IPv4'
    prompt APP_GO_TLS_NAME 'Private Go DNS name'
    prompt APP_DB_TLS_NAME 'Private incident database DNS name'
    prompt AI_TLS_NAME 'Private AI gateway certificate DNS name'
    prompt TLS_DIR 'Workstation directory containing the approved TLS files'
    prompt PUBLIC_TLS 'Public certificate mode (acme or provided)'
    printf '%s\n' \
        'Synthetic demo only: seeding creates four development accounts. Do not opt in for real data.' \
        'The installer never prints passwords, changes NSGs/firewalls, or issues private certificates.' >&"$terminal"
    prompt DEMO_SEED 'Create demo accounts (yes or no)'
    if [[ ${settings[DEMO_SEED]} == yes ]]; then
        prompt DEMO_INCIDENTS 'Enable the admin button to add synthetic demo incidents (yes or no)'
    fi
    printf '%s\n' 'Using all CPUs on the AI VM can make its other services less responsive under load.' >&"$terminal"
    while :; do
        prompt AI_INFERENCE_THREADS 'AI VM inference threads per model (positive integer to limit)'
        if valid_inference_threads "${settings[AI_INFERENCE_THREADS]}"; then break; fi
        settings[AI_INFERENCE_THREADS]=all
        printf 'Enter all or a positive whole number with up to 9 digits.\n' >&"$terminal"
    done
    printf 'Review public 80/443 and the private VM /32 allowlists before approving network exposure.\n' >&"$terminal"
    prompt NETWORK_APPROVED 'I reviewed and approved DNS, NSGs and Docker-aware firewall policy (yes or no)'
    [[ ${settings[NETWORK_APPROVED]} == yes ]] || die 'Network review not approved. Nothing was deployed.'
}

local_path() {
    case "$1" in
        '~/'*) printf '%s/%s' "$HOME" "${1:2}" ;;
        /*) printf '%s' "$1" ;;
        *) printf '%s/%s' "$2" "$1" ;;
    esac
}

valid_dns() {
    [[ $1 =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ && $1 == *.* && $1 != *..* && ! $1 =~ ^[0-9.]+$ ]]
}

private_ip() {
    local a b c d octet
    [[ $1 =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]] || return 1
    IFS=. read -r a b c d <<< "$1"
    for octet in "$a" "$b" "$c" "$d"; do
        [[ $octet == 0 || $octet != 0* ]] && (( octet <= 255 )) || return 1
    done
    [[ $a == 10 || ( $a == 172 && $b -ge 16 && $b -le 31 ) || ( $a == 192 && $b == 168 ) ]]
}

declare -A hosts keys
validate_settings() {
    local base=$1 key value subnet octet
    for key in APP_SSH AI_SSH; do
        [[ ${settings[$key]} =~ ^([a-zA-Z0-9_][a-zA-Z0-9_.-]*@)?[a-zA-Z0-9][a-zA-Z0-9_.-]*$ ]] ||
            die "Invalid $key; use an SSH alias or user@host."
    done
    hosts[app]=${settings[APP_SSH]} hosts[ai]=${settings[AI_SSH]}
    [[ ${hosts[app]} != "${hosts[ai]}" ]] || die 'The two SSH targets must differ.'
    for key in APP_KEY AI_KEY TLS_DIR; do
        value=${settings[$key]}
        if [[ -n $value ]]; then settings[$key]=$(local_path "$value" "$base"); fi
    done
    keys[app]=${settings[APP_KEY]} keys[ai]=${settings[AI_KEY]}
    for key in app ai; do
        value=${keys[$key]}
        [[ -z $value || ( -f $value && -r $value ) ]] || die "Cannot read the $key SSH key file."
    done
    if [[ $command == reset && $reset_mode == legacy ]]; then return; fi
    [[ ${settings[PUBLIC_TLS]} == acme || ${settings[PUBLIC_TLS]} == provided ]] || die 'Invalid PUBLIC_TLS.'
    [[ ${settings[DEMO_SEED]} == yes || ${settings[DEMO_SEED]} == no ]] || die 'Invalid DEMO_SEED.'
    [[ ${settings[DEMO_INCIDENTS]} == yes || ${settings[DEMO_INCIDENTS]} == no ]] || die 'Invalid DEMO_INCIDENTS.'
    [[ ${settings[DEMO_INCIDENTS]} == no || ${settings[DEMO_SEED]} == yes ]] ||
        die 'DEMO_INCIDENTS=yes requires DEMO_SEED=yes.'
    valid_inference_threads "${settings[AI_INFERENCE_THREADS]}" ||
        die 'AI_INFERENCE_THREADS must be all or a positive integer with up to 9 digits.'
    [[ ${settings[NETWORK_APPROVED]} == yes ]] || die 'Review network policy, then set NETWORK_APPROVED=yes.'
    for key in APP_PUBLIC_NAME APP_GO_TLS_NAME APP_DB_TLS_NAME AI_TLS_NAME; do
        valid_dns "${settings[$key]}" || die "Invalid DNS name: $key"
    done
    [[ ${settings[APP_PUBLIC_NAME]} != *.invalid ]] || die 'Configure the public DNS hostname.'
    for key in APP_PRIVATE_IP AI_PRIVATE_IP; do
        private_ip "${settings[$key]}" || die "Expected a private IPv4 address: $key"
    done
    [[ ${settings[APP_PRIVATE_IP]} != "${settings[AI_PRIVATE_IP]}" ]] || die 'VM addresses must differ.'
    for key in APP_EDGE_SUBNET APP_IDENTITY_SUBNET APP_INCIDENT_SUBNET; do
        subnet=${settings[$key]%/28}
        [[ ${settings[$key]} == */28 ]] && private_ip "$subnet" || die "Invalid private /28: $key"
        octet=${subnet##*.}
        (( octet % 16 == 0 )) || die "Not a /28 network address: $key"
        [[ ${settings[APP_PRIVATE_IP]} != "${subnet%.*}."* &&
           ${settings[AI_PRIVATE_IP]} != "${subnet%.*}."* ]] || die "VM addresses overlap the Docker subnet /24: $key"
    done
    [[ ${settings[APP_EDGE_SUBNET]} != "${settings[APP_IDENTITY_SUBNET]}" &&
       ${settings[APP_EDGE_SUBNET]} != "${settings[APP_INCIDENT_SUBNET]}" &&
       ${settings[APP_IDENTITY_SUBNET]} != "${settings[APP_INCIDENT_SUBNET]}" ]] || die 'Docker subnets must differ.'
    if [[ -z ${settings[REVISION]} ]]; then settings[REVISION]=$(git -C "$root" rev-parse --verify HEAD); fi
    revision=${settings[REVISION]}
    [[ $revision =~ ^[a-f0-9]{40}$ ]] || die 'REVISION must be a full Git commit ID.'
    if [[ $command == install || $command == preflight ]]; then
        git -C "$root" cat-file -e "$revision:deploy/remote/host.sh" ||
            die 'The selected commit must contain the remote host installer. Use a reviewed, committed revision.'
        if [[ ${settings[DEMO_INCIDENTS]} == yes ]]; then
            git -C "$root" grep -q '^DEMO_INCIDENTS_ENABLED=' "$revision" -- deploy/app-vm/config.env.example ||
                die 'The selected revision does not support the demo incidents button.'
        fi
        if [[ ${settings[AI_INFERENCE_THREADS]} != all ]]; then
            git -C "$root" grep -q '^AI_INFERENCE_THREADS=' "$revision" -- deploy/ai-vm/config.env.example ||
                die 'The selected revision does not support setting AI_INFERENCE_THREADS.'
        fi
    fi
}

config=$(local_path "$config" "$work_dir")
if [[ $command == reset ]]; then
    { exec {terminal}<>/dev/tty; } 2>/dev/null || die 'Reset requires an interactive terminal.'
fi
if [[ -e $config || -L $config ]]; then
    load_settings
    validate_settings "$(dirname -- "$config")"
else
    [[ $command == install ]] || die "Settings not found: $config"
    configure
    validate_settings "$work_dir"
    mkdir -p -- "$(dirname -- "$config")"
    (
        set -o noclobber
        for key in "${!settings[@]}"; do printf '%s=%s\n' "$key" "${settings[$key]}"; done | LC_ALL=C sort > "$config"
    ) || die "Could not save new settings: $config"
    printf 'Saved non-secret settings to %s\n' "$config"
fi

exec 9<"$config"
flock -n 9 || die 'Another installer is using this settings file.'
# Short local path: OpenSSH multiplexing sockets have a platform path-length limit.
control=$(mktemp -d /tmp/cyberguard-ssh.XXXXXXXX)
ssh_args=(-o StrictHostKeyChecking=yes -o ForwardAgent=no -o ForwardX11=no
    -o ClearAllForwardings=yes -o PreferredAuthentications=publickey
    -o ConnectTimeout=10 -o ServerAliveInterval=15 -o ServerAliveCountMax=3)
connected=()
cleanup() {
    local result=$1 role
    for role in "${connected[@]}"; do
        ssh "${ssh_args[@]}" -S "$control/$role" -O exit -- "${hosts[$role]}" </dev/null >/dev/null 2>&1 || true
    done
    rmdir -- "$control" 2>/dev/null || true
    if (( result != 0 )); then
        printf 'Remote setup failed during: %s. Correct the reported issue and rerun the same command.\n' "${phase:-SSH connection}" >&2
    fi
}
trap 'cleanup "$?"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

connect() {
    local role=$1 batch=yes
    local -a identity=()
    [[ ! -t 0 || ! -t 2 ]] || batch=no
    if [[ -n ${keys[$role]} ]]; then identity=(-o IdentitiesOnly=yes -i "${keys[$role]}"); fi
    printf 'Connecting to the %s VM (%s)…\n' "$role" "${hosts[$role]}"
    ssh "${ssh_args[@]}" "${identity[@]}" -S "$control/$role" -o ControlPersist=no \
        -o "BatchMode=$batch" -M -N -f -- "${hosts[$role]}" ||
        die "SSH connection to the $role VM failed. Check known_hosts, username and key; for automation unlock the key with ssh-add first."
    connected+=("$role")
}

remote() {
    local role=$1 arg line=''
    shift
    # SSH sends a string, not an argv array. Quote each argument for the remote login shell.
    for arg in "$@"; do line+="'${arg//\'/\'\\\'\'}' "; done
    ssh "${ssh_args[@]}" -S "$control/$role" -o ControlMaster=no -o BatchMode=yes \
        -o ProxyCommand=false -T -- "${hosts[$role]}" "$line"
}

host() {
    local role=$1
    shift
    remote "$role" bash -c 'exec sudo -n bash "$HOME/.local/share/cyberguard-remote/releases/$1/deploy/remote/host.sh" "${@:2}"' \
        bash "$revision" "$role" "$deployment" "$@"
}

vm_preflight() {
    remote "$1" bash -s -- "$2" "$3" "${settings[APP_PRIVATE_IP]}" \
        "${settings[APP_GO_TLS_NAME]}" "${settings[APP_DB_TLS_NAME]}" <<'REMOTE'
set -euo pipefail
sudo -n true || { echo 'Non-interactive sudo is required for the deployment operator.' >&2; exit 1; }
[[ $(id -u) != 0 ]] || { echo 'Connect as the non-root VM operator.' >&2; exit 1; }
source /etc/os-release
[[ $ID == ubuntu && $VERSION_ID == 26.04 && $(dpkg --print-architecture) == amd64 ]] ||
    { echo 'Expected Ubuntu 26.04 amd64.' >&2; exit 1; }
[[ $(ip -o -4 addr show scope global | awk '{split($4,a,"/"); print a[1]}') =~ (^|$'\n')"$1"($'\n'|$) ]] ||
    { echo "Private address $1 is not assigned to this VM." >&2; exit 1; }
if [[ $2 == ai ]]; then
    for name in "$4" "$5"; do
        resolved=$(getent ahostsv4 "$name" | awk '{print $1}' | sort -u) ||
            { echo "Private DNS lookup failed on the AI VM: $name." >&2; exit 1; }
        [[ $resolved == "$3" ]] || { echo "Private DNS for $name must resolve to application VM $3." >&2; exit 1; }
    done
fi
REMOTE
}

tls_dir=${settings[TLS_DIR]}
app_tls=(ca.crt identity.crt identity.key incident.crt incident.key go.crt go.key)
ai_tls=(ca.crt api.crt api.key)
if [[ ${settings[PUBLIC_TLS]} == provided ]]; then app_tls+=(public.crt public.key); fi

check_certificate() {
    local name=$1 dns=$2 cert_public key_public sans
    local crt="$tls_dir/$name.crt" key="$tls_dir/$name.key"
    openssl verify -x509_strict -purpose sslserver -CAfile "$tls_dir/ca.crt" \
        -untrusted "$crt" -verify_hostname "$dns" "$crt" >/dev/null 2>&1 ||
        die "Invalid TLS certificate or chain: $name.crt ($dns)"
    sans=$(openssl x509 -in "$crt" -noout -ext subjectAltName)
    printf '%s\n' "$sans" | tr ',' '\n' | grep -Eq "^[[:space:]]*DNS:${dns//./\\.}[[:space:]]*$" ||
        die "Certificate $name.crt needs the exact DNS SAN $dns."
    openssl x509 -in "$crt" -noout -checkend 86400 >/dev/null || die "Certificate expires within 24 hours: $name.crt"
    cert_public=$(openssl x509 -in "$crt" -noout -pubkey | openssl pkey -pubin -outform DER | sha256sum)
    key_public=$(openssl pkey -in "$key" -passin pass: -pubout -outform DER 2>/dev/null | sha256sum) ||
        die "Unreadable or encrypted TLS server key: $name.key"
    [[ $cert_public == "$key_public" ]] || die "TLS key does not match: $name.key"
}

preflight() {
    local file
    printf '%s\n' 'Checking the approved TLS files, public DNS and both VM prerequisites…'
    for file in "${app_tls[@]}" "${ai_tls[@]}"; do
        [[ -n $tls_dir && -f $tls_dir/$file && ! -L $tls_dir/$file && -s $tls_dir/$file && -r $tls_dir/$file ]] ||
            die "Missing readable regular file in TLS_DIR: $file"
    done
    check_certificate identity identity-db
    check_certificate incident incident-db
    check_certificate incident "${settings[APP_DB_TLS_NAME]}"
    check_certificate go "${settings[APP_GO_TLS_NAME]}"
    check_certificate api "${settings[AI_TLS_NAME]}"
    if [[ ${settings[PUBLIC_TLS]} == provided ]]; then check_certificate public "${settings[APP_PUBLIC_NAME]}"; fi
    getent ahostsv4 "${settings[APP_PUBLIC_NAME]}" >/dev/null || die 'Public DNS hostname does not resolve over IPv4.'
    vm_preflight app "${settings[APP_PRIVATE_IP]}" app
    vm_preflight ai "${settings[AI_PRIVATE_IP]}" ai
}

inspect_host() {
    git -C "$root" show "$revision:deploy/remote/host.sh" |
        remote "$1" sudo -n bash -s -- "$1" "$deployment" inspect
}

reset_host() {
    local role=$1 verb=$2 expected=$app_machine
    if [[ $role == ai ]]; then expected=$ai_machine; fi
    # Stream the current helper: reset must also work against releases deployed
    # before this action existed, without uploading or changing those releases.
    remote "$role" sudo -n bash -s -- "$role" "$deployment" "$verb" "$reset_mode" "$expected" \
        < "$root/deploy/remote/host.sh"
}

reset_deployment() {
    local answer
    phase='verify both reset targets'
    printf 'Reset scope: %s (%s), then %s (%s).\n' \
        'AI VM' "${hosts[ai]}" 'app VM' "${hosts[app]}" >&"$terminal"
    reset_host ai reset-plan >&"$terminal"
    reset_host app reset-plan >&"$terminal"
    printf '%s\n' \
        'This permanently deletes the listed CyberGuard containers, networks, volumes, credentials,' \
        'certificates, deployment state and cached models. It does not reinstall anything.' >&"$terminal"
    printf 'Type RESET to continue: ' >&"$terminal"
    IFS= read -r answer <&"$terminal" || die 'Reset cancelled; nothing was deleted.'
    [[ $answer == RESET ]] || die 'Reset cancelled; nothing was deleted.'
    phase='reset the AI VM'
    reset_host ai reset
    phase='reset the app VM'
    reset_host app reset
    printf '%s\n' 'Remote reset complete. Run preflight and install to deploy again.'
}

upload() {
    git -C "$root" archive "$revision" |
        remote "$1" bash -c '
            set -euo pipefail
            umask 077
            releases="$HOME/.local/share/cyberguard-remote/releases"
            mkdir -p -- "$releases"
            [[ $(realpath -- "$releases") == "$releases" ]] || exit 1
            target="$releases/$1"
            if [[ -e $target || -L $target ]]; then
                [[ -d $target && ! -L $target && -O $target && -f $target/.remote-release &&
                   $(cat "$target/.remote-release") == "$1" ]] || exit 1
                cat >/dev/null
                exit
            fi
            staging=$(mktemp -d "$releases/.incoming.XXXXXXXX")
            trap '"'"'rm -rf -- "$staging"'"'"' EXIT
            # Keep Git'"'"'s file modes: several files will be read by non-root container users.
            tar --no-same-owner -xpf - -C "$staging"
            [[ -f $staging/deploy/remote/host.sh ]] || exit 1
            printf "%s\n" "$1" > "$staging/.remote-release"
            mv -T --no-clobber -- "$staging" "$target"
            [[ ! -d $staging ]]
        ' bash "$revision"
}

app_config() {
    local key
    for key in APP_PUBLIC_NAME APP_GO_TLS_NAME APP_DB_TLS_NAME APP_EDGE_SUBNET APP_IDENTITY_SUBNET APP_INCIDENT_SUBNET; do
        printf '%s=%s\n' "$key" "${settings[$key]}"
    done
    printf '%s\n' "APP_BIND_IP=${settings[APP_PRIVATE_IP]}" "APP_AI_IP=${settings[AI_PRIVATE_IP]}" \
        "APP_AI_TLS_NAME=${settings[AI_TLS_NAME]}" "APP_PUBLIC_TLS=${settings[PUBLIC_TLS]}" \
        "APP_ALLOW_DEMO_SEED=${settings[DEMO_SEED]}" 'APP_NETWORK_APPROVED=yes' \
        "APP_GO_IMAGE=cyberguard-go:$revision" "APP_WEB_IMAGE=cyberguard-web:$revision"
    # False is already the app default. Keep the payload and deployment receipt
    # unchanged for setups that did not opt in, including older pinned revisions.
    if [[ ${settings[DEMO_INCIDENTS]} == yes ]]; then
        printf '%s\n' 'DEMO_INCIDENTS_ENABLED=true'
    fi
}

ai_config() {
    printf '%s\n' "AI_BIND_IP=${settings[AI_PRIVATE_IP]}" 'AI_NETWORK_APPROVED=yes' \
        "AI_GO_URL=https://${settings[APP_GO_TLS_NAME]}:8443" "AI_TLS_NAME=${settings[AI_TLS_NAME]}" \
        "AI_IMAGE=cyberguard-ai:$revision" "AI_KEV_IMAGE=cyberguard-kev:$revision"
    # The supported default needs no payload change; keep older releases and receipts valid.
    if [[ ${settings[AI_INFERENCE_THREADS]} != all ]]; then
        printf '%s\n' "AI_INFERENCE_THREADS=${settings[AI_INFERENCE_THREADS]}"
    fi
}

send_config() {
    local role=$1 payload digest
    payload=$("${role}_config")
    digest=$(printf '%s\n' "$payload" | sha256sum)
    printf '%s\n' "$payload" | host "$role" configure "${digest%% *}"
}

send_certificate() {
    local role=$1 file=$2 digest
    digest=$(sha256sum < "$tls_dir/$file")
    host "$role" receive "$file" "${digest%% *}" < "$tls_dir/$file"
}

public_check() {
    local -a trust=()
    [[ ${settings[PUBLIC_TLS]} != provided ]] || trust=(--cacert "$tls_dir/ca.crt")
    curl --fail --silent --show-error --noproxy '*' --connect-timeout 5 --max-time 15 \
        --retry 3 --retry-all-errors --retry-max-time 40 "${trust[@]}" \
        "https://${settings[APP_PUBLIC_NAME]}/api/readyz" >/dev/null
}

phase='SSH connection'
connect app
connect ai
app_machine=$(remote app cat /etc/machine-id </dev/null)
ai_machine=$(remote ai cat /etc/machine-id </dev/null)
[[ $app_machine =~ ^[a-f0-9]{32}$ && $ai_machine =~ ^[a-f0-9]{32}$ && $app_machine != "$ai_machine" ]] ||
    die 'SSH must identify two different VMs with valid machine IDs.'
# Bind receipts to the VM pair, revision and effective topology, not workstation paths or SSH aliases.
if [[ $command == reset && $reset_mode == legacy ]]; then
    deployment=legacy
else
    deployment=$(
        { printf '%s\n' "$app_machine" "$ai_machine"; app_config; ai_config; } |
            sha256sum
    )
    deployment=${deployment%% *}
fi

case "$command" in
    reset)
        reset_deployment
        exit
        ;;
    install|preflight)
        phase='prerequisite checks'
        preflight
        inspect_host app
        inspect_host ai
        if [[ $command == preflight ]]; then
            printf '%s\n' 'Preflight passed. No VM files or services were changed; live TLS/allowed paths are checked at startup.'
            exit
        fi
        phase='upload the reviewed release and prepare both hosts'
        for role in app ai; do
            upload "$role"
            host "$role" initialize </dev/null
        done
        phase='install matching configuration and TLS files'
        send_config app
        send_config ai
        for file in "${app_tls[@]}"; do send_certificate app "$file"; done
        for file in "${ai_tls[@]}"; do send_certificate ai "$file"; done
        phase='build and provision the application VM'
        host app prepare </dev/null
        phase='start the application and verify public HTTPS'
        host app up </dev/null
        public_check
        printf '%s\n' 'Application HTTPS is ready. Checking the AI VM before completing setup.'
        phase='transfer restricted AI runtime credentials'
        for file in ai_runtime_url ai_internal_token; do
            host app export-ai "$file" </dev/null | host ai receive "$file"
        done
        phase='build the AI VM and prepare pinned models'
        host ai prepare </dev/null
        ;;
    resume)
        phase='verify existing deployment state'
        host app require-installed </dev/null
        host ai require-installed </dev/null
        phase='start the existing application VM'
        host app up </dev/null
        public_check
        ;;
    status|stop)
        phase="check the configured deployment before $command"
        host app require-state </dev/null
        host ai require-state </dev/null
        phase="$command both VMs"
        if [[ $command == stop ]]; then
            host ai stop </dev/null
            host app stop </dev/null
        else
            for role in app ai; do
                printf '\n%s VM:\n' "$role"
                host "$role" status </dev/null
            done
        fi
        exit
        ;;
    check)
        phase='verify both deployed stacks and public HTTPS'
        host app check </dev/null
        host ai check </dev/null
        host app check-ai </dev/null
        public_check
        printf '%s\n' 'Both VMs passed dependency and TLS checks. Real inference is a separate acceptance check.'
        exit
        ;;
esac

phase='start the AI VM and verify both directions'
host ai up </dev/null
host ai check </dev/null
host app check-ai </dev/null
if [[ $command == install ]]; then
    phase='complete initial cutover'
    host ai installed </dev/null
    host app installed </dev/null
fi
printf '\nReady at https://%s/login\n' "${settings[APP_PUBLIC_NAME]}"
printf '%s\n' 'Readiness passed; verify real inference with a new synthetic incident as a separate acceptance step.'
if [[ ${settings[DEMO_SEED]} == yes ]]; then
    printf '%s\n' 'Demo initial passwords are on the app VM under /etc/cyberguard/app/secrets/credentials/provisioning/.'
fi
if [[ ${settings[DEMO_INCIDENTS]} == yes ]]; then
    printf '%s\n' 'Sign in as admin and open Admin → Demo data to add unused demo incidents.'
fi
