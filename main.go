package main

import (
	"context"
	"fmt"
	"math/big"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	ee "github.com/primev/preconf_blob_bidder/internal/eth"
	bb "github.com/primev/preconf_blob_bidder/internal/mevcommit"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"
)

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

    zerolog.SetGlobalLevel(zerolog.InfoLevel)
    log.Logger = log.Output(os.Stderr).With().Timestamp().Logger()

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
			&cli.UintFlag{
				Name:    "num-blob",
				Usage:   "Number of blobs to send (0 for ETH transfer)",
				EnvVars: []string{"NUM_BLOB"},
				Value:   0,
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
			numBlob := c.Uint64("num-blob")

			// Validate RPC_ENDPOINT if usePayload is false
			if !usePayload && rpcEndpoint == "" {
				return fmt.Errorf("RPC_ENDPOINT is required when USE_PAYLOAD is false")
			}

			log.Info().
				Str("bidderAddress", bidderAddress).
				Str("rpcEndpoint", bb.MaskEndpoint(rpcEndpoint)).
				Str("wsEndpoint", bb.MaskEndpoint(wsEndpoint)).
				Uint64("offset", offset).
				Bool("usePayload", usePayload).
				Float64("bidAmount", bidAmount).
				Float64("stdDevPercentage", stdDevPercentage).
				Uint64("numBlob", numBlob).
				Bool("privateKeyProvided", privateKeyHex != "").
				Msg("Configuration values")

			cfg := bb.BidderConfig{
				ServerAddress: bidderAddress,
				LogFmt:        "json",
				LogLevel:      "info",
			}

			bidderClient, err := bb.NewBidderClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to connect to mev-commit bidder API: %w", err)
			}

			log.Info().Msg("Connected to mev-commit client")

			timeout := 30 * time.Second

			// Only connect to the RPC client if usePayload is false
			var rpcClient *ethclient.Client
			if !usePayload {
				rpcClient = bb.ConnectRPCClientWithRetries(rpcEndpoint, 5, timeout)
				if rpcClient == nil {
					log.Error().
						Str("rpcEndpoint", bb.MaskEndpoint(rpcEndpoint)).
						Msg("Failed to connect to RPC client")
				} else {
					log.Info().
						Str("endpoint", bb.MaskEndpoint(rpcEndpoint)).
						Msg("(rpc) Geth client connected")
				}
			}

			// Connect to WS client
			wsClient, err := bb.ConnectWSClient(wsEndpoint)
			if err != nil {
				return fmt.Errorf("failed to connect to WebSocket client: %w", err)
			}
			log.Info().
				Str("endpoint", bb.MaskEndpoint(wsEndpoint)).
				Msg("(ws) Geth client connected")

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
					log.Warn().
						Err(err).
						Msg("Subscription error")
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
						log.Error().Msg("Transaction was not signed or created.")
					} else {
						log.Info().Msg("Transaction sent successfully")
					}

					// Check for errors before using signedTx
					if err != nil {
						log.Error().
							Err(err).
							Msg("Failed to execute transaction")
					}

					log.Info().
						Uint64("blockNumber", header.Number.Uint64()).
						Uint64("timestamp", header.Time).
						Str("hash", header.Hash().String()).
						Msg("New block received")

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
							log.Error().
								Str("rpcEndpoint", bb.MaskEndpoint(rpcEndpoint)).
								Err(err).
								Msg("Failed to send transaction")
						}
						bb.SendPreconfBid(bidderClient, signedTx.Hash().String(), int64(blockNumber), randomEthAmount)
					}

					// Handle ExecuteBlob error
					if err != nil {
						log.Error().
							Err(err).
							Msg("Failed to execute transaction")
						continue // Skip to the next iteration
					}
				}
			}
		},
	}

	// Run the app
	if err := app.Run(os.Args); err != nil {
		log.Fatal().
			Err(err).
			Msg("Application error")
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
