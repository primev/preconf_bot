#!/usr/bin/env bash

set -e

DOMAIN="${DOMAIN:-testnet.mev-commit.xyz}"
KEYSTORE_DIR="/keystore"
RPC_URL="${RPC_URL:-https://chainrpc.${DOMAIN}}"
LOG_LEVEL="${LOG_LEVEL:-info}"
LOG_FMT="${LOG_FMT:-text}"
BINARY_PATH="/usr/local/bin/mev-commit"

BOOTNODE="/dnsaddr/bootnode-v2.${DOMAIN}"

exec "${BINARY_PATH}" --peer-type "bidder" \
  --settlement-rpc-endpoint "${RPC_URL}" \
  --log-fmt "${LOG_FMT}" \
  --log-level "${LOG_LEVEL}" \
  --bootnodes "${BOOTNODE}" \
  --keystore-path "${KEYSTORE_DIR}" \
  --keystore-password "${KEYSTORE_PASSWORD}" \
  --provider-whitelist 0xB3998135372F1eE16Cb510af70ed212b5155Af62,0x2445e5e28890De3e93F39fCA817639c470F4d3b9
