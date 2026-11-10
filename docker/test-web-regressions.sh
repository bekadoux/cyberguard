#!/usr/bin/env bash
# Exercise the real HTTP checker with synthetic curl responses, without Docker/network.
set -euo pipefail
umask 077
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
state=$(mktemp -d "${TMPDIR:-/tmp}/cyberguard-web-regressions.XXXXXX")
cleanup() { rm -f -- "$state/mock-curl.sh" "$state/log"; rmdir -- "$state"; }
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

cat > "$state/mock-curl.sh" <<'MOCK'
grep() {
    if [[ ${WEB_TEST_FAULT:-} == grep-error && $* == *Set-Cookie:* ]]; then return 2; fi
    command grep "$@"
}
curl() {
    local output='' headers='' url='' method=GET conditional=0
    while [[ $# -gt 0 ]]; do
        case $1 in
            --output) output=$2; shift 2 ;;
            --dump-header) headers=$2; shift 2 ;;
            --max-time|--write-out) shift 2 ;;
            --silent|--show-error|--head|--path-as-is) shift ;;
            -X) method=$2; shift 2 ;;
            -H)
                if [[ $2 == If-None-Match:* ]]; then conditional=1; fi
                shift 2 ;;
            http://gateway.invalid*) url=$1; shift ;;
            *) return 2 ;;
        esac
    done
    local path=${url#http://gateway.invalid}
    path=${path%%\?*}
    local status=200 type=application/json cache=no-store etag='' cookie=''
    local body='{}'
    if [[ $method == POST ]]; then
        status=405
    else
        case $path in
            /|/index.html|/login|/account|/incidents|/incidents/new|/incidents/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa)
                type=text/html
                body='<html><link href="/assets/app-12345678.css"><script src="/assets/app-12345678.js"></script></html>' ;;
            /assets/app-12345678.js|/assets/app-12345678.css)
                type=text/javascript
                cache='public, max-age=31536000, immutable'
                etag='"synthetic-etag"'
                if [[ $conditional == 1 ]]; then status=304; fi ;;
            /theme.js|/favicon.svg) type=text/javascript ;;
            /ai|/ai/incidents/assessment)
                status=503
                if [[ $WEB_TEST_AI == 1 ]]; then status=404; fi ;;
            /ai/healthz|/ai/readyz) ;;
            *) status=404 ;;
        esac
    fi
    case ${WEB_TEST_FAULT:-}:$path in
        cookie:/login) cookie='Set-Cookie: synthetic=yes' ;;
        source-reference:/login) body+='<script src="/src/main.tsx"></script>' ;;
        asset-html:/assets/app-12345678.js|bootstrap-html:/theme.js|missing-html:/missing|ai-html:/ai|method-html:/login)
            type=text/html ;;
    esac
    {
        printf 'HTTP/1.1 %s\r\nContent-Type: %s\r\nCache-Control: %s\r\n' "$status" "$type" "$cache"
        printf 'X-Content-Type-Options: nosniff\r\n'
        if [[ -n $etag ]]; then printf 'ETag: %s\r\n' "$etag"; fi
        if [[ -n $cookie ]]; then printf '%s\r\n' "$cookie"; fi
        if [[ $method == POST ]]; then printf 'Allow: GET, HEAD\r\n'; fi
        printf '\r\n'
    } > "$headers"
    printf '%s' "$body" > "$output"
    printf '%s' "$status"
}
MOCK

run_case() {
    WEB_TEST_AI=$1 WEB_TEST_FAULT=$2 BASH_ENV="$state/mock-curl.sh" \
        bash "$root/docker/test-web.sh" http://gateway.invalid "$1" > "$state/log" 2>&1
}
for ai in 0 1; do
    if ! run_case "$ai" ''; then
        cat "$state/log" >&2
        echo 'Synthetic valid gateway responses were rejected.' >&2
        exit 1
    fi
    for fault in cookie source-reference asset-html bootstrap-html missing-html ai-html method-html; do
        if run_case "$ai" "$fault"; then
            printf 'Packaged web checker accepted the %s regression (AI=%s).\n' "$fault" "$ai" >&2
            exit 1
        fi
        grep -q 'Packaged web check found forbidden' "$state/log"
    done
    if run_case "$ai" grep-error; then
        echo 'Packaged web checker ignored a grep error.' >&2
        exit 1
    else
        [[ $? == 2 ]]
    fi
done
printf '%s\n' 'Packaged web assertion regressions passed: 2 valid, 14 forbidden-response and 2 grep-error cases.'
