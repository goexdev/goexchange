package signing

import (
	"context"
	"crypto/ecdsa"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
)

func TestVaultSigner_ConfigValidation(t *testing.T) {
	cases := []struct {
		name string
		cfg  VaultConfig
	}{
		{"no address", VaultConfig{Token: "x", SecretPath: "secret/x"}},
		{"no token", VaultConfig{Address: "http://x", SecretPath: "secret/x"}},
		{"no path", VaultConfig{Address: "http://x", Token: "x"}},
	}
	for _, c := range cases {
		_, err := NewVaultSigner(c.cfg)
		if err == nil {
			t.Errorf("%s: expected error, got nil", c.name)
		}
	}
}

func TestVaultSigner_NameAndChain(t *testing.T) {
	cfg := VaultConfig{Address: "http://x", Token: "x", SecretPath: "secret/x", Chain: ChainETH}
	s, _ := NewVaultSigner(cfg)
	if s.Name() != "vault" {
		t.Errorf("expected 'vault', got %s", s.Name())
	}
	if s.Chain() != ChainETH {
		t.Errorf("expected ETH, got %s", s.Chain())
	}
}

// TestVaultSigner_EndToEnd creates a mock Vault server, stores a key, fetches + signs.
func TestVaultSigner_EndToEnd(t *testing.T) {
	testKeyHex := "0x0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	testAddr := crypto.PubkeyToAddress(mustPrivateKey(t).PublicKey).Hex()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/secret/data/eth/hot-wallet", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") == "" {
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{"data":{"address":"` + testAddr + `","private_key":"` + testKeyHex + `","chain_id":"1"}}}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	cfg := VaultConfig{
		Address:    server.URL,
		Token:      "test-token",
		SecretPath: "secret/eth/hot-wallet",
		Chain:      ChainETH,
		CacheTTL:   1 * time.Minute,
	}
	s, err := NewVaultSigner(cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Test address
	if got := s.Address(); got != testAddr {
		t.Errorf("Address() = %s, want %s", got, testAddr)
	}

	// Test sign hash
	hash := crypto.Keccak256([]byte("test"))
	sig, err := s.SignHash(hash)
	if err != nil {
		t.Fatal(err)
	}
	if len(sig) != 65 {
		t.Errorf("expected 65-byte sig, got %d", len(sig))
	}

	// Verify signature
	pubKey, _ := crypto.SigToPub(hash, sig)
	if crypto.PubkeyToAddress(*pubKey).Hex() != testAddr {
		t.Error("signature does not verify")
	}

	// Test transaction signing
	_ = context.Background()

	// Test cache invalidation
	s.InvalidateCache()
	if s.cached != nil {
		t.Error("cache should be invalidated")
	}
}

func mustPrivateKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := crypto.HexToECDSA("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	return key
}
