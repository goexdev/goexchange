package tron

// Base58 + sha256 helpers for TRON address encoding. Keccak256 is
// also re-exported here because the public adapter file uses it to
// decode TRC20 log topics (keccak256 of "Transfer(address,address,
// uint256)"); we do NOT use keccak for address checksums.

import (
	"crypto/sha256"
	"math/big"

	base58Pkg "github.com/btcsuite/btcd/btcutil/base58"
	"golang.org/x/crypto/sha3"
)

// sha256Of returns the 32-byte SHA-256 digest of b. The wrapper
// exists so adapter.go can call sha256Of() the same way it calls
// keccak() without having to import crypto/sha256 itself.
func sha256Of(b []byte) []byte {
	h := sha256.Sum256(b)
	return h[:]
}

// keccak returns the 32-byte keccak256 hash of b. Used by
// ParseTransferEvents to compare the log topic[0] against the
// canonical Transfer signature.
func keccak(b []byte) []byte {
	h := sha3.NewLegacyKeccak256()
	h.Write(b)
	return h.Sum(nil)
}

// base58Alphabet is the canonical Base58 alphabet (Bitcoin order).
// It excludes 0OIl to keep addresses visually unambiguous.
const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// base58Encode encodes b as a Base58Check string. Used by
// base58CheckEncode for the checksummed TRON address form.
func base58Encode(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	// Treat the byte slice as a single big-endian integer and divide
	// by 58 repeatedly to extract base-58 "digits" from the most
	// significant end. Leading zero bytes become leading '1's.
	n := new(big.Int).SetBytes(b)
	zero := big.NewInt(0)
	fiftyEight := big.NewInt(58)
	mod := new(big.Int)
	var out []byte
	for n.Cmp(zero) > 0 {
		n.DivMod(n, fiftyEight, mod)
		out = append(out, base58Alphabet[mod.Int64()])
	}
	for _, b := range b {
		if b == 0 {
			out = append(out, base58Alphabet[0])
		} else {
			break
		}
	}
	// reverse
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}

// base58Decode reverses base58Encode. We delegate to the canonical
// btcd btcutil implementation rather than rolling our own; an
// earlier hand-rolled decoder produced inconsistent bytes
// (TUEZSdKsoDHQMeZwihtdoBiN46zxhGWYdH decoded to
// 41c8599111f29c1e1e061265b4af93ea1f274ad78a1912f84c instead of
// the canonical 41c8599111f29c1e1e061265b4af93ea1f274ad78a) and
// the round-trip unit test happily accepted both ends.
func base58Decode(s string) ([]byte, error) {
	if len(s) == 0 {
		return []byte{}, nil
	}
	return base58Pkg.Decode(s), nil
}