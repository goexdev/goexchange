// wallet-api daemon entry point.
//
// Background:
//   The /api/v1 endpoints in cmd/api are user-facing (browser, mobile
//   app). The /user-api/v2 endpoints in cmd/api are programmatic API
//   key auth. Neither path is a great place for the wallet service
//   itself because:
//
//     1. They share the same Go binary, so adding a per-wallet-feature
//        dep (e.g. the signer gRPC client) inflates every other
//        feature's binary footprint and startup time.
//     2. The signer daemon lives in a private Docker network that
//        the public API does not need to reach; today the API still
//        has the signer dial for AllocateDepositAddress. Moving the
//        signer dial behind a dedicated daemon is cleaner.
//
//   B4 of the wallet V1 plan (BOSS 2026-09-01) splits wallet logic
//   out into cmd/wallet-api: a small HTTP server that exposes the
//   wallet service over a separate port (default :8098) and is the
//   only process in the public repo that dials the signer daemon.
//
// Endpoints (V1):
//
//   GET  /wallet/v1/health
//   GET  /wallet/v1/deposit-address?chain=TRON&asset=USDT
//   GET  /wallet/v1/balance?chain=TRON&asset=USDT
//   GET  /wallet/v1/deposits?limit=50
//   GET  /wallet/v1/withdrawals?limit=50
//
// All endpoints require a Bearer JWT issued by cmd/api (the
// /api/v1/users/login handler). V2 will add api-key auth in the
// same shape as /user-api/v2.
//
// Auth secret is loaded from vault (secret/auth/jwt) so this
// daemon and cmd/api share the same signing key without needing
// to copy it onto disk.

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

	"github.com/goexdev/goexchange/internal/auth"
	"github.com/goexdev/goexchange/internal/blockchain"
	tronadapter "github.com/goexdev/goexchange/internal/blockchain/tron"
	"github.com/goexdev/goexchange/internal/config"
	"github.com/goexdev/goexchange/internal/db"
	"github.com/goexdev/goexchange/internal/notifier/templates"
	signerclient "github.com/goexdev/goexchange/internal/signer/client"
	"github.com/goexdev/goexchange/internal/vaultinit"
	"github.com/goexdev/goexchange/internal/wallet"
	"github.com/google/uuid"
	"encoding/hex"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const listenAddr = ":8098"

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	log.Info("starting goexchange-wallet-api", "listen", listenAddr)

	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Error("load config", "error", err)
		os.Exit(1)
	}

	// Vault: read DB password + JWT secret. Both are used here:
	// the DB password opens the connection pool; the JWT secret
	// authenticates incoming bearer tokens.
	vaultClient, err := vaultinit.Init(cfg, context.Background(), log)
	if err != nil {
		log.Error("init vault", "error", err)
		os.Exit(1)
	}
	_ = vaultClient // kept alive for the lifetime of main; the vault
	// client does not currently own any background resources that
	// need explicit shutdown.

	// Email templates (config/email/) — only required because the
	// service embeds the notifier package which init's templates at
	// package load. Cheap to leave in.
	if err := templates.Init(); err != nil {
		log.Error("init email templates", "error", err)
		os.Exit(1)
	}

	// DB pool.
	pool, err := db.Connect(context.Background(), cfg.Database.URL)
	if err != nil {
		log.Error("connect db", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Blockchain adapter registry. V1 only registers TRON; V2 will
	// add BTC / ETH / EVM / SOL adapters alongside.
	//
	// Providers come from environment variables. Both
	// TRON_PRIMARY_URL and TRON_BACKUP_URL are optional: with just
	// one set we run with a single provider; with both set we get
	// failover; with neither set we skip the real adapter so the
	// wallet-api still boots (registry stubs handle the rest).
	registry := blockchain.NewRegistry()
	tronAd, err := buildTronAdapter(log)
	if err != nil {
		log.Warn("tron adapter init failed (RPC URLs missing); wallet-api will use registry fallback", "error", err)
	} else {
		registry.Register(tronAd)
		log.Info("tron adapter registered",
			"network", tronAd.Network(),
			"providers", len(tronAd.Providers()))
	}
	// Stub the remaining chains so the registry has no surprises
	// when the wallet service looks them up. The V1 TRON path is
	// the only one wired through; the stubs let
	// AllocateDepositAddress fail with a clear "chain not
	// supported" for BTC/ETH callers instead of panicking on a
	// missing key.
	for _, c := range blockchain.RegisterStubs(registry) {
		_ = c
	}

	// Signer daemon client (closed-source). Defaults to the
	// docker-network host name "signer:50061" but dev runs use
	// "127.0.0.1:50061".
	signerAddr := os.Getenv("SIGNER_ADDR")
	if signerAddr == "" {
		signerAddr = "127.0.0.1:50061"
	}
	signerC, err := signerclient.NewClient(context.Background(), signerAddr)
	if err != nil {
		log.Warn("signer dial failed; AllocateDepositAddress will use registry fallback",
			"addr", signerAddr, "error", err)
	} else {
		defer signerC.Close()
		if ok, vs, _, err := signerC.Health(context.Background()); err != nil || !ok {
			log.Warn("signer unhealthy", "addr", signerAddr, "vault_status", vs, "error", err)
		} else {
			log.Info("signer connected", "addr", signerAddr, "vault_status", vs)
		}
	}

	// Wallet service V1. Wire the signer client so
	// AllocateDepositAddress prefers derivation through the daemon
	// when one is reachable.
	walletSvc := wallet.NewServiceV1(pool, registry, log)
	if signerC != nil {
		walletSvc.SetSignerClient(signerC)
	}

	srv := &server{
		log:       log,
		pool:      pool,
		jwtSecret: []byte(cfg.JWT.Secret),
		wallet:    walletSvc,
		signer:    signerC,
		tronAd:    tronAd,
		networkName: tronAd.Network(),
	}

	// TOTP service. appSecret is derived from the JWT secret
	// (>= 32 bytes after we paste it through cfg.JWT.Secret).
	// Issuer is the site name; V1 hardcodes "goexchange".
	totpSvc, err := auth.NewTOTPService(pool, string(cfg.JWT.Secret)+"-totp", "goexchange")
	if err != nil {
		log.Warn("TOTP service init failed; 2FA confirmation endpoint will return 501", "error", err)
	} else {
		srv.totp = totpSvc
		log.Info("TOTP service ready")
	}

	// Populate hotWalletAddr via signer.DeriveAddress(0). If the
	// signer is unreachable we log + continue with empty addr;
	// withdrawals will fail with "hot wallet not configured"
	// until the signer is back.
	if signerC != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		encoded, _, _, err := signerC.Derive(ctx, "TRON", 0)
		cancel()
		if err != nil {
			log.Warn("could not derive hot wallet addr", "error", err)
		} else {
			srv.hotWalletAddr = encoded
			log.Info("hot wallet address loaded", "address", encoded)
		}
	}

	// Worker context. Cancelled when main returns, so the
	// withdrawal worker exits cleanly on SIGTERM.
	workerCtx, workerCancel := context.WithCancel(context.Background())
	defer workerCancel()
	if signerC != nil && tronAd != nil {
		go srv.runWithdrawalWorker(workerCtx)
		// Confirmation watcher advances BROADCASTED/IN_BLOCK
		// rows to COMPLETED via SOLIDIFIED. It uses the same
		// tronAd and pool as the signing worker; only RPC
		// pattern differs (read-only gettransactioninfobyid
		// vs write-side broadcast).
		go srv.runConfirmationWatcher(workerCtx)
		// Sweep pipeline. The planner decides WHEN to sweep
		// (default every 10 min, threshold 10 USDT). The
		// worker drains PENDING sweep_tasks through build ->
		// sign -> broadcast. The sweep confirmation watcher
		// then walks BROADCASTED -> COMPLETED using the same
		// gettransactioninfobyid pattern as withdrawals.
		go srv.runSweepPlanner(workerCtx)
		go srv.runSweepWorker(workerCtx)
		go srv.runSweepConfirmationWatcher(workerCtx)
	} else {
		log.Warn("withdrawal worker disabled: signer or tron adapter missing")
	}

	httpSrv := &http.Server{
		Addr:              listenAddr,
		Handler:           srv.routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Graceful shutdown.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		log.Info("wallet-api shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
	}()
	log.Info("wallet-api listening", "addr", listenAddr)
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("serve", "error", err)
		os.Exit(1)
	}
}

// server holds the dependencies the wallet-api routes need.
type server struct {
	log          *slog.Logger
	pool         *pgxpool.Pool
	jwtSecret    []byte
	wallet       *wallet.ServiceV1
	signer       *signerclient.Client
	tronAd       *tronadapter.Adapter
	networkName  string // "mainnet" | "nile_testnet" — used by the worker to pick the right USDT contract
	hotWalletAddr string // populated at startup via signer.DeriveAddress(0)
	totp         *auth.TOTPService // nil → 2FA confirmation endpoint returns 501
}

// routes returns the chi-equivalent http.Handler with the wallet
// endpoints mounted under /wallet/v1/. We use the stdlib mux so this
// file has zero chi dependency; the public cmd/api process keeps
// using chi and we don't add a server-level dep here just for
// routing.
func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/wallet/v1/health", s.health)
	mux.HandleFunc("/wallet/v1/deposit-address", s.auth(s.getDepositAddress))
	mux.HandleFunc("/wallet/v1/balance", s.auth(s.getBalance))
	mux.HandleFunc("/wallet/v1/deposits", s.auth(s.listDeposits))
	// Withdrawals: GET lists, POST creates, POST .../confirm
	// moves RISK_CHECK -> PENDING via TOTP. We share one
	// auth + audit path across the three methods.
	mux.HandleFunc("/wallet/v1/withdrawals", s.auth(s.dispatchWithdrawals))
	mux.HandleFunc("/wallet/v1/withdrawals/confirm", s.auth(s.confirmWithdrawal))
	// Admin RPC control. Mounted under /admin/wallet/v1/rpc/* so
	// the admin namespace stays separate from the user endpoints.
	// Auth: same Bearer JWT as the rest of the API, plus a
	// role=admin check (the JWT service emits role on the
	// context). We do not gate these endpoints by IP allow-list
	// because the admin user is provisioned manually.
	mux.HandleFunc("/admin/wallet/v1/rpc/providers", s.adminAuth(s.listRpcProviders))
	mux.HandleFunc("/admin/wallet/v1/rpc/switch", s.adminAuth(s.switchRpcProvider))
	mux.HandleFunc("/admin/wallet/v1/rpc/failover", s.adminAuth(s.failoverRpcProvider))
	mux.HandleFunc("/admin/wallet/v1/rpc/health", s.adminAuth(s.rpcHealth))
	// Admin manual-review queue. adminAuth requires the JWT
	// claims.role == "admin" — see adminAuth middleware below.
	// Each admin handler takes no uid (the admin is operating
	// on a different user's withdrawal); we wrap them as
	// plain http.HandlerFunc to satisfy adminAuth's signature.
	mux.HandleFunc("/admin/wallet/v1/withdrawals/review",
		s.adminAuth(func(w http.ResponseWriter, r *http.Request) {
			s.listReviewWithdrawals(w, r)
		}))
	mux.HandleFunc("/admin/wallet/v1/withdrawals/review/approve",
		s.adminAuth(func(w http.ResponseWriter, r *http.Request) {
			s.approveReviewWithdrawal(w, r)
		}))
	mux.HandleFunc("/admin/wallet/v1/withdrawals/review/reject",
		s.adminAuth(func(w http.ResponseWriter, r *http.Request) {
			s.rejectReviewWithdrawal(w, r)
		}))
	return logRequest(s.log, mux)
}

// auth wraps a handler with Bearer-JWT verification. Returns 401
// if the header is missing or the signature does not verify. On
// success the user id is stashed on the request context so the
// handler can pull it out.
func (s *server) auth(next func(http.ResponseWriter, *http.Request, uuid.UUID)) http.HandlerFunc {
	// Build a one-shot Service that shares the JWT secret with
	// cmd/api. The Service does no I/O of its own; VerifyToken is
	// pure crypto + memory.
	authSvc := auth.NewService(string(s.jwtSecret), 24*time.Hour)
	return func(w http.ResponseWriter, r *http.Request) {
		raw := bearerToken(r)
		if raw == "" {
			writeJSONError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		claims, err := authSvc.VerifyToken(raw)
		if err != nil {
			writeJSONError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		uid, err := uuid.Parse(claims.UserID)
		if err != nil {
			writeJSONError(w, http.StatusUnauthorized, "invalid token subject")
			return
		}
		next(w, r, uid)
	}
}

// adminAuth is auth with role=admin gating. The JWT must have
// role="admin" in its claims; user-role tokens get 403.
//
// We re-verify per request instead of caching the verification
// result because the JWT secret can be rotated via Vault and a
// cached token from the old secret would silently keep passing.
// The auth path is in-process so the per-request cost is a single
// HMAC verify.
func (s *server) adminAuth(next http.HandlerFunc) http.HandlerFunc {
	authSvc := auth.NewService(string(s.jwtSecret), 24*time.Hour)
	return func(w http.ResponseWriter, r *http.Request) {
		raw := bearerToken(r)
		if raw == "" {
			writeJSONError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		claims, err := authSvc.VerifyToken(raw)
		if err != nil {
			writeJSONError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		if claims.Role != "admin" {
			writeJSONError(w, http.StatusForbidden, "admin role required")
			return
		}
		next(w, r)
	}
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// health is unauthenticated so docker-compose healthcheck can poll
// it without provisioning an api key.
func (s *server) health(w http.ResponseWriter, _ *http.Request) {
	ok, fail := templates.Stats()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"template_reloads_ok":    ok,
		"template_reloads_fail":  fail,
	})
}

// listRpcProviders returns the configured TRON RPC providers and
// marks the active one. Admin-only.
func (s *server) listRpcProviders(w http.ResponseWriter, _ *http.Request) {
	if s.tronAd == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "tron adapter not configured")
		return
	}
	active := s.tronAd.ActiveProvider()
	out := make([]map[string]any, 0, len(s.tronAd.Providers()))
	for _, p := range s.tronAd.Providers() {
		out = append(out, map[string]any{
			"name":         p.Name,
			"weight":       p.Weight,
			"healthy":      p.Health == nil || p.Health.Load() == 1,
			"available":    p.IsAvailable(),
			"active":       p.Name == active.Name,
			"rate_limited": p.RateLimit429At != nil && p.RateLimit429At.Load() > 0,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"providers": out,
		"active":    active.Name,
	})
}

// switchRpcProvider selects which TRON provider the next request
// will target. Body: {"provider": "<name>"}. Returns 400 if the
// name is not in the configured set.
//
// This endpoint is for ops use; it does not have rate limiting
// or audit logging yet (V2 will add the latter by writing to the
// admin_audit_log table).
func (s *server) switchRpcProvider(w http.ResponseWriter, r *http.Request) {
	if s.tronAd == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "tron adapter not configured")
		return
	}
	var body struct {
		Provider string `json:"provider"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.Provider == "" {
		writeJSONError(w, http.StatusBadRequest, "provider field required")
		return
	}
	if err := s.tronAd.SetActive(body.Provider); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.log.Info("rpc provider switched via admin", "provider", body.Provider)
	writeJSON(w, http.StatusOK, map[string]any{
		"active": s.tronAd.ActiveProvider().Name,
	})
}

// failoverRpcProvider forces the adapter to pick the next
// available provider in priority order. Useful when a provider
// is intermittently failing but has not yet been 429-marked.
//
// The endpoint returns the name of the new active provider and
// the index it landed on, so the operator can confirm the
// failover worked.
func (s *server) failoverRpcProvider(w http.ResponseWriter, _ *http.Request) {
	if s.tronAd == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "tron adapter not configured")
		return
	}
	prev := s.tronAd.ActiveProvider().Name
	idx := s.tronAd.FailoverToNext()
	if idx < 0 {
		writeJSONError(w, http.StatusServiceUnavailable, "no available providers")
		return
	}
	newActive := s.tronAd.ActiveProvider().Name
	s.log.Warn("rpc provider failed over via admin",
		"from", prev, "to", newActive)
	writeJSON(w, http.StatusOK, map[string]any{
		"previous": prev,
		"active":   newActive,
		"reason":   "manual",
	})
}

// rpcHealth is a quick summary endpoint for ops dashboards.
// Distinct from /wallet/v1/health because it talks about the
// upstream chain, not the wallet-api binary itself.
func (s *server) rpcHealth(w http.ResponseWriter, _ *http.Request) {
	if s.tronAd == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"adapter": "not_configured",
		})
		return
	}
	active := s.tronAd.ActiveProvider()
	healthyCount := 0
	total := len(s.tronAd.Providers())
	for _, p := range s.tronAd.Providers() {
		if p.IsAvailable() {
			healthyCount++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"adapter":          "tron",
		"network":          s.tronAd.Network(),
		"active":           active.Name,
		"providers_total":  total,
		"providers_ready":  healthyCount,
		"active_healthy":   active.Health == nil || active.Health.Load() == 1,
	})
}

func (s *server) getDepositAddress(w http.ResponseWriter, r *http.Request, uid uuid.UUID) {
	chain := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("chain")))
	asset := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("asset")))
	if chain == "" || asset == "" {
		writeJSONError(w, http.StatusBadRequest, "chain and asset query params required")
		return
	}
	resp, err := s.wallet.AllocateDepositAddress(r.Context(), wallet.AllocateAddressRequest{
		UserID: uid,
		Chain:  chain,
		Asset:  asset,
	})
	if err != nil {
		s.log.Warn("wallet.AllocateDepositAddress failed",
			"user_id", uid, "chain", chain, "asset", asset, "error", err)
		writeJSONError(w, http.StatusInternalServerError, "allocate deposit address failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"chain":   resp.Address.Chain,
		"asset":   resp.Address.Asset,
		"address": resp.Address.Encoded,
		"hex":     resp.Address.Hex,
		"reused":  resp.Reused,
	})
}

func (s *server) getBalance(w http.ResponseWriter, r *http.Request, uid uuid.UUID) {
	chain := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("chain")))
	asset := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("asset")))
	if chain == "" || asset == "" {
		writeJSONError(w, http.StatusBadRequest, "chain and asset query params required")
		return
	}
	amount, err := s.wallet.GetBalance(r.Context(), uid, chain, asset)
	if err != nil {
		s.log.Warn("wallet.GetBalance failed", "user_id", uid, "chain", chain, "asset", asset, "error", err)
		writeJSONError(w, http.StatusBadGateway, "balance lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"chain":  chain,
		"asset":  asset,
		"amount": amount,
	})
}

func (s *server) listDeposits(w http.ResponseWriter, r *http.Request, uid uuid.UUID) {
	limit := parseLimit(r, 50, 200)
	chain := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("chain")))
	asset := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("asset")))

	rows, err := s.pool.Query(r.Context(), `
		SELECT id, asset, amount::text, tx_hash, from_address, to_address, status, created_at, confirmed_at, credited_at
		FROM deposits
		WHERE user_id = $1
		  AND ($2 = '' OR chain = $2)
		  AND ($3 = '' OR asset = $3)
		ORDER BY created_at DESC
		LIMIT $4`,
		uid, chain, asset, limit)
	if err != nil {
		s.log.Error("list deposits query failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "list deposits failed")
		return
	}
	defer rows.Close()

	type depositItem struct {
		ID          string  `json:"id"`
		Asset       string  `json:"asset"`
		Amount      string  `json:"amount"`
		TxHash      string  `json:"tx_hash"`
		FromAddress string  `json:"from_address"`
		ToAddress   string  `json:"to_address"`
		Status      string  `json:"status"`
		CreatedAt   string  `json:"created_at"`
		ConfirmedAt *string `json:"confirmed_at,omitempty"`
		CreditedAt  *string `json:"credited_at,omitempty"`
	}
	out := []depositItem{}
	for rows.Next() {
		var d depositItem
		var confirmedAt, creditedAt *time.Time
		if err := rows.Scan(&d.ID, &d.Asset, &d.Amount, &d.TxHash, &d.FromAddress, &d.ToAddress, &d.Status, &d.CreatedAt, &confirmedAt, &creditedAt); err != nil {
			s.log.Error("deposit row scan failed", "error", err)
			continue
		}
		if confirmedAt != nil {
			s := confirmedAt.Format(time.RFC3339Nano)
			d.ConfirmedAt = &s
		}
		if creditedAt != nil {
			s := creditedAt.Format(time.RFC3339Nano)
			d.CreditedAt = &s
		}
		out = append(out, d)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

func (s *server) listWithdrawals(w http.ResponseWriter, r *http.Request, uid uuid.UUID) {
	limit := parseLimit(r, 50, 200)
	chain := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("chain")))
	asset := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("asset")))

	rows, err := s.pool.Query(r.Context(), `
		SELECT id, asset, amount::text, fee::text, dest_address, status, tx_hash, created_at, completed_at
		FROM withdrawals
		WHERE user_id = $1
		  AND ($2 = '' OR chain = $2)
		  AND ($3 = '' OR asset = $3)
		ORDER BY created_at DESC
		LIMIT $4`,
		uid, chain, asset, limit)
	if err != nil {
		s.log.Error("list withdrawals query failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "list withdrawals failed")
		return
	}
	defer rows.Close()

	type withdrawalItem struct {
		ID          string  `json:"id"`
		Asset       string  `json:"asset"`
		Amount      string  `json:"amount"`
		Fee         string  `json:"fee"`
		Destination string  `json:"destination"`
		Status      string  `json:"status"`
		TxHash      *string `json:"tx_hash,omitempty"`
		CreatedAt   string  `json:"created_at"`
		CompletedAt *string `json:"completed_at,omitempty"`
	}
	out := []withdrawalItem{}
	for rows.Next() {
		var w withdrawalItem
		var txHash, completedAt *string
		if err := rows.Scan(&w.ID, &w.Asset, &w.Amount, &w.Fee, &w.Destination, &w.Status, &txHash, &w.CreatedAt, &completedAt); err != nil {
			s.log.Error("withdrawal row scan failed", "error", err)
			continue
		}
		w.TxHash = txHash
		if completedAt != nil {
			s := *completedAt
			w.CompletedAt = &s
		}
		out = append(out, w)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

// dispatchWithdrawals routes GET/POST on /wallet/v1/withdrawals to
// the list or create handler. We keep a single auth path and a
// single idempotency header check (Idempotency-Key on POST) at
// this entry point so neither handler has to re-validate.
func (s *server) dispatchWithdrawals(w http.ResponseWriter, r *http.Request, uid uuid.UUID) {
	switch r.Method {
	case http.MethodGet:
		s.listWithdrawals(w, r, uid)
	case http.MethodPost:
		s.createWithdrawal(w, r, uid)
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// createWithdrawal is the V1 entry point for a user-initiated
// withdrawal. V1 scope is intentionally narrow:
//
//  1. We accept {chain, asset, dest_address, amount}.
//  2. We validate balance + address format only; risk scoring
//     and 2FA confirmation are V2.
//  3. We INSERT a row with status=PENDING and return its id;
//     the worker loop in runWithdrawalWorker processes it
//     asynchronously.
//
// Idempotency-Key is honoured via withdrawal_idempotency. A repeat
// POST with the same key returns the original withdrawal_id and
// does not create a new row.
func (s *server) createWithdrawal(w http.ResponseWriter, r *http.Request, uid uuid.UUID) {
	// Bot accounts are owned by the market-making bot engine and
	// never initiate user-facing withdrawals -- the matching
	// engine never raises funds this way either; bot inventory
	// is mocked per the V1 build plan. Reject the request here
	// so a misconfigured bot never accidentally drains its
	// treasury balance through the user rails.
	var isBot bool
	if err := s.pool.QueryRow(r.Context(),
		`SELECT is_bot_user FROM users WHERE id = $1`, uid).Scan(&isBot); err == nil && isBot {
		writeJSONError(w, http.StatusForbidden, "bot accounts cannot initiate withdrawals")
		return
	}
	var req struct {
		Chain   string `json:"chain"`
		Asset   string `json:"asset"`
		To      string `json:"destination"`
		Amount  string `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	chain := strings.ToUpper(strings.TrimSpace(req.Chain))
	asset := strings.ToUpper(strings.TrimSpace(req.Asset))
	to := strings.TrimSpace(req.To)
	if chain != "TRON" {
		writeJSONError(w, http.StatusBadRequest, "only TRON chain supported in V1")
		return
	}
	if asset != "USDT" {
		writeJSONError(w, http.StatusBadRequest, "only USDT asset supported in V1")
		return
	}
	if !tronValidateAddress(to) {
		writeJSONError(w, http.StatusBadRequest, "destination must be a TRON Base58 address (T-prefix)")
		return
	}
	amountFloat, err := strconv.ParseFloat(req.Amount, 64)
	if err != nil || amountFloat <= 0 {
		writeJSONError(w, http.StatusBadRequest, "amount must be a positive number")
		return
	}
	// USDT has 6 decimals on TRON; convert to integer amount.
	amountUnits := uint64(amountFloat * 1_000_000)
	const minAmount uint64 = 100_000 // 0.1 USDT
	if amountUnits < minAmount {
		writeJSONError(w, http.StatusBadRequest, "amount below minimum 0.1 USDT")
		return
	}

	// Idempotency-Key handling. Same key + same user returns the
	// original withdrawal_id; this protects against browser
	// retries.
	idemKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))

	ctx := r.Context()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		s.log.Error("withdrawal begin tx failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer tx.Rollback(ctx) // safe to call even after Commit

	if idemKey != "" {
		var existingID string
		err := tx.QueryRow(ctx,
			`SELECT withdrawal_id FROM withdrawal_idempotency WHERE user_id = $1 AND idempotency_key = $2`,
			uid, idemKey).Scan(&existingID)
		if err == nil {
			// Idempotent replay; return the existing withdrawal.
			if err := tx.Commit(ctx); err != nil {
				s.log.Error("idem commit failed", "error", err)
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"withdrawal_id": existingID,
				"status":        "PENDING",
				"replay":        true,
			})
			return
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			s.log.Error("idem lookup failed", "error", err)
			writeJSONError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}

	// Check user balance: must have amount available. We lock
	// the row for update so a concurrent withdraw does not
	// double-spend.
	var available string
	err = tx.QueryRow(ctx,
		`SELECT available::text FROM balances WHERE user_id = $1 AND asset = $2 FOR UPDATE`,
		uid, asset).Scan(&available)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSONError(w, http.StatusBadRequest, "no balance for this asset")
			return
		}
		s.log.Error("balance lookup failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	balFloat, err := strconv.ParseFloat(available, 64)
	if err != nil || balFloat < amountFloat {
		writeJSONError(w, http.StatusBadRequest, "insufficient balance")
		return
	}

	// Lock the funds: move amount from available to frozen.
	_, err = tx.Exec(ctx,
		`UPDATE balances SET available = available - $2::numeric, frozen = frozen + $2::numeric, updated_at = NOW()
		 WHERE user_id = $1 AND asset = $3`,
		uid, req.Amount, asset)
	if err != nil {
		s.log.Error("balance lock failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Insert the withdrawal row.
	//
	// Status is selected by the risk scorer:
	//   score < 50          -> PENDING (auto-approve)
	//   50 <= score < 80    -> RISK_CHECK (needs TOTP confirm)
	//   score >= 80         -> MANUAL_REVIEW (needs admin)
	//   score >= 100        -> REJECTED outright (admin alert)
	score, factors := computeRiskScore(ctx, tx, uid, req.To, amountFloat)
	initialStatus := "PENDING"
	switch {
	case score >= 100:
		initialStatus = "REJECTED"
	case score >= 80:
		initialStatus = "MANUAL_REVIEW"
	case score >= 50:
		initialStatus = "RISK_CHECK"
	}
	var withdrawalID string
	err = tx.QueryRow(ctx,
		`INSERT INTO withdrawals (user_id, chain, asset, amount, dest_address, status, receive_amount, fee, risk_score, risk_hold)
		 VALUES ($1, $2, $3, $4::numeric, $5, $6::text, $4::numeric, 0, $7::int, $8::bool)
		 RETURNING id::text`,
		uid, chain, asset, req.Amount, to, initialStatus, score,
		initialStatus != "PENDING").Scan(&withdrawalID)
	if err != nil {
		s.log.Error("withdrawal insert failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if idemKey != "" {
		_, err = tx.Exec(ctx,
			`INSERT INTO withdrawal_idempotency (user_id, idempotency_key, withdrawal_id) VALUES ($1, $2, $3)`,
			uid, idemKey, withdrawalID)
		if err != nil {
			s.log.Error("idem insert failed", "error", err)
			writeJSONError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	if err := tx.Commit(ctx); err != nil {
		s.log.Error("withdrawal commit failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.log.Info("withdrawal created",
		"withdrawal_id", withdrawalID,
		"user_id", uid,
		"chain", chain, "asset", asset,
		"amount", req.Amount, "to", to,
		"risk_score", score, "risk_status", initialStatus,
		"factors", factors)
	httpStatus := http.StatusCreated
	switch initialStatus {
	case "REJECTED":
		httpStatus = http.StatusForbidden
	case "MANUAL_REVIEW":
		httpStatus = http.StatusAccepted
	case "RISK_CHECK":
		httpStatus = http.StatusAccepted
	}
	writeJSON(w, httpStatus, map[string]any{
		"withdrawal_id": withdrawalID,
		"status":        initialStatus,
		"risk_score":    score,
		"factors":       factors,
		"action":        riskActionMessage(initialStatus),
	})
}

// riskActionMessage is the human-readable next-step string
// the wallet UI shows alongside the withdrawal_id.
func riskActionMessage(status string) string {
	switch status {
	case "PENDING":
		return "queued for processing"
	case "RISK_CHECK":
		return "2FA confirmation required: POST /wallet/v1/withdrawals/confirm"
	case "MANUAL_REVIEW":
		return "admin review required; status will update via /wallet/v1/withdrawals/<id>"
	case "REJECTED":
		return "withdrawal rejected by automated risk checks"
	}
	return ""
}

// computeRiskScore returns a heuristic risk score in [0, 100+]
// and a list of factor strings the user can read in the
// response. Score semantics:
//
//:
//   0-49   PENDING     auto-approve, worker signs immediately
//   50-79  RISK_CHECK  user must POST TOTP to /wallet/v1/withdrawals/confirm
//   80-99  MANUAL_REVIEW admin queue; status updated by admin endpoint
//   100+   REJECTED   hard reject; balance is auto-unlocked by the txn rollback
//
// V1 heuristics (no ML, no external scoring service):
//
//   amount > 100 / 1000 / 10000 USDT      +20 / +50 / +80
//   new destination (first time)           +30
//   unusual hour (UTC 22:00 - 06:00)        +15
//   account age < 24h / < 1h               +40 / +60
//   failed 2FA attempts in last 24h         +20 each
//   daily cumulative withdrawal > 5000 USDT  +30
//
// Anything we cannot compute (missing user row, etc.) returns
// 0 with no factors; the row is then PENDING. This is the
// safest default — no signal → no risk flag.
func computeRiskScore(ctx context.Context, q interface {
 QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
 Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}, uid uuid.UUID, dest string, amount float64) (int, []string) {
	var score int
	var factors []string

	// Amount thresholds.
	switch {
	case amount > 10000:
		score += 80
		factors = append(factors, fmt.Sprintf("amount>10000 (+80, now %d)", score))
	case amount > 1000:
		score += 50
		factors = append(factors, fmt.Sprintf("amount>1000 (+50, now %d)", score))
	case amount > 100:
		score += 20
		factors = append(factors, fmt.Sprintf("amount>100 (+20, now %d)", score))
	}

	// Account age.
	var createdAt time.Time
	err := q.QueryRow(ctx, `SELECT created_at FROM users WHERE id = $1`, uid).Scan(&createdAt)
	if err == nil {
		age := time.Since(createdAt)
		switch {
		case age < time.Hour:
			score += 60
			factors = append(factors, fmt.Sprintf("account_age<1h (+60, now %d)", score))
		case age < 24*time.Hour:
			score += 40
			factors = append(factors, fmt.Sprintf("account_age<24h (+40, now %d)", score))
		}
	}

	// New destination.
	var usedCount int
	if err := q.QueryRow(ctx,
		`SELECT COUNT(*) FROM withdrawals WHERE user_id = $1 AND dest_address = $2 AND id <> $3::uuid`,
		uid, dest, "00000000-0000-0000-0000-000000000000").Scan(&usedCount); err == nil {
		// The "<> $3" is a no-op sentinel: count includes the
		// row we are about to insert (which doesn't exist
		// yet). We subtract one to get the historical count.
		if usedCount > 0 {
			score += 30
			factors = append(factors, fmt.Sprintf("new_destination (+30, now %d)", score))
		}
	}

	// Daily cumulative withdrawal (last 24h).
	var dayTotal float64
	if err := q.QueryRow(ctx,
		`SELECT COALESCE(SUM(amount::numeric), 0)::float8 FROM withdrawals
		 WHERE user_id = $1 AND created_at > NOW() - INTERVAL '24 hours'
		   AND status NOT IN ('REJECTED','FAILED','CANCELLED')`,
		uid).Scan(&dayTotal); err == nil {
		if dayTotal+amount > 5000 {
			score += 30
			factors = append(factors, fmt.Sprintf("daily_cum>5000 (+30, now %d)", score))
		}
	}

	// Unusual hour (UTC 22:00 - 06:00).
	h := time.Now().UTC().Hour()
	if h >= 22 || h < 6 {
		score += 15
		factors = append(factors, fmt.Sprintf("unusual_hour (+15, now %d)", score))
	}

	// Failed 2FA attempts in last 24h. The 2FA attempts
	// counter is logged via audit_log; we read it as a
	// lightweight proxy here.
	// V1 ships without a dedicated 2FA_attempts table, so we
	// skip this factor; the endpoint will land in Risk.4.

	return score, factors
}

// confirmWithdrawal is the V1 2FA gate for RISK_CHECK rows.
// The user has already POSTed the withdrawal (which moved to
// RISK_CHECK); they now POST {withdrawal_id, code} where code
// is their current TOTP value.
//
// Behaviour:
//   - 200 + status=PENDING    code accepted, row promoted
//   - 400                     row not in RISK_CHECK
//   - 401                     TOTP code wrong or expired
//   - 501                     TOTP service not configured
func (s *server) confirmWithdrawal(w http.ResponseWriter, r *http.Request, uid uuid.UUID) {
	if s.totp == nil {
		writeJSONError(w, http.StatusNotImplemented, "2FA confirmation not available on this server")
		return
	}
	var req struct {
		WithdrawalID string `json:"withdrawal_id"`
		Code         string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.WithdrawalID == "" || req.Code == "" {
		writeJSONError(w, http.StatusBadRequest, "withdrawal_id and code are required")
		return
	}

	ctx := r.Context()
	// First check ownership + status without taking the lock.
	var ownerID string
	var status string
	if err := s.pool.QueryRow(ctx,
		`SELECT user_id::text, status FROM withdrawals WHERE id = $1::uuid`,
		req.WithdrawalID).Scan(&ownerID, &status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSONError(w, http.StatusNotFound, "withdrawal not found")
			return
		}
		s.log.Error("confirm withdrawal: query failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if ownerID != uid.String() {
		writeJSONError(w, http.StatusForbidden, "withdrawal does not belong to this user")
		return
	}
	if status != "RISK_CHECK" {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("withdrawal is in status %q, expected RISK_CHECK", status))
		return
	}

	// Verify TOTP. VerifyCode already rejects used backup
	// codes, replayed codes (V1 step=30s window), and
	// wrong codes. We log every failed attempt so the
	// risk scorer can pick it up later.
	if err := s.totp.VerifyCode(ctx, uid, req.Code); err != nil {
		s.log.Warn("2FA confirmation failed",
			"withdrawal_id", req.WithdrawalID,
			"user_id", uid, "error", err)
		writeJSONError(w, http.StatusUnauthorized, "invalid or expired TOTP code")
		return
	}

	// Promote RISK_CHECK -> PENDING. Conditional UPDATE
	// guards against a race with the worker that may have
	// already failed or moved the row (it should NOT move a
	// RISK_CHECK row, but the SQL is defensive).
	ct, err := s.pool.Exec(ctx,
		`UPDATE withdrawals
		 SET status = 'PENDING', risk_hold = false
		 WHERE id = $1::uuid AND status = 'RISK_CHECK'`,
		req.WithdrawalID)
	if err != nil {
		s.log.Error("confirm withdrawal: update failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if ct.RowsAffected() == 0 {
		writeJSONError(w, http.StatusConflict, "withdrawal no longer in RISK_CHECK")
		return
	}
	s.log.Info("withdrawal 2FA confirmed", "withdrawal_id", req.WithdrawalID, "user_id", uid)
	writeJSON(w, http.StatusOK, map[string]any{
		"withdrawal_id": req.WithdrawalID,
		"status":        "PENDING",
		"action":        "queued for processing",
	})
}

// listReviewWithdrawals is the admin endpoint that returns
// every MANUAL_REVIEW withdrawal in created_at order, plus
// any other rows with risk_score >= 80 (defensive — even
// rows that escaped the status enum still surface here).
func (s *server) listReviewWithdrawals(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(),
		`SELECT id::text, user_id::text, chain, asset, amount::text,
		        dest_address, risk_score, created_at
		 FROM withdrawals
		 WHERE status = 'MANUAL_REVIEW'
		    OR (risk_score >= 80 AND status NOT IN ('COMPLETED','FAILED','REJECTED','CANCELLED'))
		 ORDER BY created_at ASC
		 LIMIT 200`)
	if err != nil {
		s.log.Error("admin list review: query failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer rows.Close()
	type row struct {
		ID, UserID, Chain, Asset, Amount, Dest string
		Score                                 int
		CreatedAt                             string
	}
	var items []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.ID, &r.UserID, &r.Chain, &r.Asset,
			&r.Amount, &r.Dest, &r.Score, &r.CreatedAt); err != nil {
			s.log.Error("admin list review: scan failed", "error", err)
			continue
		}
		items = append(items, r)
	}
	// Re-shape for the response.
	type outItem struct {
		ID        string `json:"id"`
		UserID    string `json:"user_id"`
		Chain     string `json:"chain"`
		Asset     string `json:"asset"`
		Amount    string `json:"amount"`
		Dest      string `json:"destination"`
		RiskScore int    `json:"risk_score"`
		CreatedAt string `json:"created_at"`
	}
	out := make([]outItem, 0, len(items))
	for _, r := range items {
		out = append(out, outItem{
			ID: r.ID, UserID: r.UserID, Chain: r.Chain, Asset: r.Asset,
			Amount: r.Amount, Dest: r.Dest, RiskScore: r.Score,
			CreatedAt: r.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

// approveReviewWithdrawal moves MANUAL_REVIEW -> PENDING so
// the worker picks it up. Risk score is left on the row as
// an audit trail; only status changes.
func (s *server) approveReviewWithdrawal(w http.ResponseWriter, r *http.Request) {
	var req struct {
		WithdrawalID string `json:"withdrawal_id"`
		Note         string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	ct, err := s.pool.Exec(r.Context(),
		`UPDATE withdrawals
		 SET status = 'PENDING', risk_hold = false, error_msg = NULL
		 WHERE id = $1::uuid AND status = 'MANUAL_REVIEW'`,
		req.WithdrawalID)
	if err != nil {
		s.log.Error("admin approve: update failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if ct.RowsAffected() == 0 {
		writeJSONError(w, http.StatusConflict, "withdrawal not in MANUAL_REVIEW")
		return
	}
	s.log.Info("withdrawal approved by admin",
		"withdrawal_id", req.WithdrawalID, "note", req.Note)
	writeJSON(w, http.StatusOK, map[string]any{
		"withdrawal_id": req.WithdrawalID,
		"status":        "PENDING",
	})
}

// rejectReviewWithdrawal moves MANUAL_REVIEW -> REJECTED and
// unlocks the user's balance so the funds are not frozen
// forever.
func (s *server) rejectReviewWithdrawal(w http.ResponseWriter, r *http.Request) {
	var req struct {
		WithdrawalID string `json:"withdrawal_id"`
		Note         string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	ctx := r.Context()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer tx.Rollback(ctx)
	var userID, asset, amount string
	if err := tx.QueryRow(ctx,
		`UPDATE withdrawals
		 SET status = 'REJECTED', risk_hold = false,
		     error_msg = COALESCE(NULLIF($2, ''), 'rejected by admin')
		 WHERE id = $1::uuid AND status = 'MANUAL_REVIEW'
		 RETURNING user_id::text, asset, amount::text`,
		req.WithdrawalID, req.Note).Scan(&userID, &asset, &amount); err != nil {
		writeJSONError(w, http.StatusConflict, "withdrawal not in MANUAL_REVIEW")
		return
	}
	// Unfreeze the balance.
	if _, err := tx.Exec(ctx,
		`UPDATE balances
		 SET available = available + GREATEST($2::numeric - frozen, 0),
		     frozen = GREATEST(frozen - $2::numeric, 0),
		     updated_at = NOW()
		 WHERE user_id = $1::uuid AND asset = $3`,
		userID, amount, asset); err != nil {
		s.log.Error("admin reject: unlock failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		s.log.Error("admin reject: commit failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.log.Info("withdrawal rejected by admin",
		"withdrawal_id", req.WithdrawalID, "note", req.Note)
	writeJSON(w, http.StatusOK, map[string]any{
		"withdrawal_id": req.WithdrawalID,
		"status":        "REJECTED",
	})
}

// runWithdrawalWorker polls the withdrawals table for PENDING
// rows and runs each through the build → sign → broadcast
// pipeline. One worker goroutine per wallet-api process is enough
// for V1 traffic; rate-limit sensitivity is at the adapter level.
//
// State transitions on success:
//   PENDING → SIGNING → SIGNED → BROADCASTED → COMPLETED
// On failure the row is moved to FAILED with the error stored
// in error_msg; the row stays so an operator can inspect.
//
// Returns when ctx is cancelled.
func (s *server) runWithdrawalWorker(ctx context.Context) {
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	s.log.Info("withdrawal worker started")
	for {
		select {
		case <-ctx.Done():
			s.log.Info("withdrawal worker stopping", "reason", ctx.Err())
			return
		case <-tick.C:
			s.processPendingWithdrawals(ctx)
		}
	}
}

// processPendingWithdrawals fetches up to N pending rows and runs
// each through the build → sign → broadcast pipeline. Each row
// is processed sequentially so we do not slam the signer or the
// chain; rows still pending after maxPerTick are picked up next
// tick.
func (s *server) processPendingWithdrawals(ctx context.Context) {
	const maxPerTick = 5
	rows, err := s.pool.Query(ctx,
		`SELECT id::text, chain, asset, dest_address, amount::text, user_id::text
		 FROM withdrawals
		 WHERE status = 'PENDING'
		 ORDER BY created_at ASC
		 LIMIT $1
		 FOR UPDATE SKIP LOCKED`, maxPerTick)
	if err != nil {
		s.log.Error("withdrawal worker query failed", "error", err)
		return
	}
	type item struct {
		ID, Chain, Asset, To, Amount, UID string
	}
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.ID, &it.Chain, &it.Asset, &it.To, &it.Amount, &it.UID); err != nil {
			s.log.Error("withdrawal worker scan failed", "error", err)
			continue
		}
		items = append(items, it)
	}
	rows.Close()
	for _, it := range items {
		s.processOneWithdrawal(ctx, it)
	}
}

// runConfirmationWatcher polls withdrawals that are BROADCASTED
// or IN_BLOCK and walks them to COMPLETED via SOLIDIFIED.
//
// State transitions:
//
//   BROADCASTED -> IN_BLOCK         gettransactioninfobyid returns blockNumber
//   IN_BLOCK    -> SOLIDIFIED       blockNumber <= solidified head (>=19 deep)
//   SOLIDIFIED  -> COMPLETED        terminal; the broadcast-time burn has
//                                    already moved frozen -> 0, so this row
//                                    is purely a marker for the audit log
//
// Failure paths:
//
//   BROADCASTED -> BROADCAST_UNKNOWN   tx not findable after N ticks
//   any         -> FAILED              receipt.result != SUCCESS (revert)
//
// Polling interval is 5 s; TRON blocks land every ~3 s and
// finality takes ~60 s, so a 5 s tick keeps us inside the
// rate-limit envelope (chainstack free tier = 5 RPS sustained,
// 25 burst). One per-row tick is cheap: a single
// gettransactioninfobyid + a single getnowblock shared by all
// rows in the batch.
//
// Returns when ctx is cancelled.
func (s *server) runConfirmationWatcher(ctx context.Context) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	s.log.Info("confirmation watcher started")
	for {
		select {
		case <-ctx.Done():
			s.log.Info("confirmation watcher stopping", "reason", ctx.Err())
			return
		case <-tick.C:
			s.processPendingConfirmations(ctx)
		}
	}
}

// processPendingConfirmations walks all BROADCASTED + IN_BLOCK
// rows in one pass. We fetch the solidified head ONCE per tick
// (chainstack caps RPS) and share it across rows; the per-row
// call is only gettransactioninfobyid.
//
// Unknown-tx threshold: after `unknownThresholdTicks` ticks
// (default 12 ≈ 60s) without seeing the tx, we mark the row
// BROADCAST_UNKNOWN so an operator can investigate. We do NOT
// fail outright because a slow-broadcast tx may simply be
// delayed past the polling window of one node.
const unknownThresholdTicks = 12

func (s *server) processPendingConfirmations(ctx context.Context) {
	if s.tronAd == nil {
		return
	}
	// Solidified head. If the RPC fails we skip this tick
	// rather than spam the chain.
	solidified, err := s.tronAd.GetSolidifiedBlock(ctx)
	if err != nil {
		s.log.Warn("confirmation watcher: GetSolidifiedBlock failed", "error", err)
		return
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id::text, tx_hash
		 FROM withdrawals
		 WHERE status IN ('BROADCASTED', 'IN_BLOCK')
		   AND tx_hash IS NOT NULL
		 ORDER BY created_at ASC
		 LIMIT 50`)
	if err != nil {
		s.log.Error("confirmation watcher: query failed", "error", err)
		return
	}
	type item struct {
		ID, TxHash string
	}
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.ID, &it.TxHash); err != nil {
			s.log.Error("confirmation watcher: scan failed", "error", err)
			continue
		}
		items = append(items, it)
	}
	rows.Close()
	for _, it := range items {
		s.confirmOneWithdrawal(ctx, it, solidified.Height)
	}
}

// confirmOneWithdrawal is the per-row state machine. It reads
// the tx receipt and decides whether to transition the row to
// IN_BLOCK, COMPLETED, FAILED, or BROADCAST_UNKNOWN.
//
// `solidifiedHeight` is the head height at which a block becomes
// final on TRON (~19 blocks behind the actual head). A tx whose
// blockNumber <= solidifiedHeight is irreversible.
func (s *server) confirmOneWithdrawal(ctx context.Context, it struct {
	ID, TxHash string
}, solidifiedHeight uint64) {
	wlog := s.log.With("withdrawal_id", it.ID, "tx_hash", it.TxHash)
	tx, err := s.tronAd.GetTransaction(ctx, it.TxHash)
	if err != nil {
		// RPC failure (timeout, rate limit) — leave row alone;
		// next tick will retry.
		wlog.Warn("confirmation watcher: GetTransaction failed", "error", err)
		return
	}
	// TxStatusPending with no blockNumber means either still in
	// mempool OR the RPC didn't return a receipt at all (i.e.
	// unknown tx). Disambiguate by blockNumber.
	if tx.BlockNumber == 0 && tx.Status == blockchain.TxStatusPending {
		if err := s.bumpUnknownTick(ctx, it.ID); err != nil {
			wlog.Error("bump unknown tick failed", "error", err)
		}
		return
	}
	// Tx is in a block.
	if uint64(tx.BlockNumber) > solidifiedHeight {
		// Included but not final yet.
		_ = s.updateWithdrawalStatus(ctx, it.ID, "BROADCASTED", "IN_BLOCK", nil, nil, "")
		// Persist block_number on the row so an operator can
		// inspect or the next tick can detect "depth
		// approaching solidification".
		if _, err := s.pool.Exec(ctx,
			`UPDATE withdrawals SET block_number = $2::bigint
			 WHERE id = $1 AND (block_number IS NULL OR block_number <> $2::bigint)`,
			it.ID, tx.BlockNumber); err != nil {
			wlog.Warn("block_number persist failed", "error", err)
		}
		wlog.Info("withdrawal in block",
			"block_number", tx.BlockNumber,
			"solidified_height", solidifiedHeight)
		return
	}
	// Solidified: tx is final. Check receipt.result.
	switch tx.Status {
	case blockchain.TxStatusSuccess:
		// Note: the frozen balance was already burned at
		// BROADCASTED time (the broadcast is a commitment
		// that funds will leave the ledger; solidified is
		// just the receipt). We do not touch balances here.
		// The conditional UPDATE matches from-status; if the
		// row was BROADCASTED (this is the first time we
		// observe it was already solidified) the first call
		// 0-rows and the retry with BROADCASTED source
		// succeeds.
		if err := s.updateWithdrawalStatus(ctx, it.ID, "IN_BLOCK", "COMPLETED", nil, nil, ""); err != nil {
			_ = s.updateWithdrawalStatus(ctx, it.ID, "BROADCASTED", "COMPLETED", nil, nil, "")
		}
		wlog.Info("withdrawal completed",
			"block_number", tx.BlockNumber,
			"solidified_height", solidifiedHeight,
			"depth", solidifiedHeight-uint64(tx.BlockNumber))
	case blockchain.TxStatusFailed:
		if err := s.updateWithdrawalStatus(ctx, it.ID, "IN_BLOCK", "FAILED", nil, nil,
			"contract reverted on chain; frozen balance burned, manual credit required"); err != nil {
			_ = s.updateWithdrawalStatus(ctx, it.ID, "BROADCASTED", "FAILED", nil, nil,
				"contract reverted on chain; frozen balance burned, manual credit required")
		}
		wlog.Warn("withdrawal reverted on chain", "block_number", tx.BlockNumber)
	default:
		wlog.Warn("unexpected tx status", "status", tx.Status)
	}
}

// bumpUnknownTick increments an internal "unknown-tick count"
// stored on the withdrawal row in error_msg. Once the count
// exceeds the threshold, the row is demoted to BROADCAST_UNKNOWN.
//
// We piggyback on error_msg rather than adding a new column
// because the count is observability-only; the operator either
// retries the broadcast manually or moves on.
func (s *server) bumpUnknownTick(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE withdrawals
		 SET error_msg = COALESCE(error_msg, '') ||
		     CASE WHEN error_msg LIKE '%unknown_ticks=%' THEN '' ELSE 'unknown_ticks=' END ||
		     '1,'
		 WHERE id = $1 AND status IN ('BROADCASTED','IN_BLOCK')`, id)
	if err != nil {
		return err
	}
	var errMsg string
	if err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(error_msg, '') FROM withdrawals WHERE id = $1`, id).Scan(&errMsg); err != nil {
		return err
	}
	n := countUnknownTicks(errMsg)
	if n < unknownThresholdTicks {
		return nil
	}
	_, err = s.pool.Exec(ctx,
		`UPDATE withdrawals
		 SET status = 'BROADCAST_UNKNOWN'
		 WHERE id = $1 AND status IN ('BROADCASTED','IN_BLOCK')`, id)
	return err
}

// countUnknownTicks finds the LAST "unknown_ticks=N," substring
// in error_msg and returns N. We append "1" on each tick and let
// the operator eyeball the running list if they want history.
func countUnknownTicks(s string) int {
	const prefix = "unknown_ticks="
	idx := strings.LastIndex(s, prefix)
	if idx < 0 {
		return 0
	}
	tail := s[idx+len(prefix):]
	end := strings.IndexByte(tail, ',')
	if end < 0 {
		end = len(tail)
	}
	n := 0
	for _, c := range tail[:end] {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// runSweepWorker polls sweep_tasks and drains PENDING rows
// through build -> sign -> broadcast. It re-uses the same
// signer + adapter plumbing as runWithdrawalWorker; the only
// difference is the from/to addresses and the SQL source
// table. The confirmation watcher (runConfirmationWatcher)
// handles the BROADCASTED -> COMPLETED transition by tx_hash,
// which is set on the sweep_tasks row just like it is on
// withdrawals.
//
// Triggering sweeps is the responsibility of
// runSweepPlanner (separate tick); this worker only drains
// rows that already exist in sweep_tasks.
//
// State transitions:
//
//   PENDING -> BUILDING -> AWAITING_SIGN -> AWAITING_BROADCAST
//   AWAITING_BROADCAST -> BROADCASTED       (set tx_hash)
//   AWAITING_BROADCAST -> FAILED            (set last_error)
//
// Returns when ctx is cancelled.
func (s *server) runSweepWorker(ctx context.Context) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	s.log.Info("sweep worker started")
	for {
		select {
		case <-ctx.Done():
			s.log.Info("sweep worker stopping", "reason", ctx.Err())
			return
		case <-tick.C:
			s.processPendingSweeps(ctx)
		}
	}
}

// processPendingSweeps drains up to N PENDING sweep rows.
// Each row is processed sequentially; with sweep volume
// expected in the low double-digits per hour, sequential
// is fine for V1 and keeps the rate-limit envelope intact.
func (s *server) processPendingSweeps(ctx context.Context) {
	const maxPerTick = 3
	rows, err := s.pool.Query(ctx,
		`SELECT st.id::text,
		        st.chain,
		        st.asset,
		        st.amount::text,
		        fa.address AS from_addr,
		        ta.address AS to_addr,
		        ta.id::text  AS to_addr_id
		 FROM sweep_tasks st
		 JOIN wallet_addresses fa ON fa.id = st.from_address_id
		 JOIN wallet_addresses ta ON ta.id = st.to_address_id
		 WHERE st.status = 'PENDING'
		 ORDER BY st.created_at ASC
		 LIMIT $1
		 FOR UPDATE OF st SKIP LOCKED`, maxPerTick)
	if err != nil {
		s.log.Error("sweep worker: query failed", "error", err)
		return
	}
	type item struct {
		ID, Chain, Asset, Amount, FromAddr, ToAddr, ToAddrID string
	}
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.ID, &it.Chain, &it.Asset, &it.Amount,
			&it.FromAddr, &it.ToAddr, &it.ToAddrID); err != nil {
			s.log.Error("sweep worker: scan failed", "error", err)
			continue
		}
		items = append(items, it)
	}
	rows.Close()
	for _, it := range items {
		s.processOneSweep(ctx, it)
	}
}

// processOneSweep is the per-row state machine for sweeps.
// The build/sign/broadcast pattern mirrors runWithdrawalWorker;
// the only V1 simplification is that sweeps do not touch the
// user's balances row directly. We do not subtract the sweeped
// amount from the user's available balance because the user
// deposit credits already happened at CREDITED time; the sweep
// just consolidates the on-chain funds.
//
// If the sweep ultimately succeeds, an admin script (or the
// reconciler in V2) credits the HOT wallet's "exchange
// treasury" balance. For V1 we leave the treasury update out of
// the worker so the change stays small.
func (s *server) processOneSweep(ctx context.Context, it struct {
	ID, Chain, Asset, Amount, FromAddr, ToAddr, ToAddrID string
}) {
	wlog := s.log.With("sweep_id", it.ID, "chain", it.Chain,
		"from", it.FromAddr, "to", it.ToAddr)

	// PENDING -> BUILDING.
	if _, err := s.pool.Exec(ctx,
		`UPDATE sweep_tasks SET status = 'BUILDING', built_at = NOW()
		 WHERE id = $1 AND status = 'PENDING'`, it.ID); err != nil {
		wlog.Error("sweep PENDING->BUILDING failed", "error", err)
		return
	}

	// BuildTransfer takes (from, to, amount, contract). The
	// from-address comes from the sweep row; the contract is
	// hard-coded USDT (V1 supports only USDT-TRC20 sweeps).
	contract := "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"
	if v := os.Getenv("TRON_USDT_CONTRACT"); v != "" {
		contract = v
	}
	amountFloat, err := strconv.ParseFloat(it.Amount, 64)
	if err != nil || amountFloat <= 0 {
		wlog.Error("sweep amount parse failed", "amount", it.Amount, "error", err)
		s.failSweep(ctx, it.ID, "BUILDING", fmt.Sprintf("bad amount %q", it.Amount))
		return
	}
	amountUnits := uint64(amountFloat * 1_000_000)

	build, err := s.tronAd.BuildTransfer(ctx, it.FromAddr, it.ToAddr, amountUnits, contract)
	if err != nil {
		wlog.Error("sweep BuildTransfer failed", "error", err)
		s.failSweep(ctx, it.ID, "BUILDING", err.Error())
		return
	}
	wlog.Info("sweep build ok",
		"raw_tx_len", len(build.RawTx),
		"build_tx_hash", build.TxHash)

	// BUILDING -> AWAITING_SIGN.
	if _, err := s.pool.Exec(ctx,
		`UPDATE sweep_tasks SET status = 'AWAITING_SIGN' WHERE id = $1 AND status = 'BUILDING'`,
		it.ID); err != nil {
		wlog.Error("sweep BUILDING->AWAITING_SIGN failed", "error", err)
		return
	}

	// Note: in V1 the hot-wallet keypair is derived at index 0
	// by the signer service. V2 will pass an explicit
	// key_id per sweep (multi-hot-wallet support).
	//
	// The signer needs the network label so it can use the
	// matching chain id when computing the SHA-256 digest it
	// signs. We hand it s.networkName ("mainnet" /
	// "nile_testnet" / "private_testnet") rather than a
	// hardcoded string so the same code path works against any
	// tron network — Self.7 saw NPE / SIGERROR when the signer
	// used mainnet chain id against a private net that uses 0x27.
	signedTx, _, err := s.signer.Sign(ctx, "TRON", "TRON/hot/0", s.networkName, build.RawTx)
	if err != nil {
		wlog.Error("sweep Sign RPC failed", "error", err)
		s.failSweep(ctx, it.ID, "AWAITING_SIGN", err.Error())
		return
	}
	sig := signedTx[len(build.RawTx):]
	if len(sig) != 65 {
		wlog.Error("sweep unexpected signature length", "len", len(sig))
		s.failSweep(ctx, it.ID, "AWAITING_SIGN", fmt.Sprintf("bad signature length %d", len(sig)))
		return
	}

	// AWAITING_SIGN -> AWAITING_BROADCAST.
	if _, err := s.pool.Exec(ctx,
		`UPDATE sweep_tasks SET status = 'AWAITING_BROADCAST', signed_at = NOW() WHERE id = $1 AND status = 'AWAITING_SIGN'`,
		it.ID); err != nil {
		wlog.Error("sweep AWAITING_SIGN->AWAITING_BROADCAST failed", "error", err)
		return
	}

	// Dry-run support for sweeps: skip the broadcast and
	// persist the signed bytes for offline replay.
	if os.Getenv("TRON_DRY_RUN_BROADCAST") == "1" {
		sigHex := hex.EncodeToString(sig)
		wlog.Info("sweep dry-run: skipping broadcast",
			"raw_tx_len", len(build.RawTx),
			"sig_hex_len", len(sigHex))
		_, err = s.pool.Exec(ctx,
			`UPDATE sweep_tasks SET status = 'SIGNED', tx_hash = $2, last_error = 'dry-run sig=' || $3
			 WHERE id = $1 AND status = 'AWAITING_BROADCAST'`,
			it.ID, build.TxHash, sigHex)
		if err != nil {
			wlog.Error("sweep dry-run transition failed", "error", err)
		}
		return
	}

	// POST the JSON form the chain accepts. See the equivalent
	// block in the withdrawal worker for why raw_data_hex +
	// signature_hex triggers NPE on TRON v4.8.2.1.
	bcastResp, err := s.tronAd.BroadcastSigned(ctx, build.RawData, sig)
	if err != nil {
		wlog.Error("sweep Broadcast failed", "error", err)
		s.failSweep(ctx, it.ID, "AWAITING_BROADCAST", err.Error())
		return
	}
	txHash := bcastResp.TxHash
	if txHash == "" {
		txHash = build.TxHash
	}
	wlog.Info("sweep broadcast ok",
		"tx_hash", txHash,
		"accepted", bcastResp.Accepted)

	// AWAITING_BROADCAST -> BROADCASTED. The confirmation
	// watcher's existing scope covers withdrawal rows only,
	// so we add a parallel poll for sweep_tasks: same RPC
	// pattern, different table.
	if _, err := s.pool.Exec(ctx,
		`UPDATE sweep_tasks SET status = 'BROADCASTED', tx_hash = $2, broadcast_at = NOW() WHERE id = $1 AND status = 'AWAITING_BROADCAST'`,
		it.ID, txHash); err != nil {
		wlog.Error("sweep AWAITING_BROADCAST->BROADCASTED failed", "error", err)
	}
}

// failSweep is the catch-all error path; we always leave the
// row in FAILED with a last_error string so the operator can
// inspect.
func (s *server) failSweep(ctx context.Context, id, fromStatus, errMsg string) {
	_, err := s.pool.Exec(ctx,
		`UPDATE sweep_tasks SET status = 'FAILED', last_error = $2, completed_at = NOW()
		 WHERE id = $1 AND status = $3`, id, errMsg, fromStatus)
	if err != nil {
		s.log.Error("sweep fail update failed", "sweep_id", id, "error", err)
	}
}

// runSweepPlanner periodically scans DEPOSIT addresses and
// inserts sweep_tasks rows for any whose on-chain balance
// exceeds sweepThreshold. We do not poll the chain every tick
// (rate-limit) — V1 defaults to a 10-minute interval, configurable
// via TRON_SWEEP_PLANNER_INTERVAL (seconds).
//
// The planner is intentionally read-only on the chain side:
// it never moves funds. The sweep_worker (above) drains the
// resulting sweep_tasks rows.
//
// Returns when ctx is cancelled.
func (s *server) runSweepPlanner(ctx context.Context) {
	intervalSec := 600 // 10 min default
	if v := os.Getenv("TRON_SWEEP_PLANNER_INTERVAL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			intervalSec = n
		}
	}
	thresholdUnits := uint64(10 * 1_000_000) // 10 USDT default
	if v := os.Getenv("TRON_SWEEP_THRESHOLD_USDT"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			thresholdUnits = uint64(f * 1_000_000)
		}
	}
	contract := "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"
	if v := os.Getenv("TRON_USDT_CONTRACT"); v != "" {
		contract = v
	}
	tick := time.NewTicker(time.Duration(intervalSec) * time.Second)
	defer tick.Stop()
	s.log.Info("sweep planner started",
		"intervalSec", intervalSec,
		"threshold_units", thresholdUnits)
	for {
		select {
		case <-ctx.Done():
			s.log.Info("sweep planner stopping", "reason", ctx.Err())
			return
		case <-tick.C:
			s.planSweeps(ctx, contract, thresholdUnits)
		}
	}
}

// planSweeps scans every DEPOSIT address whose status is
// ACTIVE, asks the chain for its current USDT balance, and
// inserts a sweep_tasks row when the balance exceeds the
// threshold. We use ON CONFLICT DO NOTHING on a synthetic
// (from_address_id, status='PENDING') partial-unique index
// to avoid duplicate inserts when two planner ticks race.
func (s *server) planSweeps(ctx context.Context, contract string, thresholdUnits uint64) {
	rows, err := s.pool.Query(ctx,
		`SELECT id::text, address
		 FROM wallet_addresses
		 WHERE chain = 'TRON'
		   AND wallet_type = 'DEPOSIT'
		   AND status = 'ACTIVE'`)
	if err != nil {
		s.log.Error("sweep planner: query deposits failed", "error", err)
		return
	}
	type dep struct{ ID, Addr string }
	var deps []dep
	for rows.Next() {
		var d dep
		if err := rows.Scan(&d.ID, &d.Addr); err != nil {
			continue
		}
		deps = append(deps, d)
	}
	rows.Close()
	if len(deps) == 0 {
		return
	}

	// Find HOT wallet (V1 assumes exactly one).
	var hotID, hotAddr string
	if err := s.pool.QueryRow(ctx,
		`SELECT id::text, address FROM wallet_addresses
		 WHERE chain = 'TRON' AND wallet_type = 'HOT' AND status = 'ACTIVE'
		 ORDER BY created_at ASC LIMIT 1`).Scan(&hotID, &hotAddr); err != nil {
		s.log.Warn("sweep planner: no HOT wallet configured", "error", err)
		return
	}

	for _, d := range deps {
		bal, err := s.tronAd.GetBalance(ctx, d.Addr, contract)
		if err != nil {
			s.log.Warn("sweep planner: GetBalance failed", "addr", d.Addr, "error", err)
			continue
		}
		if bal.Available == nil || bal.Available.Uint64() < thresholdUnits {
			continue
		}
		// Insert sweep task. ON CONFLICT DO NOTHING on a
		// partial unique index means at most one PENDING row
		// per (from_address, asset). second planner tick
		// finds it already exists and skips.
		//
		// V1 cooling-off: refuse to insert if the same
		// from-address has a non-FAILED row younger than 1
		// hour. This stops the planner from creating a new
		// sweep immediately after the worker drains the old
		// one (since the partial-unique index allows a new
		// PENDING the moment the worker demotes the row to
		// SIGNED/AWAITING_BROADCAST). Without the cooldown
		// the planner would re-sweep the same address on
		// every tick.
		var existing int
		if err := s.pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM sweep_tasks
			 WHERE from_address_id = $1 AND status <> 'FAILED'
			   AND created_at > NOW() - INTERVAL '1 hour'`,
			d.ID).Scan(&existing); err != nil {
			s.log.Warn("sweep planner: cooldown query failed", "error", err)
		}
		if existing > 0 {
			s.log.Info("sweep planner: cooling off", "from", d.Addr,
				"existing_recent", existing)
			continue
		}
		amountFloat := float64(bal.Available.Uint64()) / 1_000_000
		_, err = s.pool.Exec(ctx,
			`INSERT INTO sweep_tasks
			   (chain, asset, from_address_id, to_address_id, amount, status)
			 VALUES ('TRON', 'USDT', $1::uuid, $2::uuid, $3::numeric, 'PENDING')
			 ON CONFLICT (from_address_id, asset) WHERE status = 'PENDING'
			 DO NOTHING`,
			d.ID, hotID, fmt.Sprintf("%.6f", amountFloat))
		if err != nil {
			s.log.Error("sweep planner: insert failed", "addr", d.Addr, "error", err)
			continue
		}
		s.log.Info("sweep planned",
			"from", d.Addr, "to", hotAddr,
			"amount", fmt.Sprintf("%.6f", amountFloat))
	}
}

// runSweepConfirmationWatcher mirrors runConfirmationWatcher
// but operates on sweep_tasks. Same RPC pattern (one
// getnowblock + N gettransactioninfobyid), different table.
// We keep it as a separate loop so withdrawal confirmations are
// not gated on sweep throughput (and vice versa).
//
// Returns when ctx is cancelled.
func (s *server) runSweepConfirmationWatcher(ctx context.Context) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	s.log.Info("sweep confirmation watcher started")
	for {
		select {
		case <-ctx.Done():
			s.log.Info("sweep confirmation watcher stopping", "reason", ctx.Err())
			return
		case <-tick.C:
			s.processPendingSweepConfirmations(ctx)
		}
	}
}

// processPendingSweepConfirmations walks BROADCASTED sweep
// rows and advances them to SOLIDIFIED/COMPLETED/FAILED.
//
// We share the same finality threshold (block depth >= 27)
// as the withdrawal watcher; this matches TRON's
// "19 SRs voted" guarantee and is what GetSolidifiedBlock
// returns.
func (s *server) processPendingSweepConfirmations(ctx context.Context) {
	if s.tronAd == nil {
		return
	}
	solidified, err := s.tronAd.GetSolidifiedBlock(ctx)
	if err != nil {
		s.log.Warn("sweep confirmation watcher: GetSolidifiedBlock failed", "error", err)
		return
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id::text, tx_hash
		 FROM sweep_tasks
		 WHERE status = 'BROADCASTED'
		   AND tx_hash IS NOT NULL
		 ORDER BY created_at ASC
		 LIMIT 50`)
	if err != nil {
		s.log.Error("sweep confirmation watcher: query failed", "error", err)
		return
	}
	type item struct{ ID, TxHash string }
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.ID, &it.TxHash); err != nil {
			continue
		}
		items = append(items, it)
	}
	rows.Close()
	for _, it := range items {
		tx, err := s.tronAd.GetTransaction(ctx, it.TxHash)
		if err != nil {
			s.log.Warn("sweep confirmation: GetTransaction failed", "tx_hash", it.TxHash, "error", err)
			continue
		}
		if uint64(tx.BlockNumber) > solidified.Height {
			_, _ = s.pool.Exec(ctx,
				`UPDATE sweep_tasks SET status = 'IN_BLOCK' WHERE id = $1 AND status = 'BROADCASTED'`,
				it.ID)
			continue
		}
		switch tx.Status {
		case blockchain.TxStatusSuccess:
			_, _ = s.pool.Exec(ctx,
				`UPDATE sweep_tasks SET status = 'COMPLETED', solidified_at = NOW(), completed_at = NOW() WHERE id = $1 AND status IN ('BROADCASTED','IN_BLOCK')`,
				it.ID)
			s.log.Info("sweep completed", "sweep_id", it.ID, "tx_hash", it.TxHash)
		case blockchain.TxStatusFailed:
			_, _ = s.pool.Exec(ctx,
				`UPDATE sweep_tasks SET status = 'FAILED', last_error = 'contract reverted on chain', completed_at = NOW() WHERE id = $1 AND status IN ('BROADCASTED','IN_BLOCK')`,
				it.ID)
		}
	}
}

// processOneWithdrawal is the actual state machine for a single
// withdrawal row. Errors at each stage persist back to the DB so
// the row reflects the actual outcome even if the process crashes.
//
// V1 dry-run mode: set TRON_DRY_RUN_BROADCAST=1 to stop after
// signing and persist the row as SIGNED without submitting to the
// chain. Used to verify the build+sign pipeline end-to-end
// without paying real TRX fees; an operator can later flip the
// row to PENDING and let the worker retry with broadcast enabled.
func (s *server) processOneWithdrawal(ctx context.Context, it struct {
	ID, Chain, Asset, To, Amount, UID string
}) {
	wlog := s.log.With("withdrawal_id", it.ID, "chain", it.Chain, "asset", it.Asset)

	// 1. PENDING → SIGNING. We use a per-row conditional UPDATE
	//    so two worker instances (or a parallel admin manual
	//    sign) do not race.
	if err := s.updateWithdrawalStatus(ctx, it.ID, "PENDING", "SIGNING", nil, nil, ""); err != nil {
		wlog.Error("transition PENDING->SIGNING failed", "error", err)
		return
	}
	wlog.Info("withdrawal signing started")

	// 2. Build the unsigned transaction via the adapter. The
	//    adapter talks to a chain RPC node which assembles
	//    the protobuf and returns raw_data bytes.
	//
	//    Default: native TRX (contract empty → adapter falls
	//    back to BuildNativeTransfer). On chains that have a
	//    TRC20 token deployed (mainnet, nile), set
	//    TRON_USDT_CONTRACT in the environment to override;
	//    the worker forwards that contract address verbatim.
	contract := os.Getenv("TRON_USDT_CONTRACT")
	hotWalletAddr := s.hotWalletAddr // populated at startup from signer DeriveAddress(0)
	build, err := s.tronAd.BuildTransfer(ctx, hotWalletAddr, it.To, usdtToUnits(it.Amount), contract)
	if err != nil {
		wlog.Error("BuildTransfer failed", "error", err)
		s.updateWithdrawalStatus(ctx, it.ID, "SIGNING", "FAILED", nil, nil, err.Error())
		// Unlock balance.
		s.unlockWithdrawalBalance(ctx, it.UID, it.Asset, it.Amount)
		return
	}
	wlog.Info("BuildTransfer ok", "tx_hash", build.TxHash, "raw_tx_len", len(build.RawTx))

	// 3. Sign via the signer service. signer returns
	//    raw_tx || 65-byte signature; we strip the trailing
	//    signature and use it as Transaction.signature[0].
	//
	// Pass s.networkName so the signer picks the matching
	// chain id (0x27 for private_testnet, 0x2b6653dc for
	// mainnet, 0xcd8690dc for nile_testnet). Hardcoding
	// "mainnet" here would make private_testnet withdrawals
	// fail with SIGERROR at broadcast time.
	signedTx, txHash, err := s.signer.Sign(ctx, "TRON", "TRON/hot/0", s.networkName, build.RawTx)
	if err != nil {
		wlog.Error("Sign RPC failed", "error", err)
		s.updateWithdrawalStatus(ctx, it.ID, "SIGNING", "FAILED", nil, nil, err.Error())
		s.unlockWithdrawalBalance(ctx, it.UID, it.Asset, it.Amount)
		return
	}
	sig := signedTx[len(build.RawTx):]
	if len(sig) != 65 {
		wlog.Error("unexpected signature length", "len", len(sig))
		s.updateWithdrawalStatus(ctx, it.ID, "SIGNING", "FAILED", nil, nil, fmt.Sprintf("bad signature length %d", len(sig)))
		s.unlockWithdrawalBalance(ctx, it.UID, it.Asset, it.Amount)
		return
	}
	wlog.Info("signed ok", "tx_hash", txHash, "sig_len", len(sig))

	// V1 dry-run short-circuit. Persist the signed_tx as
	// SIGNED and return without touching the chain. The chain
	// ID + tx_hash field on the row carry the bytes the
	// operator needs to inspect or replay later.
	if os.Getenv("TRON_DRY_RUN_BROADCAST") == "1" {
		// Append the 65-byte sig hex to error_msg so the
		// operator can extract it from the DB for offline
		// replay. error_msg is normally a human-readable
		// failure string; we abuse it here for a one-off
		// observability affordance.
		sigHex := hex.EncodeToString(sig)
		wlog.Info("dry-run: skipping broadcast",
			"raw_tx_len", len(build.RawTx),
			"sig_hex_len", len(sigHex),
			"tx_hash", txHash)
		// Transition from SIGNING (where the row sits at this
		// point) directly to a terminal SIGNED state. We do
		// NOT advance through BROADCASTED in dry-run because
		// the broadcast RPC was deliberately skipped; using
		// BROADCASTED as the guard status would mismatch the
		// actual row state and the conditional UPDATE would
		// 0-row.
		if err := s.updateWithdrawalStatus(ctx, it.ID, "SIGNING", "SIGNED", &txHash, nil, "dry-run sig="+sigHex); err != nil {
			wlog.Error("dry-run transition failed", "error", err)
		}
		return
	}

	// 4. Broadcast. We POST the JSON form the chain accepts:
	// {raw_data: <JSON object>, signature: [hex], txID, visible:true}.
	// The Build* path populated build.RawData with the parsed
	// object (visible=true on the chain returns it). Posting
	// raw_data_hex + signature_hex in a JSON envelope is what
	// triggers the NullPointerException we hit on TRON v4.8.2.1
	// when this method fell back to that format.
	bcastResp, err := s.tronAd.BroadcastSigned(ctx, build.RawData, sig)
	if err != nil {
		wlog.Error("Broadcast failed", "error", err)
		s.updateWithdrawalStatus(ctx, it.ID, "BROADCASTED", "FAILED", &txHash, nil, err.Error())
		// Don't unlock here — chain may still pick up the
		// signed bytes; BROADCAST_UNKNOWN + manual review
		// is the safer state.
		s.updateWithdrawalStatus(ctx, it.ID, "BROADCASTED", "BROADCAST_UNKNOWN", &txHash, nil, err.Error())
		return
	}
	txHash = bcastResp.TxHash
	if txHash == "" {
		// keep txHash from the signer — the network may not
		// have responded with its txID but the bytes were
		// submitted.
		txHash = build.TxHash
	}
	wlog.Info("broadcast ok", "tx_hash", txHash, "accepted", bcastResp.Accepted)

	// 5. SIGNING → BROADCASTED. Self.9 had a bug where the worker
	//    went straight SIGNING → COMPLETED (skipping BROADCASTED
	//    entirely), which left the row's tx_hash NULL and the
	//    state machine stuck at SIGNING forever — the inclusion
	//    watcher only queries `WHERE status IN ('BROADCASTED',
	//    'IN_BLOCK')`, so it never saw the row again.
	//
	//    The correct flow per the confirmation-watcher's design
	//    (see comment block above runConfirmationWatcher) is:
	//
	//      SIGNING  → BROADCASTED    (here: tx committed to mempool)
	//      BROADCASTED → IN_BLOCK   (confirmation watcher, after
	//                                gettransactioninfobyid returns
	//                                a non-zero blockNumber)
	//      IN_BLOCK → COMPLETED     (confirmation watcher, after
	//                                blockNumber <= solidified head)
	//
	//    The worker commits to BROADCASTED, persisting tx_hash
	//    and sent_at, then yields control to the confirmation
	//    watcher.
	if err := s.updateWithdrawalStatus(ctx, it.ID, "SIGNING", "BROADCASTED", &txHash, nil, ""); err != nil {
		wlog.Error("transition SIGNING->BROADCASTED failed", "error", err)
		return
	}
	// The frozen-balance burn (broadcast-time commitment that
	// funds have left the ledger) belongs here, not at
	// COMPLETED — by the time the chain confirms inclusion the
	// frozen amount must already be zero so we never double-
	// count or refund a tx that already moved funds.
	if err := s.burnWithdrawalBalance(ctx, it.UID, it.Asset, it.Amount); err != nil {
		wlog.Error("frozen->zero burn failed", "error", err)
	}
}

// updateWithdrawalStatus is the only place the worker changes
// withdrawal status. It always passes the previous status as
// a guard so two workers cannot race each other; a 0-row result
// means the row's status changed under us and we lost the race.
func (s *server) updateWithdrawalStatus(ctx context.Context, id, fromStatus, toStatus string, txHash *string, completedAt *time.Time, errMsg string) error {
	ct, err := s.pool.Exec(ctx,
		`UPDATE withdrawals
		 SET status = $2::text,
		     tx_hash = COALESCE($3, tx_hash),
		     completed_at = COALESCE($4, completed_at),
		     sent_at = CASE WHEN $2 IN ('BROADCASTED','COMPLETED') THEN NOW() ELSE sent_at END,
		     error_msg = NULLIF($5, '')
		 WHERE id = $1 AND status = $6::text`,
		id, toStatus, txHash, completedAt, errMsg, fromStatus)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("withdrawal %s not in expected status %s", id, fromStatus)
	}
	return nil
}

// unlockWithdrawalBalance reverses the available→frozen lock
// when the worker fails before broadcasting. Safe to call
// repeatedly; the SQL is idempotent.
func (s *server) unlockWithdrawalBalance(ctx context.Context, uid, asset, amount string) {
	// Idempotent: use GREATEST so we never drive frozen below zero
	// (this happens when a withdrawal row was inserted outside the
	// createWithdrawal path and the available→frozen lock was
	// never taken). The user can still recover the funds via an
	// admin adjustment; the worker should not crash.
	_, err := s.pool.Exec(ctx,
		`UPDATE balances
		 SET available = available + LEAST($2::numeric, frozen),
		     frozen = GREATEST(frozen - $2::numeric, 0),
		     updated_at = NOW()
		 WHERE user_id = $1 AND asset = $3`,
		uid, amount, asset)
	if err != nil {
		s.log.Error("unlock balance failed", "uid", uid, "asset", asset, "amount", amount, "error", err)
	}
}

// burnWithdrawalBalance drops the frozen amount after the
// withdrawal is broadcast (the funds are now on-chain).
func (s *server) burnWithdrawalBalance(ctx context.Context, uid, asset, amount string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE balances
		 SET frozen = GREATEST(frozen - $2::numeric, 0),
		     updated_at = NOW()
		 WHERE user_id = $1 AND asset = $3`,
		uid, amount, asset)
	return err
}

// usdtToUnits converts a human-readable USDT amount string (e.g.
// "12.5") to its 6-decimal integer representation (12_500_000).
// Returns 0 on parse error.
func usdtToUnits(amount string) uint64 {
	f, err := strconv.ParseFloat(amount, 64)
	if err != nil {
		return 0
	}
	return uint64(f * 1_000_000)
}

// tronValidateAddress does a Base58 format check (starts with T,
// 34 chars, base58 alphabet). Full Base58Check checksum is
// performed downstream by the chain when the tx is broadcast.
func tronValidateAddress(s string) bool {
	if len(s) != 34 || !strings.HasPrefix(s, "T") {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune("123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz", c) {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	const p = "Bearer "
	if !strings.HasPrefix(h, p) {
		return ""
	}
	return strings.TrimSpace(h[len(p):])
}

func parseLimit(r *http.Request, def, max int) int {
	s := r.URL.Query().Get("limit")
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}

// logRequest wraps a handler with a 200/500-aware access log so
// wallet-api ops can correlate incoming requests with downstream
// errors without grepping the body.
func logRequest(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rw, r)
		log.Info("wallet-api request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.status,
			"bytes", rw.bytes,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// Ensure unused imports do not trip build when the file grows.
var _ = fmt.Sprintf

// buildTronAdapter assembles a TRON adapter from TRON provider
// environment variables. We accept two layouts:
//
//  1. The legacy two-provider form, kept for backwards
//     compatibility with deploy-fresh.sh and existing .env files:
//       TRON_PRIMARY_URL, TRON_PRIMARY_KEY,
//       TRON_BACKUP_URL,  TRON_BACKUP_KEY,
//       TRON_PRIMARY_NAME, TRON_BACKUP_NAME (optional).
//
//  2. The generic N-provider form:
//       TRON_PROVIDER_<NAME>_URL
//       TRON_PROVIDER_<NAME>_KEY  (optional)
//     where <NAME> is the provider label we expose to the admin
//     RPC switch endpoint. Examples:
//       TRON_PROVIDER_CHAINSTACK_URL=https://.../chainstack/<token>/wallet
//       TRON_PROVIDER_NOWNODES_URL=https://.../nownodes/<token>/wallet
//       TRON_PROVIDER_DWELLIR_URL=https://.../dwellir/<token>/wallet
//     Providers are appended to the slice in alphabetical name
//     order, so the highest priority is whichever name sorts
//     first. To control priority without renaming, set
//     TRON_PROVIDER_<NAME>_WEIGHT (defaults to 1).
//
// When both forms are set the legacy form contributes its
// (possibly empty) entries first and the generic form is appended,
// so callers who migrate from primary/backup to a per-name scheme
// can do it incrementally.
func buildTronAdapter(log *slog.Logger) (*tronadapter.Adapter, error) {
	var providers []tronadapter.Provider

	// Legacy two-provider form. Empty URL means "not configured"
	// and the entry is skipped, so a single-provider deploy works.
	if url := os.Getenv("TRON_PRIMARY_URL"); url != "" {
		providers = append(providers, tronadapter.Provider{
			Name:    envOr("TRON_PRIMARY_NAME", "primary"),
			BaseURL: url,
			APIKey:  os.Getenv("TRON_PRIMARY_KEY"),
			Weight:  envInt("TRON_PRIMARY_WEIGHT", 1),
		})
	}
	if url := os.Getenv("TRON_BACKUP_URL"); url != "" {
		providers = append(providers, tronadapter.Provider{
			Name:    envOr("TRON_BACKUP_NAME", "backup"),
			BaseURL: url,
			APIKey:  os.Getenv("TRON_BACKUP_KEY"),
			Weight:  envInt("TRON_BACKUP_WEIGHT", 1),
		})
	}

	// Generic N-provider form. We scan the environment for
	// TRON_PROVIDER_*_URL variables and emit one Provider per
	// match. Names are case-preserved (admin switch uses them
	// case-sensitively) but the sort below is case-insensitive
	// so "Chainstack" and "chainstack" do not double-register.
	providerCount := 0
	for _, kv := range os.Environ() {
		// kv is "KEY=VALUE"; we want only the KEY part. Earlier
		// versions of this loop checked strings.HasSuffix(kv,
		// "_URL") on the whole entry, which fails because the
		// value (URL) does not end with _URL. Splitting first
		// and checking the KEY is the correct test.
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
		providerCount++
		providers = append(providers, tronadapter.Provider{
			Name:    name,
			BaseURL: url,
			APIKey:  os.Getenv("TRON_PROVIDER_" + name + "_KEY"),
			Weight:  envInt("TRON_PROVIDER_"+name+"_WEIGHT", 1),
		})
	}

	if len(providers) == 0 {
		return nil, errors.New("no TRON providers configured (set TRON_PRIMARY_URL or any TRON_PROVIDER_<name>_URL)")
	}
	// Network defaults to mainnet; honour TRON_NETWORK if the
	// operator wants to point at the Nile testnet (V1 + V2 dev
	// loop) or the locally-run java-tron private net (E2E
	// integration tests). Recognised values: "mainnet",
	// "nile_testnet", "private_testnet".
	network := tronadapter.NetworkMainnet
	switch os.Getenv("TRON_NETWORK") {
	case tronadapter.NetworkNile:
		network = tronadapter.NetworkNile
	case tronadapter.NetworkPrivate:
		network = tronadapter.NetworkPrivate
	}
	return tronadapter.NewAdapter(tronadapter.Config{
		Providers: providers,
		Logger:    log,
		Network:   network,
	})
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// envInt parses an env var as int with a default. Returns def on
// empty or unparseable values rather than panicking, so a typo
// in the operator's .env does not bring down the wallet-api.
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
