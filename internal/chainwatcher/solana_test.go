package chainwatcher

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSolanaDriver_Name(t *testing.T) {
	d, err := NewSolanaDriver(SolanaConfig{
		RPCURL: "https://api.mainnet-beta.solana.com",
	}, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	require.NoError(t, err)
	assert.Equal(t, "solana", d.Name())
	assert.Equal(t, "SOL", d.asset)
}

func TestSolanaDriver_IsValidAddress(t *testing.T) {
	tests := []struct {
		addr  string
		valid bool
	}{
		// Valid Solana addresses (Base58, 32-44 chars)
		{"11111111111111111111111111111111", true},
		{"EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", true}, // USDC
		{"5ya4FfJgpr68vcQ9FLLUcZ26CDwDmZB6g6XGTJMx7yZY", true},
		// Invalid
		{"", false},
		{"0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb1", false}, // 0x prefix
		{"0123456789012345678901234567890123456789012", false}, // contains 0
		{"abc", false}, // too short
		{string(make([]byte, 100)), false}, // too long
	}

	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			got := isValidSolanaAddress(tt.addr)
			assert.Equal(t, tt.valid, got, "addr=%q", tt.addr)
		})
	}
}

func TestSolanaDriver_LamportsToSOL(t *testing.T) {
	tests := []struct {
		lamports uint64
		want     string
	}{
		{0, "0"},
		{1_000_000_000, "1"},
		{1_500_000_000, "1.5"},
		{500_000_000, "0.5"},
	}
	for _, tt := range tests {
		got := lamportsToSOL(tt.lamports).String()
		assert.Equal(t, tt.want, got, "lamports=%d", tt.lamports)
	}
}

func TestSolanaDriver_GenerateAddressNotSupported(t *testing.T) {
	d, _ := NewSolanaDriver(SolanaConfig{RPCURL: "http://localhost:8899"}, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	_, err := d.GenerateAddress(context.Background())
	assert.Error(t, err)
	assert.Equal(t, ErrNotSupported, err)
}

func TestSolanaDriver_HasNoSigner(t *testing.T) {
	d, _ := NewSolanaDriver(SolanaConfig{RPCURL: "http://localhost:8899"}, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	assert.False(t, d.HasSigner(), "Solana driver should not have a built-in signer (signing is external)")
}

func TestSolanaDriver_GetHotAddress(t *testing.T) {
	d, _ := NewSolanaDriver(SolanaConfig{
		RPCURL:  "http://localhost:8899",
		HotAddr: "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
	}, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	assert.Equal(t, "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", d.GetHotAddress())
}

func TestSolanaDriver_RegisteredInFactory(t *testing.T) {
	// Just verify the driver is registered (factory init() runs on import)
	types := factory.DriverTypes()
	found := false
	for _, t := range types {
		if t == "solana" {
			found = true
			break
		}
	}
	assert.True(t, found, "solana driver should be registered in factory")
}
