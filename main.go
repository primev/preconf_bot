package main

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/log"
	ee "github.com/primev/preconf_blob_bidder/internal/eth"
	bb "github.com/primev/preconf_blob_bidder/internal/mevcommit"
	"github.com/urfave/cli/v2"
)

const NUM_BLOBS = 6

func main() {
	// Load the .env file before setting up the app
	envFile := os.Getenv("ENV_FILE")
	if envFile == "" {
		envFile = ".env"
	}
	if _, err := os.Stat(envFile); err == nil {
		if err := loadEnvFile(envFile); err != nil {
			fmt.Fprintf(os.Stderr, "Error loading .env file: %v\n", err)
			os.Exit(1)
		}
	}
	
	// Set up logging
	glogger := log.NewGlogHandler(log.NewTerminalHandler(os.Stderr, true))
	glogger.Verbosity(log.LevelInfo)
	log.SetDefault(log.NewLogger(glogger))

	app := &cli.App{
		Name:  "Preconf Bidder",
		Usage: "A tool for bidding in mev-commit preconfirmation auctions for blobs and transactions",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "env",
				Usage:   "Path to .env file",
				EnvVars: []string{"ENV_FILE"},
			},
			&cli.StringFlag{
				Name:    "bidder-address",
				Usage:   "Address of the bidder",
				EnvVars: []string{"BIDDER_ADDRESS"},
				Value:   "mev-commit-bidder:13524",
			},
			&cli.BoolFlag{
				Name:    "use-payload",
				Usage:   "Use payload for transactions",
				EnvVars: []string{"USE_PAYLOAD"},
				Value:   true,
			},
			&cli.StringFlag{
				Name:     "rpc-endpoint",
				Usage:    "RPC endpoint when use-payload is false",
				EnvVars:  []string{"RPC_ENDPOINT"},
				Required: false,
			},
			&cli.StringFlag{
				Name:     "ws-endpoint",
				Usage:    "WebSocket endpoint for transactions",
				EnvVars:  []string{"WS_ENDPOINT"},
				Required: true,
			},
			&cli.StringFlag{
				Name:      "private-key",
				Usage:     "Private key for signing transactions",
				EnvVars:   []string{"PRIVATE_KEY"},
				Required:  true,
				Hidden:    true,
				TakesFile: false,
			},
			&cli.Uint64Flag{
				Name:    "offset",
				Usage:   "Offset value for transactions",
				EnvVars: []string{"OFFSET"},
				Value:   1,
			},
			&cli.Float64Flag{
				Name:    "bid-amount",
				Usage:   "Amount to bid",
				EnvVars: []string{"BID_AMOUNT"},
				Value:   0.001,
			},
			&cli.Float64Flag{
				Name:    "bid-amount-std-dev-percentage",
				Usage:   "Standard deviation percentage for bid amount",
				EnvVars: []string{"BID_AMOUNT_STD_DEV_PERCENTAGE"},
				Value:   100.0,
			},
			&cli.BoolFlag{
				Name:    "eth-transfer",
				Usage:   "Flag for ETH transfer",
				EnvVars: []string{"ETH_TRANSFER"},
			},
			&cli.BoolFlag{
				Name:    "blob",
				Usage:   "Flag for Blob transfer",
				EnvVars: []string{"BLOB"},
			},
		},
		Action: func(c *cli.Context) error {
			// Retrieve flag values
			bidderAddress := c.String("bidder-address")
			usePayload := c.Bool("use-payload")
			rpcEndpoint := c.String("rpc-endpoint")
			wsEndpoint := c.String("ws-endpoint")
			privateKeyHex := c.String("private-key")
			offset := c.Uint64("offset")
			bidAmount := c.Float64("bid-amount")
			stdDevPercentage := c.Float64("bid-amount-std-dev-percentage")
			ethTransfer := c.Bool("eth-transfer")
			blob := c.Bool("blob")

			// Validate that only one of ethTransfer or blob is set
			if ethTransfer && blob {
				return fmt.Errorf("only one of --eth-transfer or --blob can be set at a time")
			}

			// Validate RPC_ENDPOINT if usePayload is false
			if !usePayload && rpcEndpoint == "" {
				return fmt.Errorf("RPC_ENDPOINT is required when USE_PAYLOAD is false")
			}

			// Log configuration values (excluding sensitive data)
			log.Info("Configuration values",
				"bidderAddress", bidderAddress,
				"rpcEndpoint", maskEndpoint(rpcEndpoint),
				"wsEndpoint", maskEndpoint(wsEndpoint),
				"offset", offset,
				"usePayload", usePayload,
				"bidAmount", bidAmount,
				"stdDevPercentage", stdDevPercentage,
			)



			cfg := bb.BidderConfig{
				ServerAddress: bidderAddress,
				LogFmt:        "json",
				LogLevel:      "info",
			}

			bidderClient, err := bb.NewBidderClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to connect to mev-commit bidder API: %w", err)
			}

			log.Info("Connected to mev-commit client")

			timeout := 30 * time.Second

			// Only connect to the RPC client if usePayload is false
			var rpcClient *ethclient.Client
			if !usePayload {
				rpcClient = connectRPCClientWithRetries(rpcEndpoint, 5, timeout)
				if rpcClient == nil {
					log.Error("Failed to connect to RPC client", "rpcEndpoint", maskEndpoint(rpcEndpoint))
				} else {
					log.Info("(rpc) Geth client connected", "endpoint", maskEndpoint(rpcEndpoint))
				}
			}

			// Connect to WS client
			wsClient, err := connectWSClient(wsEndpoint)
			if err != nil {
				return fmt.Errorf("failed to connect to WebSocket client: %w", err)
			}
			log.Info("(ws) Geth client connected", "endpoint", maskEndpoint(wsEndpoint))

			headers := make(chan *types.Header)
			sub, err := wsClient.SubscribeNewHead(context.Background(), headers)
			if err != nil {
				return fmt.Errorf("failed to subscribe to new blocks: %w", err)
			}

			// Authenticate with private key
			authAcct, err := bb.AuthenticateAddress(privateKeyHex, wsClient)
			if err != nil {
				return fmt.Errorf("failed to authenticate private key: %w", err)
			}

			for {
				select {
				case err := <-sub.Err():
					log.Warn("Subscription error", "err", err)
					wsClient, sub = reconnectWSClient(wsEndpoint, headers)
					continue
				case header := <-headers:
					var signedTx *types.Transaction
					var blockNumber uint64
					if ethTransfer {
						amount := new(big.Int).SetInt64(1e15)
						signedTx, blockNumber, err = ee.SelfETHTransfer(wsClient, authAcct, amount, offset)
					} else if blob {
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
						log.Error("Failed to execute transaction", "err", err)
					}

					log.Info("New block received",
						"blockNumber", header.Number,
						"timestamp", header.Time,
						"hash", header.Hash().String(),
					)

					// Compute standard deviation in ETH
					stdDev := bidAmount * stdDevPercentage / 100.0

					// Generate random amount with normal distribution
					randomEthAmount := rand.NormFloat64()*stdDev + bidAmount

					// Ensure the randomEthAmount is positive
					if randomEthAmount <= 0 {
						randomEthAmount = bidAmount // Fallback to bidAmount
					}

					if usePayload {
						// If use-payload is true, send the transaction payload to mev-commit. Don't send bundle
						sendPreconfBid(bidderClient, signedTx, int64(blockNumber), randomEthAmount)
					} else {
						// Send as a flashbots bundle and send the preconf bid with the transaction hash
						_, err = ee.SendBundle(rpcEndpoint, signedTx, blockNumber)
						if err != nil {
							log.Error("Failed to send transaction", "rpcEndpoint", maskEndpoint(rpcEndpoint), "error", err)
						}
						sendPreconfBid(bidderClient, signedTx.Hash().String(), int64(blockNumber), randomEthAmount)
					}

					// Handle ExecuteBlob error
					if err != nil {
						log.Error("Failed to execute blob tx", "err", err)
						continue // Skip to the next iteration
					}
				}
			}
		},
	}

	// Run the app
	if err := app.Run(os.Args); err != nil {
		log.Crit("Application error", "err", err)
	}
}

// maskEndpoint masks sensitive parts of the endpoint URLs
func maskEndpoint(endpoint string) string {
	if len(endpoint) > 10 {
		return endpoint[:10] + "*****"
	}
	return "*****"
}

// connectRPCClientWithRetries attempts to connect to the RPC client with retries and exponential backoff
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

		log.Warn("Failed to connect to RPC client, retrying...",
			"attempt", i+1,
			"err", err,
		)
		time.Sleep(10 * time.Duration(math.Pow(2, float64(i))) * time.Second) // Exponential backoff
	}

	log.Error("Failed to connect to RPC client after retries", "err", err)
	return nil
}

// connectWSClient attempts to connect to the WebSocket client with continuous retries
func connectWSClient(wsEndpoint string) (*ethclient.Client, error) {
	for {
		wsClient, err := bb.NewGethClient(wsEndpoint)
		if err == nil {
			return wsClient, nil
		}
		log.Warn("Failed to connect to WebSocket client", "err", err)
		time.Sleep(10 * time.Second)
	}
}

// reconnectWSClient attempts to reconnect to the WebSocket client with limited retries
func reconnectWSClient(wsEndpoint string, headers chan *types.Header) (*ethclient.Client, ethereum.Subscription) {
	var wsClient *ethclient.Client
	var sub ethereum.Subscription
	var err error

	for i := 0; i < 10; i++ { // Retry logic for WebSocket connection
		wsClient, err = connectWSClient(wsEndpoint)
		if err == nil {
			log.Info("(ws) Geth client reconnected", "endpoint", maskEndpoint(wsEndpoint))
			sub, err = wsClient.SubscribeNewHead(context.Background(), headers)
			if err == nil {
				return wsClient, sub
			}
		}
		log.Warn("Failed to reconnect WebSocket client, retrying...",
			"attempt", i+1,
			"err", err,
		)
		time.Sleep(5 * time.Second)
	}
	log.Crit("Failed to reconnect WebSocket client after retries", "err", err)
	return nil, nil
}

// sendPreconfBid sends a preconfirmation bid to the bidder client
func sendPreconfBid(bidderClient *bb.Bidder, input interface{}, blockNumber int64, randomEthAmount float64) {
	// Get current time in milliseconds
	currentTime := time.Now().UnixMilli()

	// Define bid decay start and end
	decayStart := currentTime
	decayEnd := currentTime + int64(time.Duration(36*time.Second).Milliseconds()) // Bid decay is 36 seconds (2 blocks)

	// Convert the random ETH amount to wei (1 ETH = 10^18 wei)
	bigEthAmount := big.NewFloat(randomEthAmount)
	weiPerEth := big.NewFloat(1e18)
	bigWeiAmount := new(big.Float).Mul(bigEthAmount, weiPerEth)

	// Convert big.Float to big.Int
	randomWeiAmount := new(big.Int)
	bigWeiAmount.Int(randomWeiAmount)

	// Convert the amount to a string for the bidder
	amount := randomWeiAmount.String()

	// Determine how to handle the input
	var err error
	switch v := input.(type) {
	case string:
		// Input is a string, process it as a transaction hash
		txHash := strings.TrimPrefix(v, "0x")
		log.Info("Sending bid with transaction hash", "tx", txHash)
		// Send the bid with tx hash string
		_, err = bidderClient.SendBid([]string{txHash}, amount, blockNumber, decayStart, decayEnd)

	case *types.Transaction:
		// Input is a transaction object, send the transaction object
		log.Info("Sending bid with transaction payload", "tx", v.Hash().String())
		// Send the bid with the full transaction object
		_, err = bidderClient.SendBid([]*types.Transaction{v}, amount, blockNumber, decayStart, decayEnd)

	default:
		log.Warn("Unsupported input type, must be string or *types.Transaction")
		return
	}

	if err != nil {
		log.Warn("Failed to send bid", "err", err)
	} else {
		log.Info("Sent preconfirmation bid",
			"block", blockNumber,
			"amount (ETH)", randomEthAmount,
		)
	}
}

// loadEnvFile loads the specified .env file into the environment variables
func loadEnvFile(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		// Ignore comments and empty lines
		trimmed := strings.TrimSpace(line)
		if len(trimmed) == 0 || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// Split key and value
		parts := strings.SplitN(trimmed, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		os.Setenv(key, value)
	}
	return nil
}
