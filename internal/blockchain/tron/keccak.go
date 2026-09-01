package tron

// Base58 + sha256 helpers for TRON address encoding. Keccak256 is
// also re-exported here because the public adapter file uses it to
// decode TRC20 log topics (keccak256 of "Transfer(address,address,
// uint256)"); we do NOT use keccak for address checksums.

import (
	"bytes"
	"crypto/sha256"
	"math/big"

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

// base58Decode reverses base58Encode.
func base58Decode(s string) ([]byte, error) {
	if len(s) == 0 {
		return []byte{}, nil
	}
	n := new(big.Int)
	for _, c := range s {
		idx := bytes.IndexByte([]byte(base58Alphabet), byte(c))
		if idx < 0 {
			return nil, errBadChar{c: c}
		}
		n.Mul(n, big.NewInt(58))
		n.Add(n, big.NewInt(int64(idx)))
	}
	// Convert back to bytes, padding with one zero byte per leading
	// base58 '1' so the encoded length matches.
	out := n.Bytes()
	var leadingZeros int
	for _, c := range s {
		if c == rune(base58Alphabet[0]) {
			leadingZeros++
		} else {
			break
		}
	}
	return append(make([]byte, leadingZeros), out...), nil
}

// errBadChar wraps the offending character so the caller can log it.
type errBadChar struct{ c rune }

func (e errBadChar) Error() string { return "base58: invalid character in input" }