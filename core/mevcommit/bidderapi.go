// Package mevcommit provides functionality for interacting with the mev-commit protocol,
// including sending bids for blob transactions and saving bid requests and responses.
package mevcommit

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	pb "github.com/primev/preconf_blob_bidder/core/bidderpb"
)

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
				log.Error("Failed to marshal transaction to raw format", "error", err)
				return nil, fmt.Errorf("failed to marshal transaction: %w", err)
			}
			rawTransactions[i] = hex.EncodeToString(rlpEncodedTx)
		}
	default:
		log.Warn("Unsupported input type, must be []string or []*types.Transaction")
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
		// Convert rawTransactions to []string
		rawTxStrings := make([]string, len(rawTransactions))
		for i, rawTx := range rawTransactions {
			rawTxStrings[i] = string(rawTx)
		}
		bidRequest.RawTransactions = rawTxStrings
	}

	ctx := context.Background()

	// Send the bid request to the mev-commit client
	response, err := b.client.SendBid(ctx, bidRequest)
	if err != nil {
		log.Error("Failed to send bid", "error", err)
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
			log.Error("Failed to receive bid response", "error", err)
		}

		log.Info("Bid accepted", "commitment details", msg)
	}

	// Timer before saving bid responses
	startTimeBeforeSaveResponses := time.Now()
	log.Info("End Time", "time", startTimeBeforeSaveResponses)

	return response, nil
}