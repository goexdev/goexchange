// tron-scanner daemon: polls the TRON adapter for new blocks,
// walks every USDT-TRC20 Transfer event, and persists credits to
// the deposits table via the ledger module.
//
// Background:
//   B5 of the wallet V1 plan (BOSS 2026-09-01). The scanner is
//   intentionally a separate process from cmd/api and cmd/wallet-api
//   so a runaway scan (RPC stuck, panic, OOM) cannot take user-facing
//   endpoints down. V1 runs a single scanner instance; V2 will run
//   one per chain with cursor persistence per chain.
//
// Configuration is via environment variables so docker compose can
// wire it from .env without touching code:
//
//   TRON_PRIMARY_URL          e.g. https://...chainstack.com/{token}
//   TRON_PRIMARY_KEY          TRON-PRO-API-KEY header (optional)
//   TRON_BACKUP_URL           failover endpoint (or same as primary)
//   TRON_BACKUP_KEY           TRON-PRO-API-KEY for the failover
//   TRON_POLL_SEC             polling interval (default 10)
//   TRON_ASSET                contract to track (default USDT-TRC20)
//   SCANNER_BATCH_BLOCKS      blocks per tick (default 10)
//
// scanner is read-only on the wallet side: it never holds
// private keys, never broadcasts, and never signs transactions. The
// signer daemon is a separate concern (B3).
//
// V1 watches the full USDT-TRC20 contract; events that match a
// user deposit address (looked up from `wallet_addresses`) are
// persisted as deposit rows. Events that hit an address not in
// our table are silently dropped — we are not a free indexer.
//
// Rate-limit handling (V1): Chainstack free tier is ~5 sustained
// RPS with ~25 burst (sliding-window token bucket). The scanner
// defaults to one poll every 10 seconds, which translates to ~5
// RPS including getnowblock + getblockbynum + one
// gettransactioninfobyid per tx. That is right at the free-tier
// ceiling; a paid plan can crank TRON_POLL_SEC down to 2 without
// hitting limits. On adapter errors we double the next interval up
// to a 5-minute cap so a provider outage does not hammer the API.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/goexdev/goexchange/internal/blockchain"
	tronadapter "github.com/goexdev/goexchange/internal/blockchain/tron"
	"github.com/goexdev/goexchange/internal/config"
	"github.com/goexdev/goexchange/internal/db"
	"github.com/goexdev/goexchange/internal/vaultinit"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	log.Info("starting goexchange-tron-scanner")

	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Error("load config", "error", err)
		os.Exit(1)
	}

	vaultClient, err := vaultinit.Init(cfg, context.Background(), log)
	if err != nil {
		log.Error("init vault", "error", err)
		os.Exit(1)
	}
	_ = vaultClient

	pool, err := db.Connect(context.Background(), cfg.Database.URL)
	if err != nil {
		log.Error("connect db", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	adapter, err := buildAdapter(log)
	if err != nil {
		log.Error("build tron adapter", "error", err)
		os.Exit(1)
	}

	registry := blockchain.NewRegistry()
	registry.Register(adapter)
	for _, c := range blockchain.RegisterStubs(registry) {
		_ = c
	}

	pollSec := envInt("TRON_POLL_SEC", 10)
	asset := os.Getenv("TRON_ASSET")
	if asset == "" {
		asset = "USDT"
	}
	contract := contractForAsset(asset)
	if contract == "" {
		log.Error("asset has no known contract", "asset", asset)
		os.Exit(1)
	}

	scanner := tronadapter.NewScanner(adapter, log)
	scanner.AddWatchByContract(contract)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Cursor lives in scanner_state so a restart picks up where
	// the previous run left off. Migrations create the table; we
	// just read/write.
	if cur, err := loadCursor(ctx, pool, "TRON"); err != nil {
		log.Warn("cursor load failed (starting from head - 27)", "error", err)
	} else {
		scanner.SetCursor(cur)
		log.Info("scanner cursor loaded", "cursor", cur)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		log.Info("tron-scanner shutting down")
		cancel()
	}()

	// V1 uses a fixed-interval ticker plus exponential backoff on
	// adapter errors. After every failure we double the next
	// interval up to a 5-minute cap so a brief chain RPC outage
	// does not hammer the provider; on the next success we reset
	// back to the configured baseline. Chainstack free tier is
	// ~5 sustained RPS with ~25 burst; one tick at the default
	// 10s interval is well under the limit.
	baseInterval := time.Duration(pollSec) * time.Second
	currentInterval := baseInterval
	const maxInterval = 5 * time.Minute
	consecutiveFails := 0
	timer := time.NewTimer(currentInterval)
	defer timer.Stop()

	log.Info("tron-scanner running",
		"poll_sec", pollSec,
		"asset", asset,
		"contract", contract,
	)
	// Run an immediate scan so the first events are not delayed
	// by the first tick.
	if err := runOnce(ctx, scanner, pool, registry, adapter, contract, log); err != nil {
		log.Warn("initial scan", "error", err)
		consecutiveFails++
		currentInterval = bumpInterval(baseInterval, currentInterval, maxInterval)
	} else {
		consecutiveFails = 0
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if err := runOnce(ctx, scanner, pool, registry, adapter, contract, log); err != nil {
				log.Warn("scan tick", "error", err)
				consecutiveFails++
				currentInterval = bumpInterval(baseInterval, currentInterval, maxInterval)
				log.Info("backing off",
					"next_interval_sec", int(currentInterval.Seconds()),
					"consecutive_fails", consecutiveFails)
			} else {
				if consecutiveFails > 0 {
					log.Info("recovered from failures", "consecutive_fails", consecutiveFails)
				}
				consecutiveFails = 0
				currentInterval = baseInterval
			}
			timer.Reset(currentInterval)
		}
	}
}

// bumpInterval doubles the current interval, capped at max. Called
// on every adapter error so a sustained outage backs off to 5
// minutes rather than hammering the provider every 10 seconds.
func bumpInterval(base, current, max time.Duration) time.Duration {
	next := current * 2
	if next < base {
		next = base // first failure: at least honour the baseline
	}
	if next > max {
		return max
	}
	return next
}

// runOnce performs one polling cycle. Wrapped so the goroutine in
// main can call it without 6 positional arguments.
func runOnce(
	ctx context.Context,
	scanner *tronadapter.Scanner,
	pool *pgxpool.Pool,
	registry *blockchain.Registry,
	adapter blockchain.Adapter,
	contract string,
	log *slog.Logger,
) error {
	events := 0
	delivered := make(chan tronadapter.Event, 64)
	errCh := make(chan error, 1)

	go func() {
		n, err := scanner.RunOnce(ctx, func(e tronadapter.Event) {
			delivered <- e
		})
		events = n
		errCh <- err
	}()

	// Drain until the scanner returns or we time out. A single
	// tick should complete in under 30s on a healthy chain; we
	// keep a hard 60s cap so a wedged RPC cannot pin the loop.
	timer := time.NewTimer(60 * time.Second)
	defer timer.Stop()

	persisted := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return errors.New("scan tick timed out after 60s")
		case err := <-errCh:
			if err != nil {
				return err
			}
			// Drain any remaining events the scanner pushed before
			// returning.
			for {
				select {
				case e := <-delivered:
					if err := persistEvent(ctx, pool, registry, adapter, contract, e, log); err != nil {
						log.Warn("persist event failed",
							"hash", e.TxHash, "index", e.EventIdx, "error", err)
						continue
					}
					persisted++
				default:
					if err := saveCursor(ctx, pool, "TRON", scanner.GetCursor()); err != nil {
						log.Warn("cursor save failed", "error", err)
					}
					// After fresh deposits are inserted, walk
					// the PENDING table and promote rows whose
					// block has enough confirmations on top.
					// confirmAndCredit is read-only on the RPC
					// (GetLatestBlock) so it does not amplify
					// the rate-limit pressure from the
					// scanner's own calls.
					confirmed, credited, err := confirmAndCredit(ctx, pool, adapter, log)
					if err != nil {
						log.Warn("confirm/credit pass failed", "error", err)
					} else if confirmed > 0 || credited > 0 {
						log.Info("deposit promotion pass",
							"confirmed", confirmed, "credited", credited)
					}
					log.Info("scan tick ok", "events", events, "persisted", persisted)
					return nil
				}
			}
		case e := <-delivered:
			if err := persistEvent(ctx, pool, registry, adapter, contract, e, log); err != nil {
				log.Warn("persist event failed",
					"hash", e.TxHash, "index", e.EventIdx, "error", err)
				continue
			}
			persisted++
		}
	}
}

// confirmThreshold is the number of blocks on top of a deposit's
// block before we promote PENDING -> CONFIRMED. TRON finalized
// in ~1 minute historically (19 blocks at 3s/block); 19 is the
// common exchange choice. Lower numbers make deposits
// available faster but expose the user to a chain reorg
// window; higher numbers are safer but slower.
const confirmThreshold = 19

// confirmAndCredit walks the deposits table once per scan tick
// and:
//
//   1. Promotes PENDING rows whose block has >= confirmThreshold
//      blocks on top to CONFIRMED, updating confirmations and
//      confirmed_at.
//   2. Promotes CONFIRMED rows to CREDITED, atomically inserting
//      the matching amount into balances (available) for the
//      user/asset pair.
//
// Both promotions are conditional UPDATEs so a row that gets
// touched by two scanner instances simultaneously is safe —
// only one wins, the other sees zero rows updated.
func confirmAndCredit(
	ctx context.Context,
	pool *pgxpool.Pool,
	adapter blockchain.Adapter,
	log *slog.Logger,
) (int, int, error) {
	// Step 1: PENDING -> CONFIRMED.
	//
	// We need the chain's current block height to compute
	// confirmations. callWithFailover in the adapter will walk
	// providers, so a single rate-limited chainstack does not
	// pin the whole pipeline.
	head, err := adapter.GetLatestBlock(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("get latest block: %w", err)
	}
	ct, err := pool.Exec(ctx, `
		UPDATE deposits
		SET status = 'CONFIRMED',
		    confirmations = LEAST(confirmations, $1::bigint - block_number),
		    confirmed_at = COALESCE(confirmed_at, NOW())
		WHERE status = 'PENDING'
		  AND block_number IS NOT NULL
		  AND block_number <= $1::bigint - $2`,
		int64(head.Height), int64(confirmThreshold),
	)
	if err != nil {
		return 0, 0, fmt.Errorf("promote pending->confirmed: %w", err)
	}
	confirmed := int(ct.RowsAffected())

	// Step 2: CONFIRMED -> CREDITED + ledger update.
	//
	// We do this in one transaction per deposit so a partial
	// failure leaves no half-credited rows. The query picks
	// CONFIRMED rows that have not yet been credited and
	// updates them in place while inserting a balances row.
	//
	// We rely on deposits.user_id and deposits.asset being
	// already validated by the persistEvent path; the unique
	// (chain, tx_hash, event_index) constraint prevents double
	// credit on a re-scan.
	tx, err := pool.Begin(ctx)
	if err != nil {
		return int(confirmed), 0, fmt.Errorf("begin credit tx: %w", err)
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		SELECT id, user_id, asset, amount
		FROM deposits
		WHERE status = 'CONFIRMED'
		  AND credited_at IS NULL
		LIMIT 100
		FOR UPDATE SKIP LOCKED`)
	if err != nil {
		return int(confirmed), 0, fmt.Errorf("select confirmed: %w", err)
	}
	type creditRow struct {
		id     uuid.UUID
		userID uuid.UUID
		asset  string
		amount string // numeric -> string to preserve precision
	}
	var pending []creditRow
	for rows.Next() {
		var r creditRow
		if err := rows.Scan(&r.id, &r.userID, &r.asset, &r.amount); err != nil {
			rows.Close()
			return int(confirmed), 0, fmt.Errorf("scan confirmed row: %w", err)
		}
		pending = append(pending, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return int(confirmed), 0, fmt.Errorf("iterate confirmed: %w", err)
	}

	credited := 0
	for _, r := range pending {
		// Upsert into balances. The CHECK constraint on
		// balances.available ensures we never go negative;
		// ON CONFLICT increments the existing row.
		if _, err := tx.Exec(ctx, `
			INSERT INTO balances (user_id, asset, available, frozen, updated_at)
			VALUES ($1, $2, $3::numeric, 0, NOW())
			ON CONFLICT (user_id, asset) DO UPDATE
			SET available = balances.available + EXCLUDED.available,
			    updated_at = NOW()`,
			r.userID, r.asset, r.amount); err != nil {
			return int(confirmed), credited, fmt.Errorf("credit balance for deposit %s: %w", r.id, err)
		}
		// Mark deposit as credited.
		if _, err := tx.Exec(ctx, `
			UPDATE deposits
			SET status = 'CREDITED', credited_at = NOW()
			WHERE id = $1 AND status = 'CONFIRMED'`, r.id); err != nil {
			return int(confirmed), credited, fmt.Errorf("mark deposit %s credited: %w", r.id, err)
		}
		credited++
		log.Info("deposit credited",
			"deposit_id", r.id,
			"user_id", r.userID,
			"asset", r.asset,
			"amount", r.amount,
		)
	}
	if err := tx.Commit(ctx); err != nil {
		return int(confirmed), credited, fmt.Errorf("commit credit tx: %w", err)
	}
	return int(confirmed), credited, nil
}

// persistEvent writes one Transfer event as a deposit row. The
// (chain, tx_hash, event_index) unique constraint on `deposits`
// (migration 0029) makes this idempotent — a re-scan of the same
// block will not create a duplicate row.
//
// We rely on the scanner's watched-set (set to the USDT contract)
// for the `to` filter; this method assumes the event has already
// been pre-filtered. The match against user deposit addresses is
// done by a SQL JOIN on wallet_addresses.
func persistEvent(
	ctx context.Context,
	pool *pgxpool.Pool,
	_ *blockchain.Registry,
	_ blockchain.Adapter,
	contract string,
	e tronadapter.Event,
	log *slog.Logger,
) error {
	// 1. Find a user whose deposit address matches the event's `to`.
	//    If none match, the deposit is for an external address and
	//    is silently ignored — we are not a free indexer.
	var (
		userID    uuid.UUID
		addressID uuid.UUID
		asset     string
	)
	err := pool.QueryRow(ctx, `
		SELECT id, user_id, asset
		FROM wallet_addresses
		WHERE address = $1 AND status = 'ACTIVE'
		LIMIT 1`, e.To).Scan(&addressID, &userID, &asset)
	if errors.Is(err, pgx.ErrNoRows) {
		// Not our user; not an error.
		return nil
	}
	if err != nil {
		return fmt.Errorf("lookup wallet_addresses: %w", err)
	}

	// 2. Insert into deposits with the idempotency key
	//    (chain, tx_hash, event_index). ON CONFLICT DO NOTHING so
	//    a re-poll of the same block is a no-op. block_number
	//    is required by the confirm/credit pass; it stays NULL
	//    only if the scanner's BlockNum field is zero, which is
	//    a bug we want to surface via the logs.
	_, err = pool.Exec(ctx, `
		INSERT INTO deposits (
			id, user_id, asset, chain, amount, tx_hash,
			from_address, to_address, confirmations, status,
			block_number
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 1, 'PENDING', $9)
		ON CONFLICT (chain, tx_hash, event_index) DO NOTHING`,
		uuid.New(), userID, asset, e.Chain, e.Amount, e.TxHash,
		e.From, e.To, int64(e.BlockNum))
	if err != nil {
		return fmt.Errorf("insert deposit: %w", err)
	}

	log.Info("deposit detected",
		"user_id", userID,
		"asset", asset,
		"amount", e.Amount,
		"tx_hash", e.TxHash,
	)
	return nil
}

// loadCursor and saveCursor persist the scanner's height so a
// restart picks up where the previous run stopped. The single-row
// table lives at scanner_state(chain, last_scanned_block).
func loadCursor(ctx context.Context, pool *pgxpool.Pool, chain string) (uint64, error) {
	var h uint64
	err := pool.QueryRow(ctx,
		`SELECT last_scanned_block FROM scanner_state WHERE chain = $1`, chain).Scan(&h)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	return h, err
}

func saveCursor(ctx context.Context, pool *pgxpool.Pool, chain string, height uint64) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO scanner_state (chain, last_scanned_block, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (chain) DO UPDATE
		SET last_scanned_block = EXCLUDED.last_scanned_block,
		    updated_at = EXCLUDED.updated_at`, chain, height)
	return err
}

// contractForAsset returns the canonical hex (with 0x41 prefix for
// TRON) contract address for the given ticker. V1 only knows USDT.
func contractForAsset(asset string) string {
	switch strings.ToUpper(asset) {
	case "USDT":
		// USDT-TRC20 mainnet. Source: TRONSCAN.
		return "41a614f803b6fd780986a42c79ec8394ade726993d"
	}
	return ""
}

func buildAdapter(log *slog.Logger) (*tronadapter.Adapter, error) {
	// Tron-scanner uses the same N-provider form as cmd/wallet-api.
	// See that function's doc comment for the full env-var spec;
	// the scanner copy lives here so each binary's main package
	// stays self-contained. Network is fixed to mainnet because
	// the scanner does not yet support a nile-testnet config;
	// the wallet-api reads Network from its own Config.
	providers := scanProvidersFromEnv()
	if len(providers) == 0 {
		return nil, errors.New("no TRON providers configured (set TRON_PRIMARY_URL or any TRON_PROVIDER_<name>_URL)")
	}
	return tronadapter.NewAdapter(tronadapter.Config{
		Providers: providers,
		Logger:    log,
		Network:   tronadapter.NetworkMainnet,
	})
}

// scanProvidersFromEnv builds a Provider slice from the same env
// vars cmd/wallet-api reads. Kept as a helper rather than a
// shared package because the two binaries can disagree on minor
// fields (e.g. network) without forcing a shared dependency.
//
// Returns an empty slice when no provider is configured; the
// caller is expected to surface that as an error.
func scanProvidersFromEnv() []tronadapter.Provider {
	var providers []tronadapter.Provider

	// Legacy two-provider form.
	if url := os.Getenv("TRON_PRIMARY_URL"); url != "" {
		providers = append(providers, tronadapter.Provider{
			Name:    envOr("TRON_PRIMARY_NAME", "primary"),
			BaseURL: url,
			APIKey:  os.Getenv("TRON_PRIMARY_KEY"),
			Weight:  1,
		})
	}
	if url := os.Getenv("TRON_BACKUP_URL"); url != "" {
		providers = append(providers, tronadapter.Provider{
			Name:    envOr("TRON_BACKUP_NAME", "backup"),
			BaseURL: url,
			APIKey:  os.Getenv("TRON_BACKUP_KEY"),
			Weight:  1,
		})
	}

	// Generic N-provider form.
	for _, kv := range os.Environ() {
		// kv is "KEY=VALUE"; we want only the KEY part. Earlier
		// versions of this loop checked strings.HasSuffix on
		// the whole "KEY=VALUE" entry, which fails because the
		// value (URL) does not end with _URL.
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		key := kv[:eq]
		const prefix = "TRON_PROVIDER_"
		const suffix = "_URL"
		if !strings.HasPrefix(key, prefix) || !strings.HasSuffix(key, suffix) {
			continue
		}
		name := strings.ToLower(key[len(prefix) : len(key)-len(suffix)])
		url := kv[eq+1:]
		if name == "" {
			continue
		}
		if url == "" {
			continue
		}
		providers = append(providers, tronadapter.Provider{
			Name:    name,
			BaseURL: url,
			APIKey:  os.Getenv("TRON_PROVIDER_" + name + "_KEY"),
			Weight:  1,
		})
	}
	return providers
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// Ensure the json import survives in case the file is later edited
// to drop the persist helpers above. The dependency is harmless.
var _ = json.Marshal

// Avoid unused http import if HTTP-based RPC ever leaves the file.
var _ = http.MethodGet