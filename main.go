package main

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"math/big"
	"math/rand"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/joho/godotenv"
	"github.com/primev/preconf_blob_bidder/internal/service"
	"github.com/urfave/cli/v2"
)

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
	// Load environment variables from .env file if it exists
	err := godotenv.Load()
	if err != nil {
		slog.Info("No .env file found or failed to load .env file. Continuing with existing environment variables.")
	}

	// Initialize the slog logger with JSON handler and set log level to Info
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level:     slog.LevelInfo,
		AddSource: true,
	}))
	slog.SetDefault(logger)

	app := &cli.App{
		Name:  "Preconf Bidder",
		Usage: "A tool for bidding in mev-commit preconfirmation auctions for blobs and transactions",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     FlagEnv,
				Usage:    "Environment (e.g., development, production)",
				EnvVars:  []string{"ENV"},
				Required: false, // Make it optional if not strictly required
			},
			&cli.StringFlag{
				Name:     FlagBidderAddress,
				Usage:    "Address of the mev-commit bidder",
				EnvVars:  []string{"BIDDER_ADDRESS"},
				Required: true,
			},
			&cli.BoolFlag{
				Name:     FlagUsePayload,
				Usage:    "Use payload instead of transaction hash",
				EnvVars:  []string{"USE_PAYLOAD"},
				Required: false,
			},
			&cli.StringFlag{
				Name:     FlagRpcEndpoint,
				Usage:    "RPC endpoint",
				EnvVars:  []string{"RPC_ENDPOINT"},
				Required: true,
			},
			&cli.StringFlag{
				Name:     FlagWsEndpoint,
				Usage:    "WebSocket endpoint",
				EnvVars:  []string{"WS_ENDPOINT"},
				Required: true,
			},
			&cli.StringFlag{
				Name:     FlagPrivateKey,
				Usage:    "Hex-encoded private key",
				EnvVars:  []string{"PRIVATE_KEY"},
				Required: true,
			},
			&cli.Uint64Flag{
				Name:     FlagOffset,
				Usage:    "Block number offset",
				EnvVars:  []string{"OFFSET"},
				Value:    1, // Default value if not set in .env
				Required: false,
			},
			&cli.Float64Flag{
				Name:     FlagBidAmount,
				Usage:    "Bid amount in ETH",
				EnvVars:  []string{"BID_AMOUNT"},
				Value:    0.01, // Default value if not set in .env
				Required: false,
			},
			&cli.Float64Flag{
				Name:     FlagBidAmountStdDevPercentage,
				Usage:    "Standard deviation percentage for bid amount",
				EnvVars:  []string{"BID_AMOUNT_STD_DEV_PERCENTAGE"},
				Value:    5.0, // Default value if not set in .env
				Required: false,
			},
			&cli.UintFlag{
				Name:     FlagNumBlob,
				Usage:    "Number of blobs to include in the transaction",
				EnvVars:  []string{"NUM_BLOB"},
				Value:    0, // Default value if not set in .env
				Required: false,
			},
			&cli.UintFlag{
				Name:     FlagDefaultTimeout,
				Usage:    "Default timeout in seconds",
				EnvVars:  []string{"DEFAULT_TIMEOUT"},
				Value:    30, // Default value if not set in .env
				Required: false,
			},
		},
		Action: func(c *cli.Context) error {
			// Parse command-line arguments
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

			// Initialize the Service with functional options
			svc, err := service.NewService(
				service.WithDefaultTimeout(defaultTimeout),
				service.WithRPCURL(rpcEndpoint),
				service.WithLogger(logger),
			)
			if err != nil {
				slog.Error("Failed to initialize service", "error", err)
				return err
			}

			// Log configuration values
			svc.Logger.Info("Configuration values",
				"bidderAddress", bidderAddress,
				"rpcEndpoint", svc.MaskEndpoint(rpcEndpoint),
				"wsEndpoint", svc.MaskEndpoint(wsEndpoint),
				"offset", offset,
				"usePayload", usePayload,
				"bidAmount", bidAmount,
				"stdDevPercentage", stdDevPercentage,
				"numBlob", numBlob,
				"privateKeyProvided", privateKeyHex != "",
				"defaultTimeoutSeconds", defaultTimeoutSeconds,
			)

			// Connect to RPC Client if not using payload
			if !usePayload {
				err = svc.ConnectRPCClientWithRetries(rpcEndpoint, 5)
				if err != nil {
					svc.Logger.Error("Failed to connect to RPC client", "rpcEndpoint", svc.MaskEndpoint(rpcEndpoint), "error", err)
					return err
				}
				svc.Logger.Info("Geth client connected (rpc)",
					"endpoint", svc.MaskEndpoint(rpcEndpoint),
				)
			}

			// Connect to WebSocket Client
			err = svc.ConnectWSClient(wsEndpoint)
			if err != nil {
				svc.Logger.Error("Failed to connect to WebSocket client", "error", err)
				return fmt.Errorf("failed to connect to WebSocket client: %w", err)
			}
			svc.Logger.Info("Geth client connected (ws)",
				"endpoint", svc.MaskEndpoint(wsEndpoint),
			)

			// Authenticate the private key
			err = svc.AuthenticateAddress(privateKeyHex)
			if err != nil {
				svc.Logger.Error("Failed to authenticate private key", "error", err)
				return fmt.Errorf("failed to authenticate private key: %w", err)
			}

			cfg := service.BidderConfig{
				ServerAddress: bidderAddress,
			}

			bidderClient, err := service.NewBidderClient(cfg)
			if err != nil {
				slog.Error("Failed to connect to mev-commit bidder API", "error", err)
				return fmt.Errorf("failed to connect to mev-commit bidder API: %w", err)
			}

			slog.Info("Connected to mev-commit client")

			// Subscribe to new headers
			headers := make(chan *types.Header)
			sub, err := svc.Client.SubscribeNewHead(context.Background(), headers)
			if err != nil {
				svc.Logger.Error("Failed to subscribe to new blocks", "error", err)
				return fmt.Errorf("failed to subscribe to new blocks: %w", err)
			}

			// Main event loop
			for {
				select {
				case err := <-sub.Err():
					if err != nil {
						svc.Logger.Error("Subscription error", "error", err)
					}
				case header := <-headers:
					var signedTx *types.Transaction
					var blockNumber uint64
					var err error

					if numBlob == 0 {
						amount := big.NewInt(1e15) // Example amount; adjust as needed
						signedTx, blockNumber, err = svc.SelfETHTransfer(amount, offset)
					} else {
						signedTx, blockNumber, err = svc.ExecuteBlobTransaction(int(numBlob), offset)
					}

					if err != nil {
						svc.Logger.Error("Failed to execute transaction", "error", err)
						continue
					}

					if signedTx == nil {
						svc.Logger.Error("Transaction was not signed or created.")
					} else {
						svc.Logger.Info("Transaction created successfully",
							"tx_hash", signedTx.Hash().Hex(),
						)
					}

					svc.Logger.Info("New block received",
						"blockNumber", header.Number.Uint64(),
						"timestamp", header.Time,
						"hash", header.Hash().String(),
					)

					// Compute standard deviation in ETH
					stdDev := bidAmount * stdDevPercentage / 100.0

					// Generate random amount with normal distribution
					randomEthAmount := math.Max(rand.NormFloat64()*stdDev+bidAmount, bidAmount)

					if usePayload {
						bidderClient.SendPreconfBid(bidderClient, signedTx, int64(blockNumber), randomEthAmount)
						if err != nil {
							svc.Logger.Error("Failed to send preconfirmation bid", "error", err)
						}
					} else {
						_, err = svc.SendBundle(signedTx, blockNumber)
						if err != nil {
							svc.Logger.Error("Failed to send transaction", "rpcEndpoint", svc.MaskEndpoint(rpcEndpoint), "error", err)
						}
						bidderClient.SendPreconfBid(bidderClient, signedTx.Hash().String(), int64(blockNumber), randomEthAmount)
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
