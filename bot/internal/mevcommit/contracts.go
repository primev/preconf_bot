// Currently this package is not being used for anything. Leaving in to save the code, but this code has no dependencies on the functionality of the rest of the code. 
package mevcommit

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"strings"
	"time"

	"log/slog"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Global contract addresses
var (
	BidderRegistryAddress common.Address
	BlockTrackerAddress   common.Address
	PreconfManagerAddress common.Address
)

func init() {
	// Load custom environment file if specified, otherwise default to .env
	envFile := os.Getenv("ENV_FILE")
	if envFile == "" {
		envFile = ".env" // default to .env if ENV_FILE is not set
	}

	if _, err := os.Stat(envFile); err == nil {
		if err := loadEnvFile(envFile); err != nil {
			slog.Error("Error loading .env file",
				"err", err,
				"env_file", envFile,
			)
			return
		}
	}

	// Read environment variables with default values
	bidderRegistry := os.Getenv("BIDDER_REGISTRY_ADDRESS")
	if bidderRegistry == "" {
		bidderRegistry = "0x401B3287364f95694c43ACA3252831cAc02e5C41"
	}
	BidderRegistryAddress = common.HexToAddress(bidderRegistry)

	blockTracker := os.Getenv("BLOCK_TRACKER_ADDRESS")
	if blockTracker == "" {
		blockTracker = "0x7538F3AaA07dA1990486De21A0B438F55e9639e4"
	}
	BlockTrackerAddress = common.HexToAddress(blockTracker)

	preconfManager := os.Getenv("PRECONF_MANAGER_ADDRESS")
	if preconfManager == "" {
		preconfManager = "0x9433bCD9e89F923ce587f7FA7E39e120E93eb84D"
	}
	PreconfManagerAddress = common.HexToAddress(preconfManager)

	// // Log loaded contract addresses
	// slog.Info("Loaded contract addresses",
	// 	"BidderRegistry", BidderRegistryAddress.Hex(),
	// 	"BlockTracker", BlockTrackerAddress.Hex(),
	// 	"PreconfManager", PreconfManagerAddress.Hex(),
	// )
}

// loadEnvFile loads environment variables from a specified file.
func loadEnvFile(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		slog.Error("Failed to read environment file",
			"err", err,
			"file_path", filePath,
		)
		return err
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		os.Setenv(key, value)
	}

	// slog.Info("Environment variables loaded from file",
	// 	"file_path", filePath,
	// )

	return nil
}

const defaultTimeout = 15 * time.Second

// CommitmentStoredEvent represents the data structure for the CommitmentStored event.
type CommitmentStoredEvent struct {
	CommitmentIndex     [32]byte
	Bidder              common.Address
	Commiter            common.Address
	Bid                 uint64
	BlockNumber         uint64
	BidHash             [32]byte
	DecayStartTimeStamp uint64
	DecayEndTimeStamp   uint64
	TxnHash             string
	CommitmentHash      [32]byte
	BidSignature        []byte
	CommitmentSignature []byte
	DispatchTimestamp   uint64
	SharedSecretKey     []byte
}

// LoadABI loads the ABI from the specified file path and parses it.
//
// Parameters:
// - filePath: The path to the ABI file to be loaded.
//
// Returns:
// - The parsed ABI object, or an error if loading fails.
func LoadABI(filePath string) (abi.ABI, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		slog.Error("Failed to load ABI file",
			"err", err,
			"file_path", filePath,
		)
		return abi.ABI{}, err
	}

	parsedABI, err := abi.JSON(strings.NewReader(string(data)))
	if err != nil {
		slog.Error("Failed to parse ABI file",
			"err", err,
			"file_path", filePath,
		)
		return abi.ABI{}, err
	}

	slog.Info("ABI file loaded and parsed successfully",
		"file_path", filePath,
	)

	return parsedABI, nil
}

// WindowHeight retrieves the current bidding window height from the BlockTracker contract.
//
// Parameters:
// - client: The Ethereum client instance.
//
// Returns:
// - The current window height as a big.Int, or an error if the call fails.
func WindowHeight(client *ethclient.Client) (*big.Int, error) {
	// Load the BlockTracker contract ABI
	blockTrackerABI, err := LoadABI("abi/BlockTracker.abi")
	if err != nil {
		return nil, fmt.Errorf("failed to load ABI file: %v", err)
	}

	// Bind the contract to the client
	blockTrackerContract := bind.NewBoundContract(BlockTrackerAddress, blockTrackerABI, client, client, client)

	// Call the getCurrentWindow function to retrieve the current window height
	var currentWindowResult []interface{}
	err = blockTrackerContract.Call(nil, &currentWindowResult, "getCurrentWindow")
	if err != nil {
		slog.Error("Failed to get current window",
			"err", err,
			"function", "getCurrentWindow",
		)
		return nil, fmt.Errorf("failed to get current window: %v", err)
	}

	// Extract the current window as *big.Int
	currentWindow, ok := currentWindowResult[0].(*big.Int)
	if !ok {
		slog.Error("Failed to convert current window to *big.Int")
		return nil, fmt.Errorf("conversion to *big.Int failed")
	}

	slog.Info("Retrieved current bidding window height",
		"current_window", currentWindow.String(),
	)

	return currentWindow, nil
}

// GetMinDeposit retrieves the minimum deposit required for participating in the bidding window.
//
// Parameters:
// - client: The Ethereum client instance.
//
// Returns:
// - The minimum deposit as a big.Int, or an error if the call fails.
func GetMinDeposit(client *ethclient.Client) (*big.Int, error) {
	// Load the BidderRegistry contract ABI
	bidderRegistryABI, err := LoadABI("abi/BidderRegistry.abi")
	if err != nil {
		return nil, fmt.Errorf("failed to load ABI file: %v", err)
	}

	// Bind the contract to the client
	bidderRegistryContract := bind.NewBoundContract(BidderRegistryAddress, bidderRegistryABI, client, client, client)

	// Call the minDeposit function to get the minimum deposit amount
	var minDepositResult []interface{}
	err = bidderRegistryContract.Call(nil, &minDepositResult, "minDeposit")
	if err != nil {
		slog.Error("Failed to call minDeposit function",
			"err", err,
			"function", "minDeposit",
		)
		return nil, fmt.Errorf("failed to call minDeposit function: %v", err)
	}

	// Extract the minDeposit as *big.Int
	minDeposit, ok := minDepositResult[0].(*big.Int)
	if !ok {
		slog.Error("Failed to convert minDeposit to *big.Int")
		return nil, fmt.Errorf("failed to convert minDeposit to *big.Int")
	}

	slog.Info("Retrieved minimum deposit amount",
		"min_deposit", minDeposit.String(),
	)

	return minDeposit, nil
}

// DepositIntoWindow deposits the minimum bid amount into the specified bidding window.
//
// Parameters:
// - client: The Ethereum client instance.
// - depositWindow: The window into which the deposit should be made.
// - authAcct: The authenticated account struct containing transaction authorization.
//
// Returns:
// - The transaction object if successful, or an error if the transaction fails.
func DepositIntoWindow(client *ethclient.Client, depositWindow *big.Int, authAcct *AuthAcct) (*types.Transaction, error) {
	// Load the BidderRegistry contract ABI
	bidderRegistryABI, err := LoadABI("abi/BidderRegistry.abi")
	if err != nil {
		return nil, fmt.Errorf("failed to load ABI file: %v", err)
	}

	// Bind the contract to the client
	bidderRegistryContract := bind.NewBoundContract(BidderRegistryAddress, bidderRegistryABI, client, client, client)

	// Retrieve the minimum deposit amount
	minDeposit, err := GetMinDeposit(client)
	if err != nil {
		return nil, fmt.Errorf("failed to get minDeposit: %v", err)
	}

	// Set the value for the transaction to the minimum deposit amount
	authAcct.Auth.Value = minDeposit

	// Prepare and send the transaction to deposit into the specific window
	tx, err := bidderRegistryContract.Transact(authAcct.Auth, "depositForSpecificWindow", depositWindow)
	if err != nil {
		slog.Error("Failed to create deposit transaction",
			"err", err,
			"function", "depositForSpecificWindow",
		)
		return nil, fmt.Errorf("failed to create transaction: %v", err)
	}

	slog.Info("Deposit transaction sent",
		"tx_hash", tx.Hash().Hex(),
		"window", depositWindow.String(),
	)

	// Wait for the transaction to be mined (optional)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()
	receipt, err := bind.WaitMined(ctx, client, tx)
	if err != nil {
		slog.Error("Transaction mining error",
			"err", err,
			"tx_hash", tx.Hash().Hex(),
		)
		return nil, fmt.Errorf("transaction mining error: %v", err)
	}

	// Check the transaction status
	if receipt.Status == 1 {
		slog.Info("Deposit transaction successful",
			"tx_hash", tx.Hash().Hex(),
		)
		return tx, nil
	} else {
		slog.Error("Deposit transaction failed",
			"tx_hash", tx.Hash().Hex(),
		)
		return nil, fmt.Errorf("transaction failed")
	}
}

// GetDepositAmount retrieves the deposit amount for a given address and window.
//
// Parameters:
// - client: The Ethereum client instance.
// - address: The Ethereum address to query the deposit for.
// - window: The bidding window to query the deposit for.
//
// Returns:
// - The deposit amount as a big.Int, or an error if the call fails.
func GetDepositAmount(client *ethclient.Client, address common.Address, window big.Int) (*big.Int, error) {
	// Load the BidderRegistry contract ABI
	bidderRegistryABI, err := LoadABI("abi/BidderRegistry.abi")
	if err != nil {
		return nil, fmt.Errorf("failed to load ABI file: %v", err)
	}

	// Bind the contract to the client
	bidderRegistryContract := bind.NewBoundContract(BidderRegistryAddress, bidderRegistryABI, client, client, client)

	// Call the getDeposit function to retrieve the deposit amount
	var depositResult []interface{}
	err = bidderRegistryContract.Call(nil, &depositResult, "getDeposit", address, window)
	if err != nil {
		slog.Error("Failed to call getDeposit function",
			"err", err,
			"function", "getDeposit",
		)
		return nil, fmt.Errorf("failed to call getDeposit function: %v", err)
	}

	// Extract the deposit amount as *big.Int
	depositAmount, ok := depositResult[0].(*big.Int)
	if !ok {
		slog.Error("Failed to convert deposit amount to *big.Int")
		return nil, fmt.Errorf("failed to convert deposit amount to *big.Int")
	}

	slog.Info("Retrieved deposit amount for address and window",
		"deposit_amount", depositAmount.String(),
	)

	return depositAmount, nil
}

// WithdrawFromWindow withdraws all funds from the specified bidding window.
//
// Parameters:
// - client: The Ethereum client instance.
// - authAcct: The authenticated account struct containing transaction authorization.
// - window: The window from which to withdraw funds.
//
// Returns:
// - The transaction object if successful, or an error if the transaction fails.
func WithdrawFromWindow(client *ethclient.Client, authAcct *AuthAcct, window *big.Int) (*types.Transaction, error) {
	// Load the BidderRegistry contract ABI
	bidderRegistryABI, err := LoadABI("abi/BidderRegistry.abi")
	if err != nil {
		return nil, fmt.Errorf("failed to load ABI file: %v", err)
	}

	// Bind the contract to the client
	bidderRegistryContract := bind.NewBoundContract(BidderRegistryAddress, bidderRegistryABI, client, client, client)

	// Prepare the withdrawal transaction
	withdrawalTx, err := bidderRegistryContract.Transact(authAcct.Auth, "withdrawBidderAmountFromWindow", authAcct.Address, window)
	if err != nil {
		slog.Error("Failed to create withdrawal transaction",
			"err", err,
			"function", "withdrawBidderAmountFromWindow",
		)
		return nil, fmt.Errorf("failed to create withdrawal transaction: %v", err)
	}

	slog.Info("Withdrawal transaction sent",
		"tx_hash", withdrawalTx.Hash().Hex(),
		"window", window.String(),
	)

	// Wait for the withdrawal transaction to be mined
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()
	withdrawalReceipt, err := bind.WaitMined(ctx, client, withdrawalTx)
	if err != nil {
		slog.Error("Withdrawal transaction mining error",
			"err", err,
			"tx_hash", withdrawalTx.Hash().Hex(),
		)
		return nil, fmt.Errorf("withdrawal transaction mining error: %v", err)
	}

	// Check the withdrawal transaction status
	if withdrawalReceipt.Status == 1 {
		slog.Info("Withdrawal transaction successful",
			"tx_hash", withdrawalTx.Hash().Hex(),
		)
		return withdrawalTx, nil
	} else {
		slog.Error("Withdrawal transaction failed",
			"tx_hash", withdrawalTx.Hash().Hex(),
		)
		return nil, fmt.Errorf("withdrawal failed")
	}
}

// ListenForCommitmentStoredEvent listens for the CommitmentStored event on the Ethereum blockchain.
// This function will log event details when the CommitmentStored event is detected.
//
// Parameters:
// - client: The Ethereum client instance.
//
// Note: The event listener uses a timeout of 15 seconds for subscription.
func ListenForCommitmentStoredEvent(client *ethclient.Client) {
	// Load the PreConfCommitmentStore contract ABI
	contractAbi, err := LoadABI("abi/PreConfCommitmentStore.abi")
	if err != nil {
		slog.Error("Failed to load contract ABI",
			"contract", "PreConfCommitmentStore",
			"err", err,
		)
		return
	}

	// Create a parent context that can be canceled to stop all operations
	parentCtx, parentCancel := context.WithCancel(context.Background())
	defer parentCancel()

	// Subscribe to new block headers
	headers := make(chan *types.Header)
	sub, err := client.SubscribeNewHead(parentCtx, headers)
	if err != nil {
		slog.Error("Failed to subscribe to new block headers",
			"err", err,
		)
		return
	}

	slog.Info("Subscribed to new block headers for CommitmentStored events")

	// Listen for new block headers and filter logs for the CommitmentStored event
	for {
		select {
		case err := <-sub.Err():
			slog.Error("Error with header subscription",
				"err", err,
			)
			// Cancel the parent context to terminate all ongoing log subscriptions
			parentCancel()
			return

		case header := <-headers:
			query := ethereum.FilterQuery{
				Addresses: []common.Address{PreconfManagerAddress},
				FromBlock: header.Number,
				ToBlock:   header.Number,
			}

			logs := make(chan types.Log)
			ctxLogs, cancelLogs := context.WithTimeout(parentCtx, defaultTimeout)

			// Subscribe to filter logs with the derived context
			subLogs, err := client.SubscribeFilterLogs(ctxLogs, query, logs)
			if err != nil {
				slog.Error("Failed to subscribe to logs",
					"err", err,
				)
				// Ensure cancelLogs is called to release resources
				cancelLogs()
				continue
			}

			// Process incoming logs in a separate goroutine
			go func() {
				// Ensure cancelLogs is called when the goroutine exits
				defer cancelLogs()

				for {
					select {
					case err := <-subLogs.Err():
						slog.Error("Error with log subscription",
							"err", err,
						)
						return

					case vLog := <-logs:
						var event CommitmentStoredEvent

						// Unpack the log data into the CommitmentStoredEvent struct
						err := contractAbi.UnpackIntoInterface(&event, "CommitmentStored", vLog.Data)
						if err != nil {
							slog.Error("Failed to unpack log data",
								"err", err,
							)
							continue
						}

						// Log event details
						slog.Info("CommitmentStored Event Detected",
							"commitment_index", fmt.Sprintf("%x", event.CommitmentIndex),
							"bidder", event.Bidder.Hex(),
							"commiter", event.Commiter.Hex(),
							"bid", event.Bid,
							"block_number", event.BlockNumber,
							"bid_hash", fmt.Sprintf("%x", event.BidHash),
							"decay_start_timestamp", event.DecayStartTimeStamp,
							"decay_end_timestamp", event.DecayEndTimeStamp,
							"txn_hash", event.TxnHash,
							"commitment_hash", fmt.Sprintf("%x", event.CommitmentHash),
							"bid_signature", fmt.Sprintf("%x", event.BidSignature),
							"commitment_signature", fmt.Sprintf("%x", event.CommitmentSignature),
							"dispatch_timestamp", event.DispatchTimestamp,
							"shared_secret_key", fmt.Sprintf("%x", event.SharedSecretKey),
						)
					}
				}
			}()
		}
	}
}
