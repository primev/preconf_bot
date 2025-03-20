#!/usr/bin/env bash

set -e

load_env_file() {
  local env_file="$1"
  if [ -f "$env_file" ]; then
    echo "Loading environment variables from $env_file"
    # shellcheck disable=SC1090,SC1091
    set -o allexport
    source "$env_file"
    set +o allexport
  fi
}

load_env_file "$(dirname "$(realpath "$0")")/.env"

APP_NAME="mev-commit"
ENVIRONMENT="${ENVIRONMENT:-production}"
MEV_COMMIT_VERSION="${MEV_COMMIT_VERSION:-latest}" 
DOMAIN="${DOMAIN:-testnet.mev-commit.xyz}"
AUTO_DEPOSIT_VALUE="${AUTO_DEPOSIT_VALUE:-300000000000000000}"
BINARY_PATH="${BINARY_PATH:-/usr/local/bin/mev-commit}"
ARTIFACTS_BASE_URL="https://github.com/primev/mev-commit/releases"
RPC_URL="${RPC_URL:-wss://chainrpc-wss.${DOMAIN}}"
API_URL="http://127.0.0.1:13523"

LOG_FMT="${LOG_FMT:-json}"
LOG_LEVEL="${LOG_LEVEL:-info}"

# We'll use an in-memory file descriptor so the key never touches disk.
PRIV_KEY_FD="/proc/self/fd/3"

echo "Using RPC_URL=${RPC_URL}"

INSTALLED_VERSION=""
if command -v "${BINARY_PATH}" &> /dev/null; then
  INSTALLED_VERSION=$("${BINARY_PATH}" --version 2>/dev/null | awk '{print $3}')
  echo "Installed mev-commit version: ${INSTALLED_VERSION}"
fi

if [ "${MEV_COMMIT_VERSION}" = "latest" ]; then
  LATEST_VERSION=$(curl -sIL -o /dev/null -w %{url_effective} \
    "${ARTIFACTS_BASE_URL}/latest" \
    | sed 's:.*/::' | sed 's/^v//')
  echo "Latest mev-commit version: ${LATEST_VERSION}"
else

  LATEST_VERSION="${MEV_COMMIT_VERSION}"
  echo "Using mev-commit version: ${LATEST_VERSION}"
fi

if [ -z "${INSTALLED_VERSION}" ] || [ "${INSTALLED_VERSION}" != "${LATEST_VERSION}" ]; then
  echo "Downloading mev-commit ${LATEST_VERSION}..."
  FILE="mev-commit_${LATEST_VERSION}_Linux_x86_64.tar.gz"
  DOWNLOAD_URL="${ARTIFACTS_BASE_URL}/download/v${LATEST_VERSION}/${FILE}"

  TEMP_DIR=$(mktemp -d)

  curl -sL "${DOWNLOAD_URL}" -o "${TEMP_DIR}/${FILE}"
  echo "Extracting ${FILE} to ${TEMP_DIR}..."
  tar -xzf "${TEMP_DIR}/${FILE}" -C "${TEMP_DIR}"

  echo "Installing mev-commit to ${BINARY_PATH}..."
  sudo mv "${TEMP_DIR}/mev-commit" "${BINARY_PATH}"
  sudo chmod +x "${BINARY_PATH}"

  rm -rf "${TEMP_DIR}"
  echo "mev-commit downloaded and installed to ${BINARY_PATH}"
else
  echo "Using existing mev-commit binary found at ${BINARY_PATH}"
fi

echo "**********************************************************************"
echo "  Starting up the mev-commit Bidder Node             "
echo "**********************************************************************"
echo ""
echo "Configuration variables (current defaults):"
echo "  PRIVATE_KEY_BIDDER (required)    - your Ethereum private key"
echo "  DOMAIN: ${DOMAIN}"
echo "  AUTO_DEPOSIT_VALUE: ${AUTO_DEPOSIT_VALUE} (Wei)"
echo "  ENVIRONMENT: ${ENVIRONMENT}"
echo "  MEV_COMMIT_VERSION: ${MEV_COMMIT_VERSION}"
echo "  BINARY_PATH: ${BINARY_PATH}"
echo "  RPC_URL: ${RPC_URL}"
echo ""
echo "You can set these in a .env file or pass CLI args. Example usage:"
echo "  ./entrypoint.sh --private-key <HEXKEY> --auto-deposit 1000000000000000000"
echo ""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --domain)
      DOMAIN="$2"
      shift 2
      ;;
    --private-key)
      PRIVATE_KEY_BIDDER="$2"
      shift 2
      ;;
    --auto-deposit)
      AUTO_DEPOSIT_VALUE="$2"
      shift 2
      ;;
    --binary-path)
      BINARY_PATH="$2"
      shift 2
      ;;
    --environment)
      ENVIRONMENT="$2"
      shift 2
      ;;
    --mev-commit-version)
      MEV_COMMIT_VERSION="$2"
      shift 2
      ;;
    --rpc-url)
      RPC_URL="$2"
      shift 2
      ;;
    --log-fmt)
      LOG_FMT="$2"
      shift 2
      ;;
    --log-level)
      LOG_LEVEL="$2"
      shift 2
      ;;
    *)
      echo "Unknown parameter: $1"
      exit 1
      ;;
  esac
done

if [ -z "${PRIVATE_KEY_BIDDER}" ]; then
  echo "No PRIVATE_KEY_BIDDER environment variable found."
  read -r -s -p "Enter your private key (hex, no 0x): " PRIVATE_KEY_BIDDER
  echo ""
fi

if [ -z "${PRIVATE_KEY_BIDDER}" ]; then
  echo "Error: No private key provided!"
  exit 1
fi


if [ "${AUTO_DEPOSIT_VALUE}" = "300000000000000000" ]; then
  echo "Current auto-deposit: ${AUTO_DEPOSIT_VALUE} (default 0.3 ETH)."
  read -r -p "Use the default auto-deposit value? (y/n): " USE_DEFAULT_DEPOSIT
  if [[ ! "${USE_DEFAULT_DEPOSIT,,}" =~ ^(y|yes)$ ]]; then
    read -r -p "Enter custom auto-deposit value (in Wei): " AUTO_DEPOSIT_VALUE
  fi
else
  echo "Using auto-deposit value from environment: ${AUTO_DEPOSIT_VALUE}"
fi

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
PRECONF_ADDR=$(echo "${contracts_json}" | jq -r '.PreconfManager')

FLAGS=(
  --peer-type "bidder"
  --settlement-ws-rpc-endpoint "${RPC_URL}"
  --log-fmt "${LOG_FMT}"
  --log-level "${LOG_LEVEL}"
  --bootnodes "${BOOTNODE}"
  --priv-key-file "${PRIV_KEY_FD}"
  --bidder-bid-timeout "15s"
  --bidder-registry-contract "${BIDDER_REGISTRY_ADDR}"
  --provider-registry-contract "${PROVIDER_REGISTRY_ADDR}"
  --block-tracker-contract "${BLOCK_TRACKER_ADDR}"
  --preconf-contract "${PRECONF_ADDR}"
)

echo ""
echo "Starting mev-commit with the following flags:"
for f in "${FLAGS[@]}"; do
  echo "  $f"
done
echo ""

exec 3<<< "${PRIVATE_KEY_BIDDER}"

"${BINARY_PATH}" "${FLAGS[@]}" &
MEV_COMMIT_PID=$!

wait_for_api() {
  local url="$1"
  local timeout=60
  local start_time=$(date +%s)

  echo "Waiting for mev-commit API to be ready at ${url}..."
  while true; do
    if curl -s -o /dev/null -w "%{http_code}" "${url}/health" | grep -q "200"; then
      echo "mev-commit API is ready."
      return 0
    fi

    local current_time=$(date +%s)
    local elapsed_time=$((current_time - start_time))
    if (( elapsed_time >= timeout )); then
      echo "Timeout reached, mev-commit API did not become ready."
      return 1
    fi

    sleep 1
  done
}

if ! wait_for_api "${API_URL}"; then
  echo "Failed to start mev-commit or API did not become ready."
  kill "${MEV_COMMIT_PID}"
  exit 1
fi

echo "Sending auto-deposit request..."
AUTO_DEPOSIT_RESPONSE=$(
  curl \
    --silent \
    --show-error \
    --output /dev/null \
    --write-out "%{http_code}" \
    --request POST "${API_URL}/v1/bidder/auto_deposit/${AUTO_DEPOSIT_VALUE}"
)

if [ "${AUTO_DEPOSIT_RESPONSE}" -ne 200 ]; then
  echo "Failed to send auto-deposit request, status code: ${AUTO_DEPOSIT_RESPONSE}"
  kill "${MEV_COMMIT_PID}"
  exit 1
fi
echo "Auto-deposit request sent successfully."

wait "${MEV_COMMIT_PID}"
