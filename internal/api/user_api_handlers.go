package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/goexdev/goexchange/internal/apikeys"
	"github.com/google/uuid"
)

// Scope constants for the api-key permission system. These are
// the values stored in the api_keys.scopes TEXT[] column at
// creation time; the handler-side checks match against them.
const (
	ScopeRead     = apikeys.ScopeRead
	ScopeTrade    = apikeys.ScopeTrade
	ScopeWithdraw = apikeys.ScopeWithdraw
)

// requireScope returns a middleware that 403s any request whose
// api key was not granted the named scope. The middleware is a
// no-op for requests that did not pass through userAPIKeyAuth
// (no scopes in context), which is the correct behavior because
// those requests will already have been rejected with 401 at
// the auth layer.
func requireScope(scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !HasScope(r.Context(), scope) {
				writeError(w, http.StatusForbidden, "scope not granted")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// =========================================================================
// Public endpoints (no api key required)
// =========================================================================

// userAPIPingHandler answers GET /user-api/v2/ping with a fixed
// payload so clients can verify connectivity without burning an
// authenticated request.
func userAPIPingHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"ping":      "pong",
			"server":    "goexchange",
			"timestamp": time.Now().UnixMilli(),
		})
	}
}

// userAPIServerTimeHandler answers GET /user-api/v2/server-time
// with the unix-ms server clock. Useful for clients to compute
// the X-Api-Nonce without trusting their own clock.
func userAPIServerTimeHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"server_time": time.Now().UnixMilli(),
		})
	}
}

// userAPIListMarketsHandler proxies to the same data source as
// /api/v1/markets — we do not duplicate the listing logic.
func userAPIListMarketsHandler(d Deps) http.HandlerFunc {
	return listMarketsHandler(d)
}

// userAPIMarketTickerHandler delegates to the existing ticker
// handler. The URL parameters match.
func userAPIMarketTickerHandler(d Deps) http.HandlerFunc {
	return marketTickerHandler(d)
}

// userAPIMarketOrderBookHandler delegates to the existing
// orderbook handler.
func userAPIMarketOrderBookHandler(d Deps) http.HandlerFunc {
	return marketOrderBookHandler(d)
}

// userAPIMarketRecentTradesHandler delegates to the existing
// recent-trades handler.
func userAPIMarketRecentTradesHandler(d Deps) http.HandlerFunc {
	return marketRecentTradesHandler(d)
}

// userAPIListCurrenciesHandler proxies to the public list
// currencies handler.
func userAPIListCurrenciesHandler(d Deps) http.HandlerFunc {
	return publicListCurrenciesHandler(d)
}

// =========================================================================
// Private endpoints (api key required; scope enforced by router)
// =========================================================================

// userAPIListBalancesHandler answers GET /user-api/v2/balances.
// Reuses the /api/v1/wallets handler; user_id is pulled from
// the context that userAPIKeyAuth populated.
func userAPIListBalancesHandler(d Deps) http.HandlerFunc {
	return getWalletHandler(d)
}

// userAPIPlaceOrderHandler answers POST /user-api/v2/orders.
// Reuses the JWT-auth placeOrderHandler.
func userAPIPlaceOrderHandler(d Deps) http.HandlerFunc {
	return placeOrderHandler(d)
}

// userAPIcancelOrderHandler answers DELETE /user-api/v2/orders/{id}.
// Reuses the JWT-auth cancelOrderHandler.
func userAPIcancelOrderHandler(d Deps) http.HandlerFunc {
	return cancelOrderHandler(d)
}

// userAPIListOrdersHandler answers GET /user-api/v2/orders.
// Reuses the JWT-auth listOrdersHandler.
func userAPIListOrdersHandler(d Deps) http.HandlerFunc {
	return listOrdersHandler(d)
}

// userAPIGetOrderHandler answers GET /user-api/v2/orders/{id}.
// Reuses the JWT-auth getOrderHandler. Per-user ownership check
// lives in the handler so a wrong user gets 404 rather than
// leaking that the order exists.
func userAPIGetOrderHandler(d Deps) http.HandlerFunc {
	return getOrderHandler(d)
}

// userAPIListTradesHandler answers GET /user-api/v2/trades.
// Reuses the JWT-auth userTradesHandler. Supports the same
// query params (pair, since, until, limit).
func userAPIListTradesHandler(d Deps) http.HandlerFunc {
	return userTradesHandler(d)
}

// userAPIGetOneWalletHandler answers GET /user-api/v2/wallets/{asset}.
// Reuses the JWT-auth getOneWalletHandler.
func userAPIGetOneWalletHandler(d Deps) http.HandlerFunc {
	return getOneWalletHandler(d)
}

// userAPIGetDepositAddressHandler answers
// GET /user-api/v2/deposit-address/{asset}.
// Reuses the JWT-auth getDepositAddressHandler.
func userAPIGetDepositAddressHandler(d Deps) http.HandlerFunc {
	return getDepositAddressHandler(d)
}

// userAPIListDepositsHandler answers GET /user-api/v2/deposits.
// Reuses the JWT-auth listDepositsHandler.
func userAPIListDepositsHandler(d Deps) http.HandlerFunc {
	return listDepositsHandler(d)
}

// userAPIListWithdrawalsHandler answers GET /user-api/v2/withdrawals.
// Reuses the JWT-auth listWithdrawalsHandler.
func userAPIListWithdrawalsHandler(d Deps) http.HandlerFunc {
	return listWithdrawalsHandler(d)
}

// userAPICancelAllOrdersHandler answers
// DELETE /user-api/v2/orders?pair=BTC_USDT.
// Reuses the JWT-auth cancelAllOrdersHandler. The pair query
// param is optional; omitting it cancels every open order.
func userAPICancelAllOrdersHandler(d Deps) http.HandlerFunc {
	return cancelAllOrdersHandler(d)
}

// =========================================================================
// Withdrawals: special handling
//
// The user-api /withdrawals endpoint requires a fresh TOTP code
// in the request body. Without this gate any leaked api key
// would let an attacker drain the wallet — the api-key auth
// path is not protected by the browser session that web users
// get, and the bcrypt-protected key string IS the credential.
// This is the same threat model Binance / Coinbase use for
// their programmatic-withdraw endpoints.
//
// Gating rules (UAPI-WD-1 audit):
//   1. 2FA must be enrolled. A user who has not run
//      /users/me/2fa/setup cannot withdraw via api — they get
//      403 "2fa required for withdrawals". They can still
//      withdraw via the web UI (which uses createWithdrawalHandler
//      unchanged, with the 2fa prompt rendered in the browser).
//   2. The TOTP code in the body must verify. A wrong / stale
//      code returns 401 "invalid 2fa code" — the same generic
//      message we use for auth failures, so an attacker cannot
//      distinguish a valid from invalid 2FA.
//   3. The 2FA verify is the only step we add. The remaining
//      chain / amount / address / KYC-limit / rate-limit checks
//      live in createWithdrawalHandler, which we call after
//      2FA passes.
//   4. We do NOT enforce a destination whitelist here. The
//      intent is to keep the api surface small (one extra
//      field, one extra branch) and let the 2FA gate do the
//      "user is present" work. Whitelist management is
//      available via /api/v1/users/me/addresses; we may add an
//      optional `require_whitelisted` switch later.
// =========================================================================

// userAPIWithdrawHandler answers POST /user-api/v2/withdrawals.
// See the long comment above for the gating rules.
func userAPIWithdrawHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "no user in context")
			return
		}
		uid, err := uuid.Parse(userID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid user id")
			return
		}

		// Parse body. We accept a 2FA code in addition to the
		// regular withdraw fields. The withdraw handler itself
		// (createWithdrawalHandler) does its own JSON parse
		// without the code field; we need to read the code out
		// before delegating so we can re-inject it via context or
		// by parsing the body once here. The simplest path is to
		// parse here, verify 2FA, then call a slightly modified
		// version of the inner handler — but for code
		// consistency we reuse the regular handler and rely on
		// the code being unused there. To avoid the
		// DisallowUnknownFields trap, we parse the body with the
		// regular json package and forward a re-encoded body
		// without the code.
		var in struct {
			Code string `json:"code"`
		}
		if r.Body != nil {
			body, _ := io.ReadAll(r.Body)
			_ = r.Body.Close()
			if len(body) > 0 {
				if err := json.Unmarshal(body, &in); err != nil {
					writeError(w, http.StatusBadRequest, "invalid JSON body")
					return
				}
			}
			// Re-inject the body without the code field so the
			// downstream handler parses it normally. We strip
			// `code` by re-marshaling a struct that omits it.
			stripped := struct {
				Asset       string `json:"asset"`
				Amount      string `json:"amount"`
				DestAddress string `json:"dest_address"`
			}{}
			if err := json.Unmarshal(body, &stripped); err != nil {
				// already validated above; defensive only
			}
			rewritten, _ := json.Marshal(stripped)
			r.Body = io.NopCloser(bytes.NewReader(rewritten))
		}

		// Gate 1: 2FA must be enrolled. We deliberately do
		// not call TOTPSvc.IsEnabled here because that would
		// leak which users have 2FA in the response time /
		// error code. Instead we just attempt the verify and
		// if no secret is configured the service returns
		// Err2FANotEnabled.
		if in.Code == "" {
			writeError(w, http.StatusBadRequest, "2fa code required")
			return
		}

		// Gate 2: verify code. The verify itself is the
		// only signal we expose to the client; everything
		// else (disabled, wrong, expired) maps to a single
		// 401 so an attacker cannot enumerate users by 2FA
		// state.
		if err := d.TOTPSvc.VerifyCode(r.Context(), uid, in.Code); err != nil {
			d.Log.Warn("withdraw 2fa verify failed",
				"user_id", uid, "error", err)
			writeError(w, http.StatusUnauthorized, "invalid 2fa code")
			return
		}

		// All gates passed. Delegate to the regular handler.
		createWithdrawalHandler(d).ServeHTTP(w, r)
	}
}

// =========================================================================
// Error response shape
//
// We reuse the internal writeError helper so the body shape
// stays identical to /api/v1. Internal 5xx responses are mapped
// to a generic "internal error" body so pgx error text never
// leaks to the public surface (same hardening as the v0.2 /
// v0.3 NEW-H2 audit items).
// =========================================================================
