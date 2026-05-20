#!/usr/bin/env bash
# scripts/cross-host-determinism-check.sh — verify that two physically
# distinct hosts running the pinned `(model, runtime, hardware_tag, precision,
# tokenizer)` tuple produce bit-identical output for the same prompt.
#
# Closes MVP item #1 (cross-host determinism). Reuses the same prompt + sampling
# params as `make determinism-check` (cross-process).
#
# Inputs:
#   REMOTE_HOST            — ssh user@host (key-based auth required)
#   MODEL_MIRROR_URL       — HF or mirror URL for the GGUF file (default: TheBloke TinyLlama Q4)
#   MODEL_SHA256           — expected SHA-256 of the model file
#   LOCAL_AMD64_PORT       — host port for the local amd64-platform llama-server (default 8083)
#
# Local side:
#   - Boots an amd64-platform llama-server container on $LOCAL_AMD64_PORT,
#     mounted from the same model volume the rest of the demo uses. On Apple
#     Silicon this runs via Rosetta JIT; on native x86_64 hosts it runs native.
#   - This is what makes the comparison "same hardware_tag" — both sides are
#     executing the linux/amd64 build of llama.cpp with identical args.
#
# Remote side:
#   - Provisions Docker if missing, downloads model (with SHA verify), starts
#     llama-server bound to 127.0.0.1:8080 of the remote.
#
# Comparison:
#   - 3 runs of the pinned prompt against each host = 6 hashes total.
#   - Assertion: all 6 hashes are byte-identical.

set -euo pipefail

REMOTE_HOST="${REMOTE_HOST:?need REMOTE_HOST (e.g. root@1.2.3.4)}"
MODEL_MIRROR_URL="${MODEL_MIRROR_URL:-https://huggingface.co/TheBloke/TinyLlama-1.1B-Chat-v1.0-GGUF/resolve/main/tinyllama-1.1b-chat-v1.0.Q4_K_M.gguf}"
MODEL_SHA256="${MODEL_SHA256:-9fecc3b3cd76bba89d504f29b616eedf7da85b96540e490ca5824d3f7d2776a0}"
LOCAL_AMD64_PORT="${LOCAL_AMD64_PORT:-8083}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

PROMPT_JSON='{"prompt":"Determinism check. Reply briefly.","n_predict":64,"temperature":0,"top_k":1,"top_p":1,"seed":0,"stream":false}'

# Resolve the docker-compose model volume name. Both naming schemes (the bare
# `aios-models` from older compose runs, and the `<project>_aios-models` form
# that modern docker-compose produces) may exist; we want the one that
# actually contains the GGUF file.
VOLUME_NAME=""
for candidate in $(docker volume ls --format '{{.Name}}' | grep -E '(^aios-models$|_aios-models$)'); do
  if docker run --rm -v "$candidate":/models alpine sh -c 'test -f /models/tinyllama-1.1b-chat-v1.0.Q4_K_M.gguf' 2>/dev/null; then
    VOLUME_NAME="$candidate"
    break
  fi
done
if [ -z "$VOLUME_NAME" ]; then
  echo "✗ no aios-models volume contains the GGUF. Run 'docker compose up -d model-init' first." >&2
  exit 1
fi
echo "→ local: using model volume $VOLUME_NAME"

# ── Local: boot the amd64-platform llama-server ─────────────────────────────
echo "→ local: starting amd64 llama-server on :$LOCAL_AMD64_PORT (Rosetta on Apple Silicon, native otherwise)"
docker pull --platform linux/amd64 ghcr.io/ggml-org/llama.cpp:server >/dev/null
docker rm -f aios-llama-server-amd64 >/dev/null 2>&1 || true
docker run -d --platform linux/amd64 \
  --name aios-llama-server-amd64 \
  -v "$VOLUME_NAME":/models:ro \
  -p "$LOCAL_AMD64_PORT:8080" \
  ghcr.io/ggml-org/llama.cpp:server \
    --model /models/tinyllama-1.1b-chat-v1.0.Q4_K_M.gguf \
    --host 0.0.0.0 --port 8080 --threads 4 --ctx-size 2048 >/dev/null

echo -n "→ local: waiting for amd64 llama-server health"
for i in $(seq 1 40); do
  if curl -fsS "http://localhost:$LOCAL_AMD64_PORT/health" >/dev/null 2>&1; then
    echo " ✓ ($((i*3))s)"
    break
  fi
  echo -n "."
  sleep 3
done

# ── Remote: provision + boot ─────────────────────────────────────────────────
echo "→ remote: ensuring Docker + llama-server are up on $REMOTE_HOST"
# Install Docker if missing — apt-only, idempotent.
ssh -o BatchMode=yes "$REMOTE_HOST" '
  if ! command -v docker >/dev/null; then
    apt-get update -qq && apt-get install -y -qq docker.io curl
    systemctl enable --now docker
  fi
'
scp -o BatchMode=yes "$SCRIPT_DIR/remote-llama-setup.sh" "$REMOTE_HOST":/root/remote-llama-setup.sh >/dev/null
ssh -o BatchMode=yes "$REMOTE_HOST" "
  MODEL_MIRROR_URL='$MODEL_MIRROR_URL' \
  MODEL_SHA256='$MODEL_SHA256' \
  bash /root/remote-llama-setup.sh
" | sed 's/^/    /'

# ── Capture hashes from both sides ───────────────────────────────────────────
echo ""
echo "=== Local amd64 (port $LOCAL_AMD64_PORT) ==="
LOCAL_HASHES=()
for n in 1 2 3; do
  RESP=$(curl -fsS -X POST -H 'Content-Type: application/json' -d "$PROMPT_JSON" "http://localhost:$LOCAL_AMD64_PORT/completion")
  HASH=$(echo "$RESP" | python3 -c "import json,sys,hashlib; d=json.loads(sys.stdin.read(),strict=False); print(hashlib.sha256(d['content'].encode()).hexdigest()[:16])")
  echo "  run $n  hash=$HASH"
  LOCAL_HASHES+=("$HASH")
done

echo ""
echo "=== Remote amd64 ($REMOTE_HOST) ==="
REMOTE_OUT=$(ssh -o BatchMode=yes "$REMOTE_HOST" "for n in 1 2 3; do
  RESP=\$(curl -fsS -X POST -H 'Content-Type: application/json' -d '$PROMPT_JSON' http://127.0.0.1:8080/completion)
  HASH=\$(echo \"\$RESP\" | python3 -c \"import json,sys,hashlib; d=json.loads(sys.stdin.read(),strict=False); print(hashlib.sha256(d['content'].encode()).hexdigest()[:16])\")
  echo \"  run \$n  hash=\$HASH\"
done")
echo "$REMOTE_OUT"
REMOTE_HASHES=($(echo "$REMOTE_OUT" | awk -F'hash=' '{print $2}'))

# ── Assertion ────────────────────────────────────────────────────────────────
ALL=("${LOCAL_HASHES[@]}" "${REMOTE_HASHES[@]}")
UNIQUE=$(printf '%s\n' "${ALL[@]}" | sort -u | wc -l | tr -d ' ')

echo ""
echo "=== Result ==="
echo "  6 hashes captured (3 local amd64 + 3 remote amd64)"
echo "  unique hashes: $UNIQUE"
if [ "$UNIQUE" = "1" ]; then
  echo ""
  echo "  ✓ CROSS-HOST DETERMINISM HOLDS"
  echo "    Hash: ${ALL[0]}"
  echo "    Pinned tuple: tinyllama-1.1b-chat-v1.0.Q4_K_M.gguf @ llama.cpp:server @ linux/amd64 @ q4_k_m @ greedy(seed=0)"
  echo ""
  echo "  This closes MVP item #1 — see docs/REPRODUCIBILITY.md step 1b."
  exit 0
else
  echo ""
  echo "  ✗ CROSS-HOST DETERMINISM FAILED" >&2
  echo "    Hashes: ${ALL[*]}" >&2
  echo ""
  echo "  This means the hardware_tag '$LOCAL_AMD64_PORT' is not granular enough to capture all" >&2
  echo "  determinism-affecting variation between these two hosts. Investigate:" >&2
  echo "    1. CPU microarch differences (AVX-512 vs no, microcode revision)" >&2
  echo "    2. libc / kernel scheduler variance" >&2
  echo "    3. llama.cpp build differences (compiler flags, OpenMP vs not)" >&2
  exit 1
fi
