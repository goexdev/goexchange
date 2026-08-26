package signing

import (
	"crypto/ecdsa"
	"errors"

	"github.com/ethereum/go-ethereum/crypto"
)

// EthereumDeriver implements Deriver for all EVM-family chains (account-based, secp256k1).
//
// This single implementation works for ALL EVM chains by changing config:
//   ETH:     coin_type=60, chain_id=1
//   BSC:     coin_type=60, chain_id=56 (or 97 for testnet)
//   Polygon: coin_type=60, chain_id=137
//   Arbitrum: coin_type=60, chain_id=42161
//   Optimism: coin_type=60, chain_id=10
//   Base:    coin_type=60, chain_id=8453
//   Avalanche, Fantom, etc: all coin_type=60
//
// ALL EVM chains share:
//   - secp256k1 curve
//   - Keccak256 address derivation
//   - Same signing algorithm (secp256k1 ECDSA)
//
// Adding a new EVM chain = config change only. No Go code needed.
type EthereumDeriver struct {
	chainID uint32 // For record-keeping (signing doesn't need it)
}

// NewEthereumDeriver creates a deriver for any EVM chain.
//
// For Ethereum mainnet: NewEthereumDeriver(1)
// For BSC: NewEthereumDeriver(56)
// For BSC testnet: NewEthereumDeriver(97)
// For Polygon: NewEthereumDeriver(137)
// For Arbitrum: NewEthereumDeriver(42161)
// etc.
//
// chainID is informational here - actual signing uses crypto.Sign which
// produces a 65-byte ECDSA signature. The chain_id in EIP-155/EIP-1559
// signatures is applied by go-ethereum types during tx construction.
func NewEthereumDeriver(chainID uint32) *EthereumDeriver {
	return &EthereumDeriver{chainID: chainID}
}

// NewEthereumDeriverMainnet returns the deriver for Ethereum mainnet.
func NewEthereumDeriverMainnet() *EthereumDeriver {
	return NewEthereumDeriver(1)
}

func (e *EthereumDeriver) Family() ChainFamily    { return FamilyEVM }
func (e *EthereumDeriver) CoinType() uint32       { return 60 }
func (e *EthereumDeriver) AddressPrefix() byte    { return 0 }
func (e *EthereumDeriver) ChainID() uint32        { return e.chainID }

// DeriveAddress computes an EVM address (0x...).
// Formula: keccak256(pubkey[1:])[12:]
//
// Result is checksummed (EIP-55): "0x9858EfFD232B4033E47d90003D41EC34EcaEda94"
func (e *EthereumDeriver) DeriveAddress(priv *ecdsa.PrivateKey) (string, error) {
	if priv == nil {
		return "", errors.New("nil private key")
	}
	return evmAddress(priv)
}

// SignTransaction produces a 65-byte ECDSA signature (R || S || V).
// V is the recovery id (0 or 1).
//
// For EIP-155 transactions, V is adjusted by chainwatcher during tx build:
//   V = chainId*2 + 35 + recovery_id (for legacy)
// For EIP-1559, V is encoded in the y_parity field, signature V is always 0/1.
func (e *EthereumDeriver) SignTransaction(priv *ecdsa.PrivateKey, hash []byte) ([]byte, error) {
	if priv == nil {
		return nil, errors.New("nil private key")
	}
	return crypto.Sign(hash, priv)
}