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
	"github.com/ethereum/go-ethereum/ethclient"
	ee "github.com/primev/preconf_blob_bidder/internal/eth"
	bb "github.com/primev/preconf_blob_bidder/internal/mevcommit"
	"github.com/urfave/cli/v2"
)

const (
	FlagEnv                       = "env"
	FlagServerAddress             = "server-address"
	FlagUsePayload                = "use-payload"
	FlagRpcEndpoint               = "rpc-endpoint"
	FlagWsEndpoint                = "ws-endpoint"
	FlagPrivateKey                = "private-key"
	FlagOffset                    = "offset"
	FlagBidAmount                 = "bid-amount"
	FlagBidAmountStdDevPercentage = "bid-amount-std-dev-percentage"
	FlagNumBlob                   = "num-blob"
	FlagDefaultTimeout            = "default-timeout"
	FlagRunDurationMinutes        = "run-duration-minutes"

	FlagAppName = "app-name"
	FlagVersion = "version"

	FlagPriorityFee = "priority-fee"
)

func main() {
	app := &cli.App{
		Name:  "Preconf Bidder",
		Usage: "A tool for bidding in mev-commit preconfirmation auctions for blobs and eth transfers.",
		Action: func(c *cli.Context) error {
			appName := c.String(FlagAppName)
			version := c.String(FlagVersion)
			serverAddress := c.String(FlagServerAddress)
			usePayload := c.Bool(FlagUsePayload)
			rpcEndpoint := c.String(FlagRpcEndpoint)
			wsEndpoint := c.String(FlagWsEndpoint)
			privateKeyHex := c.String(FlagPrivateKey)
			offset := c.Uint64(FlagOffset)
			bidAmount := c.Float64(FlagBidAmount)
			priorityFee := c.Uint64(FlagPriorityFee)
			stdDevPercentage := c.Float64(FlagBidAmountStdDevPercentage)
			numBlob := c.Uint(FlagNumBlob)
			defaultTimeoutSeconds := c.Uint(FlagDefaultTimeout)
			runDurationMinutes := c.Uint(FlagRunDurationMinutes)

			defaultTimeout := time.Duration(defaultTimeoutSeconds) * time.Second
			var endTime time.Time
			if runDurationMinutes > 0 {
				endTime = time.Now().Add(time.Duration(runDurationMinutes) * time.Minute)
				slog.Info("Bidder will run until", "endTime", endTime)
			} else {
				slog.Info("Bidder will run indefinitely")
			}

			if runDurationMinutes > 0 {
				fmt.Printf(" - Run Duration: %d minutes\n", runDurationMinutes)
			} else {
				fmt.Printf(" - Run Duration: infinite\n")
			}

			slog.Info("Configuration values",
				"appName", appName,
				"version", version,
				"serverAddress", serverAddress,
				"rpcEndpoint", bb.MaskEndpoint(rpcEndpoint),
				"wsEndpoint", bb.MaskEndpoint(wsEndpoint),
				"offset", offset,
				"usePayload", usePayload,
				"bidAmount", bidAmount,
				"priorityFee", priorityFee,
				"stdDevPercentage", stdDevPercentage,
				"numBlob", numBlob,
				"privateKeyProvided", privateKeyHex != "",
				"defaultTimeoutSeconds", defaultTimeoutSeconds,
			)

			cfg := bb.BidderConfig{
				ServerAddress: serverAddress,
			}

			bidderClient, err := bb.NewBidderClient(cfg)
			if err != nil {
				slog.Error("Failed to connect to mev-commit bidder API", "error", err)
				return fmt.Errorf("failed to connect to mev-commit bidder API: %w", err)
			}

			slog.Info("Connected to mev-commit client")

			timeout := defaultTimeout

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

			if privateKeyHex == "" {
				slog.Error("Private key is required")
				return fmt.Errorf("private key is required")
			}

			authAcct, err := bb.AuthenticateAddress(privateKeyHex, wsClient)
			if err != nil {
				slog.Error("Failed to authenticate private key", "error", err)
				return fmt.Errorf("failed to authenticate private key: %w", err)
			}

			for {
				if runDurationMinutes > 0 && time.Now().After(endTime) {
					slog.Info("Run duration reached, shutting down")
					return nil
				}

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
						amount := big.NewInt(1e9)
						signedTx, blockNumber, err = ee.SelfETHTransfer(wsClient, authAcct, amount, offset, big.NewInt(int64(priorityFee)))
					} else {
						// Execute Blob Transaction
						signedTx, blockNumber, err = ee.ExecuteBlobTransaction(wsClient, authAcct, int(numBlob), offset, big.NewInt(int64(priorityFee)))
					}

					if signedTx == nil {
						slog.Error("Transaction was not signed or created.")
					} else {
						slog.Info("Transaction sent successfully")
					}

					if err != nil {
						slog.Error("Failed to execute transaction", "error", err)
					}

					slog.Info("New block received",
						"blockNumber", header.Number.Uint64(),
						"timestamp", header.Time,
						"hash", header.Hash().String(),
					)

					stdDev := bidAmount * stdDevPercentage / 100.0
					randomEthAmount := rand.NormFloat64()*stdDev + bidAmount
					randomEthAmount = math.Max(randomEthAmount, bidAmount)

					if usePayload {
						bb.SendPreconfBid(bidderClient, signedTx, int64(blockNumber), randomEthAmount)
					} else {
						_, err = ee.SendBundle(rpcEndpoint, signedTx, blockNumber)
						if err != nil {
							slog.Error("Failed to send transaction",
								"rpcEndpoint", bb.MaskEndpoint(rpcEndpoint),
								"error", err,
							)
						}
						bb.SendPreconfBid(bidderClient, signedTx.Hash().String(), int64(blockNumber), randomEthAmount)
					}

					if err != nil {
						slog.Error("Failed to execute transaction", "error", err)
						continue
					}
				}
			}
		},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    FlagEnv,
				Usage:   "Path to .env file",
				EnvVars: []string{"ENV_FILE"},
			},
			&cli.StringFlag{
				Name:    FlagServerAddress,
				Usage:   "Address of the server",
				EnvVars: []string{"SERVER_ADDRESS"},
				Value:   "localhost:13524",
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
				Value:    "https://ethereum-holesky-rpc.publicnode.com",
			},
			&cli.StringFlag{
				Name:     FlagWsEndpoint,
				Usage:    "WebSocket endpoint for transactions",
				EnvVars:  []string{"WS_ENDPOINT"},
				Value:    "wss://ethereum-holesky-rpc.publicnode.com",
				Required: false,
			},
			&cli.StringFlag{
				Name:      FlagPrivateKey,
				Usage:     "Private key for signing transactions",
				EnvVars:   []string{"PRIVATE_KEY"},
				Required:  false,
				Hidden:    true,
				TakesFile: false,
			},
			&cli.Uint64Flag{
				Name:    FlagOffset,
				Usage:   "Offset is how many blocks ahead to bid for the preconf transaction",
				EnvVars: []string{"OFFSET"},
				Value:   1,
			},
			&cli.Float64Flag{
				Name:    FlagBidAmount,
				Usage:   "Amount to bid (in ETH)",
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
				Value:   15,
			},
			&cli.UintFlag{
				Name:    FlagRunDurationMinutes,
				Usage:   "Duration to run the bidder in minutes (0 to run indefinitely)",
				EnvVars: []string{"RUN_DURATION_MINUTES"},
				Value:   0,
			},
			&cli.StringFlag{
				Name:    FlagAppName,
				Usage:   "Application name, for logging purposes",
				EnvVars: []string{"APP_NAME"},
				Value:   "preconf_bidder",
			},
			&cli.StringFlag{
				Name:    FlagVersion,
				Usage:   "mev-commit version, for logging purposes",
				EnvVars: []string{"VERSION"},
				Value:   "0.8.0",
			},
			&cli.Uint64Flag{
				Name:    FlagPriorityFee,
				Usage:   "Priority fee in wei",
				EnvVars: []string{"PRIORITY_FEE"},
				Value:   1,
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		slog.Error("Application error", "error", err)
		os.Exit(1)
	}
}
