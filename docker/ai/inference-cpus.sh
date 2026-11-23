#!/usr/bin/env bash
# Shared by the setup scripts. Resolve against the engine that runs the models.

valid_inference_threads() {
    [[ $1 == all || $1 =~ ^[1-9][0-9]{0,8}$ ]]
}

configure_inference_resources() {
    local requested=$1 pids_floor=$2 available
    if ! available=$(docker info --format '{{.NCPU}}') || [[ ! $available =~ ^[1-9][0-9]{0,8}$ ]]; then
        printf 'Could not determine the CPU count of the Docker engine.\n' >&2
        return 1
    fi

    AI_INFERENCE_CPUS=$available
    if [[ $requested != all ]] && (( requested < available )); then
        AI_INFERENCE_CPUS=$requested
    fi
    # Linux counts native threads towards the PID limit. Allow room for multiple
    # model library thread pools and server/healthcheck overhead on large hosts.
    AI_INFERENCE_PIDS=$((64 + 4 * AI_INFERENCE_CPUS))
    if (( AI_INFERENCE_PIDS < pids_floor )); then AI_INFERENCE_PIDS=$pids_floor; fi
    export AI_INFERENCE_CPUS AI_INFERENCE_PIDS
}
