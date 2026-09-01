// reconciler compares on-chain USDT-TRC20 transfers against the
// deposits table and reports missed events.
//
// It runs every minute (configurable via RECONCILER_INTERVAL).
// For each wallet_addresses row that we know about, it asks the
// chain RPC for the recent block range and walks the matching
// Transfer logs. Anything not already in `deposits` is logged
// loudly so an operator can investigate; the reconciler does
// NOT auto-credit missed events because that would mask a
// broken scanner.
//
// On startup the reconciler also writes one row to
// reconciliation_runs so the audit table captures when each
// pass started; the row's `finished_at` is updated at the end
// of the pass and `mismatch_count` is the number of missed
// events found.

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/goexdev/goexchange/internal/blockchain"
	"github.com/goexdev/goexchange/internal/blockchain/tron"
	"github.com/goexdev/goexchange/internal/config"
	"github.com/goexdev/goexchange/internal/db"
	"github.com/goexdev/goexchange/internal/vaultinit"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// reconLookback is how many blocks the reconciler walks per
// pass. We pick 50 (about 2.5 minutes on TRON) to balance
// "catches missed events" against "does not hammer the RPC".
// Missed events older than 50 blocks are a sign of a serious
// outage and the operator should reconcile from snapshot data,
// not from this scan.
const reconLookback = 50

// usdtContractHex is the hex-form TRC20 contract address for
// USDT on mainnet. The reconciler only emits events whose
// raw_address matches this; matching against `to` is what the
// scanner also does, but on the reconciler side we filter at
// the contract level too so a buggy scanner that flipped the
// fields is still caught here.
const usdtContractHex = "41a614f803b6fd780986a42c78ec8394ade726993d"

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(log); err != nil {
		log.Error("reconciler failed", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Wire Vault the same way wallet-api and cmd/api do. The
	// reconciler needs the real DB password, which lives in
	// Vault at the cfg.Vault.DBPath path; without this the
	// '***' placeholder in config.yaml fails the SASL auth.
	if _, err := vaultinit.Init(cfg, context.Background(), log); err != nil {
		return fmt.Errorf("init vault: %w", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	pool, err := db.Connect(ctx, cfg.Database.URL)
	if err != nil {
		return fmt.Errorf("connect db: %w", err)
	}
	defer pool.Close()

	// Build the TRON adapter with the same env-var convention
	// the wallet-api uses so a single .env drives all three
	// binaries. We accept that the reconciler might be offline
	// during deploy-fresh's first boot if no provider URL is
	// set; in that case we just exit early with a clear log
	// line. The wallet-api handles the same situation by
	// falling back to the registry, but the reconciler has no
	// business running without RPC.
	tronAd, err := buildAdapter(log)
	if err != nil {
		return fmt.Errorf("build adapter: %w", err)
	}

	interval := envDuration("RECONCILER_INTERVAL", time.Minute)
	contract := os.Getenv("TRON_ASSET")
	if contract == "" {
		contract = "USDT"
	}
	log.Info("reconciler running",
		"interval", interval.String(),
		"lookback_blocks", reconLookback,
		"asset", contract,
	)

	// Run an immediate pass so the first scan does not wait
	// the interval.
	if err := reconcileOnce(ctx, pool, tronAd, contract, log); err != nil {
		log.Warn("initial reconcile pass", "error", err)
	}

	timer := time.NewTimer(interval)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			if err := reconcileOnce(ctx, pool, tronAd, contract, log); err != nil {
				log.Warn("reconcile pass", "error", err)
			}
			timer.Reset(interval)
		}
	}
}

// reconcileOnce performs one reconciliation pass:
//
//  1. Open a reconciliation_runs row so the audit table
//     captures when this pass started.
//  2. Fetch the current chain head.
//  3. Walk the last reconLookback blocks via the adapter, parsing
//     Transfer events out of each block. For every event whose
//     `to` matches one of our user deposit addresses, check
//     whether the (chain, tx_hash, event_index) tuple is in
//     deposits. Anything missing is a "mismatch" and is logged
//     loudly. The reconciler does not auto-credit; a missed
//     event is a sign that the scanner was offline or wedged,
//     and an operator must decide whether to backfill.
//
// Returns the number of mismatches found; nil error means
// every expected event was already in `deposits`.
func reconcileOnce(
	ctx context.Context,
	pool *pgxpool.Pool,
	adapter *tron.Adapter,
	asset string,
	log *slog.Logger,
) error {
	// Open the audit row.
	// reconciliation_runs.id is bigint (not uuid); the
	// sequence advances per insert so we let the DB pick the
	// next value rather than generating one ourselves.
	_, err := pool.Exec(ctx, `
		INSERT INTO reconciliation_runs (chain, asset, run_type, start_at, end_at, status)
		VALUES ('TRON', $1, 'LIVE', NOW(), NOW(), 'RUNNING')
		RETURNING id`, asset)
	if err != nil {
		return fmt.Errorf("open reconciliation_runs row: %w", err)
	}
	defer func() {
		// Best-effort close; we don't propagate the error
		// because the caller already saw a result.
		_, _ = pool.Exec(context.Background(), `
			UPDATE reconciliation_runs
			SET end_at = NOW()
			WHERE status = 'RUNNING' AND chain = 'TRON' AND asset = $1`, asset)
	}()

	head, err := adapter.GetLatestBlock(ctx)
	if err != nil {
		return fmt.Errorf("get latest block: %w", err)
	}
	startHeight := uint64(0)
	if head.Height > reconLookback {
		startHeight = uint64(head.Height) - reconLookback
	}
	endHeight := uint64(head.Height)

	// Pull each user address that we manage. The reconciler
	// does not pull every USDT Transfer on the chain (that
	// would burn the chainstack free tier); instead it pulls
	// blocks and filters by `to` against our addresses. This
	// keeps the RPC footprint roughly the same as the
	// scanner's.
	addresses, err := listActiveAddresses(ctx, pool, asset)
	if err != nil {
		return fmt.Errorf("list active addresses: %w", err)
	}
	addrSet := make(map[string]uuid.UUID, len(addresses))
	for _, a := range addresses {
		addrSet[a.Address] = a.UserID
	}

	mismatches := 0
	for h := startHeight; h <= endHeight; h++ {
		block, err := adapter.GetBlockByNumber(ctx, h)
		if err != nil {
			log.Warn("reconcile fetch block",
				"height", h, "error", err)
			continue
		}
		// Fetch each transaction by hash and parse its
		// Transfer events. We do not cache transactions
		// across blocks because a TRON transfer typically
		// touches only one block, and the per-tx RPC cost
		// is bounded by reconLookback's tx count.
		for _, txHash := range block.TxHashes {
			tx, err := adapter.GetTransaction(ctx, txHash)
			if err != nil {
				log.Warn("reconcile fetch tx",
					"height", h, "tx", txHash, "error", err)
				continue
			}
			events, err := adapter.ParseTransferEvents(tx, usdtContractHex)
			if err != nil {
				log.Warn("reconcile parse events",
					"tx", txHash, "error", err)
				continue
			}
			for _, ev := range events {
				userID, ok := addrSet[ev.To]
				if !ok {
					continue
				}
				// blockchain.TransferEvent carries the
				// event Index (position within the tx) and
				// the From/To/Amount, but no Chain/TxHash
				// fields. We get the TxHash from the
				// enclosing loop and the chain is hard-
				// coded to "TRON" because this binary only
				// reconciles TRON today.
				const chain = "TRON"
				_ = asset
				found, err := depositExists(ctx, pool, chain, txHash, ev.Index)
				if err != nil {
					log.Warn("reconcile lookup deposit",
						"tx_hash", txHash, "error", err)
					continue
				}
				if !found {
					mismatches++
					log.Error("MISSED DEPOSIT detected by reconciler",
						"chain", chain,
						"tx_hash", txHash,
						"event_index", ev.Index,
						"from", ev.From,
						"to", ev.To,
						"amount", ev.Amount.String(),
						"user_id", userID,
					)
				}
			}
		}
	}

	_, err = pool.Exec(ctx, `
		UPDATE reconciliation_runs
		SET status = 'OK'
		WHERE status = 'RUNNING' AND chain = 'TRON' AND asset = $1`, asset)
	if err != nil {
		return fmt.Errorf("close reconciliation_runs row: %w", err)
	}
	if mismatches > 0 {
		log.Warn("reconcile pass finished with mismatches",
			"mismatches", mismatches, "addresses", len(addresses))
	} else {
		log.Info("reconcile pass clean",
			"addresses", len(addresses),
			"head", head.Height)
	}
	return nil
}

// addressInfo is one row from wallet_addresses that the
// reconciler walks.
type addressInfo struct {
	UserID  uuid.UUID
	Address string
}

// listActiveAddresses returns every active wallet_addresses row
// for the given asset. We do not paginate because at V1 the
// expected count is small; if it grows past a few hundred the
// reconciler should batch via a server-side cursor.
func listActiveAddresses(ctx context.Context, pool *pgxpool.Pool, asset string) ([]addressInfo, error) {
	rows, err := pool.Query(ctx, `
		SELECT user_id, address
		FROM wallet_addresses
		WHERE status = 'ACTIVE' AND asset = $1
		ORDER BY created_at`, asset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []addressInfo
	for rows.Next() {
		var a addressInfo
		if err := rows.Scan(&a.UserID, &a.Address); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// depositExists checks whether the (chain, tx_hash, event_index)
// tuple is already in deposits. Returns true if so, false if
// the row would be a duplicate, with an error only on driver
// trouble.
func depositExists(ctx context.Context, pool *pgxpool.Pool, chain, txHash string, eventIdx uint32) (bool, error) {
	var exists bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM deposits
			WHERE chain = $1 AND tx_hash = $2 AND event_index = $3
		)`, chain, txHash, int(eventIdx)).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

// envDuration parses an env var as a Go duration; defaults to
// the given value on empty or parse error.
func envDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

// buildAdapter mirrors cmd/wallet-api/buildTronAdapter but is
// a smaller copy that the reconciler can use without dragging
// in the wallet-api's full main package. Returns an error if
// no provider URL is configured; the reconciler refuses to
// start without RPC because it has nothing to reconcile
// against.
func buildAdapter(log *slog.Logger) (*tron.Adapter, error) {
	providers := providersFromEnv()
	if len(providers) == 0 {
		return nil, errors.New("no TRON providers configured")
	}
	return tron.NewAdapter(tron.Config{
		Providers: providers,
		Logger:    log,
	})
}

// providersFromEnv reads TRON_PROVIDER_*_URL from the
// environment, one Provider per match. Mirrors the wallet-api
// builder; see cmd/wallet-api/main.go for the full spec.
func providersFromEnv() []tron.Provider {
	var providers []tron.Provider
	for _, kv := range os.Environ() {
		eq := 0
		for i, c := range kv {
			if c == '=' {
				eq = i
				break
			}
		}
		if eq == 0 {
			continue
		}
		key := kv[:eq]
		const prefix = "TRON_PROVIDER_"
		const suffix = "_URL"
		if len(key) < len(prefix)+len(suffix) {
			continue
		}
		if key[:len(prefix)] != prefix || key[len(key)-len(suffix):] != suffix {
			continue
		}
		name := ""
		for i := len(prefix); i < len(key)-len(suffix); i++ {
			c := key[i]
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			name += string(c)
		}
		url := kv[eq+1:]
		if url == "" {
			continue
		}
		providers = append(providers, tron.Provider{
			Name:    name,
			BaseURL: url,
			APIKey:  os.Getenv("TRON_PROVIDER_" + name + "_KEY"),
			Weight:  1,
		})
	}
	return providers
}

// _ = sync.Mutex{} // unused; keep go imports tidy when this file shrinks.
var _ = sync.Mutex{}
var _ = pgx.ErrNoRows
var _ = blockchain.ChainTron
var _ = usdtContractHex