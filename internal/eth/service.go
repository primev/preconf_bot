package eth

import (
	"log/slog"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	bb "github.com/primev/preconf_blob_bidder/internal/mevcommit"
)

// Service is used to manage the struct for eth package stateful variables used for executing and sending transactions.
type Service struct {
	Client         *ethclient.Client
	AuthAcct       bb.AuthAcct
	DefaultTimeout time.Duration
	Logger         *slog.Logger
	RPCURL         string
}


func NewService(client *ethclient.Client, authAcct bb.AuthAcct, defaultTimeout time.Duration, rpcurl string, logger *slog.Logger) *Service {
	return &Service{
		Client:         client,
		AuthAcct:       authAcct,
		DefaultTimeout: defaultTimeout,
		Logger:         logger,
		RPCURL:         rpcurl,
	}
}
