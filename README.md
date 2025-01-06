## About
This repository provides a user friendly CLI interface for making preconfirmation bids from mev-commit for ETH transfers or blob transactions and aims to support different types of transactions such as bridging in the future. Additionally a bidder can spin up the docker container and run directly with the example [bidder node docker](https://github.com/primev/bidder_node_docker)

Transactions are sent directly to the builder as transaction payloads. Currently a fixed priority fee is used alongside a preconf bid amount.

If you’re an advanced user, you can still skip the interactive mode by specifying all configurations via environment variables, .env files, or command-line flags.


## Requirements
- funded L1 address (default is Holesky)
- funded mev-commit address
- mev-commit p2p bidder node
- a good websocket endpoint (a publicly available one is set as default for Holesky, but cannot handle high throughput)

## Installation
```
git clone https://github.com/primev/preconf_bot_example.git
cd preconf_blob_bidder
```

Then `go mod tiny` to install dependencies.

## `.env` variables
Ensure that the .env file is filled out with all of the variables.
```
RPC_ENDPOINT=rpc_endpoint                   # optional, not needed if `USE_PAYLOAD` is true.
WS_ENDPOINT=ws_endpoint
PRIVATE_KEY=private_key                     # L1 private key
USE_PAYLOAD=true                            # sends tx payload direclty to providers (Default is true).
SERVER_ADDRESS="localhost:13524"            # address of the server (Default localhost:13524 to run locally)
OFFSET=1                                    # of blocks in the future to ask for the preconf bid (Default 1 for next block)
NUM_BLOB=0                                  # blob count of 0 will just send eth transfers (Default 0)
BID_AMOUNT=0.001                            # preconf bid amount (Default 0.001 ETH)
PRIORITY_FEE=1                              # priority fee in wei (Default 1 wei)
BID_AMOUNT_STD_DEV_PERCENTAGE=100           # amount of variation in the preconf bid amount (in %) (Default 100%)
DEFAULT_TIMEOUT=15                          # default context timeout for the program (Default 15 seconds)
APP_NAME=preconf_bidder                     # application name for logging purposes (Default name preconf_bidder)
VERSION=0.8.0                               # mev-commit version for logging purposes (Default version v0.8.0)
```
## Ensure Bidder Node is Running
Ensure that the mev-commit bidder node is running in the background and the autodeposit function deposited ETH into the bidder window. A quickstart can be found [here](https://docs.primev.xyz/get-started/quickstart), which will get the latest mev-commit version and start running it with an auto generated private key. 

## CLI
First build the CLI `go build -o biddercli .`

Then run the CLI `./biddercli`. Flags can be passed to quickstart the process and override default variables, otherwise follow the prompts to get started.  

## Docker
Build the docker with `sudo docker-compose up --build`. Best run with the [dockerized bidder node example](https://github.com/primev/bidder_node_docker)

## Linting
Run linting with `golangci-lint run ./...` inside the repository folder

## Testing
Run `go test -v ./...` in the main folder directory to run all the tests.

