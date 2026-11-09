#!/usr/bin/env bash
# Read-only HTTP checks against the disposable gateway supplied by smoke.sh.
set -euo pipefail
umask 077
base=${1:?A disposable gateway origin is required}
state=$(mktemp -d "${TMPDIR:-/tmp}/cyberguard-web.XXXXXX")
cleanup() { rm -f -- "$state/body" "$state/headers" "$state/index"; rmdir -- "$state"; }
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
request() {
    local expected=$1 actual
    shift
    actual=$(curl --silent --show-error --max-time 15 --output "$state/body" \
        --dump-header "$state/headers" --write-out '%{http_code}' "$@")
    if [[ $actual != "$expected" ]]; then
        printf 'Packaged web check expected HTTP %s, got %s.\n' "$expected" "$actual" >&2
        return 1
    fi
    ! grep -qi '^Set-Cookie:' "$state/headers"
    grep -qi '^X-Content-Type-Options: nosniff' "$state/headers"
}
no_store() { grep -qi '^Cache-Control: no-store' "$state/headers"; }
html() { grep -qi '^Content-Type: text/html' "$state/headers"; }
request 200 "$base/login"
html
no_store
cp "$state/body" "$state/index"
! grep -Eq '/@vite/|/src/|localhost:5173|127\.0\.0\.1:5173' "$state/index"
for path in / /index.html /login /account /incidents /incidents/new \
    /incidents/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa '/incidents?status=submitted'; do
    request 200 -H 'Accept: text/html' "$base$path"
    html
    no_store
    cmp -s "$state/body" "$state/index"
done
request 200 --head "$base/incidents/new"
html
no_store

mapfile -t assets < <(grep -Eo '(src|href)="/assets/[^" ]+"' "$state/index" | sed 's/^[^"]*"//;s/"$//' | sort -u)
[[ ${#assets[@]} -ge 2 ]]
for asset in "${assets[@]}"; do
    [[ $asset =~ ^/assets/.+-[a-zA-Z0-9_-]{8,}\.(js|css)$ ]]
    request 200 "$base$asset"
    grep -qi '^Cache-Control: public, max-age=31536000, immutable' "$state/headers"
    ! html
    etag=$(sed -n 's/^[Ee][Tt][Aa][Gg]: //p' "$state/headers" | tr -d '\r')
    [[ -n $etag ]]
    request 304 -H "If-None-Match: $etag" "$base$asset"
    grep -qi '^Cache-Control: public, max-age=31536000, immutable' "$state/headers"
done
for path in /theme.js /favicon.svg; do
    request 200 "$base$path"
    no_store
    ! html
done
for path in /missing /assets/missing.js /assets/missing-12345678.js /assets/index.js.map \
    /.env /.git/config /src/main.tsx /node_modules/vite/package.json \
    /api /api/not-found /internal /api/internal/auth/authorize \
    /api/%69nternal/auth/authorize /api//internal/auth/authorize /ai/internal/test \
    /incidents/internal/test /api%2finternal/auth/authorize; do
    request 404 --path-as-is -H 'Accept: text/html' "$base$path"
    no_store
    ! html
done
for path in /ai /ai/incidents/assessment; do
    request 503 -H 'Accept: text/html' "$base$path"
    no_store
    ! html
done
for path in /login /incidents/new /assets/anything.js; do
    request 405 -X POST "$base$path"
    no_store
    ! html
    grep -qi '^Allow: GET, HEAD' "$state/headers"
done
printf '%s\n' 'Packaged web HTTP checks passed: SPA routes, assets/cache policy, HEAD, missing files, reserved paths and methods.'
