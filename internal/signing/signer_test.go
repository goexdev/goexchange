package signing

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
)

// testPrivKey is a deterministic test private key (address: 0x52908400098527886E0F7030069857D2E4169EE7).
// NEVER USE IN PRODUCTION.
func testPrivKey() *ecdsa.PrivateKey {
	// 32-byte hex without 0x prefix
	keyHex := "52908400098527886E0F7030069857D2E4169EE70000000000000000000000000" // placeholder
	_ = keyHex
	key, err := crypto.HexToECDSA("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		panic(err)
	}
	return key
}

func TestLocalSigner_NewLocalSigner_InvalidKey(t *testing.T) {
	cases := []string{
		"not_hex",
		"0x123", // too short
	}
	for _, key := range cases {
		_, err := NewLocalSigner(ChainETH, key, "0x0000000000000000000000000000000000000000", 1)
		if err == nil {
			t.Errorf("expected error for invalid key %q", key)
		}
	}
}

func TestLocalSigner_AddressMatch(t *testing.T) {
	key := testPrivKey()
	privKeyHex := "0x" + hex.EncodeToString(crypto.FromECDSA(key))
	expectedAddr := crypto.PubkeyToAddress(key.PublicKey).Hex()

	signer, err := NewLocalSigner(ChainETH, privKeyHex, expectedAddr, 1)
	if err != nil {
		t.Fatal(err)
	}

	if signer.Address() != expectedAddr {
		t.Errorf("expected %s, got %s", expectedAddr, signer.Address())
	}
	if signer.Name() != "local" {
		t.Errorf("expected name 'local', got %s", signer.Name())
	}
}

func TestLocalSigner_AddressMismatch(t *testing.T) {
	key := testPrivKey()
	privKeyHex := "0x" + hex.EncodeToString(crypto.FromECDSA(key))
	wrongAddr := "0x1111111111111111111111111111111111111111"

	_, err := NewLocalSigner(ChainETH, privKeyHex, wrongAddr, 1)
	if err == nil {
		t.Error("expected error for address mismatch")
	}
	if err != nil && !strings.Contains(err.Error(), "does not match") {
		t.Errorf("expected mismatch error, got %v", err)
	}
}

func TestLocalSigner_SignHash(t *testing.T) {
	key := testPrivKey()
	privKeyHex := "0x" + hex.EncodeToString(crypto.FromECDSA(key))
	expectedAddr := crypto.PubkeyToAddress(key.PublicKey).Hex()

	signer, err := NewLocalSigner(ChainETH, privKeyHex, expectedAddr, 1)
	if err != nil {
		t.Fatal(err)
	}

	hash := crypto.Keccak256([]byte("hello world"))
	signature, err := signer.SignHash(hash)
	if err != nil {
		t.Fatal(err)
	}

	if len(signature) != 65 {
		t.Errorf("expected 65-byte signature, got %d", len(signature))
	}

	// Verify
	pubKey, err := crypto.SigToPub(hash, signature)
	if err != nil {
		t.Fatal(err)
	}
	if crypto.PubkeyToAddress(*pubKey).Hex() != expectedAddr {
		t.Error("signature does not verify")
	}
}

func TestLocalSigner_SignTransaction_Empty(t *testing.T) {
	key := testPrivKey()
	privKeyHex := "0x" + hex.EncodeToString(crypto.FromECDSA(key))
	expectedAddr := crypto.PubkeyToAddress(key.PublicKey).Hex()

	signer, _ := NewLocalSigner(ChainETH, privKeyHex, expectedAddr, 1)
	_, err := signer.SignTransaction(context.Background(), UnsignedTx{})
	if err == nil {
		t.Error("expected error for empty tx data")
	}
}
