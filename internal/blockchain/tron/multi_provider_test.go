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
	drpc := os.Getenv("TRON_TEST_TOKEN_DRPC")
	dwellir := os.Getenv("TRON_TEST_TOKEN_DWELLIR")
	if chainstack == "" && nownodes == "" && drpc == "" && dwellir == "" {
		t.Skip("TRON_TEST_TOKEN_{CHAINSTACK,NOWNODES,DRPC,DWELLIR} all unset")
	}

	// Build a list of providers that are actually configured. The
	// adapter supports a single provider for unit tests, so an
	// env that only sets one token still exercises the
	// callWithFailover loop (it will simply fail to find an
	// alternate and return the primary's result).
	//
	// Provider URL conventions we have verified against live
	// chains (2026-09-01):
	//
	//   - chainstack: token in path, /wallet/{method} (legacy
	//     HTTP API shape). Same path format for all read+write
	//     methods; verbs split via RPCMethod prefix.
	//
	//   - nownodes: token in path, /wallet/{method}. Identical
	//     shape to chainstack on the wire; only the host differs.
	//
	//   - drpc.org: token in path, /wallet/{method}. Same legacy
	//     HTTP API shape as chainstack/nownodes. drpc.org's free
	//     plan returns HTTP 400 code:35 ("method is not
	//     available on free plan") on every method we probed;
	//     a paid plan unlocks them. The adapter treats that as a
	//     generic 4xx and fails over to the next provider, which
	//     means drpc.org naturally degrades to chainstack+nownodes
	//     on a free-tier key.
	//
	//   - dwellir: token in path, /wallet/{method}. Same legacy
	//     HTTP API shape; identical on the wire to chainstack.
	//     dwellir also exposes /jsonrpc (rejects the
	//     {"jsonrpc":"2.0","method":...} envelope with code:-32601)
	//     and /walletsolidity, so V2 could add a
	//     RPCStyleJSONRPC mode that routes via /jsonrpc for
	//     dwellir specifically.
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
	if drpc != "" {
		providers = append(providers, Provider{
			Name:    "drpc",
			BaseURL: "https://lb.drpc.live/tron/" + drpc,
			Weight:  1,
		})
	}
	if dwellir != "" {
		providers = append(providers, Provider{
			Name:    "dwellir",
			BaseURL: "https://api-tron-mainnet.n.dwellir.com/" + dwellir,
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
		// Failover check: if drpc.org is in the list and its
		// free plan rejected the call, the active provider
		// should now point at the next available one.
		if drpc != "" {
			active := a.ActiveProvider()
			t.Logf("active provider after GetLatestBlock: %s", active.Name)
			if active.Name == "drpc" {
				t.Log("drpc answered; either it has a paid key or the free plan unlocked the method")
			} else {
				t.Logf("drpc failed (likely free-plan 400); failed over to %s", active.Name)
			}
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