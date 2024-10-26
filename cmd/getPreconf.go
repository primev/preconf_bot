package main

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/log"
	"github.com/joho/godotenv"
	ee "github.com/primev/preconf_blob_bidder/core/eth"
	bb "github.com/primev/preconf_blob_bidder/core/mevcommit"
	"golang.org/x/exp/rand"
)

var NUM_BLOBS = 6

func main() {
	// Load the .env file
	err := godotenv.Load()
	if err != nil {
		log.Crit("Error loading .env file", "err", err)
	}

	// Set up logging
	glogger := log.NewGlogHandler(log.NewTerminalHandler(os.Stderr, true))
	glogger.Verbosity(log.LevelInfo)
	log.SetDefault(log.NewLogger(glogger))

	// Read configuration from environment variables
	bidderAddress := os.Getenv("BIDDER_ADDRESS")
	if bidderAddress == "" {
		bidderAddress = "mev-commit-bidder:13524"
	}

	usePayloadEnv := os.Getenv("USE_PAYLOAD")
	usePayload := true // Default value
	if usePayloadEnv != "" {
		// Convert usePayloadEnv to bool
		var err error
		usePayload, err = parseBoolEnvVar("USE_PAYLOAD", usePayloadEnv)
		if err != nil {
			log.Crit("Invalid USE_PAYLOAD value", "err", err)
		}
	}

	// Now, load rpcEndpoint conditionally
	var rpcEndpoint string
	if !usePayload {
		rpcEndpoint = os.Getenv("RPC_ENDPOINT")
		if rpcEndpoint == "" {
			log.Crit("RPC_ENDPOINT environment variable is required when USE_PAYLOAD is false")
		}
	}

	wsEndpoint := os.Getenv("WS_ENDPOINT")
	if wsEndpoint == "" {
		log.Crit("WS_ENDPOINT environment variable is required")
	}

	privateKeyHex := os.Getenv("PRIVATE_KEY")
	if privateKeyHex == "" {
		log.Crit("PRIVATE_KEY environment variable is required")
	}

	offsetEnv := os.Getenv("OFFSET")
	var offset uint64 = 1 // Default offset
	if offsetEnv != "" {
		// Convert offsetEnv to uint64
		var err error
		offset, err = parseUintEnvVar("OFFSET", offsetEnv)
		if err != nil {
			log.Crit("Invalid OFFSET value", "err", err)
		}
	}

	// these variables are not required
	ethTransfer := os.Getenv("ETH_TRANSFER")
	blob := os.Getenv("BLOB")

	// Validate that only one of the flags is set
	if ethTransfer == "true" && blob == "true" {
		log.Crit("Only one of --ethtransfer or --blob can be set at a time")
	}

	// Log configuration values (excluding sensitive data)
	log.Info("Configuration values",
    "bidderAddress", bidderAddress,
    "rpcEndpoint", maskEndpoint(rpcEndpoint),
    "wsEndpoint", maskEndpoint(wsEndpoint),
    "offset", offset,
    "usePayload", usePayload,
)

	authAcct, err := bb.AuthenticateAddress(privateKeyHex)
	if err != nil {
		log.Crit("Failed to authenticate private key:", "err", err)
	}

	cfg := bb.BidderConfig{
		ServerAddress: bidderAddress,
		LogFmt:        "json",
		LogLevel:      "info",
	}

	bidderClient, err := bb.NewBidderClient(cfg)
	if err != nil {
		log.Crit("failed to connect to mev-commit bidder API", "err", err)
	}

	log.Info("connected to mev-commit client")

	timeout := 30 * time.Second

	// Only connect to the RPC client if usePayload is false
	if !usePayload {
		// Connect to RPC client
		client := connectRPCClientWithRetries(rpcEndpoint, 5, timeout)
		if client == nil {
			log.Error("failed to connect to RPC client", rpcEndpoint)
		}
		log.Info("(rpc) geth client connected", "endpoint", rpcEndpoint)
	}

	// Connect to WS client
	wsClient, err := connectWSClient(wsEndpoint)
	if err != nil {
		log.Crit("failed to connect to geth client", "err", err)
	}
	log.Info("(ws) geth client connected")

	headers := make(chan *types.Header)
	sub, err := wsClient.SubscribeNewHead(context.Background(), headers)
	if err != nil {
		log.Crit("failed to subscribe to new blocks", "err", err)
	}

	timer := time.NewTimer(24 * 14 * time.Hour)

	for {
		select {
		case <-timer.C:
			log.Info("Stopping the loop.")
			return
		case err := <-sub.Err():
			log.Warn("subscription error", "err", err)
			wsClient, sub = reconnectWSClient(wsEndpoint, headers)
			continue
		case header := <-headers:
			amount := new(big.Int).SetInt64(1e15)
			var signedTx *types.Transaction
			var blockNumber uint64
			if ethTransfer == "true" {
				signedTx, blockNumber, err = ee.SelfETHTransfer(wsClient, authAcct, amount, offset)
			} else if blob == "true" {
				// Execute Blob Transaction
				signedTx, blockNumber, err = ee.ExecuteBlobTransaction(wsClient, authAcct, NUM_BLOBS, offset)
			}

			if signedTx == nil {
				log.Error("Transaction was not signed or created.")
			} else {
				log.Info("Transaction sent successfully")
			}

			// Check for errors before using signedTx
			if err != nil {
				log.Error("failed to execute transaction", "err", err)
			}

			log.Info("new block received",
			"blockNumber", header.Number,
			"timestamp", header.Time,
			"hash", header.Hash().String(),
		)


			if usePayload {
				// If use-payload is true, send the transaction payload to mev-commit. Don't send bundle
				sendPreconfBid(bidderClient, signedTx, int64(blockNumber))
			} else {
				// send as a flashbots bundle and send the preconf bid with the transaction hash
				_, err = ee.SendBundle(rpcEndpoint, signedTx, blockNumber)
				if err != nil {
					log.Error("Failed to send transaction", "rpcEndpoint", rpcEndpoint, "error", err)
				}
				sendPreconfBid(bidderClient, signedTx.Hash().String(), int64(blockNumber))
			}

			// handle ExecuteBlob error
			if err != nil {
				log.Error("failed to execute blob tx", "err", err)
				continue // Skip to the next endpoint
			}
		}
	}
}


func maskEndpoint(rpcEndpoint string) string {
	if len(rpcEndpoint) > 5 {
		return rpcEndpoint[:5] + "*****"
	}
	return "*****"
}

func connectRPCClientWithRetries(rpcEndpoint string, maxRetries int, timeout time.Duration) *ethclient.Client {
	var rpcClient *ethclient.Client
	var err error

	for i := 0; i < maxRetries; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		rpcClient, err = ethclient.DialContext(ctx, rpcEndpoint)
		if err == nil {
			return rpcClient
		}

		log.Warn("failed to connect to RPC client, retrying...", "attempt", i+1, "err", err)
		time.Sleep(10 * time.Duration(math.Pow(2, float64(i)))) // Exponential backoff
	}

	log.Error("failed to connect to RPC client after retries", "err", err)
	return nil
}

func connectWSClient(wsEndpoint string) (*ethclient.Client, error) {
    for {
        wsClient, err := bb.NewGethClient(wsEndpoint)
        if err == nil {
            return wsClient, nil
        }
        log.Warn("failed to connect to websocket client", "err", err)
        time.Sleep(10 * time.Second)
    }
}

func reconnectWSClient(wsEndpoint string, headers chan *types.Header) (*ethclient.Client, ethereum.Subscription) {
	var wsClient *ethclient.Client
	var sub ethereum.Subscription
	var err error

	for i := 0; i < 10; i++ { // Retry logic for WebSocket connection
		wsClient, err = connectWSClient(wsEndpoint)
		if err == nil {
			log.Info("(ws) geth client reconnected")
			sub, err = wsClient.SubscribeNewHead(context.Background(), headers)
			if err == nil {
				return wsClient, sub
			}
		}
		log.Warn("failed to reconnect WebSocket client, retrying...", "attempt", i+1, "err", err)
		time.Sleep(5 * time.Second)
	}
	log.Crit("failed to reconnect WebSocket client after retries", "err", err)
	return nil, nil
}

func sendPreconfBid(bidderClient *bb.Bidder, input interface{}, blockNumber int64) {
	// Seed the random number generator
	rand.Seed(uint64(time.Now().UnixNano()))

	// Generate a random number
	minAmount := 0.0002
	maxAmount := 0.001
	randomEthAmount := minAmount + rand.Float64()*(maxAmount-minAmount)

	// Convert the random ETH amount to wei (1 ETH = 10^18 wei)
	randomWeiAmount := int64(randomEthAmount * 1e18)

	// Convert the amount to a string for the bidder
	amount := fmt.Sprintf("%d", randomWeiAmount)

	// Get current time in milliseconds
	currentTime := time.Now().UnixMilli()

	// Define bid decay start and end
	decayStart := currentTime
	decayEnd := currentTime + int64(time.Duration(36*time.Second).Milliseconds()) // bid decay is 36 seconds (2 blocks)

	// Determine how to handle the input
	var err error
	switch v := input.(type) {
	case string:
		// Input is a string, process it as a transaction hash
		txHash := strings.TrimPrefix(v, "0x")
		log.Info("sending bid with transaction hash", "tx", input)
		// Send the bid with tx hash string
		_, err = bidderClient.SendBid([]string{txHash}, amount, blockNumber, decayStart, decayEnd)

	case *types.Transaction:
		// Input is a transaction object, send the transaction object
		log.Info("sending bid with tx payload", "tx", input.(*types.Transaction).Hash().String())
		// Send the bid with the full transaction object
		_, err = bidderClient.SendBid([]*types.Transaction{v}, amount, blockNumber, decayStart, decayEnd)

	default:
		log.Warn("unsupported input type, must be string or *types.Transaction")
		return
	}

	if err != nil {
		log.Warn("failed to send bid", "err", err)
	} else {
		log.Info("sent preconfirmation bid", "block", blockNumber, "amount (ETH)", randomEthAmount)
	}
}

func parseBoolEnvVar(name, value string) (bool, error) {
	parsedValue, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("environment variable %s must be true or false, got '%s'", name, value)
	}
	return parsedValue, nil
}

func parseUintEnvVar(name, value string) (uint64, error) {
	parsedValue, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("environment variable %s must be a positive integer, got '%s'", name, value)
	}
	return parsedValue, nil
}
