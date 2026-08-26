package chainwatcher

import (
	"context"
	"fmt"
	"sync"

	"github.com/shopspring/decimal"
)

// MultiChainDriver is a meta-driver that delegates to per-chain drivers.
// Allows the chainwatcher Service to use a single Driver interface
// while polling multiple chains (BTC, ETH, BSC) in parallel.
type MultiChainDriver struct {
	mu      sync.RWMutex
	drivers map[string]Driver // chain_id -> driver
	defaultChain string // chain used for unspecified operations
}

func NewMultiChainDriver() *MultiChainDriver {
	return &MultiChainDriver{
		drivers: make(map[string]Driver),
	}
}

func (m *MultiChainDriver) Register(chainID string, d Driver) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.drivers[chainID] = d
	if m.defaultChain == "" {
		m.defaultChain = chainID
	}
}

func (m *MultiChainDriver) Get(chainID string) (Driver, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	d, ok := m.drivers[chainID]
	return d, ok
}

func (m *MultiChainDriver) Chains() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	chains := make([]string, 0, len(m.drivers))
	for c := range m.drivers {
		chains = append(chains, c)
	}
	return chains
}

func (m *MultiChainDriver) Name() string {
	return "multi"
}

// All operations delegate to the default chain.
// Per-chain operations should use Get(chainID).

func (m *MultiChainDriver) defaultDriver() (Driver, error) {
	if m.defaultChain == "" {
		return nil, fmt.Errorf("no drivers registered")
	}
	d, ok := m.drivers[m.defaultChain]
	if !ok {
		return nil, fmt.Errorf("default chain %s not found", m.defaultChain)
	}
	return d, nil
}

func (m *MultiChainDriver) SpawnDeposit(ctx context.Context, userID, asset, txHash string, amount decimal.Decimal) error {
	d, err := m.defaultDriver()
	if err != nil {
		return err
	}
	return d.SpawnDeposit(ctx, userID, asset, txHash, amount)
}

func (m *MultiChainDriver) GetReceived(ctx context.Context, address string) (decimal.Decimal, error) {
	d, err := m.defaultDriver()
	if err != nil {
		return decimal.Zero, err
	}
	return d.GetReceived(ctx, address)
}

func (m *MultiChainDriver) SendToAddress(ctx context.Context, asset, address string, amount decimal.Decimal) (string, error) {
	d, err := m.defaultDriver()
	if err != nil {
		return "", err
	}
	return d.SendToAddress(ctx, asset, address, amount)
}

func (m *MultiChainDriver) GetBlockCount(ctx context.Context) (int64, error) {
	d, err := m.defaultDriver()
	if err != nil {
		return 0, err
	}
	return d.GetBlockCount(ctx)
}

func (m *MultiChainDriver) GenerateAddress(ctx context.Context) (string, error) {
	d, err := m.defaultDriver()
	if err != nil {
		return "", err
	}
	return d.GenerateAddress(ctx)
}

func (m *MultiChainDriver) GetConfirmations(ctx context.Context, txHash string) (int64, error) {
	d, err := m.defaultDriver()
	if err != nil {
		return -1, err
	}
	return d.GetConfirmations(ctx, txHash)
}

func (m *MultiChainDriver) GetReceivedConfirmed(ctx context.Context, address string, minConf int) (decimal.Decimal, error) {
	d, err := m.defaultDriver()
	if err != nil {
		return decimal.Zero, err
	}
	return d.GetReceivedConfirmed(ctx, address, minConf)
}

func (m *MultiChainDriver) GetReceivedPending(ctx context.Context, address string, minConf int) (decimal.Decimal, error) {
	d, err := m.defaultDriver()
	if err != nil {
		return decimal.Zero, err
	}
	return d.GetReceivedPending(ctx, address, minConf)
}

func (m *MultiChainDriver) ListTransactions(ctx context.Context, address string, minConf int) ([]TxRecord, error) {
	d, err := m.defaultDriver()
	if err != nil {
		return nil, err
	}
	return d.ListTransactions(ctx, address, minConf)
}

func (m *MultiChainDriver) HasSigner() bool {
	d, err := m.defaultDriver()
	if err != nil {
		return false
	}
	return d.HasSigner()
}

func (m *MultiChainDriver) GetHotAddress() string {
	d, err := m.defaultDriver()
	if err != nil {
		return ""
	}
	return d.GetHotAddress()
}
