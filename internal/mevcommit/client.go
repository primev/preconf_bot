// Package mevcommit provides functionality for interacting with the mev-commit protocol,
// including setting up a bidder client, connecting to an Ethereum node, and handling
// account authentication.
package mevcommit

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math"
	"time"

	pb "github.com/primev/preconf_blob_bidder/internal/bidderpb"
	"google.golang.org/grpc"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/rs/zerolog/log"
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
	conn, err := grpc.Dial(cfg.ServerAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Error().
			Err(err).
			Str("server_address", cfg.ServerAddress).
			Msg("Failed to connect to gRPC server")
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
	// Dial the Ethereum RPC endpoint
	client, err := rpc.Dial(endpoint)
	if err != nil {
		log.Error().
			Err(err).
			Str("endpoint", MaskEndpoint(endpoint)).
			Msg("Failed to dial Ethereum RPC endpoint")
		return nil, err
	}

	// Create a new ethclient.Client using the RPC client
	ec := ethclient.NewClient(client)
	log.Info().
		Str("endpoint", MaskEndpoint(endpoint)).
		Msg("Connected to Ethereum RPC endpoint")
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
		log.Warn().Msg("No private key provided; proceeding without authentication")
		return AuthAcct{}, nil
	}

	// Convert the hex-encoded private key to an ECDSA private key
	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		log.Error().
			Err(err).
			Msg("Failed to load private key")
		return AuthAcct{}, err
	}

	// Extract the public key from the private key
	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		log.Error().Msg("Failed to assert public key type")
		return AuthAcct{}, fmt.Errorf("failed to assert public key type")
	}

	// Generate the Ethereum address from the public key
	address := crypto.PubkeyToAddress(*publicKeyECDSA)

	// Set up a context with a 15-second timeout for fetching the chain ID
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel() // Ensure the context is canceled after the operation

	chainID, err := client.ChainID(ctx)
	if err != nil {
		log.Error().
			Err(err).
			Msg("Failed to fetch chain ID")
		return AuthAcct{}, err
	}

	// Create the transaction options with the private key and chain ID
	auth, err := bind.NewKeyedTransactorWithChainID(privateKey, chainID)
	if err != nil {
		log.Error().
			Err(err).
			Msg("Failed to create authorized transactor")
		return AuthAcct{}, err
	}

	// Return the AuthAcct struct containing the private key, public key, address, and transaction options
	log.Info().
		Str("address", address.Hex()).
		Msg("Authenticated account")

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
			log.Info().
				Str("rpc_endpoint", MaskEndpoint(rpcEndpoint)).
				Int("attempt", i+1).
				Msg("Successfully connected to RPC client")
			return rpcClient
		}

		log.Warn().
			Err(err).
			Str("rpc_endpoint", MaskEndpoint(rpcEndpoint)).
			Int("attempt", i+1).
			Msg("Failed to connect to RPC client, retrying...")
		time.Sleep(10 * time.Duration(math.Pow(2, float64(i))) * time.Second) // Exponential backoff
	}

	log.Error().
		Err(err).
		Str("rpc_endpoint", MaskEndpoint(rpcEndpoint)).
		Int("max_retries", maxRetries).
		Msg("Failed to connect to RPC client after maximum retries")
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
		log.Warn().
			Err(err).
			Str("ws_endpoint", MaskEndpoint(wsEndpoint)).
			Msg("Failed to connect to WebSocket client, retrying in 10 seconds...")
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
			log.Info().
				Str("ws_endpoint", MaskEndpoint(wsEndpoint)).
				Int("attempt", i+1).
				Msg("WebSocket client reconnected")
			sub, err = wsClient.SubscribeNewHead(context.Background(), headers)
			if err == nil {
				return wsClient, sub
			}
			log.Warn().
				Err(err).
				Msg("Failed to subscribe to new headers after reconnecting")
		}
		log.Warn().
			Err(err).
			Str("ws_endpoint", MaskEndpoint(wsEndpoint)).
			Int("attempt", i+1).
			Msg("Failed to reconnect WebSocket client, retrying in 5 seconds...")
		time.Sleep(5 * time.Second)
	}

	log.Error().
		Err(err).
		Str("ws_endpoint", MaskEndpoint(wsEndpoint)).
		Int("max_retries", 10).
		Msg("Failed to reconnect WebSocket client after maximum retries")
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
