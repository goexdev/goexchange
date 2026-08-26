package signing

import (
	"crypto/ecdsa"
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// ChainFamily classifies chains by their address/transaction format.
// Same family = same Deriver implementation (fork-friendly).
//
// Adding a new family requires implementing Deriver from scratch (new algorithm).
// Adding a new chain within a family = config change only.
type ChainFamily string

const (
	// FamilyBitcoin is UTXO-based, secp256k1, base58 addresses (P2PKH).
	// Forks: BTC, BCH, BSV, LTC, DOGE, ZEC, etc.
	// Add new BTC fork by config only - just change coin_type and address_prefix.
	FamilyBitcoin ChainFamily = "bitcoin"

	// FamilyEVM is account-based, secp256k1, hex addresses.
	// Forks: ETH, BSC, Polygon, Arbitrum, Optimism, Base, Avalanche, etc.
	// All share coin_type=60. Add by config only - change chain_id and RPC.
	FamilyEVM ChainFamily = "evm"

	// FamilySolana is account-based, Ed25519, base58 addresses.
	// Solana uses raw 32-byte public key as address (no hashing).
	// Path: m/44/501/0/0 (coin type 501, all hardened).
	FamilySolana ChainFamily = "solana"

	// Future families (require new Deriver implementation):
	// FamilyCosmos    ChainFamily = "cosmos"    // Bech32
	// FamilySubstrate ChainFamily = "substrate" // sr25519
)

// Deriver handles chain-specific operations for a family of chains.
// All chains in the same family share a Deriver instance - only config differs.
//
// Lifecycle: one Deriver per family. chain_id and address_prefix come from config.
type Deriver interface {
	// Family returns the chain family this Deriver handles.
	Family() ChainFamily

	// CoinType returns the BIP-44 coin type (e.g. 60=EVM, 0=BTC, 5=DASH).
	// Used by HDSigner to derive the right path.
	CoinType() uint32

	// DeriveAddress returns the address for the given child key.
	// Different families use different address formats:
	//   bitcoin: base58check(ripemd160(sha256(pubkey)))
	//   evm:     keccak256(pubkey[1:])[12:]
	DeriveAddress(priv *ecdsa.PrivateKey) (string, error)

	// SignTransaction signs a transaction hash with the private key.
	// Returns 65-byte ECDSA signature (R || S || V).
	SignTransaction(priv *ecdsa.PrivateKey, hash []byte) ([]byte, error)

	// AddressPrefix is a byte used by some families (e.g. Bitcoin P2PKH version).
	// Returns 0 if not applicable (e.g. EVM).
	AddressPrefix() byte
}

// shared helpers (no duplication across families)

// pubKeyFromPriv extracts the uncompressed public key bytes.
func pubKeyFromPriv(priv *ecdsa.PrivateKey) ([]byte, error) {
	if priv == nil {
		return nil, errors.New("nil private key")
	}
	return crypto.FromECDSAPub(&priv.PublicKey), nil
}

// evmAddress computes the EVM-style address (0x...).
// Used by FamilyEVM and any future family that uses Keccak256 addresses.
func evmAddress(priv *ecdsa.PrivateKey) (string, error) {
	if priv == nil {
		return "", errors.New("nil private key")
	}
	addr := crypto.PubkeyToAddress(priv.PublicKey)
	return addr.Hex(), nil
}

// common.BytesToAddress is used to validate 0x addresses.
var _ = common.BytesToAddress