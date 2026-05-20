#!/usr/bin/env bash
# scripts/ci-assert-status.sh — poll the chain for a request's status and
# assert it matches the expected terminal value within a timeout.
#
# Usage:
#   ci-assert-status.sh <request_id> <expected_status> <timeout_seconds> [chain_url]
#
# Example:
#   ci-assert-status.sh 1 FINALIZED 90
#   ci-assert-status.sh 1 SLASHED 90
#   ci-assert-status.sh 1 REFUNDED 90
#
# Exit codes:
#   0 — request reached the expected status within the timeout
#   1 — timed out OR ended in a different terminal status
#
# Used by the GitHub Actions demo workflow to assert each demo target's
# documented outcome. Designed to be quiet in the success case and verbose
# in the failure case (so CI logs show what went wrong).

set -euo pipefail

if [ "$#" -lt 3 ] || [ "$#" -gt 4 ]; then
  echo "usage: $0 <request_id> <expected_status> <timeout_seconds> [chain_url]" >&2
  exit 2
fi

REQUEST_ID="$1"
EXPECTED="$2"
TIMEOUT="$3"
CHAIN_URL="${4:-http://localhost:26657}"

DEADLINE=$((SECONDS + TIMEOUT))
LAST_STATUS=""

while [ $SECONDS -lt $DEADLINE ]; do
  STATUS="$(curl -sS "$CHAIN_URL/requests/$REQUEST_ID" | python3 -c "
import json, sys
try:
    d = json.load(sys.stdin)
    print(d.get('status', '?'))
except Exception:
    print('?')
" 2>/dev/null || echo "?")"

  if [ "$STATUS" != "$LAST_STATUS" ]; then
    echo "  [t=${SECONDS}s] status=$STATUS"
    LAST_STATUS="$STATUS"
  fi

  if [ "$STATUS" = "$EXPECTED" ]; then
    echo "✓ request $REQUEST_ID reached $EXPECTED in ${SECONDS}s"
    exit 0
  fi

  # Terminal statuses other than the one we expected — fail fast instead of
  # waiting out the full timeout. (REFUNDED can be terminal even on a path
  # that "should have" finalized, so failing fast helps debugging.)
  case "$STATUS" in
    FINALIZED|SLASHED|REFUNDED)
      if [ "$STATUS" != "$EXPECTED" ]; then
        echo "✗ request $REQUEST_ID reached terminal status $STATUS but expected $EXPECTED" >&2
        # Dump the full request payload for debugging in CI logs.
        echo "--- request payload ---" >&2
        curl -sS "$CHAIN_URL/requests/$REQUEST_ID" | python3 -m json.tool >&2 || true
        exit 1
      fi
      ;;
  esac

  sleep 3
done

echo "✗ request $REQUEST_ID timed out at status=$LAST_STATUS (expected $EXPECTED) after ${TIMEOUT}s" >&2
echo "--- request payload ---" >&2
curl -sS "$CHAIN_URL/requests/$REQUEST_ID" | python3 -m json.tool >&2 || true
exit 1
