// Mock driver for dev/test.
//
// Generates fake deposits on demand (via SpawnDeposit) or periodically
// (via runMockLoop in service.go). Not used in production.
package chainwatcher

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/goexdev/goexchange/internal/config"
)

// MockDriver is a fake driver for testing.
type MockDriver struct {
	cfg config.ChainWatcherConfig
}

var ErrNotSupported = errors.New("operation not supported by this driver")

// NewMockDriver creates a mock driver.
func NewMockDriver(cfg config.ChainWatcherConfig) *MockDriver {
	return &MockDriver{cfg: cfg}
}

// Name returns "mock".
func (d *MockDriver) Name() string { return "mock" }

// SpawnDeposit generates a fake deposit (returns a fake tx hash).
func (d *MockDriver) SpawnDeposit(_ context.Context, _ string, asset string, txHash string, amount decimal.Decimal) error {
	if asset == "" {
		return ErrInvalidAsset
	}
	if !amount.IsPositive() {
		return ErrInvalidAmount
	}
	return nil
}

// GetReceived returns a fake balance (random or based on amount).
func (d *MockDriver) GetReceived(_ context.Context, _ string) (decimal.Decimal, error) {
	return decimal.Zero, nil
}

// GetReceivedConfirmed returns the confirmed balance.
func (d *MockDriver) GetReceivedConfirmed(_ context.Context, _ string, _ int) (decimal.Decimal, error) {
	return decimal.Zero, nil
}

// GetReceivedPending returns the pending (mempool) balance.
func (d *MockDriver) GetReceivedPending(_ context.Context, _ string, _ int) (decimal.Decimal, error) {
	return decimal.Zero, nil
}

// SendToAddress is not supported.
func (d *MockDriver) SendToAddress(_ context.Context, _, _ string, _ decimal.Decimal) (string, error) {
	return "", ErrNotSupported
}

// GetBlockCount returns a fake block count.
func (d *MockDriver) GetBlockCount(_ context.Context) (int64, error) {
	return 0, nil
}

// GetConfirmations returns 6 (always "confirmed").
func (d *MockDriver) GetConfirmations(_ context.Context, _ string) (int64, error) {
	return 6, nil
}

// ListTransactions returns fake records (empty for mock).
func (d *MockDriver) ListTransactions(_ context.Context, _ string, _ int) ([]TxRecord, error) {
	return nil, nil
}

// GenerateAddress returns a fake mock address.
func (d *MockDriver) GenerateAddress(_ context.Context) (string, error) {
	return "Fmock" + uuid.New().String()[:32], nil
}

// RandomTxHash generates a fake tx hash for mock deposits.
func RandomTxHash() string {
	return fmt.Sprintf("MOCK_TX_%s", uuid.New().String())
}


func (d *MockDriver) HasSigner() bool { return false }
func (d *MockDriver) GetHotAddress() string { return "mock_hot_wallet_address" }
