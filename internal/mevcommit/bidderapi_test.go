package mevcommit

import (
	"context"
	"errors"
	"io"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
	pb "github.com/primev/mev-commit/p2p/gen/go/bidderapi/v1"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
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

// Define the custom mock transaction type outside of the test function
type MockTransaction struct {
    types.Transaction
    mock.Mock
}

// Define the MarshalBinary method outside the test function
func (m *MockTransaction) MarshalBinary() ([]byte, error) {
    args := m.Called()
    return args.Get(0).([]byte), args.Error(1)
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

func TestSendBidWithTxHashes(t *testing.T) {
    // Initialize the mock Bidder client
    mockBidder := new(MockBidderClient)
    mockSendBidClient := new(MockBidderSendBidClient)

	// Setup parameters for SendBid with txHashes
	transactionHashes := []string{"0x1234567890abcdef", "0xfedcba0987654321"}

	// Remove "0x" prefix from each transaction hash
	for i, tx := range transactionHashes {
		transactionHashes[i] = strings.TrimPrefix(tx, "0x")
	}

	expectedTxHashes := []string{"1234567890abcdef", "fedcba0987654321"}
	expectedAmount := "1000000000000000000" // Example amount in wei
	expectedBlockNumber := int64(100)
	decayStart := int64(1000)
	decayEnd := int64(2000)

	// Setup expectations for SendBid
	mockBidder.On("SendBid", expectedTxHashes, expectedAmount, expectedBlockNumber, decayStart, decayEnd).Return(mockSendBidClient, nil)

	// Setup expectations for Recv to return io.EOF
	mockSendBidClient.On("Recv").Return(nil, io.EOF)

	// Call SendBid with []string input
	response, err := mockBidder.SendBid(transactionHashes, expectedAmount, expectedBlockNumber, decayStart, decayEnd)
	require.NoError(t, err)
	require.NotNil(t, response)

	// Call Recv to satisfy the expectation
	_, err = mockSendBidClient.Recv()
	require.ErrorIs(t, err, io.EOF) // Check that Recv() returns io.EOF as expected

	// Verify expectations
	mockBidder.AssertExpectations(t)
	mockSendBidClient.AssertExpectations(t)
}
func TestSendBidUnsupportedInputType(t *testing.T) {
    // Initialize the mock Bidder client and BidderSendBidClient
    mockBidder := new(MockBidderClient)
    mockSendBidClient := new(MockBidderSendBidClient)

    // Set up SendBid mock to return mockSendBidClient with an error
    mockBidder.On("SendBid", mock.AnythingOfType("int"), mock.Anything, mock.Anything, mock.Anything, mock.Anything).
        Return(mockSendBidClient, errors.New("unsupported input type"))

    // Call SendBid with unsupported input type and verify the error
    unsupportedInput := 12345
    _, err := mockBidder.SendBid(unsupportedInput, "1000000000000000000", 100, 1000, 2000)

    require.Error(t, err)
    require.Contains(t, err.Error(), "unsupported input type")
}


func TestSendBidWithRawTransactions(t *testing.T) {
    // Initialize the mock Bidder client and SendBid client
    mockBidder := new(MockBidderClient)
    mockSendBidClient := new(MockBidderSendBidClient)

    t.Run("TestSendBidWithRawTransactions", func(t *testing.T) {
        expectedAmount := "1000000000000000000" // Example amount in wei
        expectedBlockNumber := int64(100)
        decayStart := int64(1000)
        decayEnd := int64(2000)

        // Use *types.Transaction instead of MockTransaction to match SendBid function signature
        tx := new(types.Transaction)

        // Log to track the start of the test
        t.Log("Starting TestSendBidWithRawTransactions")

        // Set up expectation for SendBid to return mockSendBidClient and a marshalling error
        mockBidder.On("SendBid", mock.Anything, expectedAmount, expectedBlockNumber, decayStart, decayEnd).
            Return(mockSendBidClient, errors.New("mock marshalling error")).Once()

        // Call SendBid with []*types.Transaction input
        _, err := mockBidder.SendBid([]*types.Transaction{tx}, expectedAmount, expectedBlockNumber, decayStart, decayEnd)

        // Validate the error and log result
        require.Error(t, err, "Expected an error due to mock marshalling error")
        require.Contains(t, err.Error(), "mock marshalling error", "Error message should contain 'mock marshalling error'")

        // Verify expectations
        mockBidder.AssertExpectations(t)

        t.Log("TestSendBidWithRawTransactions completed")
    })
}

func TestSendBidSuccess(t *testing.T) {
    mockBidder := new(MockBidderClient)
    mockSendBidClient := new(MockBidderSendBidClient)

    txHashes := []string{"0xabc123", "0xdef456"}
    expectedAmount := "1000000000000000000"
    expectedBlockNumber := int64(100)
    decayStart := int64(1000)
    decayEnd := int64(2000)

    mockBidder.On("SendBid", mock.Anything, expectedAmount, expectedBlockNumber, decayStart, decayEnd).
        Return(mockSendBidClient, nil).Once()

    _, err := mockBidder.SendBid(txHashes, expectedAmount, expectedBlockNumber, decayStart, decayEnd)

    require.NoError(t, err, "Expected no error for successful bid")
    mockBidder.AssertExpectations(t)
}


func TestSendBidRequestError(t *testing.T) {
    mockBidder := new(MockBidderClient)
    mockSendBidClient := new(MockBidderSendBidClient)

    // Provide the mockSendBidClient instead of nil
    mockBidder.On("SendBid", mock.Anything, "1000000000000000000", int64(100), int64(1000), int64(2000)).
        Return(mockSendBidClient, errors.New("mock send bid error"))

    _, err := mockBidder.SendBid([]string{"0xabc123"}, "1000000000000000000", 100, 1000, 2000)

    require.Error(t, err, "Expected an error due to mock send bid error")
    require.Contains(t, err.Error(), "mock send bid error", "Error message should contain 'mock send bid error'")
}
