package eth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
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
	binary, err := signedTx.MarshalBinary()
	if err != nil {
		log.Error("Error marshal transaction", "err", err)
		return "", err
	}

	blockNum := hexutil.EncodeUint64(blkNum)

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

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	// Create a context with a timeout
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", RPCURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		log.Error("An error occurred creating the request", "err", err)
		return "", err
	}
	req.Header.Add("Content-Type", "application/json")

	// Use the default HTTP client
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Error("An error occurred during the request", "err", err)
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Error("An error occurred reading the response body", "err", err)
		return "", err
	}

// Parse the response body
var rpcResp JSONRPCResponse
err = json.Unmarshal(body, &rpcResp)
if err != nil {
	log.Error("Failed to unmarshal response", "err", err)
	return "", err
}

if rpcResp.Error != nil {
	log.Error("Received error from RPC", "code", rpcResp.Error.Code, "message", rpcResp.Error.Message)
	// Log the error and return a new error without using fmt
	return "", fmt.Errorf("RPC Error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
}

	// Return the result
	resultStr, err := json.Marshal(rpcResp.Result)
	if err != nil {
		log.Error("Failed to marshal result", "err", err)
		return "", err
	}

	return string(resultStr), nil
}
