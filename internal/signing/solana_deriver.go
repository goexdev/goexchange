// SolanaDeriver handles Solana chain operations.
//
// Solana uses Ed25519 (not secp256k1 like Bitcoin/Ethereum).
// Address = base58(public_key)  - just the 32-byte public key, no hashing
// Signing: ed25519 over the message bytes (no hashing required)
//
// Derivation path: m/44'/501'/0'/0' (all hardened for Ed25519)
// Coin type 501 is registered for Solana (SLIP-0044).
package signing

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/btcsuite/btcd/btcutil/base58"
	"github.com/tyler-smith/go-bip39"
)

// SolanaCoinType is the BIP-44/SLIP-0044 coin type for Solana.
const SolanaCoinType uint32 = 501

// SolanaDeriver implements Ed25519-based key operations for Solana.
//
// Note: This does NOT implement the standard Deriver interface (which uses
// *ecdsa.PrivateKey). It's a separate type with Ed25519-specific methods.
type SolanaDeriver struct{}

// NewSolanaDeriver creates a new Solana deriver.
func NewSolanaDeriver() *SolanaDeriver {
	return &SolanaDeriver{}
}

// Family returns "solana".
func (s *SolanaDeriver) Family() ChainFamily { return FamilySolana }

// CoinType returns 501 (Solana).
func (s *SolanaDeriver) CoinType() uint32 { return SolanaCoinType }

// DeriveSeed derives a 32-byte Ed25519 private key from a BIP-39 mnemonic
// + optional BIP-44 path.
//
// Uses SLIP-0010 (Ed25519-specific BIP-32).
//
// Solana path format: m/44'/501'/0'/0'
// All derivation steps MUST be hardened for Ed25519.
func (s *SolanaDeriver) DeriveSeed(mnemonic, path string) (ed25519.PrivateKey, error) {
	if !bip39.IsMnemonicValid(mnemonic) {
		return nil, errors.New("invalid BIP-39 mnemonic")
	}
	seed := bip39.NewSeed(mnemonic, "") // BIP-39 seed

	// SLIP-0010 master derivation: HMAC-SHA512("ed25519 seed", seed)
	I := hmacSHA512([]byte("ed25519 seed"), seed)
	currentKey := I[:32]
	currentChain := I[32:]

	// Parse and process each path component
	if len(path) < 2 || path[0] != 'm' {
		return nil, fmt.Errorf("invalid path: must start with m")
	}
	start := 1
	if start < len(path) && path[start] == '/' {
		start++
	}

	for start < len(path) {
		// Find next /
		end := start
		for end < len(path) && path[end] != '/' {
			end++
		}
		// Parse index (with optional hardened ' suffix)
		raw := path[start:end]
		hardened := false
		if len(raw) > 1 && raw[len(raw)-1] == '\'' {
			hardened = true
			raw = raw[:len(raw)-1]
		}
		var idx uint32
		if _, err := fmt.Sscanf(raw, "%d", &idx); err != nil {
			return nil, fmt.Errorf("invalid path component %q: %w", path[start:end], err)
		}

		if !hardened {
			return nil, fmt.Errorf("Ed25519 derivation only supports hardened paths (component %q not hardened)", path[start:end])
		}

		// Hardened derivation per SLIP-0010:
		// data = 0x00 || ser256(parent_key) || ser32(index)
		// (ser32 includes the hardened bit, but we already extracted it)
		data := make([]byte, 37)
		data[0] = 0x00
		copy(data[1:33], currentKey)
		binary.BigEndian.PutUint32(data[33:], idx)

		I = hmacSHA512(currentChain, data)
		currentKey = I[:32]
		currentChain = I[32:]

		start = end + 1
	}

	return currentKey, nil
}

// hmacSHA512 returns HMAC-SHA512(key, data) as 64 bytes.
func hmacSHA512(key, data []byte) []byte {
	mac := hmac.New(sha512.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

// DeriveKeyPair derives the full Ed25519 key pair (private + public) at the path.
func (s *SolanaDeriver) DeriveKeyPair(mnemonic, path string) (ed25519.PrivateKey, ed25519.PublicKey, error) {
	seed, err := s.DeriveSeed(mnemonic, path)
	if err != nil {
		return nil, nil, err
	}
	// ed25519.NewKeyFromSeed takes a 32-byte seed and produces a 64-byte private key
	// (seed || derived_public_key)
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	return priv, pub, nil
}

// DeriveAddress computes the Solana address (base58-encoded public key).
//
// Solana address = base58(public_key_bytes)
// Unlike Ethereum (keccak256(pubkey[12:])) or Bitcoin (base58check(ripemd160(sha256(pubkey)))).
// Solana uses the raw public key with no hashing or version byte.
func (s *SolanaDeriver) DeriveAddress(priv ed25519.PrivateKey) string {
	pub := priv.Public().(ed25519.PublicKey)
	return base58.Encode(pub)
}

// DeriveAddressFromMnemonic is a convenience: mnemonic + path -> Solana address.
func (s *SolanaDeriver) DeriveAddressFromMnemonic(mnemonic, path string) (string, error) {
	priv, err := s.DeriveSeed(mnemonic, path)
	if err != nil {
		return "", err
	}
	return s.DeriveAddress(priv), nil
}

// Sign signs a message using Ed25519.
//
// Returns 64-byte signature.
func (s *SolanaDeriver) Sign(priv ed25519.PrivateKey, message []byte) []byte {
	return ed25519.Sign(priv, message)
}

// Verify verifies an Ed25519 signature.
func (s *SolanaDeriver) Verify(pub ed25519.PublicKey, message, signature []byte) bool {
	return ed25519.Verify(pub, message, signature)
}
