// Package blockchain defines the adapter abstraction used by the wallet
// service to talk to multiple chains. A chain implementation supplies
// an Adapter that knows how to derive addresses, build and broadcast
// transactions, parse transfer events, and estimate the resource cost
// of a transaction (gas on EVM, Energy on TRON, sat/vB on Bitcoin).
//
// This file contains only the interface and the registry that picks the
// right Adapter for a given chain. Concrete implementations live in
// sub-packages (internal/blockchain/tron for V1, internal/blockchain/btc
// for V2, etc.).
package blockchain

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Chain identifies a blockchain network. The values are stable and
// used as database column values, so renaming one is a breaking
// change requiring a migration.
type Chain string

const (
	ChainTron Chain = "TRON"
	ChainBtc  Chain = "BTC"
	ChainEth  Chain = "ETH"
	ChainBsc  Chain = "BSC"
	ChainSol  Chain = "SOL"
)

// AllChains returns every Chain the registry knows about. Used by the
// reconciler and health checks to make sure no chain has been left
// without an adapter.
func AllChains() []Chain {
	return []Chain{ChainTron, ChainBtc, ChainEth, ChainBsc, ChainSol}
}

// ParseChain accepts the case-insensitive form a user might type and
// returns the canonical constant. Returns ErrUnknownChain if the value
// is not in AllChains.
func ParseChain(s string) (Chain, error) {
	switch Chain(toUpperASCII(s)) {
	case ChainTron, ChainBtc, ChainEth, ChainBsc, ChainSol:
		return Chain(toUpperASCII(s)), nil
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownChain, s)
}

func toUpperASCII(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			c -= 32
		}
		b[i] = c
	}
	return string(b)
}

// Errors returned by adapters. Adapters may also return chain-specific
// wrapped errors; callers should use errors.Is for the sentinels below.
var (
	ErrUnknownChain    = errors.New("blockchain: unknown chain")
	ErrAdapterNotFound = errors.New("blockchain: no adapter registered for chain")
	ErrInsufficientFee = errors.New("blockchain: insufficient resource to pay for transaction")
)

// Adapter is the contract every chain implementation must satisfy. It
// is deliberately minimal: only the operations the wallet service and
// scanner need. Anything chain-specific (signature serialization, RPC
// shapes) is hidden behind the interface boundary.
type Adapter interface {
	// Chain returns the chain identifier this adapter handles.
	Chain() Chain

	// Network returns "mainnet" or a testnet label (e.g. "nile_testnet"
	// for TRON). The value is propagated to the signer so the signer
	// knows which derivation path / key to use.
	Network() string

	// GenerateAddress derives an address at the given BIP-44-style index.
	// For chains that do not use BIP-44 (e.g. Bitcoin uses m/84'/0'/0'/0
	// for native segwit), the adapter is responsible for choosing the
	// right path internally.
	GenerateAddress(ctx context.Context, index uint32) (Address, error)

	// ValidateAddress returns true if the string is a syntactically
	// well-formed address for this chain (correct checksum, valid
	// base58/hex/bech32 encoding). It does NOT check that the address
	// has been used on chain or has balance.
	ValidateAddress(addr string) bool

	// GetLatestBlock returns the head block height. The returned block
	// is not necessarily finalized; use GetSolidifiedBlock for that.
	GetLatestBlock(ctx context.Context) (Block, error)

	// GetSolidifiedBlock returns the most recent block that the network
	// considers final (TRON: confirmed by a witness, EVM: finalized
	// epoch boundary, BTC: 6 confirmations).
	GetSolidifiedBlock(ctx context.Context) (Block, error)

	// GetBlockByNumber returns every transaction in a block, in
	// canonical order. Adapters should batch the underlying RPC calls
	// to keep the scanner's latency low.
	GetBlockByNumber(ctx context.Context, height uint64) (Block, error)

	// GetTransaction returns the parsed transaction and its Transfer
	// events. It is acceptable for the adapter to return an error when
	// the transaction has been broadcast but is not yet indexed; the
	// caller will retry.
	GetTransaction(ctx context.Context, txHash string) (Transaction, error)

	// ParseTransferEvents extracts TRC20/ERC20/etc. Transfer events
	// from a transaction's logs. The implementation must filter to the
	// given contract address; events for other contracts are ignored.
	ParseTransferEvents(tx Transaction, contract string) ([]TransferEvent, error)

	// GetBalance returns the on-chain balance of addr for the given
	// asset. For native assets, contract is "". For tokens, contract is
	// the token contract address in the chain's native encoding.
	GetBalance(ctx context.Context, addr string, contract string) (Balance, error)

	// BuildTransfer constructs an unsigned transaction that transfers
	// amount units of the given contract from the signer key identified
	// by keyID to to. The amount is in the asset's smallest unit
	// (USDT = 6 decimals on TRON, so 1 USDT = 1_000_000).
	BuildTransfer(ctx context.Context, keyID string, to string, amount uint64, contract string) (BuildResult, error)

	// Broadcast submits an already-signed transaction to the network
	// and returns the network-assigned tx hash. A non-nil error means
	// the broadcast definitively failed; a nil error with an empty
	// TxHash means the adapter is unsure (BROADCAST_UNKNOWN territory)
	// and the caller should poll GetTransaction later.
	Broadcast(ctx context.Context, signedTx []byte) (BroadcastResult, error)

	// EstimateResource returns the cost (and the resource "kind": gas,
	// energy, sat/vB) of broadcasting rawTx without actually sending
	// it. The wallet service uses this to decide whether to top up the
	// hot wallet before broadcasting a sweep or withdrawal.
	EstimateResource(ctx context.Context, rawTx []byte) (ResourceCost, error)
}

// Registry looks up the Adapter for a chain. Adapters are registered
// at process start via Register and looked up per-call via For.
type Registry struct {
	mu       sync.RWMutex
	adapters map[Chain]Adapter
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{adapters: map[Chain]Adapter{}}
}

// Register associates an adapter with its chain. Re-registering the
// same chain overwrites the previous adapter. This is intentional so
// that tests can swap adapters without restart.
func (r *Registry) Register(a Adapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[a.Chain()] = a
}

// For returns the adapter for chain or ErrAdapterNotFound.
func (r *Registry) For(chain Chain) (Adapter, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[chain]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrAdapterNotFound, chain)
	}
	return a, nil
}

// Has reports whether an adapter is registered for chain. Cheaper than
// For when the caller is just doing a presence check.
func (r *Registry) Has(chain Chain) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.adapters[chain]
	return ok
}

// Chains returns the list of chains with registered adapters. Useful
// for startup health checks: a chain with no adapter is a config bug.
func (r *Registry) Chains() []Chain {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Chain, 0, len(r.adapters))
	for c := range r.adapters {
		out = append(out, c)
	}
	return out
}