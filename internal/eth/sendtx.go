// Package eth provides functionality for sending Ethereum transactions,
// including blob transactions with preconfirmation bids. This package
// is designed to work with public Ethereum nodes and a custom Titan
// endpoint for private transactions.
package eth

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"log/slog"
	"math/big"
	"os"

	"github.com/consensys/gnark-crypto/ecc/bls12-381/fr"
	gokzg4844 "github.com/crate-crypto/go-kzg-4844"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/consensus/misc/eip4844"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	"github.com/holiman/uint256"
	"golang.org/x/exp/rand"
)

func (s *Service) SelfETHTransfer(value *big.Int, offset uint64) (*types.Transaction, uint64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), s.DefaultTimeout)
	defer cancel()

	// Use s.Client, s.AuthAcct, s.Logger
	nonce, err := s.Client.PendingNonceAt(ctx, s.AuthAcct.Address)
	if err != nil {
		s.Logger.Error("Failed to get pending nonce", "error", err)
		return nil, 0, err
	}

	// Get the current base fee per gas from the latest block header
	header, err := s.Client.HeaderByNumber(ctx, nil)
	if err != nil {
		slog.Default().Error("Failed to get latest block header",
			slog.String("function", "HeaderByNumber"),
			slog.Any("error", err))
		return nil, 0, err
	}

	// Get the chain ID
	chainID, err := s.Client.NetworkID(ctx)
	if err != nil {
		slog.Default().Error("Failed to get network ID",
			slog.String("function", "NetworkID"),
			slog.Any("error", err))
		return nil, 0, err
	}

	baseFee := header.BaseFee
	blockNumber := header.Number.Uint64()

	// Create a transaction with a priority fee.
	priorityFee := big.NewInt(2_000_000_000) // 2 gwei in wei
	maxFee := new(big.Int).Add(baseFee, priorityFee)
	tx := types.NewTx(&types.DynamicFeeTx{
		Nonce:     nonce,
		To:        &s.AuthAcct.Address,
		Value:     value,
		Gas:       500_000,
		GasFeeCap: maxFee,
		GasTipCap: priorityFee,
	})

	// Sign the transaction with the authenticated account's private key
	signer := types.LatestSignerForChainID(chainID)
	signedTx, err := types.SignTx(tx, signer, s.AuthAcct.PrivateKey)
	if err != nil {
		slog.Default().Error("Failed to sign transaction",
			slog.String("function", "SignTx"),
			slog.Any("error", err))
		return nil, 0, err
	}

	slog.Default().Info("Self ETH transfer transaction created and signed",
		slog.String("tx_hash", signedTx.Hash().Hex()),
		slog.Uint64("block_number", blockNumber))

	return signedTx, blockNumber + offset, nil
}

// ExecuteBlobTransaction executes a blob transaction with preconfirmation bids.
func (s *Service) ExecuteBlobTransaction(numBlobs int, offset uint64) (*types.Transaction, uint64, error) {

	pubKey, ok := s.AuthAcct.PrivateKey.Public().(*ecdsa.PublicKey)
	if !ok || pubKey == nil {
		slog.Default().Error("Failed to cast public key to ECDSA")
		return nil, 0, errors.New("failed to cast public key to ECDSA")
	}

	var (
		gasLimit    = uint64(500_000)
		blockNumber uint64
		nonce       uint64
	)

	// Set a timeout context
	ctx, cancel := context.WithTimeout(context.Background(), s.DefaultTimeout)
	defer cancel()

	privateKey := s.AuthAcct.PrivateKey
	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		slog.Default().Error("Failed to cast public key to ECDSA")
		return nil, 0, errors.New("failed to cast public key to ECDSA")
	}
	fromAddress := crypto.PubkeyToAddress(*publicKeyECDSA)

	nonce, err := s.Client.PendingNonceAt(ctx, s.AuthAcct.Address)
	if err != nil {
		slog.Default().Error("Failed to get pending nonce",
			slog.String("function", "PendingNonceAt"),
			slog.Any("error", err))
		return nil, 0, err
	}

	header, err := s.Client.HeaderByNumber(ctx, nil)
	if err != nil {
		slog.Default().Error("Failed to get latest block header",
			slog.String("function", "HeaderByNumber"),
			slog.Any("error", err))
		return nil, 0, err
	}

	blockNumber = header.Number.Uint64()

	chainID, err := s.Client.NetworkID(ctx)
	if err != nil {
		slog.Default().Error("Failed to get network ID",
			slog.String("function", "NetworkID"),
			slog.Any("error", err))
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
	maxFeePerGas := baseFee
	// Use for nonzero priority fee
	priorityFee := big.NewInt(5_000_000_000) // 5 gwei in wei
	maxFeePriority := new(big.Int).Add(maxFeePerGas, priorityFee)
	// Create a new BlobTx transaction
	tx := types.NewTx(&types.BlobTx{
		ChainID:    uint256.MustFromBig(chainID),
		Nonce:      nonce,
		GasTipCap:  uint256.MustFromBig(priorityFee),
		GasFeeCap:  uint256.MustFromBig(maxFeePriority),
		Gas:        gasLimit,
		To:         fromAddress,
		BlobFeeCap: uint256.MustFromBig(blobFeeCap),
		BlobHashes: blobHashes,
		Sidecar:    sideCar,
	})

	// Create the transaction options with the private key and chain ID
	auth, err := bind.NewKeyedTransactorWithChainID(privateKey, chainID)
	if err != nil {
		slog.Default().Error("Failed to create keyed transactor",
			slog.String("function", "NewKeyedTransactorWithChainID"),
			slog.Any("error", err))
		return nil, 0, err
	}

	// Sign the transaction
	signedTx, err := auth.Signer(auth.From, tx)
	if err != nil {
		slog.Default().Error("Failed to sign blob transaction",
			slog.String("function", "Signer"),
			slog.Any("error", err))
		return nil, 0, err
	}

	slog.Default().Info("Blob transaction created and signed",
		slog.String("tx_hash", signedTx.Hash().Hex()),
		slog.Uint64("block_number", blockNumber),
		slog.Int("num_blobs", numBlobs))

	return signedTx, blockNumber + offset, nil
}

// makeSidecar creates a sidecar for the given blobs by generating commitments and proofs.
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

// randBlobs generates a slice of random blobs.
func randBlobs(n int) []kzg4844.Blob {
	blobs := make([]kzg4844.Blob, n)
	for i := 0; i < n; i++ {
		blobs[i] = randBlob()
	}
	return blobs
}

// randBlob generates a single random blob.
func randBlob() kzg4844.Blob {
	var blob kzg4844.Blob
	for i := 0; i < len(blob); i += gokzg4844.SerializedScalarSize {
		fieldElementBytes := randFieldElement()
		copy(blob[i:i+gokzg4844.SerializedScalarSize], fieldElementBytes[:])
	}
	return blob
}

// randFieldElement generates a random field element.
func randFieldElement() [32]byte {
	bytes := make([]byte, 32)
	_, err := rand.Read(bytes)
	if err != nil {
		slog.Default().Error("Failed to generate random field element",
			slog.Any("error", err))
		os.Exit(1)
	}
	var r fr.Element
	r.SetBytes(bytes)

	return gokzg4844.SerializeScalar(r)
}
