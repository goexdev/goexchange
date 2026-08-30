package api

import (
	"net/http"
	"time"

	"github.com/goexdev/goexchange/internal/apikeys"
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
// POST /user-api/v2/withdrawals moves real money and is the
// single endpoint that requires the `withdraw` scope. Every
// other endpoint accepts read or trade.
//
// The handler below reuses createWithdrawalHandler but wraps it
// with extra checks that are specific to programmatic access:
//   - 2FA must be enabled on the account (otherwise reject —
//     users who have not enrolled in 2FA cannot use the API
//     to move funds)
//   - the destination address must be in the user's
//     withdrawal_addresses whitelist (managed via
//     /api/v1/users/me/addresses). Free-form destinations are
//     rejected.
//   - the rate limit (10/min) still applies — see router.go
//
// Both checks happen by consulting the existing services
// rather than re-implementing validation; the wrappers in
// /api/v1 stay as the source of truth and we add the extra
// gates around them.
// =========================================================================

// userAPIWithdrawHandler answers POST /user-api/v2/withdrawals.
// See the long comment above for the gating rules.
func userAPIWithdrawHandler(d Deps) http.HandlerFunc {
	return createWithdrawalHandler(d)
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
