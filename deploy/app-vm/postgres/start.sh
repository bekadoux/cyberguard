#!/bin/sh
# The official entrypoint still owns initialization, privileges and signal handling.
set -eu
case "$1" in
    identity) subnet=$APP_IDENTITY_SUBNET ;;
    incidents) subnet=$APP_INCIDENT_SUBNET ;;
    *) exit 1 ;;
esac
cp /run/certs/server.crt /tls/server.crt
cp /run/certs/server.key /tls/server.key
chown postgres:postgres /tls/server.crt /tls/server.key
chmod 0600 /tls/server.crt /tls/server.key
# Socket access is container-local, also used by the image's temporary init server.
printf 'local all all trust\n' > /tls/pg_hba.conf
printf 'hostssl all all 127.0.0.1/32 scram-sha-256\n' >> /tls/pg_hba.conf
if [ "$1" = incidents ]; then
    printf 'hostssl cyberguard_incidents python_incidents %s/32 scram-sha-256\n' "$APP_AI_IP" >> /tls/pg_hba.conf
fi
# This reject MUST precede the internal-network allow and any image defaults.
printf 'host all all %s/32 reject\n' "$APP_AI_IP" >> /tls/pg_hba.conf
printf 'hostssl all all %s scram-sha-256\n' "$subnet" >> /tls/pg_hba.conf
chmod 0644 /tls/pg_hba.conf
exec /usr/local/bin/docker-entrypoint.sh postgres \
    -c ssl=on -c ssl_min_protocol_version=TLSv1.2 \
    -c ssl_cert_file=/tls/server.crt -c ssl_key_file=/tls/server.key \
    -c hba_file=/tls/pg_hba.conf -c password_encryption=scram-sha-256 \
    -c max_connections=40 -c shared_buffers=128MB -c work_mem=4MB \
    -c log_statement=none -c log_min_error_statement=panic
