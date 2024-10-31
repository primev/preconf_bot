package mevcommit

import (
	"context"
	"io"
	"math/big"
	"strings"
	"testing"

	pb "github.com/primev/preconf_blob_bidder/internal/bidderpb"
	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc/metadata"
)

// MockBidderClient is a mock implementation of BidderInterface.
type MockBidderClient struct {
    mock.Mock
}

func (m *MockBidderClient) SendBid(input interface{}, amount string, blockNumber, decayStart, decayEnd int64) (pb.Bidder_SendBidClient, error) {
    args := m.Called(input, amount, blockNumber, decayStart, decayEnd)
    return args.Get(0).(pb.Bidder_SendBidClient), args.Error(1)
}

// MockBidderSendBidClient is a mock implementation of pb.Bidder_SendBidClient.
type MockBidderSendBidClient struct {
    mock.Mock
}

func (m *MockBidderSendBidClient) Recv() (*pb.Commitment, error) {
    args := m.Called()
    commitment, _ := args.Get(0).(*pb.Commitment)
    return commitment, args.Error(1)
}


func (m *MockBidderSendBidClient) Header() (metadata.MD, error) {
    return nil, nil
}

func (m *MockBidderSendBidClient) Trailer() metadata.MD {
    return nil
}

func (m *MockBidderSendBidClient) CloseSend() error {
    return nil
}

func (m *MockBidderSendBidClient) Context() context.Context {
    return context.Background()
}

func (m *MockBidderSendBidClient) SendMsg(msg interface{}) error {
    return nil
}

func (m *MockBidderSendBidClient) RecvMsg(msg interface{}) error {
    return nil
}

func TestSendPreconfBid(t *testing.T) {
    // Initialize the mock Bidder client
    mockBidder := new(MockBidderClient)
    mockSendBidClient := new(MockBidderSendBidClient)

    bidAmount := 1.0

    // Correctly calculate bidAmountInWei as "1000000000000000000"
    bigEthAmount := big.NewFloat(bidAmount)
    weiPerEth := big.NewFloat(1e18)
    bigWeiAmount := new(big.Float).Mul(bigEthAmount, weiPerEth)
    randomWeiAmount := new(big.Int)
    bigWeiAmount.Int(randomWeiAmount)
    bidAmountInWei := randomWeiAmount.String() // "1000000000000000000"

    // Define the hard-coded legitimate transaction hash
    transactionHash := "0xae0a7a0fd02f7617d815000d6322e564dcaccad49fc0b4cb3084b6c6036c37a2"

    // Expected input and parameters
    expectedInput := []string{strings.TrimPrefix(transactionHash, "0x")} // "ae0a7a0fd02f7617d815000d6322e564dcaccad49fc0b4cb3084b6c6036c37a2"
    expectedAmount := bidAmountInWei
    expectedBlockNumber := int64(100)

    // Setup expectations for SendBid
    mockBidder.On("SendBid",
        expectedInput,
        expectedAmount,
        expectedBlockNumber,
        mock.AnythingOfType("int64"), // decayStart
        mock.AnythingOfType("int64"), // decayEnd
    ).Return(mockSendBidClient, nil)

    // Setup expectations for Recv to return io.EOF (indicating end of response stream)
    mockSendBidClient.On("Recv").Return(nil, io.EOF)

    // Call SendPreconfBid with the transaction hash, block number, and bid amount
    SendPreconfBid(mockBidder, transactionHash, expectedBlockNumber, bidAmount)

    // Assert that all expectations were met
    mockBidder.AssertExpectations(t)
    mockSendBidClient.AssertExpectations(t)
}

func TestUnsupportedInputType(t *testing.T) {
    // Initialize the mock Bidder client
    mockBidder := new(MockBidderClient)

    // No expectations set because SendBid should not be called

    // Call SendPreconfBid with an unsupported input type
    SendPreconfBid(mockBidder, 12345, 100, 1.0)

    // Assert that SendBid was not called
    mockBidder.AssertNotCalled(t, "SendBid", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}
