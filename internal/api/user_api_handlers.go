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

// =========================================================================
// Error response shape
//
// We reuse the internal writeError helper so the body shape
// stays identical to /api/v1. Internal 5xx responses are mapped
// to a generic "internal error" body so pgx error text never
// leaks to the public surface (same hardening as the v0.2 /
// v0.3 NEW-H2 audit items).
// =========================================================================
