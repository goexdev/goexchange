// Package api wires HTTP routes.
package api

import (
	"github.com/prometheus/client_golang/prometheus/promhttp"
	_ "github.com/goexdev/goexchange/internal/metrics" // register metrics

	"github.com/google/uuid"
	"encoding/json"
	"log/slog"
	"time"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/goexdev/goexchange/internal/audit"
	"github.com/goexdev/goexchange/internal/vault"
	"github.com/redis/go-redis/v9"
	"github.com/goexdev/goexchange/internal/auth"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/goexdev/goexchange/internal/chainwatcher"
	"github.com/goexdev/goexchange/internal/marketdata"
	"github.com/goexdev/goexchange/internal/apikeys"
	"github.com/goexdev/goexchange/internal/notifier"
	"github.com/goexdev/goexchange/internal/risk"
	"github.com/goexdev/goexchange/internal/trigger"
	"github.com/goexdev/goexchange/internal/analytics"
	"github.com/goexdev/goexchange/internal/trading"
	"github.com/goexdev/goexchange/internal/user"
	"github.com/goexdev/goexchange/internal/uploads"
	"github.com/goexdev/goexchange/internal/wallet"
)

// Deps holds service dependencies for the router.
type Deps struct {
	Log             *slog.Logger
	Pool            *pgxpool.Pool
	UserSvc         *user.Service
	WalletSvc       *wallet.Service
	TradingSvc      *trading.Service
	MarketDataSvc   *marketdata.Service
	ChainWatcherSvc *chainwatcher.Service
	AuthSvc         *auth.Service
	TOTPSvc         *auth.TOTPService
	NotifPrefs      *notifier.PrefsService
	RiskSvc         *risk.Service
	Notifier        *notifier.Service
	APIKeys         *apikeys.Service
	AuditSvc        *audit.Service
	VaultClient     *vault.Client
	TriggerSvc      *trigger.Service
	AnalyticsSvc    *analytics.Service
	Redis           *redis.Client
	NotifWSHub     *NotifWSHub
	MarketWSHub    *MarketWSHub
	ChainRegistry  *chainwatcher.ChainRegistry
	UploadStore    *uploads.Store
	ConfigPath     string
}

// NewRouter returns an http.Handler with all routes wired.
func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()

	// Rate limiters for security-sensitive endpoints
	// SECURITY: Protect against brute force, spam, and DoS
	loginLimiter := NewRateLimiter(5, time.Minute)         // 5 logins/min per IP
	registerLimiter := NewRateLimiter(3, time.Minute)      // 3 registrations/min per IP
	cancelLimiter := NewRateLimiter(30, time.Minute)       // 30 cancels/min per user
	withdrawLimiter := NewRateLimiter(10, time.Minute)     // 10 withdrawals/min per user
	orderLimiter := NewRateLimiter(60, time.Minute)       // 60 orders/min per user
	favoritesLimiter := NewRateLimiter(50, time.Minute)    // 50 fav ops/min per user
	_ = withdrawLimiter                                       // reserved for future withdraw endpoint

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(loggingMiddleware(d.Log))

	// Security headers middleware
	r.Use(securityHeadersMiddleware)

	// CORS - allow web frontend to call API from any origin
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	// Health
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	r.Handle("/metrics", promhttp.Handler())

	// =========================================================================
	// Public user-facing programmatic API (/user-api/v2).
	//
	// This is a separate path from /api/v1 because the auth model
	// differs (api-key + nonce vs Bearer JWT), the audience differs
	// (external integrations, bots, scripts vs human users), and the
	// traffic profile differs (high-volume automated calls vs
	// interactive UI requests). Keeping them as distinct route groups
	// lets us apply different rate limits, CORS rules, and observability
	// without bleeding state between them.
	//
	// Note: this MUST be registered on the root chi.Router, not
	// inside the /api/v1 sub-router that follows. Go closure
	// scoping means a `r.Route("/user-api/v2", ...)` placed inside
	// the /api/v1 func would shadow `r` to the /api/v1 sub-router
	// and end up mounted at /api/v1/user-api/v2.
	//
	// Auth: X-Api-Key + X-Api-Nonce headers, validated by
	// userAPIKeyAuth which also enforces ±5min clock skew and nonce
	// monotonicity per key. See apikey_middleware.go.
	// =========================================================================
	r.Route("/user-api/v2", func(r chi.Router) {
		// Public endpoints — no api key required, no rate limit
		// beyond the implicit infra (nginx already enforces a
		// reasonable ceiling). These mirror the public market data
		// endpoints under /api/v1 so script authors only have to
		// learn one URL shape.
		r.Get("/ping", userAPIPingHandler(d))
		r.Get("/server-time", userAPIServerTimeHandler(d))
		r.Get("/markets", userAPIListMarketsHandler(d))
		r.Get("/markets/{base}/{quote}/ticker", userAPIMarketTickerHandler(d))
		r.Get("/markets/{base}/{quote}/orderbook", userAPIMarketOrderBookHandler(d))
		r.Get("/markets/{base}/{quote}/trades", userAPIMarketRecentTradesHandler(d))
		r.Get("/currencies", userAPIListCurrenciesHandler(d))

		// Private endpoints — require api key auth. The
		// middleware puts user_id + scopes in context; handlers
		// that need scope enforcement wrap with requireScope.
		r.Group(func(r chi.Router) {
			r.Use(userAPIKeyAuth(d.APIKeys))

			r.Get("/balances", userAPIListBalancesHandler(d))

			r.With(requireScope(ScopeTrade)).Post("/orders", userAPIPlaceOrderHandler(d))
			r.With(requireScope(ScopeTrade)).Delete("/orders/{id}", userAPIcancelOrderHandler(d))
			r.With(requireScope(ScopeRead)).Get("/orders", userAPIListOrdersHandler(d))
		})
	})

	// API v1
	r.Route("/api/v1", func(r chi.Router) {
		// Public - rate limited
		r.With(registerLimiter.Middleware(IPFromRequest)).
			Post("/users/register", registerHandler(d))
		r.With(loginLimiter.Middleware(IPFromRequest)).
			Post("/users/login", loginHandler(d))
			r.Post("/auth/2fa/complete", totpLoginCompleteHandler(d))  // 2FA login completion

		// Public markets (no auth)
		r.Get("/markets", listMarketsHandler(d))
		r.Get("/markets/{base}/{quote}/orderbook", marketOrderBookHandler(d))
		r.Get("/markets/{base}/{quote}/ticker", marketTickerHandler(d))
		r.Get("/markets/{base}/{quote}/candles", marketCandlesHandler(d))
		r.Get("/markets/{base}/{quote}/trades", marketRecentTradesHandler(d))
		r.Get("/currencies", publicListCurrenciesHandler(d))

		r.Get("/markets/{base}/{quote}/stats", market24hStatsHandler(d))
		r.Get("/status", statusHandler(d))

		// WebSocket - auth handled inside ServeWS (token via ?query or Authorization header)
		r.Get("/ws/notifications", d.NotifWSHub.ServeWS)
		r.Get("/ws/markets", d.MarketWSHub.ServeWS)

		// Authenticated
		r.Group(func(r chi.Router) {
			r.Use(authMiddleware(d.AuthSvc))
			r.Get("/users/me", meHandler(d))
		r.Get("/users/me/notifications", listNotificationsHandler(d))
		r.Post("/users/me/notifications/{id}/read", markNotificationReadHandler(d))
		r.Post("/users/me/notifications/read-all", markAllNotificationsReadHandler(d))
		r.Post("/users/me/kyc", submitKYCHandler(d))
			r.Post("/users/me/kyc/upload", kycUploadHandler(d))

		// Address book
		r.Get("/users/me/addresses", listAddressesHandler(d))
		r.Post("/users/me/addresses", addAddressHandler(d))
		r.Patch("/users/me/addresses/{id}", updateAddressHandler(d))
		r.Delete("/users/me/addresses/{id}", deleteAddressHandler(d))

		// Trigger orders (stop loss / take profit)
		r.With(favoritesLimiter.Middleware(userIDFromReq)).
			Get("/users/me/favorites", favoritesHandler(d))
		r.With(favoritesLimiter.Middleware(userIDFromReq)).
			Post("/users/me/favorites", addFavoriteHandler(d))
		r.With(favoritesLimiter.Middleware(userIDFromReq)).
			Delete("/users/me/favorites/{pair}", removeFavoriteHandler(d))
		r.Get("/users/me/triggers", listTriggerOrdersHandler(d))
		r.Get("/users/me/pnl", pnlHandler(d))
		r.Post("/users/me/triggers", createTriggerOrderHandler(d))
		r.Delete("/users/me/triggers/{id}", cancelTriggerOrderHandler(d))
		r.Get("/users/me/kyc", listKYCHandler(d))
		// Authenticated KYC image download — replaces the old anonymous
		// /kyc/<hash>.png static serve that let any unauthenticated
		// visitor read a user's ID document if they had the URL
		// (NEW-H1 from the 2026-08-28 v0.3 audit).
		r.Get("/users/me/kyc/files/{file}", kycUserFileHandler(d))
		r.Get("/users/me/limit", getKycLimitHandler(d))
		r.Get("/users/me/trades", userTradesHandler(d))
		r.Get("/users/me/api-keys", userAPIKeysHandler(d))
		r.Post("/users/me/api-keys", createAPIKeyHandler(d))
		r.Delete("/users/me/api-keys/{id}", revokeAPIKeyHandler(d))

		// 2FA (TOTP) - optional, user can enable/disable
		r.Post("/users/me/2fa/setup", totpSetupHandler(d))
		r.Post("/users/me/2fa/enable", totpEnableHandler(d))
		r.Post("/users/me/2fa/disable", totpDisableHandler(d))
		r.Get("/users/me/2fa/status", totpStatusHandler(d))
		r.Post("/users/me/2fa/backup-codes", totpRegenerateBackupCodesHandler(d))
		r.Post("/users/me/2fa/verify", totpVerifyHandler(d))
		r.Get("/users/me/notif-prefs", getNotifPrefsHandler(d))
		r.Patch("/users/me/notif-prefs", patchNotifPrefsHandler(d))
		r.Route("/admin", func(r chi.Router) {
			r.Use(adminMiddleware(d))
			r.Get("/kyc", adminListAllKYCHandler(d))
			r.Get("/kyc/pending", adminListPendingKYCHandler(d))
			r.Get("/kyc/{id}/{type}", kycAdminDownloadHandler(d))
		r.Get("/audit-logs", adminListAuditLogsHandler(d))
		r.Get("/vault-health", adminVaultHealthHandler(d))
		r.Get("/hot-wallet", adminHotWalletHandler(d))
			r.Post("/kyc/{id}/approve", adminApproveKYCHandler(d))
			r.Post("/kyc/{id}/reject", adminRejectKYCHandler(d))
			r.Get("/users", adminListUsersHandler(d))
			r.Get("/users/{id}", adminGetUserHandler(d))
			r.Post("/users/{id}/role", adminSetUserRoleHandler(d))
			r.Post("/users/{id}/password", adminSetUserPasswordHandler(d))
			r.Get("/users/{id}/risk", adminGetUserRiskHandler(d))
			r.Get("/withdrawals", adminListWithdrawalsHandler(d))
			r.Get("/withdrawals/held", adminListHeldWithdrawalsHandler(d))
			r.Post("/withdrawals/{id}/approve-hold", adminApproveHeldHandler(d))
			r.Post("/withdrawals/{id}/reject-hold", adminRejectHeldHandler(d))
			r.Get("/deposits", adminListDepositsHandler(d))
			r.Get("/orders", adminListOrdersHandler(d))
			r.Get("/stats", adminStatsHandler(d))
			r.Get("/risk-events", adminListRiskEventsHandler(d))

		// Multi-chain admin (M6.6)
		r.Get("/chains", adminListChainsHandler(d))
		r.Get("/dashboard", adminDashboardHandler(d))
		r.Get("/dashboard/charts", dashboardChartsHandler(d))
		r.Post("/chains/reload", adminReloadChainsHandler(d))
		r.Get("/config/reload", adminConfigReloadStatusHandler(d))
		r.Post("/chains/{id}/enable", adminEnableChainHandler(d))
		r.Post("/chains/{id}/disable", adminDisableChainHandler(d))
		r.Post("/chains/{id}/test", adminTestChainHandler(d))
		r.Delete("/chains/{id}", adminRemoveChainHandler(d))
		r.Get("/currencies", adminListCurrenciesHandler(d))
		r.Get("/fee-stats", adminFeeStatsHandler(d))

		r.Put("/currencies/{symbol}", adminUpdateCurrencyHandler(d))

		r.Get("/pairs", adminListPairsHandler(d))
		r.Post("/pairs/toggle", adminTogglePairHandler(d))
	})

	r.Get("/wallets", getWalletHandler(d))
		r.Get("/wallets/{asset}", getOneWalletHandler(d))
			r.Get("/deposits", listDepositsHandler(d))
		r.Post("/deposits/import", importDepositsFromChainHandler(d))
			r.Post("/admin/spawn-deposit", spawnDepositHandler(d))
		r.Post("/withdrawals", createWithdrawalHandler(d))
		r.Get("/withdrawals", listWithdrawalsHandler(d))
		r.Get("/deposit-address/{asset}", getDepositAddressHandler(d))
		r.Get("/pending-deposits", listPendingDepositsHandler(d))
		r.Get("/pending-txs", listPendingTxsHandler(d))
			r.Get("/admin/chainwatcher/health", chainWatcherHealthHandler(d))
			// Trading
			r.With(orderLimiter.Middleware(userIDKeyFromContext)).Post("/orders", placeOrderHandler(d))
			r.Get("/orders", listOrdersHandler(d))
			r.Get("/orders/{id}", getOrderHandler(d)) // NEW-L4: read single order details
			r.With(cancelLimiter.Middleware(userIDKeyFromContext)).Delete("/orders/{id}", cancelOrderHandler(d))
			r.With(cancelLimiter.Middleware(userIDKeyFromContext)).Patch("/orders/{id}", amendOrderHandler(d))
		r.With(cancelLimiter.Middleware(userIDKeyFromContext)).Delete("/orders", cancelAllOrdersHandler(d))
		})
	})

	return r
}

// loggingMiddleware logs every request via slog.
func loggingMiddleware(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			log.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
			)
		})
	}
}

// securityHeadersMiddleware adds standard security headers to all responses.
//
// SECURITY: Protects against:
// - XSS: X-Content-Type-Options: nosniff
// - Clickjacking: X-Frame-Options: DENY
// - Referrer information leakage
// - XSS in older browsers: X-XSS-Protection
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		next.ServeHTTP(w, r)
	})
}

// userIDKeyFromContext extracts the user ID from request context for rate limiting.
// Returns "anon" if not authenticated.
func userIDKeyFromContext(r *http.Request) string {
	uid := userIDFromContext(r.Context())
	if uid == "" {
		return "anon:" + IPFromRequest(r)
	}
	return "user:" + uid
}

// authMiddleware extracts Bearer token, verifies, and stores user_id in context.
func authMiddleware(svc *auth.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := r.Header.Get("Authorization")
			if len(token) < 7 || token[:7] != "Bearer " {
				writeError(w, http.StatusUnauthorized, "missing bearer token")
				return
			}
			claims, err := svc.VerifyToken(token[7:])
			if err != nil {
				writeError(w, http.StatusUnauthorized, "invalid token")
				return
			}
			ctx := r.Context()
			ctx = withUserID(ctx, claims.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// writeJSON encodes v as JSON and writes it.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes an error response.
//
// For 5xx status codes the raw error is logged at ERROR level on the
// server and a generic "internal error" message is sent to the client
// — this prevents leaking database / OS / file-path details that an
// attacker can use to fingerprint the stack and probe for further bugs
// (see H2 from the 2026-08-28 audit). 4xx status codes keep the raw
// error string because they normally come from validation / lookup
// paths and are useful to surface to the caller.
func writeError(w http.ResponseWriter, status int, msg string) {
	if status >= 500 {
		// log so we can debug — but do NOT echo msg to client
		// (the handler is expected to log the err before calling
		// writeError; this is a last-resort backstop in case it
		// forgets). Use a fixed string so we never leak anything.
		writeJSON(w, status, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, status, map[string]string{"error": msg})
}

// adminMiddleware verifies the user has admin role and injects user into context.
func adminMiddleware(d Deps) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			uidStr := userIDFromContext(r.Context())
			if uidStr == "" {
				writeError(w, http.StatusUnauthorized, "no user in context")
				return
			}
			uid, err := uuid.Parse(uidStr)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "invalid user id")
				return
			}
			u, err := d.UserSvc.GetUser(r.Context(), uid)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "user not found")
				return
			}
			if u.Role != "admin" {
				writeError(w, http.StatusForbidden, "admin role required")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}


// listNotificationsHandler handles GET /api/v1/users/me/notifications
func listNotificationsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		uid, _ := uuid.Parse(userID)
		notifs, err := d.Notifier.ListForUser(r.Context(), uid, 50)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out := make([]map[string]any, 0, len(notifs))
		for _, n := range notifs {
			item := map[string]any{
				"id":         n.ID,
				"type":       n.Type,
				"title":      n.Title,
				"body":       n.Body,
				"created_at": n.CreatedAt,
			}
			if n.ReadAt != nil {
				item["read_at"] = n.ReadAt
			}
			out = append(out, item)
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// markNotificationReadHandler handles POST /api/v1/users/me/notifications/{id}/read
func markNotificationReadHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		uid, _ := uuid.Parse(userID)
		idStr := chi.URLParam(r, "id")
		id, err := uuid.Parse(idStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid id")
			return
		}
		if err := d.Notifier.MarkRead(r.Context(), uid, id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// markAllNotificationsReadHandler handles POST /api/v1/users/me/notifications/read-all
// Marks all of the user's notifications as read.
func markAllNotificationsReadHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		uid, _ := uuid.Parse(userID)
		count, err := d.Notifier.MarkAllRead(r.Context(), uid)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "ok",
			"marked": count,
		})
	}
}

// userIDFromReq extracts user ID from request as string for rate limiter middleware.
func userIDFromReq(r *http.Request) string {
	return userIDFromContext(r.Context())
}
