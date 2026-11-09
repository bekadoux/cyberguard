#!/usr/bin/env bash
# Read rendered Compose JSON on stdin; use only fresh, network-isolated containers.
set -euo pipefail
for tool in docker jq; do
    command -v "$tool" > /dev/null || { echo "Missing readiness-test prerequisite: $tool" >&2; exit 1; }
done
configuration=$(cat)
containers=()
cleanup() {
    result=$?
    trap - EXIT
    if [[ ${#containers[@]} -gt 0 ]]; then
        docker rm --force "${containers[@]}" > /dev/null || result=1
    fi
    exit "$result"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

for service in identity-db incident-db; do
    image=$(jq -er --arg service "$service" '.services[$service].image' <<< "$configuration")
    username=$(jq -er --arg service "$service" '.services[$service].environment.POSTGRES_USER' <<< "$configuration")
    database=$(jq -er --arg service "$service" '.services[$service].environment.POSTGRES_DB' <<< "$configuration")
    initdb_args=$(jq -er --arg service "$service" '.services[$service].environment.POSTGRES_INITDB_ARGS' <<< "$configuration")
    probe=$(jq -er --arg service "$service" '
        .services[$service].healthcheck.test |
        if .[0] == "CMD" then .[1:] | @sh else error("Expected a CMD healthcheck") end
    ' <<< "$configuration")
    container=$(docker run --detach --network none --memory 512m --cpus 1 \
        --tmpfs /var/lib/postgresql/data \
        --env "POSTGRES_USER=$username" --env "POSTGRES_DB=$database" \
        --env POSTGRES_PASSWORD=readiness-test-only-password \
        --env "POSTGRES_INITDB_ARGS=$initdb_args" \
        --health-cmd "$probe" --health-interval 1s --health-timeout 3s --health-retries 30 \
        --entrypoint bash "$image" -euc '
            cat > /docker-entrypoint-initdb.d/00-readiness-test.sh <<INIT
touch /tmp/readiness-test-initializing
while [ ! -e /tmp/readiness-test-release ]; do sleep 0.1; done
INIT
            exec /usr/local/bin/docker-entrypoint.sh postgres
        ')
    containers+=("$container")
    initializing=false
    for attempt in {1..60}; do
        if docker exec "$container" test -e /tmp/readiness-test-initializing; then
            initializing=true
            break
        fi
        sleep 0.5
    done
    if [[ $initializing != true ]]; then
        echo "$service did not reach its temporary initialization server" >&2
        exit 1
    fi
    docker exec "$container" pg_isready -U "$username" -d "$database" > /dev/null
    if docker exec "$container" sh -c "$probe" > /dev/null; then
        echo "$service healthcheck accepted the socket-only initialization server" >&2
        exit 1
    fi
    docker exec "$container" touch /tmp/readiness-test-release
    healthy=false
    for attempt in {1..60}; do
        if [[ $(docker inspect --format '{{.State.Health.Status}}' "$container") == healthy ]]; then
            healthy=true
            break
        fi
        sleep 0.5
    done
    if [[ $healthy != true ]]; then
        echo "$service did not become healthy after initialization" >&2
        exit 1
    fi
    docker exec --env PGPASSWORD=readiness-test-only-password "$container" \
        psql -X --no-password -h 127.0.0.1 -U "$username" -d "$database" -c 'SELECT 1' > /dev/null
done
echo 'PostgreSQL healthchecks reject the temporary socket-only server and accept the final TCP server.'
