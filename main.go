package main

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"math/big"
	"math/rand"
	"net/url"
	"os"
	"strings"
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

func promptForInput(prompt string) string {
	fmt.Printf("%s: ", prompt)
	var input string
	if _, err := fmt.Scanln(&input); err != nil {
		slog.Warn("Error reading input", "error", err)
	}
	return input
}

func validateWebSocketURL(input string) (string, error) {
	if input == "" {
		return "", fmt.Errorf("endpoint cannot be empty")
	}

	// If no scheme is provided, default to "ws://"
	if !strings.Contains(input, "://") {
		input = "ws://" + input
	}

	// Parse the URL to ensure it's valid
	parsedURL, err := url.Parse(input)
	if err != nil {
		return "", fmt.Errorf("invalid URL format: %v", err)
	}

	// Ensure the scheme is valid for WebSocket connections
	if parsedURL.Scheme != "ws" && parsedURL.Scheme != "wss" {
		return "", fmt.Errorf("invalid scheme: %s (only ws:// or wss:// are supported)", parsedURL.Scheme)
	}

	if parsedURL.Host == "" {
		return "", fmt.Errorf("URL must include a host")
	}

	return parsedURL.String(), nil
}

func validatePrivateKey(input string) error {
	if len(input) != 64 {
		return fmt.Errorf("private key must be 64 hex characters")
	}
	return nil
}

func main() {
	// Initialize the slog logger with JSON handler and set log level to Info
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level:     slog.LevelInfo,
		AddSource: true,
	}))
	slog.SetDefault(logger)

	app := &cli.App{
		Name:  "Preconf Bidder",
		Usage: "A tool for bidding in mev-commit preconfirmation auctions for blobs and transactions.",
		Action: func(c *cli.Context) error {
			fmt.Println("-----------------------------------------------------------------------------------------------")
			fmt.Println("Welcome to Preconf Bidder!")
			fmt.Println("")
			fmt.Println("This is a quickstart tool to make preconf bids on mev-commit chain.")
			fmt.Println("")
			fmt.Println("If you already know what you're doing, you can skip the prompts by providing flags upfront.")
			fmt.Println("For example:")
			fmt.Println("  ./biddercli --private-key <your_64_char_hex_key> --ws-endpoint wss://your-node.com/ws")
			fmt.Println("")
			fmt.Println("Available flags include:")
			fmt.Println("  --private-key        Your private key for signing transactions (64 hex chars)")
			fmt.Println("  --ws-endpoint        The WebSocket endpoint for your Ethereum node")
			fmt.Println("  --rpc-endpoint       The RPC endpoint if not using payload")
			fmt.Println("  --bid-amount         The amount to bid (in ETH), default 0.001")
			fmt.Println("  --bid-amount-std-dev-percentage  Std dev percentage of bid amount, default 100.0")
			fmt.Println("  --num-blob           Number of blob transactions to send, default 0 makes the tx an eth transfer")
			fmt.Println("  --default-timeout     Default timeout in seconds, default 15")
			fmt.Println("")
			fmt.Println("You can also set environment variables like WS_ENDPOINT and PRIVATE_KEY.")
			fmt.Println("For more details, check the documentation: https://docs.primev.xyz/get-started/bidders/best-practices")
			fmt.Println("-----------------------------------------------------------------------------------------------")
			fmt.Println()
			

			// Start by trying to read values from flags/env
			wsEndpoint := c.String(FlagWsEndpoint)
			privateKeyHex := c.String(FlagPrivateKey)

			// If wsEndpoint is missing, prompt interactively
			if wsEndpoint == "" {
				fmt.Println("First, we need the WebSocket endpoint for your Ethereum node.")
				fmt.Println("This is where we'll connect to receive real-time blockchain updates.")
				fmt.Println("For example: wss://your-node-provider.com/ws")
				fmt.Println()
				var err error
				for {
					wsEndpoint = promptForInput("Please enter your WebSocket endpoint")
					wsEndpoint, err = validateWebSocketURL(wsEndpoint)
					if err == nil {
						break
					}
					fmt.Printf("Error: %s\nPlease try again.\n\n", err)
				}
				fmt.Println() // Add a blank line after successful input
			}

			// If privateKeyHex is missing, prompt interactively
			if privateKeyHex == "" {
				fmt.Println("Next, we need your private key to sign transactions.")
				fmt.Println("Your private key is a 64-character hexadecimal string.")
				fmt.Println("Make sure this is your own secure key. (We will not share it.)")
				fmt.Println()
				var err error
				for {
					privateKeyHex = promptForInput("Please enter your private key")
					err = validatePrivateKey(privateKeyHex)
					if err == nil {
						break
					}
					fmt.Printf("Error: %s\nPlease try again.\n\n", err)
				}
				fmt.Println() // Add a blank line after successful input
			}

			// Get other parameters from flags or environment
			bidderAddress := c.String(FlagBidderAddress)
			usePayload := c.Bool(FlagUsePayload)
			rpcEndpoint := c.String(FlagRpcEndpoint)
			offset := c.Uint64(FlagOffset)
			bidAmount := c.Float64(FlagBidAmount)
			stdDevPercentage := c.Float64(FlagBidAmountStdDevPercentage)
			numBlob := c.Uint(FlagNumBlob)
			defaultTimeoutSeconds := c.Uint(FlagDefaultTimeout)
			defaultTimeout := time.Duration(defaultTimeoutSeconds) * time.Second

			// Print a summary to the user before proceeding
			fmt.Println("Great! Here's what we have:")
			fmt.Printf(" - WebSocket Endpoint: %s\n", wsEndpoint)
			fmt.Printf(" - Private Key: Provided (hidden)\n")
			fmt.Printf(" - Bidder Address: %s\n", bidderAddress)
			fmt.Printf(" - Use Payload: %v\n", usePayload)
			fmt.Printf(" - Bid Amount: %f ETH\n", bidAmount)
			fmt.Printf(" - Standard Deviation: %f%%\n", stdDevPercentage)
			fmt.Printf(" - Number of Blobs: %d\n", numBlob)
			fmt.Printf(" - Default Timeout: %d seconds\n", defaultTimeoutSeconds)
			fmt.Println()
			fmt.Println("We will now connect to the blockchain and start sending transactions.")
			fmt.Println("Please wait...")
			fmt.Println()

			// Log configuration values (for debugging / dev)
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

			timeout := defaultTimeout

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
						amount := big.NewInt(1e15)
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
				Name:    FlagBidderAddress,
				Usage:   "Address of the bidder",
				EnvVars: []string{"BIDDER_ADDRESS"},
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
				Value:   15,
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		slog.Error("Application error", "error", err)
		os.Exit(1)
	}
}
