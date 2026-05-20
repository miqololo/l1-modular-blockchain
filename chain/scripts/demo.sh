#!/usr/bin/env bash
# demo.sh — end-to-end demo against a running devnet.
#
# Registers a service as `bob`, requests inference as `alice`, observes the
# inference-node submit a result, and prints the finalized request.
#
# Usage: ./demo.sh                    (against host devnet)
#        docker compose exec chain /usr/local/bin/aid ... (inside compose)
set -euo pipefail

KEYRING="${KEYRING:-test}"
HOME_DIR="${AID_HOME:-/home/aios/.aid}"
CHAIN_ID="${CHAIN_ID:-aios-devnet-1}"
NODE="${CHAIN_RPC:-http://localhost:26657}"
AID="${AID_BINARY:-aid}"

ALICE=$(${AID} keys show alice -a --keyring-backend ${KEYRING} --home ${HOME_DIR})
BOB=$(${AID} keys show bob   -a --keyring-backend ${KEYRING} --home ${HOME_DIR})

echo "═══════════════════════════════════════════════════════"
echo " aios Phase 0.5 demo"
echo "═══════════════════════════════════════════════════════"
echo " alice (requester): ${ALICE}"
echo " bob   (provider):  ${BOB}"
echo

# ─── Step 1: bob registers a service ─────────────────────────────────────
echo "Step 1/5: Register a service as bob"
TX1=$(${AID} tx aiservice register-service translate-en-fr 10aios \
  --description "EN→FR translation (demo)" \
  --from bob --keyring-backend ${KEYRING} --home ${HOME_DIR} \
  --chain-id ${CHAIN_ID} --node ${NODE} \
  --gas-prices 0.0001aios --gas auto --gas-adjustment 1.3 \
  --yes -o json)
echo "  tx: $(echo "${TX1}" | jq -r .txhash)"
sleep 6
SERVICE_ID=$(${AID} q aiservice services --node ${NODE} -o json | jq -r '.services[0].id')
echo "  service_id: ${SERVICE_ID}"
echo

# ─── Step 2: alice requests inference ────────────────────────────────────
echo "Step 2/5: Request inference as alice"
TX2=$(${AID} tx aiservice request-inference ${SERVICE_ID} "Translate 'hello world' to French" \
  --max-price 10aios \
  --from alice --keyring-backend ${KEYRING} --home ${HOME_DIR} \
  --chain-id ${CHAIN_ID} --node ${NODE} \
  --gas-prices 0.0001aios --gas auto --gas-adjustment 1.3 \
  --yes -o json)
echo "  tx: $(echo "${TX2}" | jq -r .txhash)"
sleep 6
REQUEST_ID=$(${AID} q aiservice requests --node ${NODE} -o json | jq -r '.requests[-1].id')
echo "  request_id: ${REQUEST_ID}"
echo

# ─── Step 3: inference-node picks up the event ───────────────────────────
echo "Step 3/5: inference-node observes the event and runs inference"
echo "  (tailing inference-node logs for 15s)"
docker compose logs --tail=20 inference-node 2>/dev/null || true
sleep 15
echo

# ─── Step 4: chain finalization ──────────────────────────────────────────
echo "Step 4/5: Query the finalized request"
${AID} q aiservice request ${REQUEST_ID} --node ${NODE} -o json | jq '{
  id: .request.id,
  status: .request.status,
  finalized_at: .request.finalized_at_height,
  output_hash: .request.result.output_hash,
  output_uri: .request.result.output_uri,
}'
echo

# ─── Step 5: indexer reflects it ─────────────────────────────────────────
echo "Step 5/5: Indexer view"
curl -s "http://localhost:8081/requests/${REQUEST_ID}" | jq .
echo

echo "═══════════════════════════════════════════════════════"
echo " demo complete — open http://localhost:3000 in your browser"
echo "═══════════════════════════════════════════════════════"
