// Package blockchain: stub adapters for chains the V1 release does
// not yet implement. Each stub returns ErrUnsupported so the wallet
// service fails fast with a clear message ("BTC not yet wired up")
// instead of silently mis-routing funds.
//
// When V2 brings BTC / ETH / BSC / SOL online, replace each stub with
// a real Adapter in a sub-package (internal/blockchain/btc, etc.) and
// remove the corresponding entry from this file.
package blockchain

import (
	"context"
	"errors"
	"fmt"
	"math/big"
)

// ErrUnsupported is returned by stub adapters. Callers should map it
// to HTTP 501 / "chain not yet enabled" in the API layer.
var ErrUnsupported = errors.New("blockchain: chain not yet implemented")

// stubAdapter is the boilerplate shared by every not-yet-implemented
// chain. Each method returns the chain name in its error message so
// logs are immediately useful.
type stubAdapter struct {
	chain Chain
}

func (s stubAdapter) Chain() Chain                       { return s.chain }
func (s stubAdapter) Network() string                    { return "mainnet" }
func (s stubAdapter) GenerateAddress(ctx context.Context, index uint32) (Address, error) {
	return Address{}, fmt.Errorf("%w: %s address generation", ErrUnsupported, s.chain)
}
func (s stubAdapter) ValidateAddress(string) bool { return false }
func (s stubAdapter) GetLatestBlock(context.Context) (Block, error) {
	return Block{}, fmt.Errorf("%w: %s GetLatestBlock", ErrUnsupported, s.chain)
}
func (s stubAdapter) GetSolidifiedBlock(context.Context) (Block, error) {
	return Block{}, fmt.Errorf("%w: %s GetSolidifiedBlock", ErrUnsupported, s.chain)
}
func (s stubAdapter) GetBlockByNumber(context.Context, uint64) (Block, error) {
	return Block{}, fmt.Errorf("%w: %s GetBlockByNumber", ErrUnsupported, s.chain)
}
func (s stubAdapter) GetTransaction(context.Context, string) (Transaction, error) {
	return Transaction{}, fmt.Errorf("%w: %s GetTransaction", ErrUnsupported, s.chain)
}
func (s stubAdapter) ParseTransferEvents(Transaction, string) ([]TransferEvent, error) {
	return nil, fmt.Errorf("%w: %s ParseTransferEvents", ErrUnsupported, s.chain)
}
func (s stubAdapter) GetBalance(context.Context, string, string) (Balance, error) {
	return Balance{}, fmt.Errorf("%w: %s GetBalance", ErrUnsupported, s.chain)
}
func (s stubAdapter) BuildTransfer(context.Context, string, string, uint64, string) (BuildResult, error) {
	return BuildResult{}, fmt.Errorf("%w: %s BuildTransfer", ErrUnsupported, s.chain)
}
func (s stubAdapter) Broadcast(context.Context, []byte) (BroadcastResult, error) {
	return BroadcastResult{}, fmt.Errorf("%w: %s Broadcast", ErrUnsupported, s.chain)
}
func (s stubAdapter) EstimateResource(context.Context, []byte) (ResourceCost, error) {
	return ResourceCost{}, fmt.Errorf("%w: %s EstimateResource", ErrUnsupported, s.chain)
}

// RegisterStubs installs a stub adapter for every chain that does not
// have a real implementation. The function returns the set of chains
// it stubbed so the caller can log "BTC, ETH, BSC, SOL not yet
// implemented" on startup. The TRON adapter (V1) is added separately
// in B5.
func RegisterStubs(reg *Registry) []Chain {
	stubbed := make([]Chain, 0, len(AllChains()))
	for _, c := range AllChains() {
		if reg.Has(c) {
			continue
		}
		reg.Register(stubAdapter{chain: c})
		stubbed = append(stubbed, c)
	}
	return stubbed
}

// Sentinel for tests: an empty big.Int used by stubs that need to
// return a non-nil Amount pointer without implying any value. Keeping
// it as a package var avoids allocating a new big.Int on every error
// path.
var zero = big.NewInt(0)