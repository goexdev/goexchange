package signing

import (
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
)

const testMnemonicDeriver = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

func TestBitcoinDeriver_Address(t *testing.T) {
	tests := []struct {
		name     string
		coinType uint32
		prefix   byte
		want     string
	}{
		{"BTC mainnet", 0, 0x00, "1"},
		{"BTC testnet", 1, 0x6f, "m"},
		{"LTC mainnet", 2, 0x30, "L"},
		{"DOGE mainnet", 3, 0x1e, "D"},
		{"DASH mainnet", 5, 0x4c, "X"},
		{"BCH mainnet", 145, 0x00, "1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hd, err := NewHDSignerFromMnemonic(testMnemonicDeriver)
			if err != nil {
				t.Fatal(err)
			}
			path := DerivationPathBTC(tc.coinType, 0, 0, 0)
			child, err := hd.Derive(path)
			if err != nil {
				t.Fatal(err)
			}
			priv, err := child.PrivateKey()
			if err != nil {
				t.Fatal(err)
			}

			deriver := NewBitcoinDeriver(tc.coinType, tc.prefix)
			addr, err := deriver.DeriveAddress(priv)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(addr, tc.want) {
				t.Errorf("expected %s prefix, got %s", tc.want, addr)
			}
			t.Logf("%s: %s", tc.name, addr)
		})
	}
}

func TestEthereumDeriver_Address(t *testing.T) {
	hd, err := NewHDSignerFromMnemonic(testMnemonicDeriver)
	if err != nil {
		t.Fatal(err)
	}
	child, err := hd.Derive("m/44/60/0/0/0")
	if err != nil {
		t.Fatal(err)
	}
	priv, err := child.PrivateKey()
	if err != nil {
		t.Fatal(err)
	}

	deriver := NewEthereumDeriverMainnet()
	addr, err := deriver.DeriveAddress(priv)
	if err != nil {
		t.Fatal(err)
	}
	want := "0x9858EfFD232B4033E47d90003D41EC34EcaEda94"
	if !strings.EqualFold(addr, want) {
		t.Errorf("expected %s, got %s", want, addr)
	}
}

func TestDeriverFactory(t *testing.T) {
	tests := []struct {
		name   string
		params DeriverParams
		want   ChainFamily
	}{
		{"BTC", DeriverParams{Family: FamilyBitcoin, CoinType: 0, Prefix: 0x00}, FamilyBitcoin},
		{"DASH", DeriverParams{Family: FamilyBitcoin, CoinType: 5, Prefix: 0x4c}, FamilyBitcoin},
		{"EVM", DeriverParams{Family: FamilyEVM, ChainID: 1}, FamilyEVM},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d, err := BuildDeriver(tc.params)
			if err != nil {
				t.Fatal(err)
			}
			if d.Family() != tc.want {
				t.Errorf("expected family %s, got %s", tc.want, d.Family())
			}
		})
	}
}

func TestDeriverFactory_AutoPrefix(t *testing.T) {
	d, err := BuildDeriver(DeriverParams{Family: FamilyBitcoin, CoinType: 5})
	if err != nil {
		t.Fatal(err)
	}
	bd, ok := d.(*BitcoinDeriver)
	if !ok {
		t.Fatal("not a BitcoinDeriver")
	}
	if bd.prefix != 0x4c {
		t.Errorf("expected DASH prefix 0x4c, got 0x%x", bd.prefix)
	}
}

func TestSignedSignatureLength(t *testing.T) {
	priv, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	hash := make([]byte, 32) // zero hash for testing

	btcDeriver := NewBitcoinDeriverMainnet()
	evmDeriver := NewEthereumDeriverMainnet()

	btcSig, err := btcDeriver.SignTransaction(priv, hash)
	if err != nil {
		t.Fatal(err)
	}
	if len(btcSig) != 65 {
		t.Errorf("BTC: expected 65, got %d", len(btcSig))
	}

	evmSig, err := evmDeriver.SignTransaction(priv, hash)
	if err != nil {
		t.Fatal(err)
	}
	if len(evmSig) != 65 {
		t.Errorf("EVM: expected 65, got %d", len(evmSig))
	}
}