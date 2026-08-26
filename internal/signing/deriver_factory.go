package signing

import (
	"crypto/ecdsa"
	"fmt"
	"sync"
)

// DeriverConstructor creates a Deriver from a ChainConfig-derived set of parameters.
// Registered globally in init() or in main.go.
type DeriverConstructor func(params DeriverParams) (Deriver, error)

// DeriverParams holds the parameters needed to build any family-specific deriver.
// Different families use different fields.
type DeriverParams struct {
	// Common
	ChainID int64
	Family  ChainFamily

	// Bitcoin-family
	CoinType uint32
	Prefix   byte

	// EVM-family (chain_id above, no other params needed)
}

// DeriverFactory is a global registry mapping family name to constructor.
type DeriverFactory struct {
	mu           sync.RWMutex
	constructors map[ChainFamily]DeriverConstructor
}

var factory = &DeriverFactory{
	constructors: make(map[ChainFamily]DeriverConstructor),
}

// RegisterDeriver adds a deriver constructor for a family.
//
// Called from package init() or main.go.
func RegisterDeriver(family ChainFamily, ctor DeriverConstructor) {
	factory.mu.Lock()
	defer factory.mu.Unlock()
	factory.constructors[family] = ctor
}

// BuildDeriver constructs a deriver for a chain using its family config.
func BuildDeriver(params DeriverParams) (Deriver, error) {
	factory.mu.RLock()
	ctor, ok := factory.constructors[params.Family]
	factory.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("no deriver registered for family: %s", params.Family)
	}
	return ctor(params)
}

// RegisteredFamilies returns the list of registered families.
func RegisteredFamilies() []ChainFamily {
	factory.mu.RLock()
	defer factory.mu.RUnlock()
	families := make([]ChainFamily, 0, len(factory.constructors))
	for f := range factory.constructors {
		families = append(families, f)
	}
	return families
}

// init() registers the built-in derivers.
// To add a new family, write the deriver and add a RegisterDeriver call here.
func init() {
	RegisterDeriver(FamilyBitcoin, NewBitcoinDeriverFromParams)
	RegisterDeriver(FamilyEVM, NewEVMDeriverFromParams)
	// Solana uses a separate deriver type (not implementing Deriver interface)
	// because it uses Ed25519 instead of secp256k1. See solana_deriver.go.
	RegisterDeriver(FamilySolana, NewSolanaDeriverFromParams)
		}



// NewSolanaDeriverFromParams constructs a SolanaDeriver from params.
//
// Note: The returned Deriver is a SHIM - it satisfies the interface but
// DeriveAddress/SignTransaction return errors. Real Solana operations
// should use SolanaDeriver directly (which uses Ed25519).
func NewSolanaDeriverFromParams(p DeriverParams) (Deriver, error) {
	return &SolanaDeriverShim{}, nil
}

// SolanaDeriverShim is a placeholder that satisfies the Deriver interface
// for Solana. The actual Ed25519 operations are done by SolanaDeriver directly.
//
// This shim exists so that BuildDeriver works for all registered families.
// The chainwatcher/HD code paths that USE the shim methods will get errors,
// but the registration and family lookup work correctly.
type SolanaDeriverShim struct{}

func (s *SolanaDeriverShim) Family() ChainFamily { return FamilySolana }
func (s *SolanaDeriverShim) CoinType() uint32    { return SolanaCoinType }
func (s *SolanaDeriverShim) AddressPrefix() byte { return 0 }
func (s *SolanaDeriverShim) DeriveAddress(priv *ecdsa.PrivateKey) (string, error) {
	return "", fmt.Errorf("SolanaDeriverShim: use SolanaDeriver directly for Ed25519 operations")
}
func (s *SolanaDeriverShim) SignTransaction(priv *ecdsa.PrivateKey, hash []byte) ([]byte, error) {
	return nil, fmt.Errorf("SolanaDeriverShim: use SolanaDeriver directly for Ed25519 operations")
}

// NewBitcoinDeriverFromParams constructs a BitcoinDeriver from params.
func NewBitcoinDeriverFromParams(p DeriverParams) (Deriver, error) {
	if p.CoinType == 0 {
		return NewBitcoinDeriverMainnet(), nil
	}
	if p.Prefix == 0 {
		switch p.CoinType {
		case 0:
			return NewBitcoinDeriver(0, 0x00), nil
		case 1:
			return NewBitcoinDeriver(1, 0x6f), nil
		case 2:
			return NewBitcoinDeriver(2, 0x30), nil
		case 3:
			return NewBitcoinDeriver(3, 0x1e), nil
		case 5:
			return NewBitcoinDeriver(5, 0x4c), nil
		case 145:
			return NewBitcoinDeriver(145, 0x00), nil
		}
		return nil, fmt.Errorf("bitcoin-family chain with coin_type=%d needs explicit prefix", p.CoinType)
	}
	return NewBitcoinDeriver(p.CoinType, p.Prefix), nil
}

// NewEVMDeriverFromParams constructs an EthereumDeriver from params.
func NewEVMDeriverFromParams(p DeriverParams) (Deriver, error) {
	return NewEthereumDeriver(uint32(p.ChainID)), nil
}