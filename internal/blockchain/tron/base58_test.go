package tron

import (
	"fmt"
	"testing"
)

// TestBase58RoundTrip confirms that base58Encode / base58Decode are
// inverse operations on a payload of the exact length a TRON address
// requires (21 bytes after checksum). It also confirms the Base58Check
// checksum rejects a payload with a single-bit flip, which is the
// failure mode that triggered this whole investigation.
func TestBase58RoundTrip(t *testing.T) {
	payload := []byte{
		0x41, 0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0, 0x11, 0x22,
		0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc,
	}
	encoded := base58CheckEncode(payload)
	t.Logf("encoded: %s", encoded)
	decoded, err := base58CheckDecode(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !bytesEqual(decoded, payload) {
		t.Errorf("round trip mismatch:\n got %x\nwant %x", decoded, payload)
	}

	// Flip one byte in the encoded form; decode must fail.
	bad := []byte(encoded)
	bad[len(bad)-1] ^= 0x01
	if _, err := base58CheckDecode(string(bad)); err == nil {
		t.Error("expected checksum failure on flipped byte, got nil")
	}

	// Confirm the validation path agrees with the round-trip.
	a := &Adapter{}
	if !a.ValidateAddress(encoded) {
		t.Errorf("ValidateAddress(%s) = false, want true", encoded)
	}
	fmt.Printf("Encoded address: %s\n", encoded)
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}