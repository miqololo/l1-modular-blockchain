#!/usr/bin/env bash
# init-devnet.sh — bootstrap a single-validator aios devnet.
#
# Idempotent: re-running on an existing devnet wipes and re-initializes.
# Used by both `make localnet` (direct host execution) and the Docker entrypoint.
set -euo pipefail

CHAIN_ID="${CHAIN_ID:-aios-devnet-1}"
HOME_DIR="${AID_HOME:-$HOME/.aid}"
KEYRING="${KEYRING:-test}"
DENOM="aios"
MONIKER="${MONIKER:-aios-devnet-node}"

# Dev accounts. Funded at genesis; private keys are deterministic from mnemonics below.
DEV_ALICE_MNEMONIC="${DEV_ALICE_MNEMONIC:-abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about}"
DEV_BOB_MNEMONIC="${DEV_BOB_MNEMONIC:-army van defense carry jealous true garbage claim echo media make crunch}"

# Validator stake (devnet only — single validator owns 100% of stake).
VALIDATOR_STAKE="100000000000${DENOM}"
INITIAL_BALANCE="100000000${DENOM}"

AID="${AID_BINARY:-aid}"

echo "→ wiping ${HOME_DIR}"
rm -rf "${HOME_DIR}"

echo "→ initializing chain"
${AID} init "${MONIKER}" --chain-id "${CHAIN_ID}" --home "${HOME_DIR}" >/dev/null

echo "→ setting denom + min-gas-prices"
sed -i.bak -E "s|\"stake\"|\"${DENOM}\"|g" "${HOME_DIR}/config/genesis.json"
sed -i.bak -E "s|^minimum-gas-prices *=.*|minimum-gas-prices = \"0.0001${DENOM}\"|" "${HOME_DIR}/config/app.toml"
rm -f "${HOME_DIR}/config/genesis.json.bak" "${HOME_DIR}/config/app.toml.bak"

echo "→ adding dev keys (alice, bob, validator)"
echo "${DEV_ALICE_MNEMONIC}" | ${AID} keys add alice --recover --keyring-backend "${KEYRING}" --home "${HOME_DIR}" >/dev/null
echo "${DEV_BOB_MNEMONIC}"   | ${AID} keys add bob   --recover --keyring-backend "${KEYRING}" --home "${HOME_DIR}" >/dev/null
${AID} keys add validator --keyring-backend "${KEYRING}" --home "${HOME_DIR}" >/dev/null

echo "→ funding genesis accounts"
${AID} genesis add-genesis-account "$(${AID} keys show alice -a --keyring-backend ${KEYRING} --home ${HOME_DIR})" "${INITIAL_BALANCE}" --home "${HOME_DIR}"
${AID} genesis add-genesis-account "$(${AID} keys show bob -a --keyring-backend ${KEYRING} --home ${HOME_DIR})" "${INITIAL_BALANCE}" --home "${HOME_DIR}"
${AID} genesis add-genesis-account "$(${AID} keys show validator -a --keyring-backend ${KEYRING} --home ${HOME_DIR})" "${VALIDATOR_STAKE}" --home "${HOME_DIR}"

echo "→ generating gentx (single validator)"
${AID} genesis gentx validator "${VALIDATOR_STAKE}" \
    --chain-id "${CHAIN_ID}" \
    --keyring-backend "${KEYRING}" \
    --home "${HOME_DIR}" >/dev/null

${AID} genesis collect-gentxs --home "${HOME_DIR}" >/dev/null
${AID} genesis validate-genesis --home "${HOME_DIR}" >/dev/null

echo "→ devnet initialized at ${HOME_DIR}"
echo "  chain-id:    ${CHAIN_ID}"
echo "  validator:   $(${AID} keys show validator -a --keyring-backend ${KEYRING} --home ${HOME_DIR})"
echo "  alice:       $(${AID} keys show alice -a --keyring-backend ${KEYRING} --home ${HOME_DIR})"
echo "  bob:         $(${AID} keys show bob -a --keyring-backend ${KEYRING} --home ${HOME_DIR})"
echo
echo "Start with:"
echo "  ${AID} start --home ${HOME_DIR}"
