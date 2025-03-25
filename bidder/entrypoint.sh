#!/usr/bin/env bash

set -e

DOMAIN="${DOMAIN:-testnet.mev-commit.xyz}"
KEYSTORE_DIR="/keystore"
RPC_URL="${RPC_URL:-https://chainrpc.${DOMAIN}}"
LOG_LEVEL="${LOG_LEVEL:-info}"
LOG_FMT="${LOG_FMT:-text}"
BINARY_PATH="/usr/local/bin/mev-commit"

BOOTNODE="/dnsaddr/bootnode.${DOMAIN}"
CONTRACTS_URL="https://contracts.${DOMAIN}"

contracts_json=$(curl -sL "${CONTRACTS_URL}")
if ! echo "${contracts_json}" | jq . > /dev/null 2>&1; then
  echo "Failed to fetch contracts from ${CONTRACTS_URL}"
  exit 1
fi

BIDDER_REGISTRY_ADDR=$(echo "${contracts_json}" | jq -r '.BidderRegistry')
PROVIDER_REGISTRY_ADDR=$(echo "${contracts_json}" | jq -r '.ProviderRegistry')
BLOCK_TRACKER_ADDR=$(echo "${contracts_json}" | jq -r '.BlockTracker')
PRECONF_ADDR=$(echo "${contracts_json}" | jq -r '.PreConfCommitmentStore // .PreconfManager')

exec "${BINARY_PATH}" --peer-type "bidder" \
  --settlement-rpc-endpoint "${RPC_URL}" \
  --log-fmt "${LOG_FMT}" \
  --log-level "${LOG_LEVEL}" \
  --bootnodes "${BOOTNODE}" \
  --keystore-path "${KEYSTORE_DIR}" \
  --keystore-password "${KEYSTORE_PASSWORD}" \
  --bidder-registry-contract "${BIDDER_REGISTRY_ADDR}" \
  --provider-registry-contract "${PROVIDER_REGISTRY_ADDR}" \
  --block-tracker-contract "${BLOCK_TRACKER_ADDR}" \
  --preconf-contract "${PRECONF_ADDR}"
