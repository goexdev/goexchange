package tron

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"
	"time"

	bc "github.com/goexdev/goexchange/internal/blockchain"
)

// TestChainstackIntegration exercises the adapter against a live
// Chainstack endpoint. Skip when the env var is missing (CI without
// secrets) so unit-test runs stay hermetic.
//
// Usage:
//   TRON_TEST_TOKEN=26837778a1d1d405a8bbf9894f08797c go test -v -run TestChainstackIntegration ./internal/blockchain/tron/...
//
// V1: we only smoke-test the read paths (block, transaction). The
// signer-driven build/broadcast paths require V1 Init on the signer
// side and are exercised in B6.
func TestChainstackIntegration(t *testing.T) {
	token := os.Getenv("TRON_TEST_TOKEN")
	if token == "" {
		t.Skip("TRON_TEST_TOKEN not set; skipping live integration test")
	}
	// BaseURL is the host + token path; per-method paths add the
	// "/wallet/{method}" suffix. This avoids the double-prefix bug
	// we hit when the base already included "/wallet".
	//
	// V1.1: pass two distinct entries (primary + backup both
	// pointing at the same URL) so the failover loop exercises the
	// multi-provider code path even in single-provider deploys.
	base := "https://tron-mainnet.core.chainstack.com/" + token
	a, err := NewAdapter(Config{
		Providers: []Provider{
			{Name: "chainstack-test-primary", BaseURL: base, Weight: 1},
			{Name: "chainstack-test-backup", BaseURL: base, Weight: 1},
		},
		Logger:  slog.New(slog.NewTextHandler(os.Stderr, nil)),
		Network: NetworkMainnet,
	})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	t.Run("GetLatestBlock", func(t *testing.T) {
		b, err := a.GetLatestBlock(ctx)
		if err != nil {
			t.Fatalf("GetLatestBlock: %v", err)
		}
		if b.Height == 0 {
			t.Errorf("height is zero")
		}
		t.Logf("latest block: height=%d hash=%s ts=%s",
			b.Height, b.Hash, b.LogTime.Format(time.RFC3339))
	})

	t.Run("GetSolidifiedBlock", func(t *testing.T) {
		b, err := a.GetSolidifiedBlock(ctx)
		if err != nil {
			t.Fatalf("GetSolidifiedBlock: %v", err)
		}
		if !b.IsFinal {
			t.Errorf("solidified block should have IsFinal=true")
		}
		t.Logf("solidified block: height=%d (head was at least this+27)",
			b.Height)
	})

	t.Run("GetBlockByNumber", func(t *testing.T) {
		head, _ := a.GetLatestBlock(ctx)
		h := head.Height
		if h < 100 {
			t.Skip("chain too short to test back-fill")
		}
		b, err := a.GetBlockByNumber(ctx, h-10)
		if err != nil {
			t.Fatalf("GetBlockByNumber(h-10): %v", err)
		}
		if b.Height != h-10 {
			t.Errorf("GetBlockByNumber returned height=%d, want %d", b.Height, h-10)
		}
		t.Logf("block %d: %d tx hashes", b.Height, len(b.TxHashes))
	})

	t.Run("ValidateAddress", func(t *testing.T) {
		// A real mainnet TRON address (Tron Foundation donation
		// address — well-known, Base58Check-valid).
		if !a.ValidateAddress("TLa2f6VPqDgRE67v1736s7bJ8Ray5wYjU7") {
			t.Errorf("ValidateAddress(known good) returned false")
		}
		// Wrong checksum.
		if a.ValidateAddress("TLa2f6VPqDgRE67v1736s7bJ8Ray5wYjU8") {
			t.Errorf("ValidateAddress(known bad) returned true")
		}
	})

	t.Run("ParseTransferEvents", func(t *testing.T) {
		// Events are stored as JSON-encoded strings so the same
		// field can hold heterogeneous log shapes; for the test
		// we encode one log the same way the adapter writes them.
		oneLog, _ := json.Marshal(map[string]any{
			"address": "a614f803b6fd780986a42c79ec8394ade726993d",
			"topics": []string{
				"ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef",
				"000000000000000000000000aabbccddeeff0011223344556677889900aabbcc",
				"0000000000000000000000001122334455667788990aabbccddeeff001122334",
			},
			"data": "0000000000000000000000000000000000000000000000000000000005f5e100",
		})
		tx := bc.Transaction{
			Hash:   "abc",
			Status: bc.TxStatusSuccess,
			Events: []string{string(oneLog)},
		}
		events, err := a.ParseTransferEvents(tx, "41a614f803b6fd780986a42c79ec8394ade726993d")
		if err != nil {
			t.Fatalf("ParseTransferEvents: %v", err)
		}
		if len(events) != 1 {
			t.Fatalf("expected 1 event, got %d", len(events))
		}
		if events[0].Amount.Int64() != 100_000_000 {
			t.Errorf("amount = %d, want 100000000", events[0].Amount.Int64())
		}
	})
}
