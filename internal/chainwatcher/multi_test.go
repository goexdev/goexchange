package chainwatcher

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"
)

func TestMultiChainDriver_Register(t *testing.T) {
	m := NewMultiChainDriver()
	if len(m.Chains()) != 0 {
		t.Errorf("expected 0 chains, got %d", len(m.Chains()))
	}
	if m.Name() != "multi" {
		t.Errorf("expected name 'multi', got %s", m.Name())
	}
}

func TestMultiChainDriver_RegisterMultiple(t *testing.T) {
	m := NewMultiChainDriver()
	mock := NewMockDriverForTest()
	m.Register("btc", mock)
	m.Register("eth", mock)

	if len(m.Chains()) != 2 {
		t.Errorf("expected 2 chains, got %d", len(m.Chains()))
	}

	if _, ok := m.Get("btc"); !ok {
		t.Error("btc should be registered")
	}
	if _, ok := m.Get("eth"); !ok {
		t.Error("eth should be registered")
	}
	if m.defaultChain != "btc" {
		t.Errorf("expected default chain 'btc', got %s", m.defaultChain)
	}
}

func TestMultiChainDriver_Get_NotFound(t *testing.T) {
	m := NewMultiChainDriver()
	_, ok := m.Get("nonexistent")
	if ok {
		t.Error("expected not found for non-existent chain")
	}
}

func TestMultiChainDriver_DefaultDriver_NoDrivers(t *testing.T) {
	m := NewMultiChainDriver()
	_, err := m.defaultDriver()
	if err == nil {
		t.Error("expected error when no drivers registered")
	}
}

// NewMockDriverForTest returns a minimal mock driver.
func NewMockDriverForTest() Driver {
	return &testDriver{name: "mock"}
}

type testDriver struct{ name string }

func (d *testDriver) Name() string { return d.name }
func (d *testDriver) HasSigner() bool { return false }
func (d *testDriver) GetHotAddress() string { return "test_hot" }
func (d *testDriver) SpawnDeposit(ctx context.Context, userID, asset, txHash string, amount decimal.Decimal) error { return nil }
func (d *testDriver) GetReceived(ctx context.Context, address string) (decimal.Decimal, error) { return decimal.Zero, nil }
func (d *testDriver) SendToAddress(ctx context.Context, asset, address string, amount decimal.Decimal) (string, error) { return "", nil }
func (d *testDriver) GetBlockCount(ctx context.Context) (int64, error) { return 0, nil }
func (d *testDriver) GenerateAddress(ctx context.Context) (string, error) { return "", nil }
func (d *testDriver) GetConfirmations(ctx context.Context, txHash string) (int64, error) { return 0, nil }
func (d *testDriver) GetReceivedConfirmed(ctx context.Context, address string, minConf int) (decimal.Decimal, error) { return decimal.Zero, nil }
func (d *testDriver) GetReceivedPending(ctx context.Context, address string, minConf int) (decimal.Decimal, error) { return decimal.Zero, nil }
func (d *testDriver) ListTransactions(ctx context.Context, address string, minConf int) ([]TxRecord, error) { return nil, nil }
