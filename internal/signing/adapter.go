package signing

import (
	"crypto/ecdsa"
	"context"
)

// VaultSignerAdapter adapts VaultSigner to chainwatcher.TxSigner interface.
// (The chainwatcher package can't import signing directly to avoid cycles,
// so we provide this adapter for use by the chainwatcher service.)
//
// In production, both the driver and the chainwatcher service would depend
// on a common interface in a shared package.
func (s *VaultSigner) Sign(ctx context.Context, data []byte) ([]byte, string, error) {
	signed, err := s.SignTransaction(ctx, UnsignedTx{Data: data})
	if err != nil {
		return nil, "", err
	}
	return signed.Raw, signed.TxHash, nil
}

func (s *LocalSigner) Sign(ctx context.Context, data []byte) ([]byte, string, error) {
	signed, err := s.SignTransaction(ctx, UnsignedTx{Data: data})
	if err != nil {
		return nil, "", err
	}
	return signed.Raw, signed.TxHash, nil
}

// ChainString returns the chain as a string (matches chainwatcher.Chain).
func (s *VaultSigner) ChainString() string {
	return string(s.Chain())
}

func (s *LocalSigner) ChainString() string {
	return string(s.Chain())
}


// ChainFromString converts a chain_id string to a Chain enum.
func ChainFromString(s string) Chain {
	switch s {
	case "eth", "ethereum":
		return ChainETH
	case "bsc", "bnb":
		return ChainBSC
	case "btc", "bitcoin":
		return ChainBTC
	default:
		return Chain(s)
	}
}

// LocalSignerAdapter adapts LocalSigner to chainwatcher.TxSigner interface.
type LocalSignerAdapter struct {
	Signer *LocalSigner
}

func (a *LocalSignerAdapter) Name() string    { return a.Signer.Name() }
func (a *LocalSignerAdapter) Chain() string   { return string(a.Signer.Chain()) }
func (a *LocalSignerAdapter) Address() string { return a.Signer.Address() }
// UnderlyingKey returns the raw *ecdsa.PrivateKey for go-ethereum integration.
func (a *LocalSignerAdapter) UnderlyingKey() *ecdsa.PrivateKey {
	return a.Signer.PrivateKey()
}

func (a *LocalSignerAdapter) SignTransaction(ctx context.Context, data []byte) ([]byte, string, error) {
	return a.Signer.Sign(ctx, data)
}
