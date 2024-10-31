// Package mevcommit provides functionality for interacting with the mev-commit protocol,
// including managing bids, deposits, withdrawals, and event listeners on the Ethereum blockchain.
package mevcommit

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/rs/zerolog/log"
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
			log.Fatal().
				Err(err).
				Str("env_file", envFile).
				Msg("Error loading .env file")
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

	// Log loaded contract addresses
	log.Info().
		Str("BidderRegistry", BidderRegistryAddress.Hex()).
		Str("BlockTracker", BlockTrackerAddress.Hex()).
		Str("PreconfManager", PreconfManagerAddress.Hex()).
		Msg("Loaded contract addresses")
}

// loadEnvFile loads environment variables from a specified file.
func loadEnvFile(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		log.Error().
			Err(err).
			Str("file_path", filePath).
			Msg("Failed to read environment file")
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

	log.Info().
		Str("file_path", filePath).
		Msg("Environment variables loaded from file")

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
		log.Error().
			Err(err).
			Str("file_path", filePath).
			Msg("Failed to load ABI file")
		return abi.ABI{}, err
	}

	parsedABI, err := abi.JSON(strings.NewReader(string(data)))
	if err != nil {
		log.Error().
			Err(err).
			Str("file_path", filePath).
			Msg("Failed to parse ABI file")
		return abi.ABI{}, err
	}

	log.Info().
		Str("file_path", filePath).
		Msg("ABI file loaded and parsed successfully")

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
		log.Error().
			Err(err).
			Str("function", "getCurrentWindow").
			Msg("Failed to get current window")
		return nil, fmt.Errorf("failed to get current window: %v", err)
	}

	// Extract the current window as *big.Int
	currentWindow, ok := currentWindowResult[0].(*big.Int)
	if !ok {
		log.Error().
			Msg("Failed to convert current window to *big.Int")
		return nil, fmt.Errorf("conversion to *big.Int failed")
	}

	log.Info().
		Str("current_window", currentWindow.String()).
		Msg("Retrieved current bidding window height")

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
		log.Error().
			Err(err).
			Str("function", "minDeposit").
			Msg("Failed to call minDeposit function")
		return nil, fmt.Errorf("failed to call minDeposit function: %v", err)
	}

	// Extract the minDeposit as *big.Int
	minDeposit, ok := minDepositResult[0].(*big.Int)
	if !ok {
		log.Error().
			Msg("Failed to convert minDeposit to *big.Int")
		return nil, fmt.Errorf("failed to convert minDeposit to *big.Int")
	}

	log.Info().
		Str("min_deposit", minDeposit.String()).
		Msg("Retrieved minimum deposit amount")

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
		log.Error().
			Err(err).
			Str("function", "depositForSpecificWindow").
			Msg("Failed to create deposit transaction")
		return nil, fmt.Errorf("failed to create transaction: %v", err)
	}

	log.Info().
		Str("tx_hash", tx.Hash().Hex()).
		Str("window", depositWindow.String()).
		Msg("Deposit transaction sent")

	// Wait for the transaction to be mined (optional)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()
	receipt, err := bind.WaitMined(ctx, client, tx)
	if err != nil {
		log.Error().
			Err(err).
			Str("tx_hash", tx.Hash().Hex()).
			Msg("Transaction mining error")
		return nil, fmt.Errorf("transaction mining error: %v", err)
	}

	// Check the transaction status
	if receipt.Status == 1 {
		log.Info().
			Str("tx_hash", tx.Hash().Hex()).
			Msg("Deposit transaction successful")
		return tx, nil
	} else {
		log.Error().
			Str("tx_hash", tx.Hash().Hex()).
			Msg("Deposit transaction failed")
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
		log.Error().
			Err(err).
			Str("function", "getDeposit").
			Msg("Failed to call getDeposit function")
		return nil, fmt.Errorf("failed to call getDeposit function: %v", err)
	}

	// Extract the deposit amount as *big.Int
	depositAmount, ok := depositResult[0].(*big.Int)
	if !ok {
		log.Error().
			Msg("Failed to convert deposit amount to *big.Int")
		return nil, fmt.Errorf("failed to convert deposit amount to *big.Int")
	}

	log.Info().
		Str("deposit_amount", depositAmount.String()).
		Msg("Retrieved deposit amount for address and window")

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
		log.Error().
			Err(err).
			Str("function", "withdrawBidderAmountFromWindow").
			Msg("Failed to create withdrawal transaction")
		return nil, fmt.Errorf("failed to create withdrawal transaction: %v", err)
	}

	log.Info().
		Str("tx_hash", withdrawalTx.Hash().Hex()).
		Str("window", window.String()).
		Msg("Withdrawal transaction sent")

	// Wait for the withdrawal transaction to be mined
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()
	withdrawalReceipt, err := bind.WaitMined(ctx, client, withdrawalTx)
	if err != nil {
		log.Error().
			Err(err).
			Str("tx_hash", withdrawalTx.Hash().Hex()).
			Msg("Withdrawal transaction mining error")
		return nil, fmt.Errorf("withdrawal transaction mining error: %v", err)
	}

	// Check the withdrawal transaction status
	if withdrawalReceipt.Status == 1 {
		log.Info().
			Str("tx_hash", withdrawalTx.Hash().Hex()).
			Msg("Withdrawal transaction successful")
		return withdrawalTx, nil
	} else {
		log.Error().
			Str("tx_hash", withdrawalTx.Hash().Hex()).
			Msg("Withdrawal transaction failed")
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
        log.Fatal().
            Err(err).
            Str("contract", "PreConfCommitmentStore").
            Msg("Failed to load contract ABI")
    }

    // Create a parent context that can be canceled to stop all operations
    parentCtx, parentCancel := context.WithCancel(context.Background())
    defer parentCancel()

    // Subscribe to new block headers
    headers := make(chan *types.Header)
    sub, err := client.SubscribeNewHead(parentCtx, headers)
    if err != nil {
        log.Fatal().
            Err(err).
            Msg("Failed to subscribe to new block headers")
    }

    log.Info().
        Msg("Subscribed to new block headers for CommitmentStored events")

    // Listen for new block headers and filter logs for the CommitmentStored event
    for {
        select {
        case err := <-sub.Err():
            log.Error().
                Err(err).
                Msg("Error with header subscription")
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
                log.Error().
                    Err(err).
                    Msg("Failed to subscribe to logs")
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
                        log.Error().
                            Err(err).
                            Msg("Error with log subscription")
                        return

                    case vLog := <-logs:
                        var event CommitmentStoredEvent

                        // Unpack the log data into the CommitmentStoredEvent struct
                        err := contractAbi.UnpackIntoInterface(&event, "CommitmentStored", vLog.Data)
                        if err != nil {
                            log.Error().
                                Err(err).
                                Msg("Failed to unpack log data")
                            continue
                        }

                        // Log event details
                        log.Info().
                            Str("commitment_index", fmt.Sprintf("%x", event.CommitmentIndex)).
                            Str("bidder", event.Bidder.Hex()).
                            Str("commiter", event.Commiter.Hex()).
                            Uint64("bid", event.Bid).
                            Uint64("block_number", event.BlockNumber).
                            Str("bid_hash", fmt.Sprintf("%x", event.BidHash)).
                            Uint64("decay_start_timestamp", event.DecayStartTimeStamp).
                            Uint64("decay_end_timestamp", event.DecayEndTimeStamp).
                            Str("txn_hash", event.TxnHash).
                            Str("commitment_hash", fmt.Sprintf("%x", event.CommitmentHash)).
                            Str("bid_signature", fmt.Sprintf("%x", event.BidSignature)).
                            Str("commitment_signature", fmt.Sprintf("%x", event.CommitmentSignature)).
                            Uint64("dispatch_timestamp", event.DispatchTimestamp).
                            Str("shared_secret_key", fmt.Sprintf("%x", event.SharedSecretKey)).
                            Msg("CommitmentStored Event Detected")
                    }
                }
            }()
        }
    }
}
