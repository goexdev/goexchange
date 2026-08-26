package signing

import (
	"crypto/ecdsa"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/tyler-smith/go-bip32"
	"github.com/tyler-smith/go-bip39"
)

// HDSigner derives child keys from a master seed using BIP-32/44.
// This allows one mnemonic to control multiple chain hot wallets,
// each with a different derivation path (m/44/60/0/0/0, /1, /2, etc.).
type HDSigner struct {
	masterKey *bip32.Key
}

// NewHDSignerFromMnemonic creates an HD signer from a BIP-39 mnemonic phrase.
// Mnemonic must be 12/15/18/21/24 words.
//
// Returns error if mnemonic is invalid or seed derivation fails.
func NewHDSignerFromMnemonic(mnemonic string) (*HDSigner, error) {
	if !bip39.IsMnemonicValid(mnemonic) {
		return nil, fmt.Errorf("invalid BIP-39 mnemonic")
	}
	seed := bip39.NewSeed(mnemonic, "") // empty passphrase
	masterKey, err := bip32.NewMasterKey(seed)
	if err != nil {
		return nil, fmt.Errorf("create master key: %w", err)
	}
	return &HDSigner{masterKey: masterKey}, nil
}

// Derive returns a new HDSigner at the given BIP-32/44 path.
// Example: m/44/60/0/0/0 (first EVM address)
func (h *HDSigner) Derive(path string) (*HDSigner, error) {
	if h.masterKey == nil {
		return nil, errors.New("no master key")
	}
	key, err := parseBIP32Path(h.masterKey, path)
	if err != nil {
		return nil, fmt.Errorf("derive path %s: %w", path, err)
	}
	return &HDSigner{masterKey: key}, nil
}

// parseBIP32Path manually parses "m/44/60/0/0/0" and derives each level.
// BIP-44 convention: first 3 components (purpose, coin_type, account) are hardened
// (add 0x80000000), last 2 (change, address_index) are non-hardened.
func parseBIP32Path(key *bip32.Key, path string) (*bip32.Key, error) {
	if len(path) < 2 || path[0] != 'm' {
		return nil, fmt.Errorf("invalid path: must start with m")
	}
	// Skip "m" or "m/"
	start := 1
	if start < len(path) && path[start] == '/' {
		start++
	}
	current := key
	depth := 0 // count of components derived
	for start < len(path) {
		// Find next /
		end := start
		for end < len(path) && path[end] != '/' {
			end++
		}
		// Parse index
		var idx uint32
		for i := start; i < end; i++ {
			c := path[i]
			if c < '0' || c > '9' {
				return nil, fmt.Errorf("invalid path: non-numeric at %d", i)
			}
			idx = idx*10 + uint32(c-'0')
		}
		// BIP-44: first 3 components are hardened
		if depth < 3 {
			idx += 0x80000000
		}
		child, err := current.NewChildKey(idx)
		if err != nil {
			return nil, fmt.Errorf("derive child %d at depth %d: %w", idx, depth, err)
		}
		current = child
		depth++
		start = end + 1
	}
	return current, nil
}

// PrivateKey returns the raw *ecdsa.PrivateKey for this derived node.
func (h *HDSigner) PrivateKey() (*ecdsa.PrivateKey, error) {
	if h.masterKey == nil {
		return nil, errors.New("no key")
	}
	if !h.masterKey.IsPrivate {
		return nil, errors.New("not a private key node")
	}
	// Convert BIP32 key (32 bytes) to ECDSA private key
	priv, err := crypto.ToECDSA(h.masterKey.Key)
	if err != nil {
		return nil, fmt.Errorf("convert to ecdsa: %w", err)
	}
	return priv, nil
}

// PublicAddress returns the EVM address (0x...) for this derived key.
func (h *HDSigner) PublicAddress() (string, error) {
	priv, err := h.PrivateKey()
	if err != nil {
		return "", err
	}
	addr := crypto.PubkeyToAddress(priv.PublicKey)
	return addr.Hex(), nil
}

// DerivationPath is a helper to build BIP-44 paths for EVM chains.
// All EVM chains use coin_type=60 (Ethereum).
// Format: m/44/60/account/0/address_index
func DerivationPath(account, addressIndex uint32) string {
	return fmt.Sprintf("m/44/60/%d/0/%d", account, addressIndex)
}

// DerivationPathBTC is a convenience helper for Bitcoin-family chains.
// Returns BIP-44 path: m/44/COIN_TYPE/account/0/address_index
func DerivationPathBTC(coinType, account, change, addressIndex uint32) string {
	return fmt.Sprintf("m/44/%d/%d/%d/%d", coinType, account, change, addressIndex)
}

// DerivationPathCustom builds a custom path for non-standard chains.
// For BTC: m/84/0/account/0/index (native segwit)
func DerivationPathCustom(coinType, account, change, addressIndex uint32) string {
	return fmt.Sprintf("m/44/%d/%d/%d/%d", coinType, account, change, addressIndex)
}

// Common paths
const (
	// ETHFirstAddress is the first EVM address (BIP-44 m/44/60/0/0/0)
	ETHFirstAddress = "m/44/60/0/0/0"
	// BTCFirstAddress is the first BTC native segwit address (m/84/0/0/0/0)
	BTCFirstAddress = "m/84/0/0/0/0"
)

// ValidateAddress returns nil if the address is a valid 0x address.
func ValidateAddress(addr string) error {
	if !common.IsHexAddress(addr) {
		return fmt.Errorf("invalid EVM address: %s", addr)
	}
	return nil
}