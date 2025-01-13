package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/big"
	"math/rand"
	"net/url"
	"os"
	"strconv"
	"strings"
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

	// New flags for AppName and Version
	FlagAppName = "app-name"
	FlagVersion = "version"

	FlagPriorityFee = "priority-fee"
)

// promptForInput prompts the user for input and returns the entered string
func promptForInput(prompt string) string {
	fmt.Printf("%s: ", prompt)
	var input string
	if _, err := fmt.Scanln(&input); err != nil {
		slog.Warn("Error reading input", "error", err)
	}
	return input
}

// validateWebSocketURL validates and formats the WebSocket URL
func validateWebSocketURL(input string) (string, error) {
	if input == "" {
		return "", fmt.Errorf("endpoint cannot be empty")
	}

	if !strings.Contains(input, "://") {
		input = "ws://" + input
	}

	parsedURL, err := url.Parse(input)
	if err != nil {
		return "", fmt.Errorf("invalid URL format: %v", err)
	}

	if parsedURL.Scheme != "ws" && parsedURL.Scheme != "wss" {
		return "", fmt.Errorf("invalid scheme: %s (only ws:// or wss:// are supported)", parsedURL.Scheme)
	}

	if parsedURL.Host == "" {
		return "", fmt.Errorf("URL must include a host")
	}

	return parsedURL.String(), nil
}

// validatePrivateKey ensures the private key is a 64-character hexadecimal string
func validatePrivateKey(input string) error {
	if len(input) != 64 {
		return fmt.Errorf("private key must be 64 hex characters")
	}
	return nil
}

func getOrDefault(c *cli.Context, flagName, envVar, defaultValue string) string {
    val := c.String(flagName)
    if val == "" {
        val = os.Getenv(envVar)
        if val == "" {
            val = defaultValue
        }
    }
    return val
}

func getOrDefaultBool(c *cli.Context, flagName, envVar string, defaultValue bool) bool {
    // 1. Check if the flag was explicitly set via command line
    if c.IsSet(flagName) {
        return c.Bool(flagName) // Use the flag's value
    }

    // 2. Check if the environment variable exists AND has a non-empty value
    envVal, envVarExists := os.LookupEnv(envVar)
    if envVarExists && envVal != "" {
        // Environment variable exists and is not empty, try to parse it
        parsedVal, err := strconv.ParseBool(envVal)
        if err == nil {
            return parsedVal // Parsed successfully, use parsed value
        }
        slog.Warn("Environment variable set but could not be parsed as bool. Using default value.", "envVar", envVar, "value", envVal, "error", err, "default", defaultValue)
    }

    // 3. Either the environment variable is not set, or it's empty.
    //    In either case, use the default value (which is true in your case)
    return defaultValue
}

func getOrDefaultUint64(c *cli.Context, flagName, envVar string, defaultValue uint64) uint64 {
    val := c.Uint64(flagName)
    if !c.IsSet(flagName) {
        envVal := os.Getenv(envVar)
        if envVal == "" {
            return defaultValue
        }
        parsedVal, err := strconv.ParseUint(envVal, 10, 64)
        if err != nil {
            return defaultValue
        }
        return parsedVal
    }
    return val
}

func getOrDefaultFloat64(c *cli.Context, flagName, envVar string, defaultValue float64) float64 {
    val := c.Float64(flagName)
    if !c.IsSet(flagName) {
        envVal := os.Getenv(envVar)
        if envVal == "" {
            return defaultValue
        }
        parsedVal, err := strconv.ParseFloat(envVal, 64)
        if err != nil {
            return defaultValue
        }
        return parsedVal
    }
    return val
}

func getOrDefaultUint(c *cli.Context, flagName, envVar string, defaultValue uint) uint {
    val := c.Uint(flagName)
    if !c.IsSet(flagName) {
        envVal := os.Getenv(envVar)
        if envVal == "" {
            return defaultValue
        }
        parsedVal, err := strconv.ParseUint(envVal, 10, 64)
        if err != nil {
            return defaultValue
        }
        return uint(parsedVal)
    }
    return val
}

func main() {
    app := &cli.App{
        Name:  "Preconf Bidder",
        Usage: "A tool for bidding in mev-commit preconfirmation auctions for blobs and eth transfers.",
        Action: func(c *cli.Context) error {
            // Retrieve AppName and Version from flags or environment variables, with defaults
            appName := getOrDefault(c, FlagAppName, "APP_NAME", "preconf_bidder")
            version := getOrDefault(c, FlagVersion, "VERSION", "0.8.0")

            // Initialize the custom pretty-print JSON handler with INFO level
            handler := NewCustomJSONHandler(os.Stderr, slog.LevelInfo)

            // Add default attributes to every log entry
            logger := slog.New(handler).With(
                slog.String("app", appName),
                slog.String("version", version),
            )

            slog.SetDefault(logger)

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
            fmt.Println("  --private-key            Your private key for signing transactions (64 hex chars)")
            fmt.Println("  --ws-endpoint            The WebSocket endpoint for your Ethereum node")
            fmt.Println("  --rpc-endpoint           The RPC endpoint if not using payload")
            fmt.Println("  --bid-amount             The amount to bid (in ETH), default 0.001")
            fmt.Println("  --priority-fee           The priority fee in wei, default 1")
            fmt.Println("  --bid-amount-std-dev-percentage  Std dev percentage of bid amount, default 100.0")
            fmt.Println("  --num-blob                       Number of blob transactions to send, default 0 makes the tx an eth transfer")
            fmt.Println("  --default-timeout        Default client context timeout in seconds, default 15")
            fmt.Println("  --run-duration-minutes   Duration to run the bidder in minutes (0 for infinite)")
            fmt.Println("  --app-name               Application name for logging")
            fmt.Println("  --version                Application version for logging")
            fmt.Println("")
            fmt.Println("You can also set environment variables like WS_ENDPOINT and PRIVATE_KEY.")
            fmt.Println("For more details, check the documentation: https://docs.primev.xyz/get-started/bidders/best-practices")
            fmt.Println("-----------------------------------------------------------------------------------------------")
            fmt.Println()

            // Get values from flags, environment, or use defaults
            serverAddress := getOrDefault(c, FlagServerAddress, "SERVER_ADDRESS", "localhost:13524")
            usePayload := getOrDefaultBool(c, FlagUsePayload, "USE_PAYLOAD", true)
            rpcEndpoint := getOrDefault(c, FlagRpcEndpoint, "RPC_ENDPOINT", "https://ethereum-holesky-rpc.publicnode.com")
            wsEndpoint := getOrDefault(c, FlagWsEndpoint, "WS_ENDPOINT", "wss://ethereum-holesky-rpc.publicnode.com")
            privateKeyHex := getOrDefault(c, FlagPrivateKey, "PRIVATE_KEY", "") // No default, required
            offset := getOrDefaultUint64(c, FlagOffset, "OFFSET", 1)
            bidAmount := getOrDefaultFloat64(c, FlagBidAmount, "BID_AMOUNT", 0.001)
            priorityFee := getOrDefaultUint64(c, FlagPriorityFee, "PRIORITY_FEE", 1)
            stdDevPercentage := getOrDefaultFloat64(c, FlagBidAmountStdDevPercentage, "BID_AMOUNT_STD_DEV_PERCENTAGE", 100.0)
            numBlob := getOrDefaultUint(c, FlagNumBlob, "NUM_BLOB", 0)
            defaultTimeoutSeconds := getOrDefaultUint(c, FlagDefaultTimeout, "DEFAULT_TIMEOUT", 15)
            runDurationMinutes := getOrDefaultUint(c, FlagRunDurationMinutes, "RUN_DURATION_MINUTES", 0)

            // Validate wsEndpoint if provided
            if wsEndpoint != "" {
                var err error
                wsEndpoint, err = validateWebSocketURL(wsEndpoint)
                if err != nil {
                    slog.Error("WS_ENDPOINT validation error", "err", err)
                    return err
                }
            }
            
            // Interactive prompts if wsEndpoint or privateKeyHex are not provided
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
                fmt.Println()
            }

            if privateKeyHex == "" {
                fmt.Println("A private key is needed to sign transactions.")
                fmt.Println("A private key is a 64-character hexadecimal string.")
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
                fmt.Println()
            }

            defaultTimeout := time.Duration(defaultTimeoutSeconds) * time.Second
            var endTime time.Time
            if runDurationMinutes > 0 {
                endTime = time.Now().Add(time.Duration(runDurationMinutes) * time.Minute)
                slog.Info("Bidder will run until", "endTime", endTime)
            } else {
                slog.Info("Bidder will run indefinitely")
            }

            fmt.Println("Great! Here's what we have:")
            fmt.Printf(" - WebSocket Endpoint: %s\n", wsEndpoint)
            fmt.Printf(" - Private Key: Provided (hidden)\n")
            fmt.Printf(" - Server Address: %s\n", serverAddress)
            fmt.Printf(" - Use Payload: %v\n", usePayload)
            fmt.Printf(" - Bid Amount: %f ETH\n", bidAmount)
			fmt.Printf(" - Priority Fee: %d wei\n", priorityFee)
            fmt.Printf(" - Standard Deviation: %f%%\n", stdDevPercentage)
            fmt.Printf(" - Number of Blobs: %d\n", numBlob)
            fmt.Printf(" - Default Timeout: %d seconds\n", defaultTimeoutSeconds)
            if runDurationMinutes > 0 {
                fmt.Printf(" - Run Duration: %d minutes\n", runDurationMinutes)
            } else {
                fmt.Printf(" - Run Duration: infinite\n")
            }
            fmt.Println()
            fmt.Println("We will now connect to the blockchain and start sending transactions.")
            fmt.Println("Please wait...")
            fmt.Println()

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
            &cli.Int64Flag{
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

// CustomJSONHandler is a custom slog.Handler that formats logs as pretty-printed JSON with customized timestamp
type CustomJSONHandler struct {
	encoder *json.Encoder
	level   slog.Level
}

// NewCustomJSONHandler creates a new instance of CustomJSONHandler
func NewCustomJSONHandler(w io.Writer, level slog.Level) *CustomJSONHandler {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ") // Set indentation for pretty-printing
	return &CustomJSONHandler{
		encoder: encoder,
		level:   level,
	}
}

// Handle processes each log record
func (h *CustomJSONHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level < h.level {
		return nil // Skip logs below the set level
	}

	// Create a map to hold the log entry
	logEntry := make(map[string]interface{})

	// Customize the timestamp to include only milliseconds
	logEntry["time"] = r.Time.Format("2006-01-02T15:04:05.000Z07:00") // RFC3339 with milliseconds

	// Set the log level
	logEntry["level"] = r.Level.String()

	// Set the message
	logEntry["msg"] = r.Message

	// Add all other attributes
	r.Attrs(func(attr slog.Attr) bool {
		logEntry[attr.Key] = attr.Value.Any()
		return true
	})

	// Encode the log entry as pretty JSON
	return h.encoder.Encode(logEntry)
}

// Enabled checks if the handler is enabled for the given level
func (h *CustomJSONHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.level
}

// WithAttrs returns a new handler with the given attributes
func (h *CustomJSONHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// Create a new handler and copy attributes if necessary
	// Since we're retaining field names, we don't need to handle attrs specially here
	return h
}

// WithGroup returns a new handler with the given group name
func (h *CustomJSONHandler) WithGroup(name string) slog.Handler {
	// Groups can be handled if needed, but for simplicity, we ignore them here
	return h
}
