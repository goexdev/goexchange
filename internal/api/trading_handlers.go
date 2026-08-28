package api

import (
	"github.com/goexdev/goexchange/internal/marketdata"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/goexdev/goexchange/internal/audit"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/goexdev/goexchange/internal/matching"
	"github.com/goexdev/goexchange/internal/trading"
)

// placeOrderHandler handles POST /api/v1/orders (authenticated).
//
// Body: { pair, side, price, quantity }
func placeOrderHandler(d Deps) http.HandlerFunc {
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

		var in struct {
			Pair     string `json:"pair"`
			Side     string `json:"side"`
			Type     string `json:"type"` // "LIMIT" (default) or "MARKET"
			Price    string `json:"price"`
			Quantity string `json:"quantity"`
			STPMode  string `json:"stp_mode"` // self-trade prevention
		}
		if err := decodeJSON(r, &in); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}


		// SECURITY: Validate side. Accept mixed case so the UI's natural
		// "Buy"/"Sell" labels don't trigger 400s (M7 from the 2026-08-28 audit).
		// We normalise to uppercase before downstream use.
		in.Side = strings.ToUpper(in.Side)
			if in.Side != "BUY" && in.Side != "SELL" {
				writeError(w, http.StatusBadRequest, "side must be BUY or SELL")
				return
			}

		// Determine order type
		orderType := trading.OrderTypeLimit
		if in.Type == "MARKET" {
			orderType = trading.OrderTypeMarket
		}


		// Parse price only for limit orders
		var price decimal.Decimal
		if orderType == trading.OrderTypeLimit {
			var err error
			price, err = decimal.NewFromString(in.Price)
			if err != nil {
				writeError(w, http.StatusBadRequest, "price must be a decimal string")
				return
			}
			// SECURITY: Validate price is positive
			if price.LessThanOrEqual(decimal.Zero) {
				writeError(w, http.StatusBadRequest, "price must be > 0")
				return
			}
			// SECURITY: Cap max price at 1 billion to prevent overflow attacks
			if price.GreaterThan(decimal.NewFromInt(1000000000)) {
				writeError(w, http.StatusBadRequest, "price exceeds maximum")
				return
			}
		}

		quantity, err := decimal.NewFromString(in.Quantity)
		if err != nil {
			writeError(w, http.StatusBadRequest, "quantity must be a decimal string")
			return
		}
		// SECURITY: Validate quantity is positive
		if quantity.LessThanOrEqual(decimal.Zero) {
			writeError(w, http.StatusBadRequest, "quantity must be > 0")
			return
		}
		// SECURITY: Cap max quantity at 1 million to prevent DoS
		if quantity.GreaterThan(decimal.NewFromInt(1000000)) {
			writeError(w, http.StatusBadRequest, "quantity exceeds maximum")
			return
		}

		stpMode := trading.STPRejectTaker
		if in.STPMode == "CANCEL_MAKER" {
			stpMode = trading.STPCancelMaker  // typo - should be STPCancelMaker
		}
		result, err := d.TradingSvc.PlaceOrder(r.Context(), trading.PlaceOrderInput{
			UserID:   uid,
			Pair:     in.Pair,
			Side:     in.Side,
			Type:     string(orderType),
			Price:    price,
			Quantity: quantity,
			STPMode:  stpMode,
		})
		if err != nil {
			switch {
			case errors.Is(err, trading.ErrUnknownPair):
				writeError(w, http.StatusBadRequest, "unknown trading pair")
			case errors.Is(err, trading.ErrInvalidSide),
				errors.Is(err, trading.ErrInvalidType),
				errors.Is(err, trading.ErrInvalidPrice),
				errors.Is(err, trading.ErrInvalidQty):
				writeError(w, http.StatusBadRequest, err.Error())
			case errors.Is(err, trading.ErrInsufficient):
				writeError(w, http.StatusBadRequest, "insufficient balance")
			default:
				d.Log.Error("place order failed", "error", err, "user_id", uid)
				writeError(w, http.StatusInternalServerError, "internal error")
			}
			return
		}

		writeJSON(w, http.StatusCreated, result)
	}
}

// auditCancel logs a cancel order audit entry.
// SECURITY: Records every cancel attempt (success or failure) with user ID,
// order ID, pair, and error info for forensics.
func auditCancel(d Deps, r *http.Request, userID uuid.UUID, orderID *uuid.UUID, pair string, success bool, err error) {
	if d.AuditSvc == nil {
		return
	}
	entry := audit.LogEntry{
		Action:      "trading.cancel_order",
		TargetType:  "order",
		TargetID:    orderID,
		TargetLabel: pair,
		Details: map[string]any{
			"user_id": userID,
			"pair":    pair,
		},
	}
	if success {
		entry.Status = "success"
	} else {
		entry.Status = "failure"
		if err != nil {
			entry.ErrorMsg = err.Error()
		}
	}
	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.RemoteAddr
	}
	if idx := strings.LastIndex(ip, ":"); idx > 0 {
		if ip[0] == '[' {
			ip = ip[1:idx-1]
		} else {
			ip = ip[:idx]
		}
	}
	entry.IP = ip
	entry.UserAgent = r.Header.Get("User-Agent")
	d.AuditSvc.Log(r.Context(), entry)
}

// auditCancelAll logs a cancel-all audit entry.
func auditCancelAll(d Deps, r *http.Request, userID uuid.UUID, pair string, cancelled int, success bool, err error) {
	if d.AuditSvc == nil {
		return
	}
	entry := audit.LogEntry{
		Action:      "trading.cancel_all",
		TargetType:  "user_orders",
		TargetLabel: pair,
		Details: map[string]any{
			"user_id":  userID,
			"pair":     pair,
			"count":    cancelled,
		},
	}
	if success {
		entry.Status = "success"
	} else {
		entry.Status = "failure"
		if err != nil {
			entry.ErrorMsg = err.Error()
		}
	}
	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.RemoteAddr
	}
	if idx := strings.LastIndex(ip, ":"); idx > 0 {
		if ip[0] == '[' {
			ip = ip[1:idx-1]
		} else {
			ip = ip[:idx]
		}
	}
	entry.IP = ip
	entry.UserAgent = r.Header.Get("User-Agent")
	d.AuditSvc.Log(r.Context(), entry)
}

// cancelOrderHandler handles DELETE /api/v1/orders/{id} (authenticated).
//
// Path: /api/v1/orders/{id}
//
// The pair is *not* a required query param — the service derives it from
// the order row itself (H4 from the 2026-08-28 audit). We keep the
// query param as an optional override for legacy clients; if it does
// match the order's actual pair we ignore it (logging at WARN) so a
// stale value can't misroute the cancel.
func cancelOrderHandler(d Deps) http.HandlerFunc {
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

		orderIDStr := chi.URLParam(r, "id")
		orderID, err := uuid.Parse(orderIDStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid order id")
			return
		}

		// `pair` query param is accepted for backwards compatibility but
		// no longer required. If supplied, we keep it for the audit log.
		pairHint := r.URL.Query().Get("pair")

		err = d.TradingSvc.CancelOrder(r.Context(), orderID, uid)
		if err != nil {
			// SECURITY: Audit log all cancel attempts (success and failure)
			auditCancel(d, r, uid, &orderID, pairHint, false, err)
			switch {
			case errors.Is(err, trading.ErrOrderNotFound):
				writeError(w, http.StatusNotFound, "order not found")
			case errors.Is(err, trading.ErrNotOwner):
				writeError(w, http.StatusForbidden, "not the order owner")
			case errors.Is(err, trading.ErrAlreadyClosed):
				writeError(w, http.StatusConflict, "order already closed")
			default:
				d.Log.Error("cancel order failed", "error", err)
				writeError(w, http.StatusInternalServerError, "internal error")
			}
			return
		}

		// SECURITY: Audit log successful cancel
		auditCancel(d, r, uid, &orderID, pairHint, true, nil)

		writeJSON(w, http.StatusOK, map[string]string{"status": "CANCELLED", "order_id": orderID.String()})
	}
}

// listOrdersHandler handles GET /api/v1/orders (authenticated).
//
// Returns the user's recent orders (max 50). The response carries two
// counters so the SPA can show "your active orders" without a second
// round-trip (M6 from the 2026-08-28 audit):
//   count   — number of orders in the returned list
//   active  — number of those still OPEN or PARTIAL
//
// Both `pair` and `status` query params are supported.
func listOrdersHandler(d Deps) http.HandlerFunc {
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

		pair := r.URL.Query().Get("pair")
		status := r.URL.Query().Get("status")
		orders, err := d.TradingSvc.ListOrdersFiltered(r.Context(), uid, pair, status, 50)
		if err != nil {
			d.Log.Error("list orders failed", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		if orders == nil {
			orders = []*trading.OrderRecord{}
		}
		active := 0
		for _, o := range orders {
			if o.Status == matching.StatusOpen || o.Status == matching.StatusPartial {
				active++
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"orders": orders,
			"count":  len(orders),
			"active": active,
		})
	}
}

// listMarketsHandler handles GET /api/v1/markets.
//
// Query params:
//   enabled_only=true  -> only enabled pairs (for public trading UI)
//   enabled_only=false -> all pairs including disabled (for admin UI)
func listMarketsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		enabledOnly := r.URL.Query().Get("enabled_only") == "true"

		var pairs []*marketdata.Market
		if enabledOnly {
			pairs = d.MarketDataSvc.ListEnabledMarkets()
		} else {
			pairs = d.MarketDataSvc.ListMarkets()
		}

		out := make([]map[string]any, 0, len(pairs))
		for _, p := range pairs {
			out = append(out, map[string]any{
				"base":    p.Base,
				"quote":   p.Quote,
				"pair":    p.Pair,
				"enabled": p.Enabled,
			})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// adminTogglePairHandler handles POST /api/v1/admin/pairs/toggle
//
// Body: {"base":"BTC","quote":"USDT","enabled":true}
func adminTogglePairHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Base    string `json:"base"`
			Quote   string `json:"quote"`
			Enabled bool   `json:"enabled"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if req.Base == "" || req.Quote == "" {
			writeError(w, http.StatusBadRequest, "base and quote required")
			return
		}
		pair := req.Base + "_" + req.Quote
		action := "market.pair_disable"
		if req.Enabled {
			action = "market.pair_enable"
		}
		if !d.MarketDataSvc.SetPairEnabled(req.Base, req.Quote, req.Enabled) {
			auditLogAdmin(d, r, audit.LogEntry{
				Action:      action,
				TargetType:  "market_pair",
				TargetLabel: pair,
				Status:      "failure",
				ErrorMsg:     "pair not found",
			})
			writeError(w, http.StatusNotFound, "pair not found")
			return
		}
		auditLogAdmin(d, r, audit.LogEntry{
			Action:      action,
			TargetType:  "market_pair",
			TargetLabel: pair,
			Details: map[string]any{
				"base":    req.Base,
				"quote":   req.Quote,
				"enabled": req.Enabled,
			},
		})
		d.Log.Info("pair toggled", "base", req.Base, "quote", req.Quote, "enabled", req.Enabled)
		writeJSON(w, http.StatusOK, map[string]any{
			"base":    req.Base,
			"quote":   req.Quote,
			"enabled": req.Enabled,
		})
	}
}

// marketOrderBookHandler handles GET /api/v1/markets/{base}/{quote}/orderbook.
func marketOrderBookHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		base := chi.URLParam(r, "base")
		quote := chi.URLParam(r, "quote")
		if base == "" || quote == "" {
			writeError(w, http.StatusBadRequest, "missing base or quote")
			return
		}
		depth := 20 // default
		snap, _ := d.MarketDataSvc.GetOrderBook(r.Context(), strings.ToUpper(base), strings.ToUpper(quote), depth)
		writeJSON(w, http.StatusOK, snap)
	}
}

// marketTickerHandler handles GET /api/v1/markets/{base}/{quote}/ticker.
func marketTickerHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		base := chi.URLParam(r, "base")
		quote := chi.URLParam(r, "quote")
		if base == "" || quote == "" {
			writeError(w, http.StatusBadRequest, "missing base or quote")
			return
		}
		t, _ := d.MarketDataSvc.GetTicker(r.Context(), strings.ToUpper(base), strings.ToUpper(quote))
		writeJSON(w, http.StatusOK, t)
	}
}

// userTradesHandler handles GET /api/v1/users/me/trades
// Returns the recent trades for the authenticated user.
// Query params: pair (optional, e.g. "BTC_USDT"), limit (default 100)
func userTradesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		uid, _ := uuid.Parse(userID)
		if uid == uuid.Nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		pairFilter := r.URL.Query().Get("pair")
		limit := 100
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 500 {
				limit = n
			}
		}
		trades, err := d.TradingSvc.GetUserTrades(r.Context(), uid, pairFilter, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if trades == nil {
			trades = []trading.UserTrade{}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"trades": trades,
			"count": len(trades),
		})
	}
}

// cancelAllOrdersHandler handles DELETE /api/v1/orders
// Cancels all open orders for the authenticated user (optionally filtered by pair).
func cancelAllOrdersHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		uid, _ := uuid.Parse(userID)
		if uid == uuid.Nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		pairFilter := r.URL.Query().Get("pair")
		count, err := d.TradingSvc.CancelAllOrders(r.Context(), uid, pairFilter)
		if err != nil {
			auditCancelAll(d, r, uid, pairFilter, 0, false, err)
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		auditCancelAll(d, r, uid, pairFilter, count, true, nil)
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "ok",
			"cancelled": count,
		})
	}
}

// market24hStatsHandler handles GET /api/v1/markets/{base}/{quote}/stats
// Returns 24-hour market statistics (high, low, change, volume).
func market24hStatsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		base := chi.URLParam(r, "base")
		quote := chi.URLParam(r, "quote")
		if base == "" || quote == "" {
			writeError(w, http.StatusBadRequest, "missing base or quote")
			return
		}
		stats, err := d.TradingSvc.Get24hStats(r.Context(), strings.ToUpper(base), strings.ToUpper(quote))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, stats)
	}
}

// marketRecentTradesHandler handles GET /api/v1/markets/{base}/{quote}/trades
// Returns the most recent N trades for a pair (default 50).
func marketRecentTradesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		base := chi.URLParam(r, "base")
		quote := chi.URLParam(r, "quote")
		if base == "" || quote == "" {
			writeError(w, http.StatusBadRequest, "missing base or quote")
			return
		}
		limit := 50
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 200 {
				limit = n
			}
		}
		trades, err := d.TradingSvc.GetRecentTrades(r.Context(), strings.ToUpper(base), strings.ToUpper(quote), limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if trades == nil {
			trades = []trading.RecentTrade{}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"trades": trades,
			"pair":   strings.ToUpper(base) + "_" + strings.ToUpper(quote),
		})
	}
}

// marketCandlesHandler handles GET /api/v1/markets/{base}/{quote}/candles
// Query params:
//   interval: 60 (1m), 300 (5m), 900 (15m), 3600 (1h), 86400 (1d) - default 300
//   from, to: unix milliseconds (optional, default last 24h)
func marketCandlesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		base := chi.URLParam(r, "base")
		quote := chi.URLParam(r, "quote")
		if base == "" || quote == "" {
			writeError(w, http.StatusBadRequest, "missing base or quote")
			return
		}
		interval := 300 // default 5m
		if v := r.URL.Query().Get("interval"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				interval = n
			}
		}
		var fromMs, toMs int64
		if v := r.URL.Query().Get("from"); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				fromMs = n
			}
		}
		if v := r.URL.Query().Get("to"); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				toMs = n
			}
		}
		candles, err := d.TradingSvc.GetCandles(r.Context(), strings.ToUpper(base), strings.ToUpper(quote), interval, fromMs, toMs)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if candles == nil {
			candles = []trading.Candle{}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"candles":  candles,
			"interval": interval,
			"base":     strings.ToUpper(base),
			"quote":    strings.ToUpper(quote),
		})
	}
}

// ensure imports used
var (
	_ matching.Trade
)

// amendOrderHandler amends an existing open order (price and/or quantity).
//
// PATCH /api/v1/orders/{id}?pair=BTC_USDT
// Body: {"price": "29500", "quantity": "0.05"}
//
// Returns the updated order details. May trigger immediate matches if
// the new price crosses the spread.
func amendOrderHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userIDStr := userIDFromContext(r.Context())
		if userIDStr == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		userID, err := uuid.Parse(userIDStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid user id")
			return
		}

		orderID, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid order id")
			return
		}
		pair := strings.ToUpper(r.URL.Query().Get("pair"))
		if pair == "" {
			writeError(w, http.StatusBadRequest, "missing pair query param")
			return
		}

		var body struct {
			Price    string `json:"price"`
			Quantity string `json:"quantity"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid body")
			return
		}
		// BUG #10 fix: at least one of price/quantity must be supplied.
		// Empty string means "leave unchanged" for that field. This lets
		// clients amend just the price or just the quantity.
		if body.Price == "" && body.Quantity == "" {
			writeError(w, http.StatusBadRequest, "must supply price or quantity")
			return
		}
		// Parse whatever was supplied. Use decimal.Zero as the sentinel
		// meaning "leave unchanged" — the service layer checks IsZero().
		var price decimal.Decimal
		if body.Price != "" {
			var err error
			price, err = decimal.NewFromString(body.Price)
			if err != nil || price.IsNegative() {
				writeError(w, http.StatusBadRequest, "invalid price")
				return
			}
		}
		var quantity decimal.Decimal
		if body.Quantity != "" {
			var err error
			quantity, err = decimal.NewFromString(body.Quantity)
			if err != nil || quantity.IsNegative() {
				writeError(w, http.StatusBadRequest, "invalid quantity")
				return
			}
		}

		result, err := d.TradingSvc.AmendOrder(r.Context(), trading.AmendOrderInput{
			OrderID:  orderID,
			UserID:   userID,
			Pair:     pair,
			Price:    price,
			Quantity: quantity,
		})
		if err != nil {
			switch {
			case errors.Is(err, trading.ErrOrderNotFound):
				writeError(w, http.StatusNotFound, "order not found")
			case errors.Is(err, trading.ErrNotOwner):
				writeError(w, http.StatusForbidden, "not the order owner")
			case errors.Is(err, trading.ErrAlreadyClosed):
				writeError(w, http.StatusConflict, "order is already filled or cancelled")
			case errors.Is(err, trading.ErrInvalidQty), errors.Is(err, trading.ErrInvalidPrice):
				writeError(w, http.StatusBadRequest, err.Error())
			case errors.Is(err, trading.ErrInsufficient):
				writeError(w, http.StatusPaymentRequired, "insufficient balance")
			default:
				slog.Error("amend order failed", "error", err, "order_id", orderID)
				writeError(w, http.StatusInternalServerError, "amend failed")
			}
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"order_id":  result.OrderID,
			"status":    result.Status,
			"filled":    result.Filled.String(),
			"remaining": result.Remaining.String(),
		})
	}
}

// adminListPairsHandler handles GET /api/v1/admin/pairs.
func adminListPairsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pairs := d.MarketDataSvc.ListMarkets()
		writeJSON(w, http.StatusOK, pairs)
	}
}
