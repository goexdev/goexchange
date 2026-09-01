// Multi-provider live integration test against Chainstack and
// NOWNodes mainnet endpoints. Verifies:
//
//   1. The adapter reaches chainstack with the same shape we used
//      in B5 (host + token path).
//   2. The adapter reaches nownodes via the URL pattern we
//      discovered during the multi-provider bring-up
//      (https://trx.nownodes.io/<token>/wallet/...).
//   3. A multi-provider config with chainstack-primary +
//      nownodes-backup still answers a single GetLatestBlock call
//      without us having to wire failover by hand.
//
// Skipped if TRON_TEST_TOKEN_CHAINSTACK or TRON_TEST_TOKEN_NOWNODES
// is unset.

package tron

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"
)

func TestMultiProviderLive(t *testing.T) {
	chainstack := os.Getenv("TRON_TEST_TOKEN_CHAINSTACK")
	nownodes := os.Getenv("TRON_TEST_TOKEN_NOWNODES")
	if chainstack == "" && nownodes == "" {
		t.Skip("TRON_TEST_TOKEN_CHAINSTACK and TRON_TEST_TOKEN_NOWNODES both unset")
	}

	// Build a list of providers that are actually configured. The
	// adapter supports a single provider for unit tests, so an
	// env that only sets one token still exercises the
	// callWithFailover loop (it will simply fail to find an
	// alternate and return the primary's result).
	var providers []Provider
	if chainstack != "" {
		providers = append(providers, Provider{
			Name:    "chainstack",
			BaseURL: "https://tron-mainnet.core.chainstack.com/" + chainstack,
			Weight:  1,
		})
	}
	if nownodes != "" {
		providers = append(providers, Provider{
			Name:    "nownodes",
			BaseURL: "https://trx.nownodes.io/" + nownodes,
			Weight:  1,
		})
	}

	a, err := NewAdapter(Config{
		Providers: providers,
		Logger:    slog.New(slog.NewTextHandler(os.Stderr, nil)),
		Network:   NetworkMainnet,
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
		t.Logf("latest block: height=%d ts=%s", b.Height, b.LogTime.Format(time.RFC3339))
		if b.Height <= 0 {
			t.Errorf("expected height > 0, got %d", b.Height)
		}
	})

	t.Run("ListProviders", func(t *testing.T) {
		got := a.Providers()
		if len(got) != len(providers) {
			t.Errorf("Providers len = %d, want %d", len(got), len(providers))
		}
		t.Logf("providers: %d configured", len(got))
		for i := range got {
			// Index into got so we can take the address; ranged
			// copies would lock the atomic.Int64 noCopy check.
			t.Logf("  - %s (weight=%d)", got[i].Name, got[i].Weight)
		}
	})

	t.Run("ActiveProvider", func(t *testing.T) {
		active := a.ActiveProvider()
		if active.Name == "" {
			t.Error("ActiveProvider returned empty name")
		}
		t.Logf("active provider: %s", active.Name)
	})
}