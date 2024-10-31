package eth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	// Added for defaultTimeout
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/rs/zerolog/log"
)

type JSONRPCResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *RPCError       `json:"error"`
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

func SendBundle(RPCURL string, signedTx *types.Transaction, blkNum uint64) (string, error) {
	// Marshal the signed transaction into binary
	binary, err := signedTx.MarshalBinary()
	if err != nil {
		log.Error().
			Err(err).
			Msg("Error marshaling transaction")
		return "", err
	}

	// Encode the block number
	blockNum := hexutil.EncodeUint64(blkNum)

	// Prepare the Flashbots payload
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

	// Marshal the payload into JSON
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		log.Error().
			Err(err).
			Msg("Error marshaling payload")
		return "", err
	}

	// Create a context with a timeout
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	// Create a new HTTP POST request with the JSON payload
	req, err := http.NewRequestWithContext(ctx, "POST", RPCURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		log.Error().
			Err(err).
			Msg("An error occurred creating the request")
		return "", err
	}
	req.Header.Add("Content-Type", "application/json")

	// Send the HTTP request using the default client
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Error().
			Err(err).
			Msg("An error occurred during the request")
		return "", err
	}
	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Error().
			Err(err).
			Msg("An error occurred reading the response body")
		return "", err
	}

	// Parse the JSON-RPC response
	var rpcResp JSONRPCResponse
	err = json.Unmarshal(body, &rpcResp)
	if err != nil {
		log.Error().
			Err(err).
			Msg("Failed to unmarshal response")
		return "", err
	}

	// Check if the RPC response contains an error
	if rpcResp.Error != nil {
		log.Error().
			Int("code", rpcResp.Error.Code).
			Str("message", rpcResp.Error.Message).
			Msg("Received error from RPC")
		return "", fmt.Errorf("RPC Error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	// Marshal the result back to a string
	resultStr, err := json.Marshal(rpcResp.Result)
	if err != nil {
		log.Error().
			Err(err).
			Msg("Failed to marshal result")
		return "", err
	}

	return string(resultStr), nil
}
