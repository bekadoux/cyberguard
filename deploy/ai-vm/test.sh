#!/usr/bin/env bash
# No root, daemon changes, network access or real credentials required.
set -euo pipefail
# shellcheck source=deploy/ai-vm/common.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/common.sh"
for script in "$AI_DEPLOY_DIR"/*.sh; do bash -n "$script"; done
shellcheck -x -P "$AI_REPO_ROOT" "$AI_DEPLOY_DIR"/*.sh "$AI_DEPLOY_DIR/tests/"*.sh
mkdir -p "$AI_REPO_ROOT/.local"
temporary=$(mktemp -d "$AI_REPO_ROOT/.local/ai-vm-test.XXXXXX")
trap 'rm -rf -- "$temporary"' EXIT
mkdir -p "$temporary/repo/deploy/ai-vm" "$temporary/models" "$temporary/bin"
cp "$AI_DEPLOY_DIR/"{common.sh,manage.sh,prepare-models.sh} "$temporary/repo/deploy/ai-vm/"
test_dir=$temporary/repo/deploy/ai-vm
config=$temporary/config.env
printf 'AI_MODEL_DIR=%s\n' "$temporary/models" > "$config"
cat > "$temporary/bin/docker" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$TEST_DOCKER_LOG"
SH
cat > "$temporary/bin/curl" <<'SH'
#!/usr/bin/env bash
printf 'download\n' >> "$TEST_CURL_LOG"
while [[ $# -gt 0 ]]; do
    if [[ $1 == -o ]]; then
        if [[ ${TEST_BAD_DOWNLOAD:-no} == yes ]]; then printf xyz > "$2"; else printf abc > "$2"; fi
        exit
    fi
    shift
done
exit 1
SH
chmod 0755 "$temporary/bin/"*
export TEST_DOCKER_LOG=$temporary/docker.log TEST_CURL_LOG=$temporary/curl.log
mock_path=$temporary/bin:$PATH
expect_failure() {
    if "$@" > "$temporary/error" 2>&1; then fail 'Expected rejection did not occur.'; fi
}
# Parent Compose settings and AI overrides cannot redirect this project.
PATH=$mock_path AI_BIND_IP=0.0.0.0 COMPOSE_FILE=unrelated.yaml COMPOSE_PROJECT_NAME=ordinary \
    bash "$test_dir/manage.sh" --config "$config" config >/dev/null
grep -Fq -- '--env-file /dev/null' "$TEST_DOCKER_LOG"
grep -Fq -- '--project-name cyberguard-ai-vm' "$TEST_DOCKER_LOG"
[[ $(wc -l < "$TEST_DOCKER_LOG") == 1 ]]
for value in 'AI_BIND_IP=0.0.0.0' 'AI_GO_URL=http://go' 'AI_PORT=0' 'AI_SECRET_GID=bad' 'UNKNOWN=value' 'AI_MODEL_DIR=/var/../etc' "AI_MODEL_DIR=$temporary/repo/unsafe"; do
    printf '%s\n' "$value" > "$temporary/bad.env"
    expect_failure env PATH="$mock_path" bash "$test_dir/manage.sh" --config "$temporary/bad.env" config
done
# shellcheck disable=SC2016 # This is deliberately unevaluated, malicious input.
printf 'AI_TLS_NAME=$(touch %s)\n' "$temporary/executed" > "$temporary/bad.env"
expect_failure env PATH="$mock_path" bash "$test_dir/manage.sh" --config "$temporary/bad.env" config
[[ ! -e $temporary/executed ]]
printf 'AI_PORT=8443\nAI_PORT=8444\n' > "$temporary/bad.env"
expect_failure env PATH="$mock_path" bash "$test_dir/manage.sh" --config "$temporary/bad.env" config
hash=$(printf abc | sha256sum | cut -d' ' -f1)
jq -n --arg hash "$hash" '{models:[{repo:"fixture/model",revision:("a"*40),directory:"fixture/model.v1",files:[{name:"model.bin",size:3,sha256:$hash}]}]}' > "$test_dir/models.json"
expect_failure env PATH="$mock_path" bash "$test_dir/prepare-models.sh" --config "$config" --check
[[ ! -e $TEST_CURL_LOG ]]
PATH=$mock_path bash "$test_dir/prepare-models.sh" --config "$config" >/dev/null
PATH=$mock_path bash "$test_dir/prepare-models.sh" --config "$config" >/dev/null
PATH=$mock_path bash "$test_dir/prepare-models.sh" --config "$config" --check >/dev/null
[[ $(wc -l < "$TEST_CURL_LOG") == 1 ]]
printf xyz > "$temporary/models/fixture/model.v1/model.bin"
expect_failure env PATH="$mock_path" bash "$test_dir/prepare-models.sh" --config "$config"
[[ $(cat "$temporary/models/fixture/model.v1/model.bin") == xyz ]]
rm "$temporary/models/fixture/model.v1/model.bin"
expect_failure env PATH="$mock_path" TEST_BAD_DOWNLOAD=yes bash "$test_dir/prepare-models.sh" --config "$config"
[[ ! -e $temporary/models/fixture/model.v1/model.bin ]]
[[ -z $(find "$temporary/models" -name '*.partial.*' -print) ]]
rmdir "$temporary/models/fixture/model.v1"
mkdir "$temporary/elsewhere"
ln -s "$temporary/elsewhere" "$temporary/models/fixture/model.v1"
expect_failure env PATH="$mock_path" bash "$test_dir/prepare-models.sh" --config "$config"
[[ -z $(find "$temporary/elsewhere" -type f -print) ]]
# A deployment must fail before Docker if credential permissions are too broad.
mkdir -m 0700 "$temporary/secrets"
printf 'AI_SECRETS_DIR=%s\nAI_SECRET_GID=%s\n' "$temporary/secrets" "$(id -g)" >> "$config"
for name in ai_runtime_url ai_internal_token api.crt api.key ca.crt; do
    printf 'SYNTHETIC_SECRET_DO_NOT_LOG\n' > "$temporary/secrets/$name"
    chmod 0640 "$temporary/secrets/$name"
done
chmod 0666 "$temporary/secrets/ai_internal_token"
before=$(sha256sum "$temporary/secrets/"*)
expect_failure env PATH="$mock_path" bash "$test_dir/manage.sh" --config "$config" up
[[ $before == "$(sha256sum "$temporary/secrets/"*)" ]]
[[ $(wc -l < "$TEST_DOCKER_LOG") == 1 ]]
if grep -Fq 'SYNTHETIC_SECRET_DO_NOT_LOG' "$temporary/error"; then fail 'Secret leaked in diagnostics.'; fi
printf '%s\n' 'Deployment configuration isolation, literal parsing, model checksums, idempotency, permissions and overwrite protection passed.'
