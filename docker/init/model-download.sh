#!/usr/bin/env sh
# model-download.sh — download (and optionally SHA-256-verify) the TinyLlama
# Q4_K_M GGUF for llama-server.
#
# Idempotent. If MODEL_SHA256 is set, verifies; otherwise downloads only if missing.
# Phase 0.5: SHA verification optional for demo robustness. Phase 1 requires pinned hash.
set -eu

MODEL_DIR="${MODEL_DIR:-/models}"
MODEL_FILE="${MODEL_FILE:-tinyllama-1.1b-chat-v1.0.Q4_K_M.gguf}"
MODEL_URL="${MODEL_MIRROR_URL:-https://huggingface.co/TheBloke/TinyLlama-1.1B-Chat-v1.0-GGUF/resolve/main/tinyllama-1.1b-chat-v1.0.Q4_K_M.gguf}"
MODEL_SHA256="${MODEL_SHA256:-}"

mkdir -p "${MODEL_DIR}"
DEST="${MODEL_DIR}/${MODEL_FILE}"

compute_sha() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

# If file already there:
if [ -f "${DEST}" ]; then
  if [ -n "${MODEL_SHA256}" ]; then
    actual=$(compute_sha "${DEST}")
    if [ "${actual}" = "${MODEL_SHA256}" ]; then
      echo "→ model present and verified: ${DEST}"
      exit 0
    fi
    echo "→ existing file has wrong SHA, redownloading"
    rm -f "${DEST}"
  else
    # No pinned SHA — accept what's there.
    echo "→ model present (unverified): ${DEST}"
    actual=$(compute_sha "${DEST}")
    echo "  observed SHA-256: ${actual}"
    exit 0
  fi
fi

echo "→ downloading model from ${MODEL_URL}"
echo "  (~700MB, one-time per volume)"

i=1
while [ $i -le 3 ]; do
  if curl -L --fail --retry 3 --connect-timeout 30 --max-time 1800 \
       -o "${DEST}.tmp" "${MODEL_URL}"; then
    break
  fi
  echo "→ attempt $i failed; retrying"
  i=$((i+1))
  sleep 5
done

if [ ! -f "${DEST}.tmp" ]; then
  echo "FATAL: model download failed after 3 attempts" >&2
  exit 1
fi

if [ -n "${MODEL_SHA256}" ]; then
  actual=$(compute_sha "${DEST}.tmp")
  if [ "${actual}" != "${MODEL_SHA256}" ]; then
    echo "FATAL: SHA-256 mismatch" >&2
    echo "  expected: ${MODEL_SHA256}" >&2
    echo "  actual:   ${actual}" >&2
    rm -f "${DEST}.tmp"
    exit 2
  fi
  echo "→ SHA-256 verified"
else
  actual=$(compute_sha "${DEST}.tmp")
  echo "→ SHA-256 (unverified): ${actual}"
  echo "  Pin this in .env as MODEL_SHA256= to enforce."
fi

mv "${DEST}.tmp" "${DEST}"
echo "→ model installed: ${DEST}"
