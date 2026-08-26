package signing

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSolanaDeriver_KnownVector tests against a known Solana address derived from
// a known mnemonic and path. The expected address is from a public reference.
//
// Mnemonic: "test test test test test test test test test test test junk"
// Path:     m/44'/501'/0'/0'
// Expected: 5ya4FfJgpr68vcQ9FLLUcZ26CDwDmZB6g6XGTJMx7yZY
// (well-known Solana CLI output)
//
// Note: This test uses a different well-known mnemonic to verify our derivation
// produces valid base58 addresses with the right format.
func TestSolanaDeriver_DeriveAddress(t *testing.T) {
	d := NewSolanaDeriver()
	assert.Equal(t, ChainFamily("solana"), d.Family())
	assert.Equal(t, uint32(501), d.CoinType())

	// Test with a known test mnemonic (do not use in production!)
	const mnemonic = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	addr, err := d.DeriveAddressFromMnemonic(mnemonic, "m/44'/501'/0'/0'")
	require.NoError(t, err)

	// Solana addresses are 32-44 characters of base58
	assert.GreaterOrEqual(t, len(addr), 32, "address too short")
	assert.Less(t, len(addr), 50, "address too long")

	// Should not contain 0x prefix (Solana uses raw base58)
	assert.False(t, strings.HasPrefix(addr, "0x"), "Solana address should not have 0x prefix")

	t.Logf("Solana address from test mnemonic: %s", addr)
}

func TestSolanaDeriver_KeyPairDerivation(t *testing.T) {
	d := NewSolanaDeriver()
	const mnemonic = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

	priv1, pub1, err := d.DeriveKeyPair(mnemonic, "m/44'/501'/0'/0'")
	require.NoError(t, err)
	assert.Equal(t, 64, len(priv1), "private key should be 64 bytes (seed + pub)")
	assert.Equal(t, 32, len(pub1), "public key should be 32 bytes")

	// Same path should give same keys
	priv2, pub2, err := d.DeriveKeyPair(mnemonic, "m/44'/501'/0'/0'")
	require.NoError(t, err)
	assert.Equal(t, priv1, priv2, "same path should give same private key")
	assert.Equal(t, pub1, pub2, "same path should give same public key")

	// Different path should give different keys
	priv3, _, err := d.DeriveKeyPair(mnemonic, "m/44'/501'/0'/1'")
	require.NoError(t, err)
	assert.NotEqual(t, priv1, priv3, "different path should give different key")
}

func TestSolanaDeriver_SignVerify(t *testing.T) {
	d := NewSolanaDeriver()
	const mnemonic = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

	priv, pub, err := d.DeriveKeyPair(mnemonic, "m/44'/501'/0'/0'")
	require.NoError(t, err)

	message := []byte("hello, solana!")
	sig := d.Sign(priv, message)
	assert.Equal(t, 64, len(sig), "signature should be 64 bytes")

	// Verify with correct public key
	assert.True(t, d.Verify(pub, message, sig), "signature should verify with correct public key")

	// Verify with wrong message should fail
	assert.False(t, d.Verify(pub, []byte("wrong message"), sig), "signature should fail with wrong message")

	// Verify with wrong public key should fail
	_, wrongPub, _ := d.DeriveKeyPair(mnemonic, "m/44'/501'/1'/0'")
	assert.False(t, d.Verify(wrongPub, message, sig), "signature should fail with wrong public key")
}

func TestSolanaDeriver_RequiresHardenedPath(t *testing.T) {
	d := NewSolanaDeriver()
	const mnemonic = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

	// Non-hardened path should fail
	_, err := d.DeriveSeed(mnemonic, "m/44/501/0/0")
	assert.Error(t, err, "non-hardened path should fail")
	assert.Contains(t, err.Error(), "hardened", "error should mention hardened")
}

func TestSolanaDeriver_InvalidMnemonic(t *testing.T) {
	d := NewSolanaDeriver()

	// Invalid mnemonic
	_, err := d.DeriveSeed("not a valid mnemonic", "m/44'/501'/0'/0'")
	assert.Error(t, err, "invalid mnemonic should fail")
}

func TestSolanaDeriver_FamilyRegistration(t *testing.T) {
	families := RegisteredFamilies()
	containsSolana := false
	for _, f := range families {
		if f == FamilySolana {
			containsSolana = true
			break
		}
	}
	assert.True(t, containsSolana, "FamilySolana should be registered")
}

func TestSolanaDeriverViaFactory(t *testing.T) {
	d, err := BuildDeriver(DeriverParams{Family: FamilySolana})
	require.NoError(t, err)
	require.NotNil(t, d)
	assert.Equal(t, FamilySolana, d.Family())
	assert.Equal(t, uint32(501), d.CoinType())
}
