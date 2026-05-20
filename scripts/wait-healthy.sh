#!/usr/bin/env bash
# wait-healthy.sh — block until all compose services are healthy or timeout.
set -euo pipefail

TIMEOUT="${TIMEOUT:-300}"   # seconds
INTERVAL=3

services=$(docker compose ps --services 2>/dev/null || true)
if [ -z "${services}" ]; then
  echo "no services found; is compose running?"
  exit 1
fi

start=$(date +%s)
while :; do
  now=$(date +%s)
  elapsed=$((now - start))
  if [ ${elapsed} -ge ${TIMEOUT} ]; then
    echo "TIMEOUT after ${TIMEOUT}s — last status:"
    docker compose ps
    exit 1
  fi

  all_healthy=true
  for svc in ${services}; do
    state=$(docker inspect "aios-${svc}" --format='{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' 2>/dev/null || echo "missing")
    case "${state}" in
      healthy|exited|running)
        # exited is acceptable for one-shot init containers like model-init
        ;;
      *)
        all_healthy=false
        ;;
    esac
  done

  if ${all_healthy}; then
    echo "all services healthy after ${elapsed}s"
    exit 0
  fi
  sleep ${INTERVAL}
done
