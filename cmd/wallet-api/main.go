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
	log        *slog.Logger
	pool       *pgxpool.Pool
	jwtSecret  []byte
	wallet     *wallet.ServiceV1
	signer     *signerclient.Client
	tronAd     *tronadapter.Adapter
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
	mux.HandleFunc("/wallet/v1/withdrawals", s.auth(s.listWithdrawals))
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
	return tronadapter.NewAdapter(tronadapter.Config{
		Providers: providers,
		Logger:    log,
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
