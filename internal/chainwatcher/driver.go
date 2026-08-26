// Package chainwatcher - Driver interface for chain watchers.
//
// A Driver knows how to talk to one specific blockchain (BTC, ETH, etc.)
// and exposes 3 operations:
//
//   - SpawnDeposit: trigger a test deposit (MOCK ONLY, for dev/test)
//   - GetReceived: how much an address has received
//   - SendToAddress: send asset from hot wallet to address
//
// goexchange calls these through the chainwatcher.Service. Real chain
// drivers poll the node for new blocks; mock driver generates fake txs.
package chainwatcher

import (
	"context"

	"github.com/shopspring/decimal"
)

// TxRecord represents one on-chain transaction received by an address.
// Used for real-time detection of new deposits with their actual tx_hash.
type TxRecord struct {
	TxHash      string          // on-chain transaction hash (real, not synthetic)
	Amount      decimal.Decimal // amount received
	Address     string          // recipient (our assigned address)
	Confirmations int64         // number of confirmations
	BlockHeight int64           // block height where confirmed (-1 if mempool)
	Category    string          // "receive" for incoming
	Time        int64           // unix timestamp from chain
}

// Driver is the chain interface for the chainwatcher.
type Driver interface {
	// Name returns the driver name (e.g. "mock", "btc", "bsc").
	Name() string

	// SpawnDeposit triggers a fake deposit (MOCK ONLY).
	// Real drivers return ErrNotSupported.
	SpawnDeposit(ctx context.Context, userID, asset, txHash string, amount decimal.Decimal) error

	// GetReceived returns how much `asset` an address has received.
	// For poll-based drivers, this returns the current balance.
	// Includes unconfirmed transactions.
	GetReceived(ctx context.Context, address string) (decimal.Decimal, error)

	// SendToAddress sends `amount` of `asset` from hot wallet to `address`.
	// Returns the tx hash on success.
	SendToAddress(ctx context.Context, asset, address string, amount decimal.Decimal) (string, error)

	// GetBlockCount returns the current block height.
	GetBlockCount(ctx context.Context) (int64, error)

	// GenerateAddress creates a new deposit address (for user assignment).
	// Mock returns a fake address. Real drivers call chain RPC.
	GenerateAddress(ctx context.Context) (string, error)

	// GetConfirmations returns the number of confirmations for a tx.
	// 0 if tx is in mempool, -1 if not found.
	GetConfirmations(ctx context.Context, txHash string) (int64, error)

	// GetReceivedConfirmed returns balance with >= minConf confirmations.
	// Excludes mempool transactions.
	GetReceivedConfirmed(ctx context.Context, address string, minConf int) (decimal.Decimal, error)

	// GetReceivedPending returns mempool balance (received but < minConf).
	// Used to display "pending" deposits before crediting.
	GetReceivedPending(ctx context.Context, address string, minConf int) (decimal.Decimal, error)

	// ListTransactions returns all transactions involving `address` with >= minConf.
	// Used to detect new deposits by comparing to last-known state.
	// Each TxRecord includes the real on-chain tx_hash.
	ListTransactions(ctx context.Context, address string, minConf int) ([]TxRecord, error)

	// HasSigner returns true if this driver supports real signed withdrawals.
	HasSigner() bool
	// GetHotAddress returns the hot wallet address for withdrawals.
	GetHotAddress() string
}
