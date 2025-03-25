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
AUTO_DEPOSIT_AMOUNT                         # Auto deposit amount in wei for bidder node
KEYSTORE_DIR                                # Directory of your keystore file
KEYSTORE_PASSWORD                           # Password for your keystore file
VALIDATOR_OPT_IN_ROUTER_ADDRESS             # Validator opt-in router address (0x821798d7b9d57dF7Ed7616ef9111A616aB19ed64 for mainnet, 0x251Fbc993f58cBfDA8Ad7b0278084F915aCE7fc3 for holesky)
BEACON_API_URL                              # Beacon chain rpc url used by bidder bot and bidder node
L1_RPC_URL                                  # L1 RPC URL
SETTLEMENT_RPC_URL                          # RPC URL for the mev-commit chain (https://chainrpc.mev-commit.xyz/ or https://chainrpc.testnet.mev-commit.xyz/)
MEV_COMMIT_VERSION                          # Tagged release of mev-commit to use for the bidder node
LOG_LEVEL                                   # Log level (Default info)
LOG_FMT                                     # Log format (Default text)
BID_AMOUNT                                  # Amount to bid in wei for each bid
MEV_COMMIT_PROPOSER_NOTIFY_OFFSET           # Amount of time before an upcoming proposer's slot for the bidder node to notify the bot
DD_API_KEY                                  # Datadog API key for the agent
```
