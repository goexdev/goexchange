package chainwatcher

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// stubDriver is a no-op driver for testing/dev.
type stubDriver struct {
	name    string
	asset   string
	hotAddr string
}

func (d *stubDriver) Name() string          { return d.name }
func (d *stubDriver) Chain() string         { return d.name }
func (d *stubDriver) HasSigner() bool       { return false }
func (d *stubDriver) GetHotAddress() string { return d.hotAddr }

func (d *stubDriver) SpawnDeposit(_ context.Context, _, _, _ string, _ decimal.Decimal) error {
	return nil
}
func (d *stubDriver) GetReceived(_ context.Context, _ string) (decimal.Decimal, error) {
	return decimal.Zero, nil
}
func (d *stubDriver) GetReceivedConfirmed(_ context.Context, _ string, _ int) (decimal.Decimal, error) {
	return decimal.Zero, nil
}
func (d *stubDriver) GetReceivedPending(_ context.Context, _ string, _ int) (decimal.Decimal, error) {
	return decimal.Zero, nil
}
func (d *stubDriver) SendToAddress(_ context.Context, _, to string, _ decimal.Decimal) (string, error) {
	return "stub-tx", nil
}
func (d *stubDriver) GetBlockCount(_ context.Context) (int64, error) {
	return 0, nil
}
func (d *stubDriver) GenerateAddress(_ context.Context) (string, error) {
	return "stub-addr", nil
}
func (d *stubDriver) GetConfirmations(_ context.Context, _ string) (int64, error) {
	return int64(time.Now().Unix() % 10), nil
}
func (d *stubDriver) ListTransactions(_ context.Context, _ string, _ int) ([]TxRecord, error) {
	return nil, nil
}
func (d *stubDriver) GetBalance(_ context.Context) (decimal.Decimal, error) {
	return decimal.NewFromInt(1000), nil
}
func (d *stubDriver) SweepToHotWallet(_ context.Context, _ string, _ decimal.Decimal) error {
	return nil
}