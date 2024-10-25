// Package eth provides functionality for sending Ethereum transactions,
// including blob transactions with preconfirmation bids. This package
// is designed to work with public Ethereum nodes and a custom Titan
// endpoint for private transactions.
package eth

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"math/big"

	"github.com/consensys/gnark-crypto/ecc/bls12-381/fr"
	gokzg4844 "github.com/crate-crypto/go-kzg-4844"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/consensus/misc/eip4844"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/log"
	"github.com/holiman/uint256"
	bb "github.com/primev/preconf_blob_bidder/core/mevcommit"
	"golang.org/x/exp/rand"
)


func SelfETHTransfer(client *ethclient.Client, authAcct bb.AuthAcct, value *big.Int, offset uint64) (*types.Transaction, uint64, error) {
	// Get the account's nonce
	nonce, err := client.PendingNonceAt(context.Background(), authAcct.Address)
	if err != nil {
		return nil, 0, err
	}

	// Get the current base fee per gas from the latest block header
	header, err := client.HeaderByNumber(context.Background(), nil)
	if err != nil {
		return nil, 0, err
	}
	baseFee := header.BaseFee

	
	blockNumber := header.Number.Uint64()

	// Set the max priority fee per gas to be 2 times the base fee
	maxPriorityFee := new(big.Int).Mul(baseFee, big.NewInt(2))

	// Set the max fee per gas to be 2 times the max priority fee
	maxFeePerGas := new(big.Int).Mul(maxPriorityFee, big.NewInt(2))

	// Get the chain ID (this does not work with the Titan RPC)
	chainID, err := client.NetworkID(context.Background())
	if err != nil {
		return nil, 0, err
	}

	// Create a new EIP-1559 transaction
	tx := types.NewTx(&types.DynamicFeeTx{
		Nonce:     nonce,
		To:        &authAcct.Address,
		Value:     value,
		Gas:       500_000,
		GasFeeCap: maxFeePerGas,
		GasTipCap: big.NewInt((0)),
	})

	// Sign the transaction with the authenticated account's private key
	signer := types.LatestSignerForChainID(chainID)
	signedTx, err := types.SignTx(tx, signer, authAcct.PrivateKey)
	if err != nil {
		log.Error("Failed to sign transaction", "error", err)
		return nil, 0, err
	}

	return signedTx, blockNumber + offset, nil

}


func ExecuteBlobTransaction(client *ethclient.Client, authAcct bb.AuthAcct, numBlobs int, offset uint64) (*types.Transaction, uint64, error) {
	var (
		gasLimit    = uint64(500_000)
		blockNumber uint64
		nonce       uint64
	)

	privateKey := authAcct.PrivateKey
	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, 0, errors.New("failed to cast public key to ECDSA")
	}
	fromAddress := crypto.PubkeyToAddress(*publicKeyECDSA)

	nonce, err := client.PendingNonceAt(context.Background(), authAcct.Address)
	if err != nil {
		return nil, 0, err
	}

	header, err := client.HeaderByNumber(context.Background(), nil)
	if err != nil {
		return nil, 0, err
	}
	
	blockNumber = header.Number.Uint64()

	chainID, err := client.NetworkID(context.Background())
	if err != nil {
		return nil, 0, err
	}

	// Calculate the blob fee cap and ensure it is sufficient for transaction replacement
	parentExcessBlobGas := eip4844.CalcExcessBlobGas(*header.ExcessBlobGas, *header.BlobGasUsed)
	blobFeeCap := eip4844.CalcBlobFee(parentExcessBlobGas)
	blobFeeCap.Add(blobFeeCap, big.NewInt(1)) // Ensure it's at least 1 unit higher to replace a transaction

	// Generate random blobs and their corresponding sidecar
	blobs := randBlobs(numBlobs)
	sideCar := makeSidecar(blobs)
	blobHashes := sideCar.BlobHashes()

	// Incrementally increase blob fee cap for replacement
	incrementFactor := big.NewInt(110) // 10% increase
	blobFeeCap.Mul(blobFeeCap, incrementFactor).Div(blobFeeCap, big.NewInt(100))

	baseFee := header.BaseFee

	// Set the max priority fee per gas to be 2 times the base fee
	maxPriorityFee := new(big.Int).Mul(baseFee, big.NewInt(2))

	// Set the max fee per gas to be 2 times the max priority fee
	maxFeePerGas := new(big.Int).Mul(maxPriorityFee, big.NewInt(2))


	// Create a new BlobTx transaction
	tx := types.NewTx(&types.BlobTx{
		ChainID:    uint256.MustFromBig(chainID),
		Nonce:      nonce,
		GasTipCap: uint256.NewInt(0),
		GasFeeCap:  uint256.MustFromBig(maxFeePerGas),
		Gas:        gasLimit,
		To:         fromAddress,
		BlobFeeCap: uint256.MustFromBig(blobFeeCap),
		BlobHashes: blobHashes,
		Sidecar:    sideCar,
	})

	// Sign the transaction with the authenticated account's private key
	auth, err := bind.NewKeyedTransactorWithChainID(privateKey, chainID)
	if err != nil {
		log.Error("Failed to create keyed transactor", "error", err)
		return nil, 0, err
	}

	signedTx, err := auth.Signer(auth.From, tx)
	if err != nil {
		log.Error("Failed to sign transaction", "error", err)
		return nil, 0, err
	}
	return signedTx, blockNumber + offset, nil
}


func makeSidecar(blobs []kzg4844.Blob) *types.BlobTxSidecar {
	var (
		commitments []kzg4844.Commitment
		proofs      []kzg4844.Proof
	)

	// Generate commitments and proofs for each blob
	for _, blob := range blobs {
		c, _ := kzg4844.BlobToCommitment(&blob)
		p, _ := kzg4844.ComputeBlobProof(&blob, c)

		commitments = append(commitments, c)
		proofs = append(proofs, p)
	}

	return &types.BlobTxSidecar{
		Blobs:       blobs,
		Commitments: commitments,
		Proofs:      proofs,
	}
}

func randBlobs(n int) []kzg4844.Blob {
	blobs := make([]kzg4844.Blob, n)
	for i := 0; i < n; i++ {
		blobs[i] = randBlob()
	}
	return blobs
}

func randBlob() kzg4844.Blob {
	var blob kzg4844.Blob
	for i := 0; i < len(blob); i += gokzg4844.SerializedScalarSize {
		fieldElementBytes := randFieldElement()
		copy(blob[i:i+gokzg4844.SerializedScalarSize], fieldElementBytes[:])
	}
	return blob
}

func randFieldElement() [32]byte {
	bytes := make([]byte, 32)
	_, err := rand.Read(bytes)
	if err != nil {
		panic("failed to get random field element")
	}
	var r fr.Element
	r.SetBytes(bytes)

	return gokzg4844.SerializeScalar(r)
}