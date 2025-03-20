// Package mevcommit provides functionality for interacting with the mev-commit protocol,
// including setting up a bidder client, connecting to an Ethereum node, and handling
// account authentication.
package mevcommit

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"log/slog"
	"math"
	"time"

	pb "github.com/primev/mev-commit/p2p/gen/go/bidderapi/v1"
	"google.golang.org/grpc"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"google.golang.org/grpc/credentials/insecure"
)

// BidderConfig holds the configuration settings for the mev-commit bidder node.
type BidderConfig struct {
	ServerAddress string `json:"server_address" yaml:"server_address"` // The address of the gRPC server for the bidder node.
	LogFmt        string `json:"log_fmt" yaml:"log_fmt"`               // The format for logging output.
	LogLevel      string `json:"log_level" yaml:"log_level"`           // The level of logging detail.
}

// Bidder utilizes the mev-commit bidder client to interact with the mev-commit chain.
type Bidder struct {
	client pb.BidderClient // gRPC client for interacting with the mev-commit bidder service.
}

// GethConfig holds configuration settings for a Geth node to connect to the mev-commit chain.
type GethConfig struct {
	Endpoint string `json:"endpoint" yaml:"endpoint"` // The RPC endpoint for connecting to the Ethereum node.
}

// AuthAcct holds the private key, public key, address, and transaction authorization information for an account.
type AuthAcct struct {
	PrivateKey *ecdsa.PrivateKey  // The private key for the account.
	PublicKey  *ecdsa.PublicKey   // The public key derived from the private key.
	Address    common.Address     // The Ethereum address derived from the public key.
	Auth       *bind.TransactOpts // The transaction options for signing transactions.
}

// NewBidderClient creates a new gRPC client connection to the bidder service and returns a Bidder instance.
//
// Parameters:
// - cfg: The BidderConfig struct containing the server address and logging settings.
//
// Returns:
// - A pointer to a Bidder struct, or an error if the connection fails.
func NewBidderClient(cfg BidderConfig) (*Bidder, error) {
	// Establish a gRPC connection to the bidder service
	conn, err := grpc.NewClient(cfg.ServerAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		slog.Error("Failed to connect to gRPC server",
			"error", err,
			"server_address", cfg.ServerAddress,
		)
		return nil, err
	}

	// Create a new bidder client using the gRPC connection
	client := pb.NewBidderClient(conn)
	return &Bidder{client: client}, nil
}

// NewGethClient connects to an Ethereum-compatible chain using the provided RPC endpoint.
//
// Parameters:
// - endpoint: The RPC endpoint of the Ethereum node.
//
// Returns:
// - A pointer to an ethclient.Client for interacting with the Ethereum node, or an error if the connection fails.
func NewGethClient(endpoint string) (*ethclient.Client, error) {
	// Create a context with a 15-second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Use DialContext to establish a connection with the 15-second timeout
	client, err := rpc.DialContext(ctx, endpoint)
	if err != nil {
		slog.Error("Failed to dial Ethereum RPC endpoint",
			"error", err,
			"endpoint", MaskEndpoint(endpoint),
		)
		return nil, err
	}

	// Create a new ethclient.Client using the RPC client
	ec := ethclient.NewClient(client)
	slog.Info("Connected to Ethereum RPC endpoint",
		"endpoint", MaskEndpoint(endpoint),
	)
	return ec, nil
}

// AuthenticateAddress converts a hex-encoded private key string to an AuthAcct struct,
// which contains the account's private key, public key, address, and transaction authorization.
//
// Parameters:
// - privateKeyHex: The hex-encoded private key string.
// - client: The ethclient.Client to interact with the Ethereum node.
//
// Returns:
// - An AuthAcct struct, or an error if authentication fails.
func AuthenticateAddress(privateKeyHex string, client *ethclient.Client) (AuthAcct, error) {
	if privateKeyHex == "" {
		slog.Warn("No private key provided; proceeding without authentication")
		return AuthAcct{}, nil
	}

	// Convert the hex-encoded private key to an ECDSA private key
	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		slog.Error("Failed to load private key",
			"error", err,
		)
		return AuthAcct{}, err
	}

	// Extract the public key from the private key
	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		slog.Error("Failed to assert public key type")
		return AuthAcct{}, fmt.Errorf("failed to assert public key type")
	}

	// Generate the Ethereum address from the public key
	address := crypto.PubkeyToAddress(*publicKeyECDSA)

	// Set up a context with a 15-second timeout for fetching the chain ID
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel() // Ensure the context is canceled after the operation

	chainID, err := client.ChainID(ctx)
	if err != nil {
		slog.Error("Failed to fetch chain ID",
			"error", err,
		)
		return AuthAcct{}, err
	}

	// Create the transaction options with the private key and chain ID
	auth, err := bind.NewKeyedTransactorWithChainID(privateKey, chainID)
	if err != nil {
		slog.Error("Failed to create authorized transactor",
			"error", err,
		)
		return AuthAcct{}, err
	}

	// Return the AuthAcct struct containing the private key, public key, address, and transaction options
	slog.Info("Authenticated account",
		"address", address.Hex(),
	)

	return AuthAcct{
		PrivateKey: privateKey,
		PublicKey:  publicKeyECDSA,
		Address:    address,
		Auth:       auth,
	}, nil
}

// ConnectRPCClientWithRetries attempts to connect to the RPC client with retries and exponential backoff.
//
// Parameters:
// - rpcEndpoint: The RPC endpoint to connect to.
// - maxRetries: The maximum number of retry attempts.
// - timeout: The timeout duration for each connection attempt.
//
// Returns:
// - A pointer to an ethclient.Client if successful, or nil if all retries fail.
func ConnectRPCClientWithRetries(rpcEndpoint string, maxRetries int, timeout time.Duration) *ethclient.Client {
	var rpcClient *ethclient.Client
	var err error

	for i := 0; i < maxRetries; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		rpcClient, err = ethclient.DialContext(ctx, rpcEndpoint)
		if err == nil {
			slog.Info("Successfully connected to RPC client",
				"rpc_endpoint", MaskEndpoint(rpcEndpoint),
				"attempt", i+1,
			)
			return rpcClient
		}

		slog.Warn("Failed to connect to RPC client, retrying...",
			"error", err,
			"rpc_endpoint", MaskEndpoint(rpcEndpoint),
			"attempt", i+1,
		)
		time.Sleep(10 * time.Duration(math.Pow(2, float64(i))) * time.Second) // Exponential backoff
	}

	slog.Error("Failed to connect to RPC client after maximum retries",
		"error", err,
		"rpc_endpoint", MaskEndpoint(rpcEndpoint),
		"max_retries", maxRetries,
	)
	return nil
}

// ConnectWSClient attempts to connect to the WebSocket client with continuous retries.
//
// Parameters:
// - wsEndpoint: The WebSocket endpoint to connect to.
//
// Returns:
// - A pointer to an ethclient.Client if successful, or an error if unable to connect.
func ConnectWSClient(wsEndpoint string) (*ethclient.Client, error) {
	for {
		wsClient, err := NewGethClient(wsEndpoint)
		if err == nil {
			return wsClient, nil
		}
		slog.Warn("Failed to connect to WebSocket client, retrying in 10 seconds...",
			"error", err,
			"ws_endpoint", MaskEndpoint(wsEndpoint),
		)
		time.Sleep(10 * time.Second)
	}
}

// ReconnectWSClient attempts to reconnect to the WebSocket client with limited retries.
//
// Parameters:
// - wsEndpoint: The WebSocket endpoint to reconnect to.
// - headers: The channel to subscribe to new headers.
//
// Returns:
// - A pointer to an ethclient.Client and an ethereum.Subscription if successful, or nil values if all retries fail.
func ReconnectWSClient(wsEndpoint string, headers chan *types.Header) (*ethclient.Client, ethereum.Subscription) {
	var wsClient *ethclient.Client
	var sub ethereum.Subscription
	var err error

	for i := 0; i < 10; i++ { // Retry logic for WebSocket connection
		wsClient, err = ConnectWSClient(wsEndpoint)
		if err == nil {
			slog.Info("WebSocket client reconnected",
				"ws_endpoint", MaskEndpoint(wsEndpoint),
				"attempt", i+1,
			)

			// Create a context with a 15-second timeout for the subscription
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			sub, err = wsClient.SubscribeNewHead(ctx, headers)
			if err == nil {
				return wsClient, sub
			}

			slog.Warn("Failed to subscribe to new headers after reconnecting",
				"error", err,
			)
		}

		slog.Warn("Failed to reconnect WebSocket client, retrying in 5 seconds...",
			"error", err,
			"ws_endpoint", MaskEndpoint(wsEndpoint),
			"attempt", i+1,
		)
		time.Sleep(5 * time.Second)
	}

	slog.Error("Failed to reconnect WebSocket client after maximum retries",
		"error", err,
		"ws_endpoint", MaskEndpoint(wsEndpoint),
		"max_retries", 10,
	)
	return nil, nil
}

// MaskEndpoint masks sensitive parts of the endpoint URLs.
//
// Parameters:
// - endpoint: The full endpoint URL.
//
// Returns:
// - A masked version of the endpoint if its length exceeds 10 characters, otherwise a fixed mask.
func MaskEndpoint(endpoint string) string {
	if len(endpoint) > 10 {
		return endpoint[:10] + "*****"
	}
	return "*****"
}
