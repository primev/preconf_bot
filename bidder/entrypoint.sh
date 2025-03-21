#!/usr/bin/env bash

set -e

MEV_COMMIT_VERSION="${MEV_COMMIT_VERSION:-v1.1.0-rc1}"
DOMAIN="${DOMAIN:-testnet.mev-commit.xyz}"
AUTO_DEPOSIT_VALUE="${AUTO_DEPOSIT_VALUE:-300000000000000000}"
KEYSTORE_PATH="./keystore"
ARTIFACTS_BASE_URL="https://github.com/primev/mev-commit/releases/download"
RPC_URL="${RPC_URL:-https://chainrpc.${DOMAIN}}"
LOG_LEVEL="${LOG_LEVEL:-info}"

BINARY_PATH="/usr/local/bin/mev-commit"

VERSION=${MEV_COMMIT_VERSION#v}

ARCH=$(uname -m)
if [ "${ARCH}" = "x86_64" ]; then
  FILE="mev-commit_${VERSION}_Linux_x86_64.tar.gz"
elif [ "${ARCH}" = "aarch64" ] || [ "${ARCH}" = "arm64" ]; then
  FILE="mev-commit_${VERSION}_Linux_arm64.tar.gz"
else
  echo "Error: Unsupported architecture: ${ARCH}"
  exit 1
fi

DOWNLOAD_URL="${ARTIFACTS_BASE_URL}/${MEV_COMMIT_VERSION}/${FILE}"
TEMP_DIR=$(mktemp -d)

curl -sL "${DOWNLOAD_URL}" -o "${TEMP_DIR}/${FILE}"
tar -xzf "${TEMP_DIR}/${FILE}" -C "${TEMP_DIR}"

sudo mv "${TEMP_DIR}/mev-commit" "${BINARY_PATH}"
sudo chmod +x "${BINARY_PATH}"

rm -rf "${TEMP_DIR}"

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
  --log-fmt "json" \
  --log-level "${LOG_LEVEL}" \
  --bootnodes "${BOOTNODE}" \
  --keystore-path "${KEYSTORE_PATH}" \
  --keystore-password "${KEYSTORE_PASSWORD}" \
  --bidder-registry-contract "${BIDDER_REGISTRY_ADDR}" \
  --provider-registry-contract "${PROVIDER_REGISTRY_ADDR}" \
  --block-tracker-contract "${BLOCK_TRACKER_ADDR}" \
  --preconf-contract "${PRECONF_ADDR}"
