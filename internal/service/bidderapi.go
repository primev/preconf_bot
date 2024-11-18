package service

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"math/big"
	"strings"
	"time"

	"log/slog"

	"github.com/ethereum/go-ethereum/core/types"
	pb "github.com/primev/preconf_blob_bidder/internal/bidderpb"
	"google.golang.org/grpc"
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

// BidderInterface defines the methods that Bidder and MockBidderClient must implement.
type BidderInterface interface {
	SendBid(input interface{}, amount string, blockNumber, decayStart, decayEnd int64) (pb.Bidder_SendBidClient, error)
}

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

// SendPreconfBid sends a preconfirmation bid to the bidder client
func (b *Bidder) SendPreconfBid(bidderClient BidderInterface, input interface{}, blockNumber int64, randomEthAmount float64) {
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
		slog.Info("Sending bid with transaction hash",
			"txHash", txHash,
			"amount", amount,
			"blockNumber", blockNumber,
			"decayStart", decayStart,
			"decayEnd", decayEnd,
		)
		// Send the bid with tx hash string
		responseClient, err = bidderClient.SendBid([]string{txHash}, amount, blockNumber, decayStart, decayEnd)

	case *types.Transaction:
		// Check for nil transaction
		if v == nil {
			slog.Warn("Transaction is nil, cannot send bid.")
			return
		}
		// Input is a transaction object, send the transaction object
		slog.Info("Sending bid with transaction payload",
			"txHash", v.Hash().String(),
			"amount", amount,
			"blockNumber", blockNumber,
			"decayStart", decayStart,
			"decayEnd", decayEnd,
		)
		// Send the bid with the full transaction object
		responseClient, err = bidderClient.SendBid([]*types.Transaction{v}, amount, blockNumber, decayStart, decayEnd)

	default:
		slog.Warn("Unsupported input type, must be string or *types.Transaction",
			"inputType", fmt.Sprintf("%T", input),
		)
		return
	}

	// Check if there was an error sending the bid
	if err != nil {
		slog.Warn("Failed to send bid",
			"err", err,
			"txHash", fmt.Sprintf("%v", input),
			"amount", amount,
			"blockNumber", blockNumber,
			"decayStart", decayStart,
			"decayEnd", decayEnd,
		)
		return
	}

	// Call Recv() to handle the response and complete the expectation in your tests
	_, recvErr := responseClient.Recv()
	if recvErr == io.EOF {
		slog.Info("Bid response received: EOF",
			"txHash", fmt.Sprintf("%v", input),
			"blockNumber", blockNumber,
			"amount_ETH", randomEthAmount,
			"decayStart", decayStart,
			"decayEnd", decayEnd,
		)
	} else if recvErr != nil {
		slog.Warn("Error receiving bid response",
			"err", recvErr,
			"txHash", fmt.Sprintf("%v", input),
			"blockNumber", blockNumber,
			"decayStart", decayStart,
			"decayEnd", decayEnd,
		)
	} else {
		slog.Info("Sent preconfirmation bid and received response",
			"block", blockNumber,
			"amount_ETH", randomEthAmount,
			"decayStart", decayStart,
			"decayEnd", decayEnd,
		)
	}
}

// SendBid handles sending a bid request after preparing the input data.
func (b *Bidder) SendBid(input interface{}, amount string, blockNumber, decayStart, decayEnd int64) (pb.Bidder_SendBidClient, error) {
	txHashes, rawTransactions, err := b.parseInput(input)
	if err != nil {
		return nil, err
	}

	bidRequest := b.createBidRequest(amount, blockNumber, decayStart, decayEnd, txHashes, rawTransactions)

	response, err := b.sendBidRequest(bidRequest)
	if err != nil {
		return nil, err
	}

	b.receiveBidResponses(response)

	return response, nil
}

// parseInput processes the input and converts it to either transaction hashes or raw transactions.
func (b *Bidder) parseInput(input interface{}) ([]string, []string, error) {
	var txHashes []string
	var rawTransactions []string

	switch v := input.(type) {
	case []string:
		txHashes = make([]string, len(v))
		for i, hash := range v {
			txHashes[i] = strings.TrimPrefix(hash, "0x")
		}
	case []*types.Transaction:
		rawTransactions = make([]string, len(v))
		for i, tx := range v {
			rlpEncodedTx, err := tx.MarshalBinary()
			if err != nil {
				slog.Error("Failed to marshal transaction to raw format",
					"err", err,
				)
				return nil, nil, fmt.Errorf("failed to marshal transaction: %w", err)
			}
			rawTransactions[i] = hex.EncodeToString(rlpEncodedTx)
		}
	default:
		slog.Warn("Unsupported input type, must be []string or []*types.Transaction",
			"inputType", fmt.Sprintf("%T", input),
		)
		return nil, nil, fmt.Errorf("unsupported input type: %T", input)
	}

	return txHashes, rawTransactions, nil
}

// createBidRequest builds a Bid request using the provided data.
func (b *Bidder) createBidRequest(amount string, blockNumber, decayStart, decayEnd int64, txHashes, rawTransactions []string) *pb.Bid {
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

	return bidRequest
}

// sendBidRequest sends the prepared bid request to the mev-commit client.
func (b *Bidder) sendBidRequest(bidRequest *pb.Bid) (pb.Bidder_SendBidClient, error) {
	ctx := context.Background()
	response, err := b.client.SendBid(ctx, bidRequest)
	if err != nil {
		slog.Error("Failed to send bid",
			"err", err,
		)
		return nil, fmt.Errorf("failed to send bid: %w", err)
	}

	return response, nil
}

// receiveBidResponses processes the responses from the bid request.
func (b *Bidder) receiveBidResponses(response pb.Bidder_SendBidClient) {
	for {
		msg, err := response.Recv()
		if err == io.EOF {
			// End of stream
			break
		}
		if err != nil {
			slog.Error("Failed to receive bid response",
				"err", err,
			)
			continue
		}

		slog.Info("Bid accepted",
			"commitmentDetails", msg,
		)
	}

	startTimeBeforeSaveResponses := time.Now()
	slog.Info("End Time",
		"time", startTimeBeforeSaveResponses,
	)
}
