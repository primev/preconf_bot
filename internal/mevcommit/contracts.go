// Package mevcommit provides functionality for interacting with the mev-commit protocol,
// including managing bids, deposits, withdrawals, and event listeners on the Ethereum blockchain.
package mevcommit

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"os"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Contract addresses used within the mev-commit protocol.
const (
	// latest contracts as of v0.6.1
	bidderRegistryAddress         = "0x401B3287364f95694c43ACA3252831cAc02e5C41"
	blockTrackerAddress           = "0x7538F3AaA07dA1990486De21A0B438F55e9639e4"
	PreconfManagerAddress 		  = "0x9433bCD9e89F923ce587f7FA7E39e120E93eb84D"
)

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
		log.Println("Failed to load ABI file:", err)
		return abi.ABI{}, err
	}

	parsedABI, err := abi.JSON(strings.NewReader(string(data)))
	if err != nil {
		log.Println("Failed to parse ABI file:", err)
		return abi.ABI{}, err
	}

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
		log.Println("Failed to load ABI file:", err)
		return nil, err
	}

	// Bind the contract to the client
	blockTrackerContract := bind.NewBoundContract(common.HexToAddress(blockTrackerAddress), blockTrackerABI, client, client, client)

	// Call the getCurrentWindow function to retrieve the current window height
	var currentWindowResult []interface{}
	err = blockTrackerContract.Call(nil, &currentWindowResult, "getCurrentWindow")
	if err != nil {
		log.Println("Failed to get current window:", err)
		return nil, err
	}

	// Extract the current window as *big.Int
	currentWindow, ok := currentWindowResult[0].(*big.Int)
	if !ok {
		log.Println("Failed to convert current window to *big.Int")
		return nil, fmt.Errorf("conversion to *big.Int failed")
	}

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
	bidderRegistryContract := bind.NewBoundContract(common.HexToAddress(bidderRegistryAddress), bidderRegistryABI, client, client, client)

	// Call the minDeposit function to get the minimum deposit amount
	var minDepositResult []interface{}
	err = bidderRegistryContract.Call(nil, &minDepositResult, "minDeposit")
	if err != nil {
		return nil, fmt.Errorf("failed to call minDeposit function: %v", err)
	}

	// Extract the minDeposit as *big.Int
	minDeposit, ok := minDepositResult[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("failed to convert minDeposit to *big.Int")
	}

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
	bidderRegistryContract := bind.NewBoundContract(common.HexToAddress(bidderRegistryAddress), bidderRegistryABI, client, client, client)

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
		return nil, fmt.Errorf("failed to create transaction: %v", err)
	}

	// Wait for the transaction to be mined (optional)
	receipt, err := bind.WaitMined(context.Background(), client, tx)
	if err != nil {
		return nil, fmt.Errorf("transaction mining error: %v", err)
	}

	// Check the transaction status
	if receipt.Status == 1 {
		fmt.Println("Transaction successful")
		return tx, nil
	} else {
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
	bidderRegistryContract := bind.NewBoundContract(common.HexToAddress(bidderRegistryAddress), bidderRegistryABI, client, client, client)

	// Call the getDeposit function to retrieve the deposit amount
	var depositResult []interface{}
	err = bidderRegistryContract.Call(nil, &depositResult, "getDeposit", address, window)
	if err != nil {
		return nil, fmt.Errorf("failed to call getDeposit function: %v", err)
	}

	// Extract the deposit amount as *big.Int
	depositAmount, ok := depositResult[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("failed to convert deposit amount to *big.Int")
	}

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
	bidderRegistryContract := bind.NewBoundContract(common.HexToAddress(bidderRegistryAddress), bidderRegistryABI, client, client, client)

	// Prepare the withdrawal transaction
	withdrawalTx, err := bidderRegistryContract.Transact(authAcct.Auth, "withdrawBidderAmountFromWindow", authAcct.Address, window)
	if err != nil {
		return nil, fmt.Errorf("failed to create withdrawal transaction: %v", err)
	}

	// Wait for the withdrawal transaction to be mined
	withdrawalReceipt, err := bind.WaitMined(context.Background(), client, withdrawalTx)
	if err != nil {
		return nil, fmt.Errorf("withdrawal transaction mining error: %v", err)
	}

	// Check the withdrawal transaction status
	if withdrawalReceipt.Status == 1 {
		fmt.Println("Withdrawal successful")
		return withdrawalTx, nil
	} else {
		return nil, fmt.Errorf("withdrawal failed")
	}
}

// ListenForCommitmentStoredEvent listens for the CommitmentStored event on the Ethereum blockchain.
// This function will print event details when the CommitmentStored event is detected.
//
// Parameters:
// - client: The Ethereum client instance.
//
// Note: The event listener is not currently functioning correctly (as per the TODO comment).
func ListenForCommitmentStoredEvent(client *ethclient.Client) {
	// Load the PreConfCommitmentStore contract ABI
	contractAbi, err := LoadABI("abi/PreConfCommitmentStore.abi")
	if err != nil {
		log.Fatalf("Failed to load contract ABI: %v", err)
	}

	// Subscribe to new block headers
	headers := make(chan *types.Header)
	sub, err := client.SubscribeNewHead(context.Background(), headers)
	if err != nil {
		log.Fatalf("Failed to subscribe to new head: %v", err)
	}

	// Listen for new block headers and filter logs for the CommitmentStored event
	for {
		select {
		case err := <-sub.Err():
			log.Fatalf("Error with header subscription: %v", err)
		case header := <-headers:
			query := ethereum.FilterQuery{
				Addresses: []common.Address{common.HexToAddress(PreconfManagerAddress)},
				FromBlock: header.Number,
				ToBlock:   header.Number,
			}

			logs := make(chan types.Log)
			subLogs, err := client.SubscribeFilterLogs(context.Background(), query, logs)
			if err != nil {
				log.Printf("Failed to subscribe to logs: %v", err)
				continue
			}

			// Process incoming logs
			for {
				select {
				case err := <-subLogs.Err():
					log.Printf("Error with log subscription: %v", err)
					break
				case vLog := <-logs:
					var event CommitmentStoredEvent

					// Unpack the log data into the CommitmentStoredEvent struct
					err := contractAbi.UnpackIntoInterface(&event, "CommitmentStored", vLog.Data)
					if err != nil {
						log.Printf("Failed to unpack log data: %v", err)
						continue
					}

					// Print event details
					fmt.Printf("CommitmentStored Event: \n")
					fmt.Printf("CommitmentIndex: %x\n", event.CommitmentIndex)
					fmt.Printf("Bidder: %s\n", event.Bidder.Hex())
					fmt.Printf("Commiter: %s\n", event.Commiter.Hex())
					fmt.Printf("Bid: %d\n", event.Bid)
					fmt.Printf("BlockNumber: %d\n", event.BlockNumber)
					fmt.Printf("BidHash: %x\n", event.BidHash)
					fmt.Printf("DecayStartTimeStamp: %d\n", event.DecayStartTimeStamp)
					fmt.Printf("DecayEndTimeStamp: %d\n", event.DecayEndTimeStamp)
					fmt.Printf("TxnHash: %s\n", event.TxnHash)
					fmt.Printf("CommitmentHash: %x\n", event.CommitmentHash)
					fmt.Printf("BidSignature: %x\n", event.BidSignature)
					fmt.Printf("CommitmentSignature: %x\n", event.CommitmentSignature)
					fmt.Printf("DispatchTimestamp: %d\n", event.DispatchTimestamp)
					fmt.Printf("SharedSecretKey: %x\n", event.SharedSecretKey)
				}
			}
		}
	}
}
