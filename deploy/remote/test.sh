#!/usr/bin/env bash
# Exercise workstation orchestration without contacting VMs, and host receipt/transfer guards.
set -euo pipefail
umask 077
root=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
scratch=$(mktemp -d)
trap 'rm -rf -- "$scratch"' EXIT
for tool in git openssl script setsid; do command -v "$tool" >/dev/null; done
repo="$scratch/repo"
mkdir -p "$repo/deploy/remote" "$repo/docker/ai" "$scratch/bin" "$scratch/tls"
cp "$root/deploy/remote-setup.sh" "$repo/deploy/"
cp "$root/deploy/remote/host.sh" "$repo/deploy/remote/"
cp "$root/docker/ai/inference-cpus.sh" "$repo/docker/ai/"
git -C "$repo" init --quiet
git -C "$repo" add deploy docker
git -C "$repo" -c user.name=Fixture -c user.email=fixture@example.invalid \
    -c core.hooksPath=/dev/null commit --quiet --no-gpg-sign -m 'test: prepare fixture release'
old_revision=$(git -C "$repo" rev-parse HEAD)
mkdir -p "$repo/deploy/app-vm" "$repo/deploy/ai-vm"
cp "$root/deploy/app-vm/config.env.example" "$repo/deploy/app-vm/"
cp "$root/deploy/ai-vm/config.env.example" "$repo/deploy/ai-vm/"
git -C "$repo" add deploy
git -C "$repo" -c user.name=Fixture -c user.email=fixture@example.invalid \
    -c core.hooksPath=/dev/null commit --quiet --no-gpg-sign -m 'test: support demo incidents and thread limits'
revision=$(git -C "$repo" rev-parse HEAD)
printf 'must-not-upload\n' > "$repo/untracked"
printf 'private test key\n' > "$scratch/app ssh key"
printf 'another private test key\n' > "$scratch/ai ssh key"

tls="$scratch/tls"
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 3 -subj '/CN=Test CA' \
    -addext 'basicConstraints=critical,CA:TRUE' -addext 'keyUsage=critical,keyCertSign,cRLSign' \
    -addext 'subjectKeyIdentifier=hash' -keyout "$tls/ca.key" -out "$tls/ca.crt" 2>/dev/null
issue() {
    local name=$1 cn=$2 sans=$3
    openssl req -new -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -subj "/CN=$cn" \
        -keyout "$tls/$name.key" -out "$scratch/csr" 2>/dev/null
    printf '%s\n' 'basicConstraints=critical,CA:FALSE' 'keyUsage=critical,digitalSignature' \
        'extendedKeyUsage=serverAuth' 'subjectKeyIdentifier=hash' 'authorityKeyIdentifier=keyid:always' \
        "subjectAltName=$sans" > "$scratch/server.ext"
    openssl x509 -req -in "$scratch/csr" -CA "$tls/ca.crt" -CAkey "$tls/ca.key" \
        -CAserial "$scratch/serial" -CAcreateserial -days 3 -extfile "$scratch/server.ext" \
        -out "$tls/$name.crt" 2>/dev/null
}
issue identity identity-db DNS:identity-db
issue incident incident-db DNS:incident-db,DNS:incidents.internal
issue go app.internal DNS:app.internal
issue api ai.internal DNS:ai.internal
issue public cg.example.org DNS:cg.example.org

cat > "$scratch/settings" <<EOF
APP_SSH=app-test
APP_KEY=app ssh key
AI_SSH=ai-test
AI_KEY=ai ssh key
REVISION=$revision
APP_PUBLIC_NAME=cg.example.org
APP_PRIVATE_IP=10.0.0.4
AI_PRIVATE_IP=10.0.0.5
TLS_DIR=tls
PUBLIC_TLS=provided
NETWORK_APPROVED=yes
EOF

cat > "$scratch/bin/ssh" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
args="$*"
[[ $args == *StrictHostKeyChecking=yes* && $args == *ForwardAgent=no* ]]
while [[ $1 != -- ]]; do shift; done
shift
case "$1" in app-test) role=app ;; ai-test) role=ai ;; *) exit 42 ;; esac
shift
[[ $args == *"/$role "* ]]
if [[ $args == *' -O exit '* ]]; then
    echo "$role close" >> "$FIXTURE/trace"
    exit
fi
if [[ $args == *' -M -N -f '* ]]; then
    [[ $args == *IdentitiesOnly=yes*" -i $FIXTURE/$role ssh key "* ]]
    if [[ $args == *BatchMode=yes* ]]; then echo "$role batch"; else echo "$role tty"; fi >> "$FIXTURE/trace"
    exit
fi
[[ $args == *ControlMaster=no* && $args == *ProxyCommand=false* ]]
case "$*" in
    *'/etc/machine-id'*)
        if [[ $role == app || ${SAME_MACHINE:-0} == 1 ]]; then
            printf '%032d\n' 1
        else
            printf '%032d\n' 2
        fi ;;
    *"'-s'"*"'inspect'"*) cat >/dev/null; echo "$role inspect" >> "$FIXTURE/trace" ;;
    *"'-s'"*"'reset-plan'"*)
        cat >/dev/null
        echo "$role reset-plan" >> "$FIXTURE/trace"
        [[ ${RESET_REJECT_TARGET:-none} != "$role" ]]
        echo "$role reset inventory"
        ;;
    *"'-s'"*"'reset'"*)
        cat >/dev/null
        echo "$role reset" >> "$FIXTURE/trace"
        [[ ${RESET_FAIL_TARGET:-none} != "$role" ]]
        ;;
    *"'-s'"*) cat >/dev/null; echo "$role os-dns" >> "$FIXTURE/trace" ;;
    *'tar --no-same-owner -xpf -'*)
        tar -tf - > "$FIXTURE/archive-$role"
        grep -qx 'deploy/remote/host.sh' "$FIXTURE/archive-$role"
        if grep -Eq 'untracked|ca\.key|ssh key' "$FIXTURE/archive-$role"; then exit 9; fi
        echo "$role upload" >> "$FIXTURE/trace" ;;
    *"'receive'"*)
        file=${1%\' }; file=${file##*\'}
        if [[ $file =~ ^[a-f0-9]{64}$ ]]; then
            digest=$file
            rest=${1%"'$digest' "}; rest=${rest%\' }; file=${rest##*\'}
        else
            digest=''
        fi
        payload=$(cat)
        [[ -n $payload ]]
        case "$file" in
            ca.key|*migrat*|*admin*|*password*) exit 9 ;;
            ai_runtime_url|ai_internal_token)
                [[ $role == ai && $payload == 'fixture AI credential' ]] ;;
            *) [[ $digest =~ ^[a-f0-9]{64}$ ]] ;;
        esac
        echo "$role receive $file" >> "$FIXTURE/trace" ;;
    *"'export-ai'"*)
        [[ $role == app && ( $* == *"'ai_runtime_url'"* || $* == *"'ai_internal_token'"* ) ]]
        echo 'fixture AI credential'
        echo "$role export-ai" >> "$FIXTURE/trace" ;;
    *"'configure'"*)
        cat > "$FIXTURE/config-$role"
        echo "$role configure" >> "$FIXTURE/trace" ;;
    *"'initialize'"*|*"'prepare'"*|*"'up'"*|*"'check'"*|*"'check-ai'"*|*"'installed'"*|*"'require-installed'"*|*"'require-state'"*|*"'status'"*|*"'stop'"*)
        action=${1%\' }; action=${action##*\'}
        echo "$role $action" >> "$FIXTURE/trace"
        if [[ ${AI_START_FAILS:-0} == 1 && $role == ai && $action == up ]]; then exit 1; fi ;;
    *) echo 'Unexpected SSH command' >&2; exit 4 ;;
esac
MOCK

cat > "$scratch/bin/curl" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
[[ $* == *'--cacert '* && $* == *https://cg.example.org/api/readyz* ]]
echo 'public verify' >> "$FIXTURE/trace"
MOCK
cat > "$scratch/bin/getent" <<'MOCK'
#!/usr/bin/env bash
[[ $* == 'ahostsv4 cg.example.org' ]]
MOCK
chmod +x "$scratch/bin/"*
export FIXTURE="$scratch" PATH="$scratch/bin:$PATH"
run() { bash "$repo/deploy/remote-setup.sh" --config "$scratch/settings" "$@"; }
reject() { if "$@"; then echo "Unexpected success: $*" >&2; exit 1; fi; }

export SAME_MACHINE=1
reject run preflight > "$scratch/out" 2>&1
unset SAME_MACHINE
grep -q 'two different VMs' "$scratch/out"

: > "$scratch/trace"
cp "$scratch/settings" "$scratch/missing-seed"
printf 'DEMO_INCIDENTS=yes\n' >> "$scratch/missing-seed"
reject bash "$repo/deploy/remote-setup.sh" --config "$scratch/missing-seed" preflight > "$scratch/out" 2>&1
grep -q 'DEMO_INCIDENTS=yes requires DEMO_SEED=yes' "$scratch/out"
sed "s/^REVISION=.*/REVISION=$old_revision/" "$scratch/settings" > "$scratch/old-release"
printf 'DEMO_SEED=yes\nDEMO_INCIDENTS=yes\n' >> "$scratch/old-release"
reject bash "$repo/deploy/remote-setup.sh" --config "$scratch/old-release" preflight > "$scratch/out" 2>&1
grep -q 'selected revision does not support the demo incidents button' "$scratch/out"
sed "s/^REVISION=.*/REVISION=$old_revision/" "$scratch/settings" > "$scratch/old-threads"
printf 'AI_INFERENCE_THREADS=4\n' >> "$scratch/old-threads"
reject bash "$repo/deploy/remote-setup.sh" --config "$scratch/old-threads" preflight > "$scratch/out" 2>&1
grep -q 'selected revision does not support setting AI_INFERENCE_THREADS' "$scratch/out"
cp "$scratch/settings" "$scratch/bad-threads"
printf 'AI_INFERENCE_THREADS=1.5\n' >> "$scratch/bad-threads"
reject bash "$repo/deploy/remote-setup.sh" --config "$scratch/bad-threads" preflight > "$scratch/out" 2>&1
grep -q 'AI_INFERENCE_THREADS must be all or a positive integer' "$scratch/out"
[[ ! -s $scratch/trace ]]

: > "$scratch/trace"
run preflight </dev/null
grep -qx 'app batch' "$scratch/trace"
grep -qx 'ai batch' "$scratch/trace"
grep -qx 'app inspect' "$scratch/trace"
grep -qx 'ai inspect' "$scratch/trace"
if grep -Eq 'upload|initialize|configure|prepare|receive| up$' "$scratch/trace"; then
    echo 'Preflight tried to change a host.' >&2
    exit 1
fi

: > "$scratch/trace"
export AI_START_FAILS=1
reject run install > "$scratch/out" 2>&1
unset AI_START_FAILS
grep -q 'Checking the AI VM' "$scratch/out"
grep -q 'failed during: start the AI VM' "$scratch/out"
if grep -q ' installed$' "$scratch/trace"; then echo 'Cutover was attempted after AI failed.' >&2; exit 1; fi

: > "$scratch/trace"
printf '' | script -q -e -c "bash '$repo/deploy/remote-setup.sh' --config '$scratch/settings' install" /dev/null > "$scratch/out"
grep -qx 'app tty' "$scratch/trace"
grep -qx 'ai tty' "$scratch/trace"
grep -qx 'APP_AI_TLS_NAME=ai.internal' "$scratch/config-app"
grep -qx 'AI_GO_URL=https://app.internal:8443' "$scratch/config-ai"
grep -qx 'APP_ALLOW_DEMO_SEED=no' "$scratch/config-app"
reject grep -q '^DEMO_INCIDENTS_ENABLED=' "$scratch/config-app"
reject grep -q '^AI_INFERENCE_THREADS=' "$scratch/config-ai"
grep -qx 'ai receive ai_runtime_url' "$scratch/trace"
grep -qx 'ai receive ai_internal_token' "$scratch/trace"
grep -qx 'app receive public.key' "$scratch/trace"
grep -qx 'ai receive api.key' "$scratch/trace"
[[ $(grep -E ' installed$' "$scratch/trace") == $'ai installed\napp installed' ]]
grep -q 'https://cg.example.org/login' "$scratch/out"
if grep -q 'fixture AI credential' "$scratch/out" "$scratch/trace"; then echo 'A credential was printed.' >&2; exit 1; fi

: > "$scratch/trace"
run stop
[[ $(grep -E ' stop$' "$scratch/trace") == $'ai stop\napp stop' ]]
run resume
run status
run check
if grep -Eq 'upload|initialize|configure|prepare|receive| installed$' "$scratch/trace"; then
    echo 'An ordinary lifecycle command tried to provision or open writes.' >&2
    exit 1
fi

# Reset needs an actual TTY and does not reset anything before both inventories
# pass and the operator confirms. A reset can target a previously pinned release.
reset_pty() {
    script -q -e -c "bash '$repo/deploy/remote-setup.sh' --config '$1' reset ${2:-}" /dev/null
}
: > "$scratch/trace"
reject setsid --wait bash "$repo/deploy/remote-setup.sh" --config "$scratch/settings" reset </dev/null > "$scratch/out" 2>&1
grep -q 'Reset requires an interactive terminal' "$scratch/out"
[[ ! -s $scratch/trace ]]
reject reset_pty "$scratch/settings" <<< cancel > "$scratch/out"
grep -q 'Reset cancelled; nothing was deleted' "$scratch/out"
[[ $(grep -E ' reset(-plan)?$' "$scratch/trace") == $'ai reset-plan\napp reset-plan' ]]
: > "$scratch/trace"
RESET_REJECT_TARGET=app reject reset_pty "$scratch/settings" <<< RESET > "$scratch/out"
[[ $(grep -E ' reset(-plan)?$' "$scratch/trace") == $'ai reset-plan\napp reset-plan' ]]
: > "$scratch/trace"
RESET_FAIL_TARGET=ai reject reset_pty "$scratch/settings" <<< RESET > "$scratch/out"
[[ $(grep -E ' reset(-plan)?$' "$scratch/trace") == $'ai reset-plan\napp reset-plan\nai reset' ]]
: > "$scratch/trace"
reset_pty "$scratch/settings" <<< RESET > "$scratch/out"
[[ $(grep -E ' reset(-plan)?$' "$scratch/trace") == $'ai reset-plan\napp reset-plan\nai reset\napp reset' ]]
grep -q 'Remote reset complete' "$scratch/out"
reject grep -Eq 'upload|initialize|configure|prepare|receive| up$' "$scratch/trace"
grep -E '^(APP|AI)_(SSH|KEY)=' "$scratch/settings" > "$scratch/legacy"
reset_pty "$scratch/legacy" --legacy <<< RESET > "$scratch/out"
grep -q 'Remote reset complete' "$scratch/out"

# First setup saves both choices and only sends each setting to the relevant VM.
printf '%s\n' app-test "$scratch/app ssh key" ai-test "$scratch/ai ssh key" \
    cg.example.org 10.0.0.4 10.0.0.5 '' '' '' "$tls" provided yes yes 0 4 yes |
    script -q -e -c "bash '$repo/deploy/remote-setup.sh' --config '$scratch/interactive' install" /dev/null > "$scratch/out"
grep -q 'Enable the admin button to add synthetic demo incidents (yes or no) \[no\]' "$scratch/out"
grep -qx 'DEMO_SEED=yes' "$scratch/interactive"
grep -qx 'DEMO_INCIDENTS=yes' "$scratch/interactive"
grep -qx 'APP_ALLOW_DEMO_SEED=yes' "$scratch/config-app"
grep -qx 'DEMO_INCIDENTS_ENABLED=true' "$scratch/config-app"
reject grep -q 'DEMO_INCIDENTS' "$scratch/config-ai"
grep -q 'AI VM inference threads per model (positive integer to limit) \[all\]' "$scratch/out"
grep -q 'Enter all or a positive whole number' "$scratch/out"
grep -qx 'AI_INFERENCE_THREADS=4' "$scratch/interactive"
grep -qx 'AI_INFERENCE_THREADS=4' "$scratch/config-ai"
reject grep -q 'AI_INFERENCE_THREADS' "$scratch/config-app"
grep -q 'Admin → Demo data' "$scratch/out"
: > "$scratch/trace"
bash "$repo/deploy/remote-setup.sh" --config "$scratch/interactive" resume </dev/null > "$scratch/out"
grep -q 'Admin → Demo data' "$scratch/out"
reject grep -Eq 'upload|initialize|configure|prepare|receive| installed$' "$scratch/trace"

# Exercise the actual host helper logic in private temporary state, not /etc or Docker.
(
    source "$root/deploy/remote/host.sh"
    repo=$root
    role=app
    operator=$(id -un)
    gid=$(id -g)
    state="$scratch/state"
    etc="$scratch/etc"
    config="$etc/config.env"
    mkdir -p "$state" "$etc"
    sed "s/^APP_SECRET_GID=.*/APP_SECRET_GID=$gid/" \
        "$repo/deploy/app-vm/config.env.example" > "$config"
    mark bootstrapped
    sha() { local digest; digest=$(sha256sum < "$1"); printf '%s' "${digest%% *}"; }
    configure_vm "$(sha "$scratch/config-app")" < "$scratch/config-app"
    prepare_guard="$scratch/db-intact"
    touch -- "$prepare_guard"
    manager() { printf '%s\n' "$1" >> "$scratch/host-actions"; }
    make_app_credentials() { mkdir -p "$etc/secrets/credentials"; }
    check_database_volumes() { [[ -f $prepare_guard ]] || fail 'Missing managed volumes.'; }
    prepare
    prepare
    [[ $(cat "$scratch/host-actions") == $'build\ndb-up\nbootstrap\nmigrate\nbootstrap-ai\nseed-demo' ]]
    rm -- "$prepare_guard"
    if (require_prepared); then echo 'Missing volumes were not detected.' >&2; exit 1; fi
    touch -- "$prepare_guard"
    mkdir -p "$etc/secrets/tls"
    if (receive_file ca.crt "$(sha "$tls/ca.crt")" < "$tls/identity.crt"); then
        echo 'Truncated or different upload was accepted.' >&2
        exit 1
    fi
    [[ ! -e $etc/secrets/tls/ca.crt ]]
    receive_file ca.crt "$(sha "$tls/ca.crt")" < "$tls/ca.crt"
    receive_file ca.crt "$(sha "$tls/ca.crt")" < "$tls/ca.crt"
    if (receive_file ca.crt "$(sha "$tls/identity.crt")" < "$tls/identity.crt"); then
        echo 'Existing CA was overwritten.' >&2
        exit 1
    fi
    if (receive_file ca.key "$(sha "$tls/ca.key")" < "$tls/ca.key"); then echo 'CA key was accepted.' >&2; exit 1; fi
    if (export_ai identity_migration_url); then echo 'Unscoped credential export.' >&2; exit 1; fi
    printf 'APP_PUBLIC_NAME=changed.example.org\n' >> "$config"
    if (require_configured); then echo 'Manual configuration drift was accepted.' >&2; exit 1; fi

    role=ai
    etc="$scratch/ai-etc"
    config="$etc/config.env"
    state="$scratch/ai-state"
    mkdir -p "$etc/secrets" "$state"
    sed "s/^AI_SECRET_GID=.*/AI_SECRET_GID=$gid/" \
        "$repo/deploy/ai-vm/config.env.example" > "$config"
    mark bootstrapped
    configure_vm "$(sha "$scratch/config-ai")" < "$scratch/config-ai"
    grep -qx 'AI_INFERENCE_THREADS=4' "$config"
    if (receive_file ai_internal_token <<< invalid); then echo 'Malformed token accepted.' >&2; exit 1; fi
    receive_file ai_internal_token < <(printf '%064d\n' 0)
    receive_file ai_runtime_url < <(printf 'postgres://python_incidents:%064d@incidents.internal:5432/cyberguard_incidents?sslmode=verify-full&sslrootcert=/run/certs/ca.crt\n' 0)
    if (receive_file go.key "$(sha "$tls/go.key")" < "$tls/go.key"); then echo 'App key reached AI.' >&2; exit 1; fi
)

# Reset only the flagged project and reserved unlabeled volumes. Use temp paths
# and a fake Docker engine; never alter the real workstation or another project.
(
    source "$root/deploy/remote/host.sh"
    role=app
    project=cyberguard-app-vm
    etc="$scratch/reset-etc"
    data="$scratch/reset-data"
    state="$scratch/reset-state"
    config="$etc/config.env"
    machine=$(cat /etc/machine-id)
    mkdir "$etc" "$data"
    touch "$scratch/reset-container" "$scratch/reset-network" "$scratch/reset-volume" "$scratch/reset-unlabeled"
    touch "$scratch/unrelated-volume"
    lock_host() { :; }
    docker() {
        [[ $1 == --host && $2 == unix:///var/run/docker.sock ]] || fail 'Expected local Docker socket.'
        shift 2
        local filter="label=com.docker.compose.project=$project"
        case "$*" in
            "ps --all --quiet --filter $filter")
                if [[ -e $scratch/reset-container ]]; then echo app-container; fi ;;
            "network ls --quiet --filter $filter")
                if [[ -e $scratch/reset-network ]]; then echo app-network; fi ;;
            "volume ls --quiet --filter $filter")
                if [[ -e $scratch/reset-volume ]]; then echo "${project}_identity-data"; fi ;;
            'volume ls --format {{.Name}}')
                echo unrelated-volume
                if [[ -e $scratch/reset-unlabeled ]]; then echo "${project}_web-data"; fi ;;
            'volume inspect --format '*)
                if [[ ${FOREIGN_VOLUME:-no} == yes ]]; then echo unrelated; fi ;;
            'container rm --force -- app-container') rm -- "$scratch/reset-container" ;;
            'network rm -- app-network') rm -- "$scratch/reset-network" ;;
            "volume rm -- ${project}_identity-data")
                [[ ${BUSY_VOLUME:-no} != yes ]] || return 1
                rm -- "$scratch/reset-volume" ;;
            "volume rm -- ${project}_web-data") rm -- "$scratch/reset-unlabeled" ;;
            *) fail 'Unexpected Docker command during reset.' ;;
        esac
    }
    reject_reset() {
        if (reset_host "$@") > "$scratch/out" 2>&1; then
            echo 'Unexpected reset success.' >&2
            exit 1
        fi
    }
    reject_reset reset-plan managed "$machine"
    grep -q 'Unmanaged resources remain' "$scratch/out"
    mkdir "$state"
    reject_reset reset-plan legacy "$machine"
    rmdir "$state"
    printf 'APP_PROJECT_NAME=cyberguard-app-vm-custom\n' > "$config"
    reject_reset reset-plan legacy "$machine"
    printf 'APP_PROJECT_NAME=%s\n' "$project" > "$config"
    FOREIGN_VOLUME=yes reject_reset reset-plan legacy "$machine"
    reject_reset reset legacy '11111111111111111111111111111111'
    [[ -e $scratch/reset-container ]]
    (
        # Host functions call the stub when validating the reset mount inventory.
        # shellcheck disable=SC2317
        findmnt() { printf '%s/nested-mount\n' "$data"; }
        reject_reset reset-plan legacy "$machine"
    )
    reset_host reset-plan legacy "$machine" > "$scratch/preview"
    grep -q "${project}_web-data" "$scratch/preview"
    reject grep -q 'unrelated-volume' "$scratch/preview"
    [[ -e $scratch/reset-container ]]
    BUSY_VOLUME=yes reject_reset reset legacy "$machine"
    [[ -d $etc && -d $data && -e $scratch/reset-volume ]]
    reset_host reset legacy "$machine"
    [[ ! -e $etc && ! -e $data && ! -e $scratch/reset-volume && ! -e $scratch/reset-unlabeled ]]
    [[ -e $scratch/unrelated-volume ]]
    # An already-clean host can be retried after the peer failed.
    reset_host reset-plan managed "$machine" >/dev/null

    # Managed reset uses the original deployment fingerprint and sudo operator.
    mkdir -m 0700 "$state" "$data"
    operator=$(id -un)
    deployment=$(printf 'a%.0s' {1..64})
    # Called indirectly by the sourced receipt validator.
    # shellcheck disable=SC2317
    stat() {
        if [[ $* == "-c %u:%a $state" ]]; then echo 0:700
        else command stat "$@"; fi
    }
    printf '%s:%s\n' "${deployment/a/b}" "$operator" > "$state/deployment"
    reject_reset reset-plan managed "$machine"
    grep -q 'Use the original settings' "$scratch/out"
    printf '%s:%s\n' "$deployment" "$operator" > "$state/deployment"
    reset_host reset managed "$machine"
    [[ ! -e $state && ! -e $data && -e $scratch/unrelated-volume ]]
)

echo 'Remote setup: SSH orchestration, scoped transfer, cutover, resume and reset checks passed.'
