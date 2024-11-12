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
		Level:     slog.LevelInfo,
		AddSource: true,
	}))
	slog.SetDefault(logger)

	app := &cli.App{
		Name:  "Preconf Bidder",
		Usage: "A tool for bidding in mev-commit preconfirmation auctions for blobs and transactions",
		Flags: []cli.Flag{
			// Your flags here...
		},
		Action: func(c *cli.Context) error {
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

			var rpcClient *ethclient.Client
			if !usePayload {
				rpcClient, err = bb.ConnectRPCClientWithRetries(rpcEndpoint, 5, timeout)
				if err != nil {
					slog.Error("Failed to connect to RPC client", "rpcEndpoint", bb.MaskEndpoint(rpcEndpoint), "error", err)
				}
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

			authAcct, err := bb.AuthenticateAddress(privateKeyHex, wsClient)
			if err != nil {
				slog.Error("Failed to authenticate private key", "error", err)
				return fmt.Errorf("failed to authenticate private key: %w", err)
			}

			service := ee.NewService(wsClient, authAcct, defaultTimeout, rpcEndpoint, logger)

			for {
				select {
				case err := <-sub.Err():
					if err != nil {
						slog.Error("Subscription error", "error", err)
					}
				case header := <-headers:
					var signedTx *types.Transaction
					var blockNumber uint64
					var err error

					if numBlob == 0 {
						amount := big.NewInt(1e15)
						signedTx, blockNumber, err = service.SelfETHTransfer(amount, offset)
					} else {
						signedTx, blockNumber, err = service.ExecuteBlobTransaction(int(numBlob), offset)
					}

					if err != nil {
						service.Logger.Error("Failed to execute transaction", "error", err)
						continue
					}

					if signedTx == nil {
						slog.Error("Transaction was not signed or created.")
					} else {
						slog.Info("Transaction created successfully")
					}

					slog.Info("New block received",
						"blockNumber", header.Number.Uint64(),
						"timestamp", header.Time,
						"hash", header.Hash().String(),
					)

					// Compute standard deviation in ETH
					stdDev := bidAmount * stdDevPercentage / 100.0

					// Generate random amount with normal distribution
					randomEthAmount := math.Max(rand.NormFloat64()*stdDev+bidAmount, bidAmount)

					if usePayload {
						bb.SendPreconfBid(bidderClient, signedTx, int64(blockNumber), randomEthAmount)
					} else {
						_, err = service.SendBundle(signedTx, blockNumber)
						if err != nil {
							slog.Error("Failed to send transaction", "rpcEndpoint", bb.MaskEndpoint(rpcEndpoint), "error", err)
						}
						bb.SendPreconfBid(bidderClient, signedTx.Hash().String(), int64(blockNumber), randomEthAmount)
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
