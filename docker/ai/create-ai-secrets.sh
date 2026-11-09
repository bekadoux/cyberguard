#!/bin/sh
# Add-only: never rotate credentials over an existing database.
set -eu
umask 077
root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
dir=${DOCKER_SECRETS_DIR:-"$root/.local/docker-secrets"}
case "$dir" in /*) ;; *) dir="$root/$dir";; esac
[ -d "$dir" ] || { echo 'Run make docker-secrets first' >&2; exit 1; }
for name in ai_runtime_password ai_runtime_url ai_internal_token; do
  [ ! -e "$dir/$name" ] || { echo "Refusing to overwrite $name" >&2; exit 1; }
done
command -v openssl >/dev/null
openssl rand -hex 32 > "$dir/ai_runtime_password"
openssl rand -hex 32 > "$dir/ai_internal_token"
printf 'postgres://python_incidents:%s@incident-db:5432/cyberguard_incidents?sslmode=disable\n' \
  "$(cat "$dir/ai_runtime_password")" > "$dir/ai_runtime_url"
chgrp "$(id -g)" "$dir/ai_runtime_password" "$dir/ai_internal_token" "$dir/ai_runtime_url"
chmod 0640 "$dir/ai_runtime_password" "$dir/ai_internal_token" "$dir/ai_runtime_url"
echo 'AI secrets created. Keep them private and retain them with DB volumes.'
