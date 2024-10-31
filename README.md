## About
This repository provides an example workflow that gets preconfirmation bids from mev-commit for eth transfers. Transactions are sent directly to the builder as transaction payloads. Currently a fixed priority fee is used alongside a preconf bid amount.


## Requirements
- funded holesky address
- funded mev-commit address
- mev-commit p2p bidder node (v0.6)

## Installation
```
git clone https://github.com/primev/preconf_bot_example.git
cd preconf_blob_bidder
```

Then `go mod tiny` to install dependencies.

## `.env` variables
Ensure that the .env file is filled out with all of the variables.
```
RPC_ENDPOINT=rpc_endpoint # optional, not needed if `USE_PAYLOAD` is true.
WS_ENDPOINT=ws_endpoint
PRIVATE_KEY=private_key   # L1 private key
USE_PAYLOAD=true
BIDDER_ADDRESS="127.0.0.1:13524"
OFFSET=1   # of blocks in the future to ask for the preconf bid
NUM_BLOB=0 # blob count of 0 will just send eth transfers
BID_AMOUNT=0.0025       # preconf bid amount
BID_AMOUNT_STD_DEV_PERCENTAGE=200   # amount of variation in the preconf bid amount (in %)
DEFAULT_TIMEOUT=0
```
## How to run
Ensure that the mev-commit bidder node is running in the background. A quickstart can be found [here](https://docs.primev.xyz/get-started/quickstart), which will get the latest mev-commit version and start running it with an auto generated private key. 

## Docker
Build the docker with `sudo docker-compose up --build`. Best run with the unofficial [dockerized bidder node example](https://github.com/primev/bidder_node_docker)

## Linting
Run linting with `golangci-lint run ./...` inside the repository folder