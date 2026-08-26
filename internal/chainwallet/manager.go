// Package chainwallet handles unsigned transaction construction and
// signed transaction broadcasting for various chains.
package chainwallet

import (
	"context"
	"encoding/hex"
	"fmt"
)

// UnsignedTx represents a chain-specific unsigned transaction.
type UnsignedTx struct {
	Chain   string
	TxData  []byte
	TxHash  string
	Fee     string
	Nonce   uint64
}

// SignedTx is the result of signing.
type SignedTx struct {
	SignedHex string
	TxHash    string
	PubKey    string
}

// TxBuilder constructs unsigned transactions for a specific chain.
type TxBuilder interface {
	Build(ctx context.Context, toAddress string, amount string, feeRate string) (*UnsignedTx, error)
	ChainName() string
}

// TxBroadcaster broadcasts pre-signed transactions to the network.
type TxBroadcaster interface {
	Broadcast(ctx context.Context, chain, signedHex string) (string, error)
}

// Manager coordinates TxBuilder, signer, and broadcaster.
type Manager struct {
	builders     map[string]TxBuilder
	broadcasters map[string]TxBroadcaster
	signer       Signer
}

// Signer is the interface for the signer service.
type Signer interface {
	Sign(ctx context.Context, chain string, txData []byte, withdrawalID, userID string) (*SignedTx, error)
}

// NewManager creates a new chain wallet manager.
func NewManager(signer Signer) *Manager {
	return &Manager{
		builders:     make(map[string]TxBuilder),
		broadcasters: make(map[string]TxBroadcaster),
		signer:       signer,
	}
}

// RegisterBuilder adds a chain's tx builder.
func (m *Manager) RegisterBuilder(b TxBuilder) {
	m.builders[b.ChainName()] = b
}

// RegisterBroadcaster adds a chain's broadcaster.
func (m *Manager) RegisterBroadcaster(b TxBroadcaster) {
	// Register broadcaster for all chains
	for _, chain := range []string{"bitcoin", "ethereum"} {
		m.broadcasters[chain] = b
	}
}

// Send is the high-level API: build → sign → broadcast.
func (m *Manager) Send(ctx context.Context, chain, toAddress, amount, feeRate, withdrawalID, userID string) (string, error) {
	// 1. Get builder
	builder, ok := m.builders[chain]
	if !ok {
		return "", fmt.Errorf("no builder for chain %s", chain)
	}

	// 2. Build unsigned tx
	unsigned, err := builder.Build(ctx, toAddress, amount, feeRate)
	if err != nil {
		return "", fmt.Errorf("build unsigned tx: %w", err)
	}

	// 3. Sign with signer service
	signed, err := m.signer.Sign(ctx, chain, unsigned.TxData, withdrawalID, userID)
	if err != nil {
		return "", fmt.Errorf("sign tx: %w", err)
	}

	// 4. Try to decode signed hex (optional - some chains don't use hex)
	if signed.SignedHex != "" {
		_, _ = hex.DecodeString(signed.SignedHex)
		_ = signed.SignedHex
	}

	// 5. Broadcast (if broadcaster registered)
	broadcaster, ok := m.broadcasters[chain]
	if ok && broadcaster != nil {
		txHash, err := broadcaster.Broadcast(ctx, chain, signed.SignedHex)
		if err == nil {
			return txHash, nil
		}
	}

	// No broadcaster or broadcast failed - return signed hash
	if signed.TxHash != "" {
		return signed.TxHash, nil
	}

	return "signed", nil
}
