## About
This repository provides a user friendly CLI interface for making preconfirmation bids from mev-commit for ETH transfers or blob transactions. Transactions are sent directly to the builder as transaction payloads. Currently a fixed priority fee is used alongside a preconf bid amount.

If you’re an advanced user, you can still skip the interactive mode by specifying all configurations via environment variables, .env files, or command-line flags.


## Requirements
- funded holesky address
- funded mev-commit address
- mev-commit p2p bidder node (v0.7)
- a good websocket endpoint (a publicly available one is set as default, but cannot handle high throughput)

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
USE_PAYLOAD=true                            # sends tx payload direclty to providers.
BIDDER_ADDRESS="localhost:13524"
OFFSET=1                                    # of blocks in the future to ask for the preconf bid
NUM_BLOB=0                                  # blob count of 0 will just send eth transfers
BID_AMOUNT=0.001                            # preconf bid amount
BID_AMOUNT_STD_DEV_PERCENTAGE=100           # amount of variation in the preconf bid amount (in %)
DEFAULT_TIMEOUT=15
```
## How to run
Ensure that the mev-commit bidder node is running in the background. A quickstart can be found [here](https://docs.primev.xyz/get-started/quickstart), which will get the latest mev-commit version and start running it with an auto generated private key. 

## Docker
Build the docker with `sudo docker-compose up --build`. Best run with the unofficial [dockerized bidder node example](https://github.com/primev/bidder_node_docker)

## Linting
Run linting with `golangci-lint run ./...` inside the repository folder

## Testing
Run `go test -v ./...` in the main folder directory to run all the tests.

## CLI
First build the CLI `go build -o biddercli .`

Then run the CLI `./biddercli`. Flags can be passed to quickstart the process and override default variables, otherwise follow the prompts to get started.  