package service

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	pb "github.com/primev/preconf_blob_bidder/internal/bidderpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/consensys/gnark-crypto/ecc/bls12-381/fr"
	gokzg4844 "github.com/crate-crypto/go-kzg-4844"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/consensus/misc/eip4844"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	"github.com/holiman/uint256"
	"golang.org/x/exp/rand"
)

// AuthAcct holds the private key, public key, address, and transaction authorization information for an account.
type AuthAcct struct {
	PrivateKey *ecdsa.PrivateKey
	PublicKey  *ecdsa.PublicKey
	Address    common.Address
	Auth       *bind.TransactOpts
}

// Service manages stateful variables and provides methods for interacting with Ethereum and mev-commit.
type Service struct {
	Client                *ethclient.Client
	AuthAcct              *AuthAcct
	Logger                *slog.Logger
	DefaultTimeout        time.Duration
	RPCURL                string
	BidderRegistryAddress *common.Address
	BlockTrackerAddress   *common.Address
	PreconfManagerAddress *common.Address

	// Cached ABIs (optional)
	bidderRegistryABI *abi.ABI
	blockTrackerABI   *abi.ABI

	// Bidder client interface (optional)
	BidderClient pb.BidderClient
}

type JSONRPCResponse struct {
	Result   json.RawMessage `json:"result"`
	RPCError RPCError        `json:"error"`
	ID       int             `json:"id,omitempty"`
	Jsonrpc  string          `json:"jsonrpc,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type FlashbotsPayload struct {
	Jsonrpc string                   `json:"jsonrpc"`
	Method  string                   `json:"method"`
	Params  []map[string]interface{} `json:"params"`
	ID      int                      `json:"id"`
}

// Functional options pattern for flexible initialization
type ServiceOption func(*Service) error

func WithClient(client *ethclient.Client) ServiceOption {
	return func(s *Service) error {
		s.Client = client
		return nil
	}
}

func WithAuthAcct(authAcct *AuthAcct) ServiceOption {
	return func(s *Service) error {
		s.AuthAcct = authAcct
		return nil
	}
}

func WithLogger(logger *slog.Logger) ServiceOption {
	return func(s *Service) error {
		s.Logger = logger
		return nil
	}
}

func WithDefaultTimeout(timeout time.Duration) ServiceOption {
	return func(s *Service) error {
		s.DefaultTimeout = timeout
		return nil
	}
}

func WithRPCURL(rpcurl string) ServiceOption {
	return func(s *Service) error {
		s.RPCURL = rpcurl
		return nil
	}
}

func WithBidderRegistryAddress(address common.Address) ServiceOption {
	return func(s *Service) error {
		s.BidderRegistryAddress = &address
		return nil
	}
}

func WithBlockTrackerAddress(address common.Address) ServiceOption {
	return func(s *Service) error {
		s.BlockTrackerAddress = &address
		return nil
	}
}

func WithPreconfManagerAddress(address common.Address) ServiceOption {
	return func(s *Service) error {
		s.PreconfManagerAddress = &address
		return nil
	}
}

// NewService initializes and returns a new Service instance with the given options.
func NewService(opts ...ServiceOption) (*Service, error) {
	s := &Service{
		DefaultTimeout: 30 * time.Second, // Set default timeout if needed
		Logger:         slog.Default(),   // Set default logger if needed
	}
	for _, opt := range opts {
		if err := opt(s); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// NewBidderClient initializes a new gRPC client connection to the bidder service.
func (s *Service) NewBidderClient(serverAddress string) error {
	conn, err := grpc.NewClient(serverAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		if s.Logger != nil {
			s.Logger.Error("Failed to connect to gRPC server",
				"error", err,
				"server_address", s.MaskEndpoint(serverAddress),
			)
		}
		return err
	}

	client := pb.NewBidderClient(conn)
	s.BidderClient = client
	if s.Logger != nil {
		s.Logger.Info("Connected to mev-commit bidder client", "server_address", s.MaskEndpoint(serverAddress))
	}
	return nil
}

// NewGethClient establishes a connection to the Ethereum RPC endpoint.
func (s *Service) NewGethClient(endpoint string) error {
	ctx, cancel := context.WithTimeout(context.Background(), s.DefaultTimeout)
	defer cancel()

	client, err := rpc.DialContext(ctx, endpoint)
	if err != nil {
		if s.Logger != nil {
			s.Logger.Error("Failed to dial Ethereum RPC endpoint",
				"error", err,
				"endpoint", s.MaskEndpoint(endpoint),
			)
		}
		return err
	}

	s.Client = ethclient.NewClient(client)
	if s.Logger != nil {
		s.Logger.Info("Connected to Ethereum RPC endpoint", "endpoint", s.MaskEndpoint(endpoint))
	}
	return nil
}

// AuthenticateAddress converts a hex-encoded private key string to an AuthAcct struct.
func (s *Service) AuthenticateAddress(privateKeyHex string) error {
	if privateKeyHex == "" {
		if s.Logger != nil {
			s.Logger.Warn("No private key provided; proceeding without authentication")
		}
		return fmt.Errorf("no private key provided")
	}

	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		if s.Logger != nil {
			s.Logger.Error("Failed to load private key",
				"error", err,
			)
		}
		return err
	}

	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		if s.Logger != nil {
			s.Logger.Error("Failed to assert public key type")
		}
		return fmt.Errorf("failed to assert public key type")
	}

	address := crypto.PubkeyToAddress(*publicKeyECDSA)

	if s.Client == nil {
		return fmt.Errorf("client is not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.DefaultTimeout)
	defer cancel()

	chainID, err := s.Client.ChainID(ctx)
	if err != nil {
		if s.Logger != nil {
			s.Logger.Error("Failed to fetch chain ID",
				"error", err,
			)
		}
		return err
	}

	auth, err := bind.NewKeyedTransactorWithChainID(privateKey, chainID)
	if err != nil {
		if s.Logger != nil {
			s.Logger.Error("Failed to create authorized transactor",
				"error", err,
			)
		}
		return err
	}

	s.AuthAcct = &AuthAcct{
		PrivateKey: privateKey,
		PublicKey:  publicKeyECDSA,
		Address:    address,
		Auth:       auth,
	}

	if s.Logger != nil {
		s.Logger.Info("Authenticated account",
			"address", address.Hex(),
		)
	}

	return nil
}

// ConnectRPCClientWithRetries attempts to connect to the RPC client with retries and exponential backoff.
func (s *Service) ConnectRPCClientWithRetries(rpcEndpoint string, maxRetries int) error {
	var err error

	for i := 0; i < maxRetries; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), s.DefaultTimeout)
		defer cancel()

		s.Client, err = ethclient.DialContext(ctx, rpcEndpoint)
		if err == nil {
			if s.Logger != nil {
				s.Logger.Info("Successfully connected to RPC client",
					"rpc_endpoint", s.MaskEndpoint(rpcEndpoint),
					"attempt", i+1,
				)
			}
			return nil
		}

		if s.Logger != nil {
			s.Logger.Warn("Failed to connect to RPC client, retrying...",
				"error", err,
				"rpc_endpoint", s.MaskEndpoint(rpcEndpoint),
				"attempt", i+1,
			)
		}
		time.Sleep(10 * time.Duration(math.Pow(2, float64(i))) * time.Second) // Exponential backoff
	}

	if s.Logger != nil {
		s.Logger.Error("Failed to connect to RPC client after maximum retries",
			"error", err,
			"rpc_endpoint", s.MaskEndpoint(rpcEndpoint),
			"max_retries", maxRetries,
		)
	}
	return err
}

// ConnectWSClient attempts to connect to the WebSocket client with continuous retries.
func (s *Service) ConnectWSClient(wsEndpoint string) error {
	for {
		err := s.NewGethClient(wsEndpoint)
		if err == nil {
			if s.Logger != nil {
				s.Logger.Info("Connected to WebSocket client",
					"ws_endpoint", s.MaskEndpoint(wsEndpoint),
				)
			}
			return nil
		}
		if s.Logger != nil {
			s.Logger.Warn("Failed to connect to WebSocket client, retrying in 10 seconds...",
				"error", err,
				"ws_endpoint", s.MaskEndpoint(wsEndpoint),
			)
		}
		time.Sleep(10 * time.Second)
	}
}

// ReconnectWSClient attempts to reconnect to the WebSocket client with limited retries.
func (s *Service) ReconnectWSClient(wsEndpoint string, headers chan *types.Header) error {
	var err error

	for i := 0; i < 10; i++ { // Retry logic for WebSocket connection
		err = s.ConnectWSClient(wsEndpoint)
		if err == nil {
			if s.Logger != nil {
				s.Logger.Info("WebSocket client reconnected",
					"ws_endpoint", s.MaskEndpoint(wsEndpoint),
					"attempt", i+1,
				)
			}

			// Subscribe to new headers
			if s.Client == nil {
				return fmt.Errorf("client is not initialized")
			}
			_, err := s.Client.SubscribeNewHead(context.Background(), headers)
			if err == nil {
				// Handle subscription as needed
				return nil
			}

			if s.Logger != nil {
				s.Logger.Warn("Failed to subscribe to new headers after reconnecting",
					"error", err,
				)
			}
		}

		if s.Logger != nil {
			s.Logger.Warn("Failed to reconnect WebSocket client, retrying in 5 seconds...",
				"error", err,
				"ws_endpoint", s.MaskEndpoint(wsEndpoint),
				"attempt", i+1,
			)
		}
		time.Sleep(5 * time.Second)
	}

	if s.Logger != nil {
		s.Logger.Error("Failed to reconnect WebSocket client after maximum retries",
			"error", err,
			"ws_endpoint", s.MaskEndpoint(wsEndpoint),
			"max_retries", 10,
		)
	}
	return err
}

// LoadABI loads and parses the ABI from a file.
func (s *Service) LoadABI(filePath string) (abi.ABI, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		if s.Logger != nil {
			s.Logger.Error("Failed to load ABI file",
				"err", err,
				"file_path", filePath,
			)
		}
		return abi.ABI{}, err
	}

	parsedABI, err := abi.JSON(strings.NewReader(string(data)))
	if err != nil {
		if s.Logger != nil {
			s.Logger.Error("Failed to parse ABI file",
				"err", err,
				"file_path", filePath,
			)
		}
		return abi.ABI{}, err
	}

	if s.Logger != nil {
		s.Logger.Info("ABI file loaded and parsed successfully",
			"file_path", filePath,
		)
	}

	return parsedABI, nil
}

// MaskEndpoint masks sensitive parts of the endpoint URLs.
func (s *Service) MaskEndpoint(endpoint string) string {
	if len(endpoint) > 10 {
		return endpoint[:10] + "*****"
	}
	return "*****"
}

// Example Method: WindowHeight
func (s *Service) WindowHeight() (*big.Int, error) {
	if s.BlockTrackerAddress == nil {
		return nil, fmt.Errorf("BlockTrackerAddress is not set")
	}

	if s.blockTrackerABI == nil {
		var err error
		abi, err := s.LoadABI("abi/BlockTracker.abi")
		if err != nil {
			return nil, fmt.Errorf("failed to load ABI file: %v", err)
		}
		s.blockTrackerABI = &abi
	}

	if s.Client == nil {
		return nil, fmt.Errorf("client is not initialized")
	}

	blockTrackerContract := bind.NewBoundContract(*s.BlockTrackerAddress, *s.blockTrackerABI, s.Client, s.Client, s.Client)

	var currentWindowResult []interface{}
	err := blockTrackerContract.Call(nil, &currentWindowResult, "getCurrentWindow")
	if err != nil {
		if s.Logger != nil {
			s.Logger.Error("Failed to get current window",
				"err", err,
				"function", "getCurrentWindow",
			)
		}
		return nil, fmt.Errorf("failed to get current window: %v", err)
	}

	currentWindow, ok := currentWindowResult[0].(*big.Int)
	if !ok {
		if s.Logger != nil {
			s.Logger.Error("Failed to convert current window to *big.Int")
		}
		return nil, fmt.Errorf("conversion to *big.Int failed")
	}

	if s.Logger != nil {
		s.Logger.Info("Retrieved current bidding window height",
			"current_window", currentWindow.String(),
		)
	}

	return currentWindow, nil
}

// Example Method: GetMinDeposit
func (s *Service) GetMinDeposit() (*big.Int, error) {
	if s.BidderRegistryAddress == nil {
		return nil, fmt.Errorf("BidderRegistryAddress is not set")
	}

	if s.bidderRegistryABI == nil {
		var err error
		abi, err := s.LoadABI("abi/BidderRegistry.abi")
		if err != nil {
			return nil, fmt.Errorf("failed to load ABI file: %v", err)
		}
		s.bidderRegistryABI = &abi
	}

	if s.Client == nil {
		return nil, fmt.Errorf("client is not initialized")
	}

	bidderRegistryContract := bind.NewBoundContract(*s.BidderRegistryAddress, *s.bidderRegistryABI, s.Client, s.Client, s.Client)

	var minDepositResult []interface{}
	err := bidderRegistryContract.Call(nil, &minDepositResult, "minDeposit")
	if err != nil {
		if s.Logger != nil {
			s.Logger.Error("Failed to call minDeposit function",
				"err", err,
				"function", "minDeposit",
			)
		}
		return nil, fmt.Errorf("failed to call minDeposit function: %v", err)
	}

	minDeposit, ok := minDepositResult[0].(*big.Int)
	if !ok {
		if s.Logger != nil {
			s.Logger.Error("Failed to convert minDeposit to *big.Int")
		}
		return nil, fmt.Errorf("failed to convert minDeposit to *big.Int")
	}

	if s.Logger != nil {
		s.Logger.Info("Retrieved minimum deposit amount",
			"min_deposit", minDeposit.String(),
		)
	}

	return minDeposit, nil
}

// Example Method: DepositIntoWindow
func (s *Service) DepositIntoWindow(depositWindow *big.Int) (*types.Transaction, error) {
	if s.BidderRegistryAddress == nil {
		return nil, fmt.Errorf("BidderRegistryAddress is not set")
	}

	if s.AuthAcct == nil || s.AuthAcct.Auth == nil {
		return nil, fmt.Errorf("AuthAcct is not initialized")
	}

	if s.bidderRegistryABI == nil {
		var err error
		abi, err := s.LoadABI("abi/BidderRegistry.abi")
		if err != nil {
			return nil, fmt.Errorf("failed to load ABI file: %v", err)
		}
		s.bidderRegistryABI = &abi
	}

	if s.Client == nil {
		return nil, fmt.Errorf("client is not initialized")
	}

	bidderRegistryContract := bind.NewBoundContract(*s.BidderRegistryAddress, *s.bidderRegistryABI, s.Client, s.Client, s.Client)

	minDeposit, err := s.GetMinDeposit()
	if err != nil {
		return nil, fmt.Errorf("failed to get minDeposit: %v", err)
	}

	s.AuthAcct.Auth.Value = minDeposit

	tx, err := bidderRegistryContract.Transact(s.AuthAcct.Auth, "depositForSpecificWindow", depositWindow)
	if err != nil {
		if s.Logger != nil {
			s.Logger.Error("Failed to create deposit transaction",
				"err", err,
				"function", "depositForSpecificWindow",
			)
		}
		return nil, fmt.Errorf("failed to create transaction: %v", err)
	}

	if s.Logger != nil {
		s.Logger.Info("Deposit transaction sent",
			"tx_hash", tx.Hash().Hex(),
			"window", depositWindow.String(),
		)
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.DefaultTimeout)
	defer cancel()
	receipt, err := bind.WaitMined(ctx, s.Client, tx)
	if err != nil {
		if s.Logger != nil {
			s.Logger.Error("Transaction mining error",
				"err", err,
				"tx_hash", tx.Hash().Hex(),
			)
		}
		return nil, fmt.Errorf("transaction mining error: %v", err)
	}

	if receipt.Status == 1 {
		if s.Logger != nil {
			s.Logger.Info("Deposit transaction successful",
				"tx_hash", tx.Hash().Hex(),
			)
		}
		return tx, nil
	} else {
		if s.Logger != nil {
			s.Logger.Error("Deposit transaction failed",
				"tx_hash", tx.Hash().Hex(),
			)
		}
		return nil, fmt.Errorf("transaction failed")
	}
}

// SendBundle sends a signed transaction bundle to the specified RPC URL.
// It returns the result as a string or an error if the operation fails.
func (s *Service) SendBundle(signedTx *types.Transaction, blkNum uint64) (string, error) {
	// Marshal the signed transaction into binary format.
	binary, err := signedTx.MarshalBinary()
	if err != nil {
		s.Logger.Error("Error marshaling transaction", "error", err)
		return "", err
	}

	// Encode the block number in hex.
	blockNum := hexutil.EncodeUint64(blkNum)

	// Construct the Flashbots payload.
	payload := FlashbotsPayload{
		Jsonrpc: "2.0",
		Method:  "eth_sendBundle",
		Params: []map[string]interface{}{
			{
				"txs": []string{
					hexutil.Encode(binary),
				},
				"blockNumber": blockNum,
			},
		},
		ID: 1,
	}

	// Marshal the payload into JSON.
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		s.Logger.Error("Error marshaling payload", "error", err)
		return "", err
	}

	// Create a context with a timeout.
	ctx, cancel := context.WithTimeout(context.Background(), s.DefaultTimeout)
	defer cancel()

	// Create a new HTTP POST request with the JSON payload.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.RPCURL, bytes.NewReader(payloadBytes))
	if err != nil {
		s.Logger.Error("An error occurred creating the request", "error", err)
		return "", err
	}
	req.Header.Add("Content-Type", "application/json")

	// Execute the HTTP request.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.Logger.Error("An error occurred during the request", "error", err)
		return "", err
	}
	defer resp.Body.Close()

	// Read the response body.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		s.Logger.Error("An error occurred reading the response body", "error", err)
		return "", err
	}

	// Unmarshal the response into JSONRPCResponse struct.
	var rpcResp JSONRPCResponse
	err = json.Unmarshal(body, &rpcResp)
	if err != nil {
		s.Logger.Error("Failed to unmarshal response", "error", err)
		return "", err
	}

	// Check for RPC errors.
	if rpcResp.RPCError.Code != 0 {
		s.Logger.Error("Received error from RPC", "code", rpcResp.RPCError.Code, "message", rpcResp.RPCError.Message)
		return "", fmt.Errorf("request failed %d: %s", rpcResp.RPCError.Code, rpcResp.RPCError.Message)
	}

	// Marshal the result to a string.
	resultStr, err := json.Marshal(rpcResp.Result)
	if err != nil {
		s.Logger.Error("Failed to marshal result", "error", err)
		return "", err
	}

	return string(resultStr), nil
}

func (s *Service) SelfETHTransfer(value *big.Int, offset uint64) (*types.Transaction, uint64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), s.DefaultTimeout)
	defer cancel()

	// Use s.Client, s.AuthAcct, s.Logger
	nonce, err := s.Client.PendingNonceAt(ctx, s.AuthAcct.Address)
	if err != nil {
		s.Logger.Error("Failed to get pending nonce", "error", err)
		return nil, 0, err
	}

	// Get the current base fee per gas from the latest block header
	header, err := s.Client.HeaderByNumber(ctx, nil)
	if err != nil {
		slog.Default().Error("Failed to get latest block header",
			slog.String("function", "HeaderByNumber"),
			slog.Any("error", err))
		return nil, 0, err
	}

	// Get the chain ID
	chainID, err := s.Client.NetworkID(ctx)
	if err != nil {
		slog.Default().Error("Failed to get network ID",
			slog.String("function", "NetworkID"),
			slog.Any("error", err))
		return nil, 0, err
	}

	baseFee := header.BaseFee
	blockNumber := header.Number.Uint64()

	// Create a transaction with a priority fee.
	priorityFee := big.NewInt(2_000_000_000) // 2 gwei in wei
	maxFee := new(big.Int).Add(baseFee, priorityFee)
	tx := types.NewTx(&types.DynamicFeeTx{
		Nonce:     nonce,
		To:        &s.AuthAcct.Address,
		Value:     value,
		Gas:       500_000,
		GasFeeCap: maxFee,
		GasTipCap: priorityFee,
	})

	// Sign the transaction with the authenticated account's private key
	signer := types.LatestSignerForChainID(chainID)
	signedTx, err := types.SignTx(tx, signer, s.AuthAcct.PrivateKey)
	if err != nil {
		slog.Default().Error("Failed to sign transaction",
			slog.String("function", "SignTx"),
			slog.Any("error", err))
		return nil, 0, err
	}

	slog.Default().Info("Self ETH transfer transaction created and signed",
		slog.String("tx_hash", signedTx.Hash().Hex()),
		slog.Uint64("block_number", blockNumber))

	return signedTx, blockNumber + offset, nil
}

// ExecuteBlobTransaction executes a blob transaction with preconfirmation bids.
func (s *Service) ExecuteBlobTransaction(numBlobs int, offset uint64) (*types.Transaction, uint64, error) {

	pubKey, ok := s.AuthAcct.PrivateKey.Public().(*ecdsa.PublicKey)
	if !ok || pubKey == nil {
		slog.Default().Error("Failed to cast public key to ECDSA")
		return nil, 0, errors.New("failed to cast public key to ECDSA")
	}

	var (
		gasLimit    = uint64(500_000)
		blockNumber uint64
		nonce       uint64
	)

	// Set a timeout context
	ctx, cancel := context.WithTimeout(context.Background(), s.DefaultTimeout)
	defer cancel()

	privateKey := s.AuthAcct.PrivateKey
	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		slog.Default().Error("Failed to cast public key to ECDSA")
		return nil, 0, errors.New("failed to cast public key to ECDSA")
	}
	fromAddress := crypto.PubkeyToAddress(*publicKeyECDSA)

	nonce, err := s.Client.PendingNonceAt(ctx, s.AuthAcct.Address)
	if err != nil {
		slog.Default().Error("Failed to get pending nonce",
			slog.String("function", "PendingNonceAt"),
			slog.Any("error", err))
		return nil, 0, err
	}

	header, err := s.Client.HeaderByNumber(ctx, nil)
	if err != nil {
		slog.Default().Error("Failed to get latest block header",
			slog.String("function", "HeaderByNumber"),
			slog.Any("error", err))
		return nil, 0, err
	}

	blockNumber = header.Number.Uint64()

	chainID, err := s.Client.NetworkID(ctx)
	if err != nil {
		slog.Default().Error("Failed to get network ID",
			slog.String("function", "NetworkID"),
			slog.Any("error", err))
		return nil, 0, err
	}

	// Calculate the blob fee cap and ensure it is sufficient for transaction replacement
	parentExcessBlobGas := eip4844.CalcExcessBlobGas(*header.ExcessBlobGas, *header.BlobGasUsed)
	blobFeeCap := eip4844.CalcBlobFee(parentExcessBlobGas)
	blobFeeCap.Add(blobFeeCap, big.NewInt(1)) // Ensure it's at least 1 unit higher to replace a transaction

	// Generate random blobs and their corresponding sidecar
	blobs := randBlobs(numBlobs)
	sideCar := makeSidecar(blobs)
	blobHashes := sideCar.BlobHashes()

	// Incrementally increase blob fee cap for replacement
	incrementFactor := big.NewInt(110) // 10% increase
	blobFeeCap.Mul(blobFeeCap, incrementFactor).Div(blobFeeCap, big.NewInt(100))

	baseFee := header.BaseFee
	maxFeePerGas := baseFee
	// Use for nonzero priority fee
	priorityFee := big.NewInt(5_000_000_000) // 5 gwei in wei
	maxFeePriority := new(big.Int).Add(maxFeePerGas, priorityFee)
	// Create a new BlobTx transaction
	tx := types.NewTx(&types.BlobTx{
		ChainID:    uint256.MustFromBig(chainID),
		Nonce:      nonce,
		GasTipCap:  uint256.MustFromBig(priorityFee),
		GasFeeCap:  uint256.MustFromBig(maxFeePriority),
		Gas:        gasLimit,
		To:         fromAddress,
		BlobFeeCap: uint256.MustFromBig(blobFeeCap),
		BlobHashes: blobHashes,
		Sidecar:    sideCar,
	})

	// Create the transaction options with the private key and chain ID
	auth, err := bind.NewKeyedTransactorWithChainID(privateKey, chainID)
	if err != nil {
		slog.Default().Error("Failed to create keyed transactor",
			slog.String("function", "NewKeyedTransactorWithChainID"),
			slog.Any("error", err))
		return nil, 0, err
	}

	// Sign the transaction
	signedTx, err := auth.Signer(auth.From, tx)
	if err != nil {
		slog.Default().Error("Failed to sign blob transaction",
			slog.String("function", "Signer"),
			slog.Any("error", err))
		return nil, 0, err
	}

	slog.Default().Info("Blob transaction created and signed",
		slog.String("tx_hash", signedTx.Hash().Hex()),
		slog.Uint64("block_number", blockNumber),
		slog.Int("num_blobs", numBlobs))

	return signedTx, blockNumber + offset, nil
}

// makeSidecar creates a sidecar for the given blobs by generating commitments and proofs.
func makeSidecar(blobs []kzg4844.Blob) *types.BlobTxSidecar {
	var (
		commitments []kzg4844.Commitment
		proofs      []kzg4844.Proof
	)

	// Generate commitments and proofs for each blob
	for _, blob := range blobs {
		c, _ := kzg4844.BlobToCommitment(&blob)
		p, _ := kzg4844.ComputeBlobProof(&blob, c)

		commitments = append(commitments, c)
		proofs = append(proofs, p)
	}

	return &types.BlobTxSidecar{
		Blobs:       blobs,
		Commitments: commitments,
		Proofs:      proofs,
	}
}

// randBlob generates a single random blob.
func randBlob() kzg4844.Blob {
	var blob kzg4844.Blob
	for i := 0; i < len(blob); i += gokzg4844.SerializedScalarSize {
		fieldElementBytes := randFieldElement()
		copy(blob[i:i+gokzg4844.SerializedScalarSize], fieldElementBytes[:])
	}
	return blob
}

// randBlobs generates a slice of random blobs.
func randBlobs(n int) []kzg4844.Blob {
	blobs := make([]kzg4844.Blob, n)
	for i := 0; i < n; i++ {
		blobs[i] = randBlob()
	}
	return blobs
}

// randFieldElement generates a random field element.
func randFieldElement() [32]byte {
	bytes := make([]byte, 32)
	_, err := rand.Read(bytes)
	if err != nil {
		slog.Default().Error("Failed to generate random field element",
			slog.Any("error", err))
		os.Exit(1)
	}
	var r fr.Element
	r.SetBytes(bytes)

	return gokzg4844.SerializeScalar(r)
}
