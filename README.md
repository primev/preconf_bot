## About

A mev-commit bidder client service that integrates with a separate bidder node in a single docker compose setup. `bot` houses bidder client logic while `bidder` houses setup to run a bidder node from https://github.com/primev/mev-commit.

## Requirements

- funded L1 address
- funded mev-commit address
- Websocket endpoint (a publicly available one is set as default for Holesky, but cannot handle high throughput)

## `.env` variables

```
RPC_ENDPOINT=rpc_endpoint                   # RPC endpoint when use-payload is false (optional)
WS_ENDPOINT=ws_endpoint                     # WebSocket endpoint for transactions (Default wss://ethereum-holesky-rpc.publicnode.com)
PRIVATE_KEY=private_key                     # Private key for signing transactions
USE_PAYLOAD=true                            # Use payload for transactions (Default true)
SERVER_ADDRESS=localhost:13524              # Address of the server (Default localhost:13524)
OFFSET=1                                    # Offset is how many blocks ahead to bid for the preconf transaction (Default 1)
NUM_BLOB=0                                  # Number of blobs to send (0 for ETH transfer) (Default 0)
BID_AMOUNT=0.001                            # Amount to bid in ETH (Default 0.001)
PRIORITY_FEE=1                              # Priority fee in wei (Default 1)
BID_AMOUNT_STD_DEV_PERCENTAGE=100           # Standard deviation percentage for bid amount (Default 100.0)
DEFAULT_TIMEOUT=15                          # Default timeout in seconds (Default 15)
RUN_DURATION_MINUTES=0                      # Duration to run the bidder in minutes (0 to run indefinitely) (Default 0)
APP_NAME=preconf_bidder                     # Application name, for logging purposes (Default preconf_bidder)
VERSION=0.8.0                              # mev-commit version, for logging purposes (Default 0.8.0)
```

## Ensure Bidder Node is Running

If running the `bot` directly on your machine, ensure a mev-commit bidder node is running and the autodeposit function deposited ETH into the bidder window. A quickstart to run a bidder node can be found [here](https://docs.primev.xyz/get-started/quickstart).

Otherwise, by using the docker compose setup, the bidder node will be started automatically.