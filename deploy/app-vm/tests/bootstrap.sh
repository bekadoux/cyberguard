#!/usr/bin/env bash
# ONLY in disposable Ubuntu, without a host Docker socket. Privileged actions mocked.
set -euo pipefail
[[ -f /.dockerenv && ! -e /var/run/docker.sock ]] || { echo 'Disposable container required.' >&2; exit 1; }
mkdir /mock
export PATH=/mock:$PATH
useradd --uid 10001 --create-home cg_app_operator
cat > /mock/apt-get <<'SH'
#!/bin/bash
printf '%s\n' "$*" >> /apt-calls
SH
cat > /mock/systemctl <<'SH'
#!/bin/bash
printf '%s\n' "$*" >> /systemctl-calls
SH
cat > /mock/docker <<'SH'
#!/bin/bash
echo 'Mock Docker/Compose/Buildx available'
SH
cat > /mock/curl <<'SH'
#!/bin/bash
while [[ $# -gt 0 ]]; do
    if [[ $1 == -o ]]; then printf 'mock Docker key\n' > "$2"; exit; fi
    shift
done
exit 1
SH
cat > /mock/gpg <<'SH'
#!/bin/bash
echo "fpr:::::::::${MOCK_FINGERPRINT:-9DC858229FC7DD38854AE2D88D81803C0EBFCD88}:"
SH
cat > /mock/dpkg-query <<'SH'
#!/bin/bash
if [[ ${MOCK_CONFLICT:-} == "${@: -1}" ]]; then echo 'install ok installed'; else exit 1; fi
SH
cat > /mock/dpkg <<'SH'
#!/bin/bash
if [[ $1 == --print-architecture ]]; then echo "${MOCK_ARCH:-amd64}"; else exit 1; fi
SH
chmod 0755 /mock/*
script=/repo/deploy/app-vm/bootstrap.sh
bash "$script" --owner cg_app_operator >/dev/null
printf 'preserve config\n' >> /etc/cyberguard/app/config.env
printf 'preserve synthetic secret\n' > /etc/cyberguard/app/secrets/sentinel
before=$(sha256sum /etc/cyberguard/app/config.env /etc/cyberguard/app/secrets/sentinel)
bash "$script" --owner cg_app_operator >/dev/null
[[ $before == "$(sha256sum /etc/cyberguard/app/config.env /etc/cyberguard/app/secrets/sentinel)" ]]
[[ $(stat -c '%a:%U' /etc/cyberguard/app/secrets) == 700:cg_app_operator ]]
grep -Fq -- 'install -y --no-upgrade --no-install-recommends docker-ce' /apt-calls
before_calls=$(wc -l < /apt-calls)
if MOCK_CONFLICT=docker.io bash "$script" --owner cg_app_operator >/dev/null 2>&1; then exit 1; fi
if MOCK_ARCH=arm64 bash "$script" --owner cg_app_operator >/dev/null 2>&1; then exit 1; fi
[[ $(wc -l < /apt-calls) == "$before_calls" ]]
if MOCK_FINGERPRINT=unexpected bash "$script" --owner cg_app_operator >/dev/null 2>&1; then exit 1; fi
chmod 0755 /etc/cyberguard/app/secrets
if bash "$script" --owner cg_app_operator >/dev/null 2>&1; then exit 1; fi
chmod 0700 /etc/cyberguard/app/secrets
mv /etc/apt/keyrings/docker.asc /etc/apt/keyrings/docker.asc.saved
ln -s /root/must-not-create /etc/apt/keyrings/docker.asc
if bash "$script" --owner cg_app_operator >/dev/null 2>&1; then exit 1; fi
[[ ! -e /root/must-not-create ]]
rm /etc/apt/keyrings/docker.asc
mv /etc/apt/keyrings/docker.asc.saved /etc/apt/keyrings/docker.asc
printf 'ID=ubuntu\nVERSION_ID=24.04\n' > /etc/os-release
if bash "$script" --owner cg_app_operator >/dev/null 2>&1; then exit 1; fi
printf '%s\n' 'Application bootstrap repeatability and rejection tests passed (apt/systemd/network mocked).'
