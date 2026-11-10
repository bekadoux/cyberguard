#!/usr/bin/env bash
# Run explicitly with sudo on the AI VM. Never start the application here.
set -euo pipefail
# shellcheck source=deploy/ai-vm/common.sh
source "$(dirname -- "${BASH_SOURCE[0]}")/common.sh"

owner=${SUDO_USER:-}
if [[ $# == 2 && $1 == --owner ]]; then owner=$2; elif [[ $# != 0 ]]; then
    fail 'Usage: sudo bash deploy/ai-vm/bootstrap.sh [--owner USER]'
fi
[[ $EUID == 0 ]] || fail 'Bootstrap requires sudo/root.'
if [[ -z $owner ]] || ! id "$owner" >/dev/null 2>&1; then fail 'Select an existing operator with --owner USER.'; fi
# This is the trusted OS release file, not deployment configuration.
source /etc/os-release
[[ $ID == ubuntu && $VERSION_ID == 26.04 ]] || fail 'This bootstrap supports Ubuntu 26.04 only.'
[[ $(dpkg --print-architecture) == amd64 ]] || fail 'This deployment targets amd64.'
available=$(df -Pk /var | awk 'NR==2 {print $4}')
((available >= 20 * 1024 * 1024)) || fail 'At least 20 GiB free under /var is required for images and models.'
for package in docker.io docker-compose docker-compose-v2 docker-doc docker-buildx podman-docker containerd runc; do
    if dpkg-query -W -f='${Status}' "$package" 2>/dev/null | grep -qx 'install ok installed'; then
        fail "Conflicting package $package is installed; review it manually. Nothing was removed."
    fi
done
# Do not silently replace an existing apt source or Docker signing key.
source_file=/etc/apt/sources.list.d/docker.sources
key_file=/etc/apt/keyrings/docker.asc
expected_source='Types: deb
URIs: https://download.docker.com/linux/ubuntu
Suites: resolute
Components: stable
Architectures: amd64
Signed-By: /etc/apt/keyrings/docker.asc'
if [[ -e /etc/apt/sources.list.d/docker.list || -L /etc/apt/sources.list.d/docker.list ]]; then fail 'Review the existing docker.list before bootstrap.'; fi
if [[ -e $source_file || -L $source_file ]]; then
    [[ -f $source_file && ! -L $source_file && $(cat "$source_file") == "$expected_source" ]] || fail 'Existing Docker apt source differs; review manually.'
fi
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends ca-certificates curl gnupg git jq make openssl
install -d -m 0755 /etc/apt/keyrings
temporary=$(mktemp)
trap 'rm -f -- "$temporary"' EXIT
curl -fsSL --proto '=https' --connect-timeout 15 --max-time 120 \
    https://download.docker.com/linux/ubuntu/gpg -o "$temporary"
fingerprint=$(gpg --batch --show-keys --with-colons "$temporary" | awk -F: '$1=="fpr" {print $10; exit}')
[[ $fingerprint == 9DC858229FC7DD38854AE2D88D81803C0EBFCD88 ]] || fail 'Unexpected Docker apt signing key.'
if [[ -e $key_file || -L $key_file ]]; then
    if [[ ! -f $key_file || -L $key_file ]] || ! cmp -s "$temporary" "$key_file"; then fail 'Existing Docker signing key differs; review manually.'; fi
else
    install -m 0644 "$temporary" "$key_file"
fi
if [[ ! -e $source_file ]]; then printf '%s\n' "$expected_source" > "$source_file"; fi
apt-get update
# Install missing packages, but leave already-installed versions for deliberate upgrades.
apt-get install -y --no-upgrade --no-install-recommends \
    docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
systemctl enable --now docker
docker version >/dev/null
docker compose version
docker buildx version

group=$(id -gn "$owner")
ensure_directory() {
    local path=$1 mode=$2 user=$3 directory_group=$4
    if [[ -e $path || -L $path ]]; then
        [[ -d $path && ! -L $path ]] || fail "Expected a real directory: $path"
        [[ $(stat -c %a "$path") == "$mode" && $(stat -c %U "$path") == "$user" && $(stat -c %G "$path") == "$directory_group" ]] || fail "Existing directory has unexpected permissions/owner: $path"
    else
        install -d -m "$mode" -o "$user" -g "$directory_group" "$path"
    fi
}
ensure_directory /etc/cyberguard 755 root root
ensure_directory /etc/cyberguard/ai 755 root root
ensure_directory /etc/cyberguard/ai/secrets 700 "$owner" "$group"
ensure_directory /var/lib/cyberguard-ai 755 root root
ensure_directory /var/lib/cyberguard-ai/models 755 "$owner" "$group"
config=/etc/cyberguard/ai/config.env
if [[ -e $config || -L $config ]]; then
    [[ -f $config && ! -L $config ]] || fail 'Existing configuration must be a regular file, not a symlink.'
else
    sed "s/^AI_SECRET_GID=.*/AI_SECRET_GID=$(id -g "$owner")/" "$AI_DEPLOY_DIR/config.env.example" > "$temporary"
    install -m 0644 "$temporary" "$config"
fi
printf '%s\n' 'Host prepared. Existing configuration/secrets were retained; no models or application services were started.' \
    'Docker access still requires sudo unless separately authorized. Review docs/azure-ai-deployment.md next.'
