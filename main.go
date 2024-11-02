package main

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"math/rand"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	ee "github.com/primev/preconf_blob_bidder/internal/eth"
	bb "github.com/primev/preconf_blob_bidder/internal/mevcommit"
	"github.com/urfave/cli/v2"
)

// Define flag names as constants
const (
	FlagEnv                       = "env"
	FlagBidderAddress             = "bidder-address"
	FlagUsePayload                = "use-payload"
	FlagRpcEndpoint               = "rpc-endpoint"
	FlagWsEndpoint                = "ws-endpoint"
	FlagPrivateKey                = "private-key"
	FlagOffset                    = "offset"
	FlagBidAmount                 = "bid-amount"
	FlagBidAmountStdDevPercentage = "bid-amount-std-dev-percentage"
	FlagNumBlob                   = "num-blob"
	FlagDefaultTimeout            = "default-timeout"
)

func main() {
	// Initialize the slog logger with JSON handler and set log level to Info
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level:      slog.LevelInfo,
		AddSource:  true,
	}))
	slog.SetDefault(logger)

	app := &cli.App{
		Name:  "Preconf Bidder",
		Usage: "A tool for bidding in mev-commit preconfirmation auctions for blobs and transactions",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    FlagEnv,
				Usage:   "Path to .env file",
				EnvVars: []string{"ENV_FILE"},
			},
			&cli.StringFlag{
				Name:    FlagBidderAddress,
				Usage:   "Address of the bidder",
				EnvVars: []string{"BIDDER_ADDRESS"},
				Value:   "mev-commit-bidder:13524",
			},
			&cli.BoolFlag{
				Name:    FlagUsePayload,
				Usage:   "Use payload for transactions",
				EnvVars: []string{"USE_PAYLOAD"},
				Value:   true,
			},
			&cli.StringFlag{
				Name:     FlagRpcEndpoint,
				Usage:    "RPC endpoint when use-payload is false",
				EnvVars:  []string{"RPC_ENDPOINT"},
				Required: false,
			},
			&cli.StringFlag{
				Name:     FlagWsEndpoint,
				Usage:    "WebSocket endpoint for transactions",
				EnvVars:  []string{"WS_ENDPOINT"},
				Required: true,
			},
			&cli.StringFlag{
				Name:      FlagPrivateKey,
				Usage:     "Private key for signing transactions",
				EnvVars:   []string{"PRIVATE_KEY"},
				Required:  true,
				Hidden:    true,
				TakesFile: false,
			},
			&cli.Uint64Flag{
				Name:    FlagOffset,
				Usage:   "Offset value for transactions",
				EnvVars: []string{"OFFSET"},
				Value:   1,
			},
			&cli.Float64Flag{
				Name:    FlagBidAmount,
				Usage:   "Amount to bid",
				EnvVars: []string{"BID_AMOUNT"},
				Value:   0.001,
			},
			&cli.Float64Flag{
				Name:    FlagBidAmountStdDevPercentage,
				Usage:   "Standard deviation percentage for bid amount",
				EnvVars: []string{"BID_AMOUNT_STD_DEV_PERCENTAGE"},
				Value:   100.0,
			},
			&cli.UintFlag{
				Name:    FlagNumBlob,
				Usage:   "Number of blobs to send (0 for ETH transfer)",
				EnvVars: []string{"NUM_BLOB"},
				Value:   0,
			},
			&cli.UintFlag{
				Name:    FlagDefaultTimeout,
				Usage:   "Default timeout in seconds",
				EnvVars: []string{"DEFAULT_TIMEOUT"},
				Value:   15, // Default to 15 seconds
			},
		},
		Action: func(c *cli.Context) error {
			// Retrieve flag values using constants
			bidderAddress := c.String(FlagBidderAddress)
			usePayload := c.Bool(FlagUsePayload)
			rpcEndpoint := c.String(FlagRpcEndpoint)
			wsEndpoint := c.String(FlagWsEndpoint)
			privateKeyHex := c.String(FlagPrivateKey)
			offset := c.Uint64(FlagOffset)
			bidAmount := c.Float64(FlagBidAmount)
			stdDevPercentage := c.Float64(FlagBidAmountStdDevPercentage)
			numBlob := c.Uint(FlagNumBlob)
			defaultTimeoutSeconds := c.Uint(FlagDefaultTimeout)
			defaultTimeout := time.Duration(defaultTimeoutSeconds) * time.Second

			// Log the defaultTimeout value
			slog.Info("Configuration values",
				"bidderAddress", bidderAddress,
				"rpcEndpoint", bb.MaskEndpoint(rpcEndpoint),
				"wsEndpoint", bb.MaskEndpoint(wsEndpoint),
				"offset", offset,
				"usePayload", usePayload,
				"bidAmount", bidAmount,
				"stdDevPercentage", stdDevPercentage,
				"numBlob", numBlob,
				"privateKeyProvided", privateKeyHex != "",
				"defaultTimeoutSeconds", defaultTimeoutSeconds,
			)

			cfg := bb.BidderConfig{
				ServerAddress: bidderAddress,
			}

			bidderClient, err := bb.NewBidderClient(cfg)
			if err != nil {
				slog.Error("Failed to connect to mev-commit bidder API", "error", err)
				return fmt.Errorf("failed to connect to mev-commit bidder API: %w", err)
			}

			slog.Info("Connected to mev-commit client")

			timeout := defaultTimeout // Use the defaultTimeout here

			// Only connect to the RPC client if usePayload is false
			var rpcClient *ethclient.Client
			if !usePayload {
				rpcClient = bb.ConnectRPCClientWithRetries(rpcEndpoint, 5, timeout)
				if rpcClient == nil {
					slog.Error("Failed to connect to RPC client", "rpcEndpoint", bb.MaskEndpoint(rpcEndpoint))
				} else {
					slog.Info("Geth client connected (rpc)",
						"endpoint", bb.MaskEndpoint(rpcEndpoint),
					)
				}
			}

			// Connect to WS client
			wsClient, err := bb.ConnectWSClient(wsEndpoint)
			if err != nil {
				slog.Error("Failed to connect to WebSocket client", "error", err)
				return fmt.Errorf("failed to connect to WebSocket client: %w", err)
			}
			slog.Info("Geth client connected (ws)",
				"endpoint", bb.MaskEndpoint(wsEndpoint),
			)

			headers := make(chan *types.Header)
			sub, err := wsClient.SubscribeNewHead(context.Background(), headers)
			if err != nil {
				slog.Error("Failed to subscribe to new blocks", "error", err)
				return fmt.Errorf("failed to subscribe to new blocks: %w", err)
			}

			// Authenticate with private key
			authAcct, err := bb.AuthenticateAddress(privateKeyHex, wsClient)
			if err != nil {
				slog.Error("Failed to authenticate private key", "error", err)
				return fmt.Errorf("failed to authenticate private key: %w", err)
			}

			for {
				select {
				case err := <-sub.Err():
					slog.Warn("Subscription error", "error", err)
					wsClient, sub = bb.ReconnectWSClient(wsEndpoint, headers)
					continue
				case header := <-headers:
					var signedTx *types.Transaction
					var blockNumber uint64
					if numBlob == 0 {
						// Perform ETH Transfer
						amount := new(big.Int).SetInt64(1e15)
						signedTx, blockNumber, err = ee.SelfETHTransfer(wsClient, authAcct, amount, offset)
					} else {
						// Execute Blob Transaction
						signedTx, blockNumber, err = ee.ExecuteBlobTransaction(wsClient, authAcct, int(numBlob), offset)
					}

					if signedTx == nil {
						slog.Error("Transaction was not signed or created.")
					} else {
						slog.Info("Transaction sent successfully")
					}

					// Check for errors before using signedTx
					if err != nil {
						slog.Error("Failed to execute transaction", "error", err)
					}

					slog.Info("New block received",
						"blockNumber", header.Number.Uint64(),
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
						if numBlob == 0 {
							bb.SendPreconfBid(bidderClient, signedTx, int64(blockNumber), randomEthAmount)
						} else {
							bb.SendPreconfBid(bidderClient, signedTx, int64(blockNumber), randomEthAmount)
						}
					} else {
						// Send as a flashbots bundle and send the preconf bid with the transaction hash
						_, err = ee.SendBundle(rpcEndpoint, signedTx, blockNumber)
						if err != nil {
							slog.Error("Failed to send transaction",
								"rpcEndpoint", bb.MaskEndpoint(rpcEndpoint),
								"error", err,
							)
						}
						bb.SendPreconfBid(bidderClient, signedTx.Hash().String(), int64(blockNumber), randomEthAmount)
					}

					// Handle ExecuteBlob error
					if err != nil {
						slog.Error("Failed to execute transaction", "error", err)
						continue // Skip to the next iteration
					}
				}
			}
		},
	}

	// Run the app
	if err := app.Run(os.Args); err != nil {
		slog.Error("Application error", "error", err)
		os.Exit(1)
	}
}
