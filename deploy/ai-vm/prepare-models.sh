#!/usr/bin/env bash
set -euo pipefail
# shellcheck source=deploy/ai-vm/common.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/common.sh"
[[ $# == 2 || $# == 3 ]] && [[ $1 == --config ]] || fail 'Usage: prepare-models.sh --config FILE [--check]'
check_only=no
if [[ $# == 3 ]]; then [[ $3 == --check ]] || fail 'Unknown argument.'; check_only=yes; fi
load_config "$2"
[[ -d $AI_MODEL_DIR && ! -L $AI_MODEL_DIR ]] || fail 'Prepare the model directory first.'
# Serialize downloads/checks. Runtime is read-only; never repair files under a live worker.
exec 9<"$AI_MODEL_DIR"
flock -n 9 || fail 'Another model preparation/check is running.'
manifest=$AI_DEPLOY_DIR/models.json
rows=$(jq -er '.models[] | . as $m | .files[] | [$m.repo,$m.revision,$m.directory,.name,(.size|tostring),.sha256] | @tsv' "$manifest")
temporary=''
trap 'if [[ -n $temporary ]]; then rm -f -- "$temporary"; fi' EXIT
while IFS=$'\t' read -r repo revision directory name size hash; do
    [[ $repo =~ ^[a-zA-Z0-9._-]+/[a-zA-Z0-9._-]+$ && $revision =~ ^[a-f0-9]{40}$ ]] || fail 'Invalid model source.'
    [[ $directory =~ ^[a-zA-Z0-9/_.-]+$ && $directory != /* && $name =~ ^[a-zA-Z0-9._-]+$ && $name != .* ]] || fail 'Invalid model destination.'
    [[ $hash =~ ^[a-f0-9]{64}$ && $size =~ ^[1-9][0-9]{0,11}$ ]] || fail 'Invalid model checksum/size.'
    current=$AI_MODEL_DIR
    IFS=/ read -ra components <<< "$directory"
    for component in "${components[@]}"; do
        [[ -n $component && $component != . && $component != .. ]] || fail 'Invalid model path component.'
        current=$current/$component
        [[ ! -L $current ]] || fail 'Refusing a symlink in the model destination.'
        if [[ ! -d $current && $check_only == no ]]; then mkdir -m 0755 -- "$current"; fi
    done
    destination=$AI_MODEL_DIR/$directory/$name
    if [[ -e $destination || -L $destination ]]; then
        [[ -f $destination && ! -L $destination ]] || fail 'Model destination must be a regular file.'
        [[ $(stat -c %s "$destination") == "$size" && $(sha256sum "$destination" | cut -d' ' -f1) == "$hash" ]] || fail "Existing artifact does not match its pin: $name. Investigate; nothing was overwritten."
        continue
    fi
    [[ $check_only == no ]] || fail "Model preparation incomplete: $name"
    available=$(df -Pk "$AI_MODEL_DIR" | awk 'NR==2 {print $4}')
    ((available * 1024 >= size + 256 * 1024 * 1024)) || fail 'Insufficient model-download disk space.'
    temporary=$(mktemp "${destination}.partial.XXXXXX")
    printf 'Downloading pinned artifact: %s/%s\n' "$repo" "$name"
    curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' \
        --retry 3 --connect-timeout 15 --max-time 3600 \
        "https://huggingface.co/$repo/resolve/$revision/$name" -o "$temporary"
    [[ $(stat -c %s "$temporary") == "$size" && $(sha256sum "$temporary" | cut -d' ' -f1) == "$hash" ]] || fail "Downloaded artifact failed verification: $name"
    chmod 0644 "$temporary"
    mv -n -- "$temporary" "$destination"
    [[ ! -e $temporary ]] || fail 'Destination changed during preparation; refusing replacement.'
    temporary=''
done <<< "$rows"
printf '%s\n' 'Models prepared: all pinned artifacts verified. This does not validate real inference.'
