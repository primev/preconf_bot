#!/usr/bin/env bash

set -e

DOMAIN="${DOMAIN:-testnet.mev-commit.xyz}"
KEYSTORE_DIR="/keystore"
RPC_URL="${RPC_URL:-https://chainrpc.${DOMAIN}}"
LOG_LEVEL="${LOG_LEVEL:-info}"
LOG_FMT="${LOG_FMT:-text}"
BINARY_PATH="/usr/local/bin/mev-commit"

BOOTNODE="/dnsaddr/bootnode.${DOMAIN}"

exec "${BINARY_PATH}" --peer-type "bidder" \
  --settlement-rpc-endpoint "${RPC_URL}" \
  --log-fmt "${LOG_FMT}" \
  --log-level "${LOG_LEVEL}" \
  --bootnodes "${BOOTNODE}" \
  --keystore-path "${KEYSTORE_DIR}" \
  --keystore-password "${KEYSTORE_PASSWORD}"
