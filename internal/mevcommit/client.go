// Package mevcommit provides functionality for interacting with the mev-commit protocol,
// including setting up a bidder client, connecting to an Ethereum node, and handling
// account authentication.
package mevcommit

import (
	"crypto/ecdsa"
	"math"

	pb "github.com/primev/preconf_blob_bidder/internal/bidderpb"
	"google.golang.org/grpc"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rpc"
	"google.golang.org/grpc/credentials/insecure"

	"context"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
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
		log.Crit("Failed to connect to gRPC server", "err", err)
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
		return nil, err
	}

	// Create a new ethclient.Client using the RPC client
	ec := ethclient.NewClient(client)
	return ec, nil
}

// AuthenticateAddress converts a hex-encoded private key string to an AuthAcct struct,
// which contains the account's private key, public key, address, and transaction authorization.
//
// Parameters:
// - privateKeyHex: The hex-encoded private key string.
//
// Returns:
// - A pointer to an AuthAcct struct, or an error if authentication fails.
func AuthenticateAddress(privateKeyHex string, client *ethclient.Client) (AuthAcct, error) {
	if privateKeyHex == "" {
		return AuthAcct{}, nil
	}

	// Convert the hex-encoded private key to an ECDSA private key
	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		log.Crit("Failed to load private key", "err", err)
		return AuthAcct{}, err
	}

	// Extract the public key from the private key
	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		log.Crit("Failed to assert public key type")
	}

	// Generate the Ethereum address from the public key
	address := crypto.PubkeyToAddress(*publicKeyECDSA)

	// Set up a context with a 5-second timeout for fetching the chain ID
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel() // Ensure the context is canceled after the operation

	chainID, err := client.ChainID(ctx)
	if err != nil {
		log.Crit("Failed to fetch chain ID", "err", err)
		return AuthAcct{}, err
	}

	// Create the transaction options with the private key and chain ID
	auth, err := bind.NewKeyedTransactorWithChainID(privateKey, chainID)
	if err != nil {
		log.Crit("Failed to create authorized transactor", "err", err)
	}

	// Return the AuthAcct struct containing the private key, public key, address, and transaction options
	return AuthAcct{
		PrivateKey: privateKey,
		PublicKey:  publicKeyECDSA,
		Address:    address,
		Auth:       auth,
	}, nil
}

// ConnectRPCClientWithRetries attempts to connect to the RPC client with retries and exponential backoff
func ConnectRPCClientWithRetries(rpcEndpoint string, maxRetries int, timeout time.Duration) *ethclient.Client {
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

// ConnectWSClient attempts to connect to the WebSocket client with continuous retries
func ConnectWSClient(wsEndpoint string) (*ethclient.Client, error) {
	for {
		wsClient, err := NewGethClient(wsEndpoint)
		if err == nil {
			return wsClient, nil
		}
		log.Warn("Failed to connect to WebSocket client", "err", err)
		time.Sleep(10 * time.Second)
	}
}

// ReconnectWSClient attempts to reconnect to the WebSocket client with limited retries
func ReconnectWSClient(wsEndpoint string, headers chan *types.Header) (*ethclient.Client, ethereum.Subscription) {
	var wsClient *ethclient.Client
	var sub ethereum.Subscription
	var err error

	for i := 0; i < 10; i++ { // Retry logic for WebSocket connection
		wsClient, err = ConnectWSClient(wsEndpoint)
		if err == nil {
			log.Info("(ws) Geth client reconnected", "endpoint", MaskEndpoint(wsEndpoint))
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

// MaskEndpoint masks sensitive parts of the endpoint URLs
func MaskEndpoint(endpoint string) string {
	if len(endpoint) > 10 {
		return endpoint[:10] + "*****"
	}
	return "*****"
}