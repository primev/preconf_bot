// Package mevcommit provides functionality for interacting with the mev-commit protocol,
// including sending bids for blob transactions and saving bid requests and responses.
package mevcommit

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	pb "github.com/primev/preconf_blob_bidder/internal/bidderpb"
	"github.com/rs/zerolog/log"
)

// BidderInterface defines the methods that Bidder and MockBidderClient must implement.
type BidderInterface interface {
	SendBid(input interface{}, amount string, blockNumber, decayStart, decayEnd int64) (pb.Bidder_SendBidClient, error)
}

// SendPreconfBid sends a preconfirmation bid to the bidder client
func SendPreconfBid(bidderClient BidderInterface, input interface{}, blockNumber int64, randomEthAmount float64) {
	// Get current time in milliseconds
	currentTime := time.Now().UnixMilli()

	// Define bid decay start and end
	decayStart := currentTime
	decayEnd := currentTime + int64(time.Duration(36*time.Second).Milliseconds()) // Bid decay is 36 seconds (2 blocks)

	// Convert the random ETH amount to wei (1 ETH = 10^18 wei)
	bigEthAmount := big.NewFloat(randomEthAmount)
	weiPerEth := big.NewFloat(1e18)
	bigWeiAmount := new(big.Float).Mul(bigEthAmount, weiPerEth)

	// Convert big.Float to big.Int
	randomWeiAmount := new(big.Int)
	bigWeiAmount.Int(randomWeiAmount)

	// Convert the amount to a string for the bidder
	amount := randomWeiAmount.String()

	// Determine how to handle the input
	var responseClient pb.Bidder_SendBidClient
	var err error
	switch v := input.(type) {
	case string:
		// Input is a string, process it as a transaction hash
		txHash := strings.TrimPrefix(v, "0x")
		log.Info().
			Str("tx", txHash).
			Msg("Sending bid with transaction hash")
		// Send the bid with tx hash string
		responseClient, err = bidderClient.SendBid([]string{txHash}, amount, blockNumber, decayStart, decayEnd)

	case *types.Transaction:
		// Check for nil transaction
		if v == nil {
			log.Warn().Msg("Transaction is nil, cannot send bid.")
			return
		}
		// Input is a transaction object, send the transaction object
		log.Info().
			Str("tx", v.Hash().String()).
			Msg("Sending bid with transaction payload")
		// Send the bid with the full transaction object
		responseClient, err = bidderClient.SendBid([]*types.Transaction{v}, amount, blockNumber, decayStart, decayEnd)

	default:
		log.Warn().
			Msg("Unsupported input type, must be string or *types.Transaction")
		return
	}

	// Check if there was an error sending the bid
	if err != nil {
		log.Warn().
			Err(err).
			Msg("Failed to send bid")
		return
	}

	// Call Recv() to handle the response and complete the expectation in your tests
	_, recvErr := responseClient.Recv()
	if recvErr == io.EOF {
		log.Info().Msg("Bid response received: EOF")
	} else if recvErr != nil {
		log.Warn().Err(recvErr).Msg("Error receiving bid response")
	} else {
		log.Info().
			Int64("block", blockNumber).
			Float64("amount (ETH)", randomEthAmount).
			Msg("Sent preconfirmation bid and received response")
	}
}

// SendBid method as defined earlier
func (b *Bidder) SendBid(input interface{}, amount string, blockNumber, decayStart, decayEnd int64) (pb.Bidder_SendBidClient, error) {
	// Prepare variables to hold transaction hashes or raw transactions
	var txHashes []string
	var rawTransactions []string

	// Determine the input type and process accordingly
	switch v := input.(type) {
	case []string:
		// If input is a slice of transaction hashes
		txHashes = make([]string, len(v))
		for i, hash := range v {
			txHashes[i] = strings.TrimPrefix(hash, "0x")
		}
	case []*types.Transaction:
		// If input is a slice of *types.Transaction, convert to raw transactions
		rawTransactions = make([]string, len(v))
		for i, tx := range v {
			rlpEncodedTx, err := tx.MarshalBinary()
			if err != nil {
				log.Error().
					Err(err).
					Msg("Failed to marshal transaction to raw format")
				return nil, fmt.Errorf("failed to marshal transaction: %w", err)
			}
			rawTransactions[i] = hex.EncodeToString(rlpEncodedTx)
		}
	default:
		log.Warn().
			Str("inputType", fmt.Sprintf("%T", input)).
			Msg("Unsupported input type, must be []string or []*types.Transaction")
		return nil, fmt.Errorf("unsupported input type: %T", input)
	}

	// Create a new bid request with the appropriate transaction data
	bidRequest := &pb.Bid{
		Amount:              amount,
		BlockNumber:         blockNumber,
		DecayStartTimestamp: decayStart,
		DecayEndTimestamp:   decayEnd,
	}

	if len(txHashes) > 0 {
		bidRequest.TxHashes = txHashes
	} else if len(rawTransactions) > 0 {
		bidRequest.RawTransactions = rawTransactions
	}

	ctx := context.Background()

	// Send the bid request to the mev-commit client
	response, err := b.client.SendBid(ctx, bidRequest)
	if err != nil {
		log.Error().
			Err(err).
			Msg("Failed to send bid")
		return nil, fmt.Errorf("failed to send bid: %w", err)
	}

	// Continuously receive bid responses
	for {
		msg, err := response.Recv()
		if err == io.EOF {
			// End of stream
			break
		}
		if err != nil {
			log.Error().
				Err(err).
				Msg("Failed to receive bid response")
			continue
		}

		log.Info().
			Interface("commitmentDetails", msg).
			Msg("Bid accepted")
	}

	// Timer before saving bid responses
	startTimeBeforeSaveResponses := time.Now()
	log.Info().
		Time("time", startTimeBeforeSaveResponses).
		Msg("End Time")

	return response, nil
}
