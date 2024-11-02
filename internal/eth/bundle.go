// Package eth provides functionalities related to Ethereum interactions,
// including sending transaction bundles via Flashbots.
package eth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"log/slog"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
)

type JSONRPCResponse struct {
	Result    json.RawMessage `json:"result"`
	RPCError  RPCError         `json:"error"`
	ID        int              `json:"id,omitempty"`
	Jsonrpc   string           `json:"jsonrpc,omitempty"`
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


// SendBundle sends a signed transaction bundle to the specified RPC URL.
// It returns the result as a string or an error if the operation fails.
func SendBundle(rpcurl string, signedTx *types.Transaction, blkNum uint64) (string, error) {
	// Marshal the signed transaction into binary format.
	binary, err := signedTx.MarshalBinary()
	if err != nil {
		slog.Error("Error marshaling transaction",
			"error", err,
		)
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
		slog.Error("Error marshaling payload",
			"error", err,
		)
		return "", err
	}

	// Create a context with a timeout.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	// Create a new HTTP POST request with the JSON payload.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rpcurl, bytes.NewReader(payloadBytes))
	if err != nil {
		slog.Error("An error occurred creating the request",
			"error", err,
		)
		return "", err
	}
	req.Header.Add("Content-Type", "application/json")

	// Execute the HTTP request.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Error("An error occurred during the request",
			"error", err,
		)
		return "", err
	}
	defer resp.Body.Close()

	// Read the response body.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("An error occurred reading the response body",
			"error", err,
		)
		return "", err
	}

	// Unmarshal the response into JSONRPCResponse struct.
	var rpcResp JSONRPCResponse
	err = json.Unmarshal(body, &rpcResp)
	if err != nil {
		slog.Error("Failed to unmarshal response",
			"error", err,
		)
		return "", err
	}

	// Check for RPC errors.
	if rpcResp.RPCError.Code != 0 {
		slog.Error("Received error from RPC",
			"code", rpcResp.RPCError.Code,
			"message", rpcResp.RPCError.Message,
		)
		return "", fmt.Errorf("request failed %d: %s", rpcResp.RPCError.Code, rpcResp.RPCError.Message)
	}

	// Marshal the result to a string.
	resultStr, err := json.Marshal(rpcResp.Result)
	if err != nil {
		slog.Error("Failed to marshal result",
			"error", err,
		)
		return "", err
	}

	return string(resultStr), nil
}
