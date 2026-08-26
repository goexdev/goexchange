package signing

import (
	"strings"
	"testing"
)

// Test vector from BIP-39 spec:
// Mnemonic: "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
// m/44/60/0/0/0 address: 0x9858EfFD232B4033E47d90003D41EC34EcaEda94

const (
	testMnemonic = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	testPath     = "m/44/60/0/0/0"
	testAddress  = "0x9858EfFD232B4033E47d90003D41EC34EcaEda94"
)

func TestHDSigner_NewHDSignerFromMnemonic(t *testing.T) {
	hd, err := NewHDSignerFromMnemonic(testMnemonic)
	if err != nil {
		t.Fatal(err)
	}
	if hd == nil {
		t.Fatal("nil signer")
	}
	if hd.masterKey == nil {
		t.Fatal("nil master key")
	}
}

func TestHDSigner_NewHDSignerFromInvalidMnemonic(t *testing.T) {
	_, err := NewHDSignerFromMnemonic("invalid mnemonic")
	if err == nil {
		t.Error("expected error for invalid mnemonic")
	}
}

func TestHDSigner_Derive(t *testing.T) {
	hd, err := NewHDSignerFromMnemonic(testMnemonic)
	if err != nil {
		t.Fatal(err)
	}
	child, err := hd.Derive(testPath)
	if err != nil {
		t.Fatal(err)
	}
	addr, err := child.PublicAddress()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(addr, testAddress) {
		t.Errorf("expected %s, got %s", testAddress, addr)
	}
}

func TestHDSigner_DeriveMultiple(t *testing.T) {
	hd, err := NewHDSignerFromMnemonic(testMnemonic)
	if err != nil {
		t.Fatal(err)
	}
	addrs := []string{}
	paths := []string{
		"m/44/60/0/0/0",
		"m/44/60/0/0/1",
		"m/44/60/1/0/0",
	}
	for _, p := range paths {
		child, err := hd.Derive(p)
		if err != nil {
			t.Fatal(err)
		}
		addr, err := child.PublicAddress()
		if err != nil {
			t.Fatal(err)
		}
		addrs = append(addrs, addr)
	}
	for i := 0; i < len(addrs); i++ {
		for j := i + 1; j < len(addrs); j++ {
			if addrs[i] == addrs[j] {
				t.Errorf("addresses %d and %d are the same: %s", i, j, addrs[i])
			}
		}
	}
}

func TestDerivationPath(t *testing.T) {
	tests := []struct {
		account, index uint32
		expected       string
	}{
		{0, 0, "m/44/60/0/0/0"},
		{0, 1, "m/44/60/0/0/1"},
		{1, 0, "m/44/60/1/0/0"},
		{5, 10, "m/44/60/5/0/10"},
	}
	for _, tc := range tests {
		got := DerivationPath(tc.account, tc.index)
		if got != tc.expected {
			t.Errorf("DerivationPath(%d, %d) = %s, want %s", tc.account, tc.index, got, tc.expected)
		}
	}
}

func TestValidateAddress(t *testing.T) {
	if err := ValidateAddress("0x9858EfFD232B4033E47d90003D41EC34EcaEda94"); err != nil {
		t.Error(err)
	}
	if err := ValidateAddress("invalid"); err == nil {
		t.Error("expected error for invalid address")
	}
}