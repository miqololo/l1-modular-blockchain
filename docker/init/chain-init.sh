#!/usr/bin/env bash
# chain-init.sh — initialize the devnet inside the chain container.
#
# Runs once on first compose-up (when the home dir is empty). Subsequent boots
# just call `aid start`. The compose chain service uses this as its command.
set -euo pipefail

HOME_DIR="${AID_HOME:-/home/aios/.aid}"
INIT_MARKER="${HOME_DIR}/.initialized"

if [ ! -f "${INIT_MARKER}" ]; then
  echo "→ first boot; initializing devnet"
  /usr/local/bin/init-devnet.sh
  touch "${INIT_MARKER}"
else
  echo "→ devnet already initialized; skipping"
fi

echo "→ starting aid"
exec /usr/local/bin/aid start --home "${HOME_DIR}" \
  --rpc.laddr tcp://0.0.0.0:26657 \
  --grpc.address 0.0.0.0:9090 \
  --api.address tcp://0.0.0.0:1317 \
  --api.enable
