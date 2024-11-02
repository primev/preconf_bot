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
	"github.com/rs/zerolog/log"
)

type JSONRPCResponse struct {
	Result json.RawMessage `json:"result"`
	RPCError       
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

func SendBundle(rpcurl string, signedTx *types.Transaction, blkNum uint64) (string, error) {
	binary, err := signedTx.MarshalBinary()
	if err != nil {
		log.Error().
			Err(err).
			Msg("Error marshaling transaction")
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
		log.Error().
			Err(err).
			Msg("Error marshaling payload")
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rpcurl, bytes.NewReader(payloadBytes))
	if err != nil {
		log.Error().
			Err(err).
			Msg("An error occurred creating the request")
		return "", err
	}
	req.Header.Add("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Error().
			Err(err).
			Msg("An error occurred during the request")
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Error().
			Err(err).
			Msg("An error occurred reading the response body")
		return "", err
	}

	var rpcResp JSONRPCResponse
	err = json.Unmarshal(body, &rpcResp)
	if err != nil {
		log.Error().
			Err(err).
			Msg("Failed to unmarshal response")
		return "", err
	}

	if rpcResp.Code != 0 {
		log.Error().
			Int("code", rpcResp.Code).
			Str("message", rpcResp.Message).
			Msg("Received error from RPC")
		return "", fmt.Errorf("request failed %d: %s", rpcResp.Code, rpcResp.Message)
	}

	resultStr, err := json.Marshal(rpcResp.Result)
	if err != nil {
		log.Error().
			Err(err).
			Msg("Failed to marshal result")
		return "", err
	}

	return string(resultStr), nil
}
