## About

A mev-commit bidder bot that integrates with a separate bidder node in a single docker compose setup.

## Requirements
* Docker compose compatible machine
* Keystore address funded on both L1 and the mev-commit chain (mainnet and holesky supported)
* Various RPC endpoints etc. as specified below

## To run 

`docker compose up -d --build` from the root directory with a properly populated .env file. See `env.example`. 

## `.env` variables

```
DOMAIN                                      # "testnet.mev-commit.xyz" or "mev-commit.xyz"
AUTO_DEPOSIT_AMOUNT=100000000000000000
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
