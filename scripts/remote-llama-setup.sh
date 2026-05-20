#!/usr/bin/env bash
# scripts/remote-llama-setup.sh — set up a minimal llama-server on a remote
# host for the cross-host determinism test (MVP item #1).
#
# Idempotent: safe to re-run; only does work it hasn't already done.
#
# Inputs (env vars, must match the local pinned configuration):
#   MODEL_MIRROR_URL — HF or mirror URL for the GGUF file
#   MODEL_SHA256     — expected SHA-256 of the model file (verified)
#   LLAMA_IMAGE      — llama.cpp Docker image (default: ghcr.io/ggml-org/llama.cpp:server)
#
# After completion:
#   - The model file is at /root/aios-cross-host/models/tinyllama-1.1b-chat-v1.0.Q4_K_M.gguf
#   - A container named aios-llama-server-remote is running, listening on 127.0.0.1:8080
#   - `curl http://127.0.0.1:8080/health` returns {"status":"ok"}

set -euo pipefail

: "${MODEL_MIRROR_URL:?need MODEL_MIRROR_URL}"
: "${MODEL_SHA256:?need MODEL_SHA256}"
: "${LLAMA_IMAGE:=ghcr.io/ggml-org/llama.cpp:server}"

WORKDIR=/root/aios-cross-host
MODEL_DIR="$WORKDIR/models"
MODEL_FILE="$MODEL_DIR/tinyllama-1.1b-chat-v1.0.Q4_K_M.gguf"

mkdir -p "$MODEL_DIR"

# ── Step 1: download model if missing or hash mismatch ──────────────────────
if [ -f "$MODEL_FILE" ]; then
  ACTUAL_SHA=$(sha256sum "$MODEL_FILE" | cut -d' ' -f1)
  if [ "$ACTUAL_SHA" = "$MODEL_SHA256" ]; then
    echo "✓ model already present and SHA-256 matches"
  else
    echo "✗ model present but SHA-256 mismatches ($ACTUAL_SHA != $MODEL_SHA256), re-downloading"
    rm -f "$MODEL_FILE"
  fi
fi

if [ ! -f "$MODEL_FILE" ]; then
  echo "→ downloading model from $MODEL_MIRROR_URL"
  curl -fL --retry 3 -o "$MODEL_FILE" "$MODEL_MIRROR_URL"
  ACTUAL_SHA=$(sha256sum "$MODEL_FILE" | cut -d' ' -f1)
  if [ "$ACTUAL_SHA" != "$MODEL_SHA256" ]; then
    echo "✗ downloaded model SHA-256 mismatches ($ACTUAL_SHA != $MODEL_SHA256)" >&2
    exit 1
  fi
  echo "✓ model downloaded and SHA-256 verified"
fi

# ── Step 2: pull the llama.cpp image ────────────────────────────────────────
echo "→ pulling $LLAMA_IMAGE"
docker pull "$LLAMA_IMAGE" >/dev/null

# ── Step 3: (re)start the llama-server container ────────────────────────────
docker rm -f aios-llama-server-remote >/dev/null 2>&1 || true

echo "→ starting aios-llama-server-remote (bound to 127.0.0.1:8080)"
docker run -d \
  --name aios-llama-server-remote \
  --restart unless-stopped \
  -v "$MODEL_DIR:/models:ro" \
  -p 127.0.0.1:8080:8080 \
  "$LLAMA_IMAGE" \
    --model /models/tinyllama-1.1b-chat-v1.0.Q4_K_M.gguf \
    --host 0.0.0.0 \
    --port 8080 \
    --threads 4 \
    --ctx-size 2048 \
  >/dev/null

# ── Step 4: wait for health ─────────────────────────────────────────────────
echo "→ waiting for llama-server to respond"
for i in $(seq 1 30); do
  if curl -fsS http://127.0.0.1:8080/health >/dev/null 2>&1; then
    echo "✓ llama-server is healthy after ${i}s"
    exit 0
  fi
  sleep 2
done

echo "✗ llama-server did not become healthy within 60s" >&2
docker logs --tail=50 aios-llama-server-remote >&2 || true
exit 1
