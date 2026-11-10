#!/usr/bin/env bash
# Run ONLY inside the disposable Ubuntu container described in the deployment guide.
set -euo pipefail
[[ -f /.dockerenv && ! -e /var/run/docker.sock ]] || { echo 'Disposable container required.' >&2; exit 1; }
mkdir /mock
export PATH=/mock:$PATH
useradd --uid 10001 --create-home cg_ai_operator
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
    if [[ $1 == -o ]]; then printf 'mock Docker public key\n' > "$2"; exit; fi
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
script=/repo/deploy/ai-vm/bootstrap.sh
bash "$script" --owner cg_ai_operator >/dev/null
printf 'preserve existing config\n' >> /etc/cyberguard/ai/config.env
printf 'synthetic retained secret\n' > /etc/cyberguard/ai/secrets/sentinel
before=$(sha256sum /etc/cyberguard/ai/config.env /etc/cyberguard/ai/secrets/sentinel)
bash "$script" --owner cg_ai_operator >/dev/null
[[ $before == "$(sha256sum /etc/cyberguard/ai/config.env /etc/cyberguard/ai/secrets/sentinel)" ]]
[[ $(stat -c '%a:%U' /etc/cyberguard/ai/secrets) == 700:cg_ai_operator ]]
grep -Fq -- 'install -y --no-upgrade --no-install-recommends docker-ce' /apt-calls
before_calls=$(wc -l < /apt-calls)
if MOCK_CONFLICT=docker.io bash "$script" --owner cg_ai_operator >/dev/null 2>&1; then exit 1; fi
if MOCK_ARCH=arm64 bash "$script" --owner cg_ai_operator >/dev/null 2>&1; then exit 1; fi
[[ $(wc -l < /apt-calls) == "$before_calls" ]]
if MOCK_FINGERPRINT=unexpected bash "$script" --owner cg_ai_operator >/dev/null 2>&1; then exit 1; fi
[[ $before == "$(sha256sum /etc/cyberguard/ai/config.env /etc/cyberguard/ai/secrets/sentinel)" ]]
mv /etc/apt/keyrings/docker.asc /etc/apt/keyrings/docker.asc.saved
ln -s /root/must-not-create /etc/apt/keyrings/docker.asc
if bash "$script" --owner cg_ai_operator >/dev/null 2>&1; then exit 1; fi
[[ ! -e /root/must-not-create ]]
rm /etc/apt/keyrings/docker.asc
mv /etc/apt/keyrings/docker.asc.saved /etc/apt/keyrings/docker.asc
printf 'ID=ubuntu\nVERSION_ID=24.04\n' > /etc/os-release
if bash "$script" --owner cg_ai_operator >/dev/null 2>&1; then exit 1; fi
printf '%s\n' 'Ubuntu bootstrap safety checks passed with mocked apt/systemd/network: reruns preserve config/secrets; conflicts, wrong OS/architecture and wrong signing keys are rejected.'
