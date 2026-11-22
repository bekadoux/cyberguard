#!/usr/bin/env bash
# Compose contract and lifecycle orchestration, without real models or persistent services.
set -euo pipefail
umask 077
root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"
for tool in docker jq script openssl; do command -v "$tool" >/dev/null; done
scratch=$(mktemp -d)
trap 'rm -rf -- "$scratch"' EXIT

export LOCAL_BIND_IP=192.168.7.5 LOCAL_WEB_PORT=18088 LOCAL_OPERATOR_UID=1000 DOCKER_SECRET_GID=1000
export LOCAL_MODEL_DIR="$scratch/models" LOCAL_AI_IMAGE=ai:test LOCAL_KEV_IMAGE=kev:test
export LOCAL_PREPARE_IMAGE=prepare:test AI_MODE=fake
docker compose --env-file /dev/null -f compose.yaml -f compose.ai.yaml -f compose.full.yaml \
    --profile ai-models --profile tools --profile local-setup config --format json > "$scratch/compose.json"
jq -e '
  ([.services | to_entries[] | select(.value.ports) | .key] == ["web"]) and
  (.services.web.ports == [{host_ip:"192.168.7.5",target:8080,published:"18088",protocol:"tcp",mode:"ingress"}]) and
  ([.services | to_entries[] | select(.value.networks | has("local-ingress")) | .key] == ["web"]) and
  ([.services | to_entries[] | select(.value.networks | has("model-download")) | .key] == ["prepare-models"]) and
  (.networks | [.edge, .identity, .incidents, .["ai-inference"]] | all(.internal)) and
  (.services | .["ai-worker"].environment.AI_MODE == "models" and
    (.["ai-kev"].volumes + .["ai-llm"].volumes | length == 2 and all(.target == "/models" and .read_only)) and
    (.["prepare-models"] | .profiles == ["local-setup"] and (.volumes | length == 1 and all(.target == "/models" and (.read_only | not))) and (has("secrets") | not)))
' "$scratch/compose.json" >/dev/null

mkdir -p "$scratch/repo/docker/ai" "$scratch/bin"
cp docker/{local-setup,create-secrets}.sh "$scratch/repo/docker/"
cp docker/ai/create-ai-secrets.sh "$scratch/repo/docker/ai/"
cd "$scratch/repo"
export FIXTURE_DIR="$scratch" PATH="$scratch/bin:$PATH"
digest=$(printf '%s' "$PWD" | sha256sum)
export FIXTURE_PROJECT="cyberguard-full-${digest:0:16}"

cat > "$scratch/bin/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$FIXTURE_DIR/docker.log"
case "$1 $2" in
    'context show') echo default ;;
    'context inspect') echo unix:///var/run/docker.sock ;;
    'info --format') echo linux/x86_64 ;;
    'volume ls')
        if [[ -f $FIXTURE_DIR/volumes ]]; then cat "$FIXTURE_DIR/volumes"; fi ;;
    'ps -aq'|'network ls'|'image inspect'|'build --tag'|'compose version') ;;
    'compose --env-file')
        while [[ $1 != --project-name ]]; do shift; done
        [[ $2 == "$FIXTURE_PROJECT" ]]
        [[ ! -v COMPOSE_PROFILES && ! -v COMPOSE_FILE ]]
        [[ $AI_MODE == models && $DOCKER_SECRETS_DIR == "$PWD/.local/full-stack/secrets" ]]
        while [[ $# -gt 0 ]]; do
            case "$1" in
                config|pull|exec|logs) exit 0 ;;
                ps)
                    if [[ $* == *--status* && -f $FIXTURE_DIR/running ]]; then echo ai-kev; fi
                    exit 0 ;;
                up)
                    if [[ $* == *'identity-db incident-db' ]]; then
                        printf '%s\n' "${FIXTURE_PROJECT}_identity-data" "${FIXTURE_PROJECT}_incident-data" > "$FIXTURE_DIR/volumes"
                    elif [[ $* == *'ai-kev ai-llm' ]]; then
                        touch "$FIXTURE_DIR/running"
                    fi
                    exit 0 ;;
                run)
                    if [[ $* == *'prepare-models' && ${FAIL_MODELS:-0} == 1 ]]; then exit 1; fi
                    exit 0 ;;
                stop) rm -f -- "$FIXTURE_DIR/running"; exit 0 ;;
                down)
                    if [[ ${LEAVE_VOLUME:-0} != 1 ]]; then
                        rm -f -- "$FIXTURE_DIR/running" "$FIXTURE_DIR/volumes"
                    fi
                    exit 0 ;;
            esac
            shift
        done
        exit 1 ;;
    *) exit 1 ;;
esac
EOF

cat > "$scratch/bin/ip" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' '2: docker0    inet 172.17.0.1/16 scope global docker0' \
    '3: eth0    inet 192.168.7.5/24 scope global eth0'
EOF
cat > "$scratch/bin/ss" <<'EOF'
#!/usr/bin/env bash
if [[ $* == *:8088 ]]; then echo '8088 is busy'; fi
EOF
chmod +x "$scratch/bin/"{docker,ip,ss}
export COMPOSE_PROFILES=tools COMPOSE_FILE=wrong DOCKER_SECRETS_DIR=wrong
unset DOCKER_HOST DOCKER_CONTEXT

run() { bash docker/local-setup.sh "$@"; }
terminal() { script -q -e -c "bash docker/local-setup.sh $*" /dev/null; }
reject() { if "$@"; then echo "Unexpected success: $*" >&2; exit 1; fi; }
secrets=.local/full-stack/secrets

# No mutation until an operator accepts the LAN and model prompts.
reject terminal <<< $'lan\nn' > "$scratch/declined"
[[ ! -e .local/full-stack ]]
! grep -Eq 'build --tag| up --wait ' "$scratch/docker.log"
reject run resume </dev/null > "$scratch/no-setup" 2>&1
grep -q 'No full local setup' "$scratch/no-setup"

# Interrupt model preparation after successful seeding. Passwords go only to the TTY.
export FAIL_MODELS=1
reject terminal '> "$FIXTURE_DIR/stdout"' <<< $'0.0.0.0\nlan\ny\n8088\n18088\n\ny' > "$scratch/first"
unset FAIL_MODELS
grep -q '192.168.7.5' "$scratch/first"
grep -q 'Choose an unused TCP port' "$scratch/first"
grep -q 'shown only once' "$scratch/first"
grep -q 'dev_admin_password' "$scratch/first"
[[ -e .local/full-stack/seed-demo.done && ! -e .local/full-stack/models.done ]]
for account in employee_a employee_b analyst admin; do
    password=$(<"$secrets/dev_${account}_password")
    [[ $password =~ ^[a-f0-9]{64}$ ]]
    grep -qF "$password" "$scratch/first"
    if grep -qF "$password" "$scratch/stdout" "$scratch/docker.log"; then
        echo 'A password leaked outside the setup terminal.' >&2
        exit 1
    fi
done

# Resume preparation and then stop/resume again. Finished one-offs are never replayed.
: > "$scratch/docker.log"
run </dev/null > "$scratch/resumed"
grep -q 'http://192.168.7.5:18088/login' "$scratch/resumed"
[[ -e .local/full-stack/models.done && -d .local/full-stack-models ]]
reject run prepare-models > "$scratch/running" 2>&1
grep -q 'Stop the full stack' "$scratch/running"
run stop >/dev/null
run resume </dev/null >> "$scratch/resumed"
run check
[[ $(grep -c ' -T prepare-models$' "$scratch/docker.log") == 1 ]]
[[ $(grep -c ' -T prepare-models --check$' "$scratch/docker.log") == 1 ]]
if grep -Eq ' -T (bootstrap-|migrate-|seed-demo)' "$scratch/docker.log" ||
    grep -q 'shown only once' "$scratch/resumed"; then
    echo 'Setup replayed a completed provisioning or password-display step.' >&2
    exit 1
fi
grep -q 'stop web ai-worker' "$scratch/docker.log"

# An incomplete database pair must not be silently recreated.
cp "$scratch/volumes" "$scratch/saved-volumes"
head -n 1 "$scratch/saved-volumes" > "$scratch/volumes"
reject run resume > "$scratch/missing" 2>&1
grep -q 'database volume is missing' "$scratch/missing"
mv "$scratch/saved-volumes" "$scratch/volumes"

reject terminal destroy <<< no > "$scratch/cancelled"
[[ -d $secrets && -f $scratch/volumes ]]
export LEAVE_VOLUME=1
reject terminal destroy <<< destroy > "$scratch/incomplete-destroy"
unset LEAVE_VOLUME
grep -q 'Secrets were kept' "$scratch/incomplete-destroy"
[[ -d $secrets && -f $scratch/volumes ]]
terminal destroy <<< destroy > "$scratch/destroyed"
[[ ! -e .local/full-stack && -d .local/full-stack-models && ! -e $scratch/volumes ]]
grep -q 'down --volumes --remove-orphans' "$scratch/docker.log"
terminal destroy --purge-models <<< destroy > "$scratch/purged"
[[ ! -e .local/full-stack-models ]]

echo 'Full local setup: Compose isolation and lifecycle checks passed.'
