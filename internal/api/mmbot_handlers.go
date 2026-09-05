// Admin HTTP handlers for the per-pair market-making bot.
//
// Routes (registered in internal/api/router.go under /admin):
//
//	POST /admin/mmbot/start   {pair, mid_price, quote_seed, base_seed,
//	                          spread_bps?, treasury_wallet?,
//	                          min_quote_per_side?}
//	                          -> {bot: {...}}
//	POST /admin/mmbot/stop    {bot_id, return_inventory?}
//	                          -> {bot: {...}, returned_quote, returned_base}
//	GET  /admin/mmbot/status?bot_id=...
//	                          -> {bot: {...}}
//	GET  /admin/mmbot/list?pair=...&status=...
//	                          -> {bots: [{...}]}
//
// All handlers route through the gRPC Client in
// internal/mmbot. The handlers are intentionally thin: they
// translate JSON <-> the mmbot package types and surface
// errors as HTTP status codes. Validation lives in the bot
// engine (core); the admin handler just relays.
//
// Auth: handlers run under the /admin route group which uses
// adminMiddleware. Only callers with the admin role reach
// these endpoints.
package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/goexdev/goexchange/internal/audit"
	"github.com/goexdev/goexchange/internal/mmbot"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// mmbotGRPCStatus maps a gRPC error returned by the mm-bot engine
// to an HTTP status code. The mapping preserves the public
// contract admin tooling (and the React page) relies on:
//
//	NotFound -> 404 (admin clicked an old link or stale bot_id)
//	InvalidArgument -> 400 (start params out of range, etc.)
//	FailedPrecondition -> 409 (already RUNNING, partial unique
//	                     index conflict on pair, etc.)
//	Unavailable / Unknown -> 503 (engine down / decode error)
//
// Anything else falls back to 503 with the raw error string.
// Callers see a meaningful status code, not a generic 500.
func mmbotGRPCStatus(err error) (int, string) {
	if err == nil {
		return http.StatusOK, ""
	}
	// The mmbot package wraps with fmt.Errorf("%w", err) so the
	// gRPC status code is reachable via status.Code.
	s, ok := status.FromError(err)
	if !ok {
		// Not a gRPC error (e.g. local validation). Fall through.
		return http.StatusServiceUnavailable, err.Error()
	}
	switch s.Code() {
	case codes.NotFound:
		return http.StatusNotFound, s.Message()
	case codes.InvalidArgument:
		return http.StatusBadRequest, s.Message()
	case codes.FailedPrecondition:
		return http.StatusConflict, s.Message()
	case codes.Unavailable, codes.Internal, codes.Unknown:
		return http.StatusServiceUnavailable, s.Message()
	default:
		return http.StatusServiceUnavailable, s.Message()
	}
}

// guard that keeps linter happy when this file is built without
// the errors package being used elsewhere in the function set.
var _ = errors.Is

// startMMBotHandler: POST /admin/mmbot/start
func startMMBotHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Pair            string `json:"pair"`
			MidPrice        string `json:"mid_price"`
			QuoteSeed       string `json:"quote_seed"`
			BaseSeed        string `json:"base_seed"`
			SpreadBps       int32  `json:"spread_bps,omitempty"`
			TreasuryWallet  string `json:"treasury_wallet,omitempty"`
			MinQuotePerSide string `json:"min_quote_per_side,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if req.Pair == "" || req.MidPrice == "" || req.QuoteSeed == "" || req.BaseSeed == "" {
			writeError(w, http.StatusBadRequest, "pair, mid_price, quote_seed, base_seed are required")
			return
		}
		params := mmbot.StartParams{
			Pair:            req.Pair,
			MidPrice:        req.MidPrice,
			QuoteSeed:       req.QuoteSeed,
			BaseSeed:        req.BaseSeed,
			SpreadBps:       req.SpreadBps,
			TreasuryWallet:  req.TreasuryWallet,
			MinQuotePerSide: req.MinQuotePerSide,
		}
		state, err := d.MMBotClient.Start(r.Context(), params)
		if err != nil {
			auditLogAdmin(d, r, audit.LogEntry{
				Action:      "mmbot.start",
				TargetType:  "mmbot",
				TargetLabel: req.Pair,
				Status:      "failure",
				ErrorMsg:    err.Error(),
			})
			httpStatus, httpMsg := mmbotGRPCStatus(err); writeError(w, httpStatus, httpMsg)
			return
		}
		auditLogAdmin(d, r, audit.LogEntry{
			Action:      "mmbot.start",
			TargetType:  "mmbot",
			TargetLabel: state.BotID,
			Details: map[string]any{
				"pair":       req.Pair,
				"bot_id":     state.BotID,
				"mid_price":  req.MidPrice,
				"spread_bps": req.SpreadBps,
			},
		})
		d.Log.Info("mmbot started", "bot_id", state.BotID, "pair", state.Pair, "mid", state.MidPrice)
		writeJSON(w, http.StatusOK, map[string]any{"bot": stateToJSON(state)})
	}
}

// stopMMBotHandler: POST /admin/mmbot/stop
func stopMMBotHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			BotID           string `json:"bot_id"`
			ReturnInventory *bool  `json:"return_inventory,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if req.BotID == "" {
			writeError(w, http.StatusBadRequest, "bot_id is required")
			return
		}
		// Default: return inventory to treasury on stop (the
		// "stop = liquidate" semantics). Caller can pass
		// return_inventory=false to pause without moving funds.
		ret := true
		if req.ReturnInventory != nil {
			ret = *req.ReturnInventory
		}
		res, err := d.MMBotClient.Stop(r.Context(), req.BotID, ret)
		if err != nil {
			auditLogAdmin(d, r, audit.LogEntry{
				Action:      "mmbot.stop",
				TargetType:  "mmbot",
				TargetLabel: req.BotID,
				Status:      "failure",
				ErrorMsg:    err.Error(),
			})
			httpStatus, httpMsg := mmbotGRPCStatus(err); writeError(w, httpStatus, httpMsg)
			return
		}
		auditLogAdmin(d, r, audit.LogEntry{
			Action:      "mmbot.stop",
			TargetType:  "mmbot",
			TargetLabel: req.BotID,
			Details: map[string]any{
				"return_inventory": ret,
				"returned_quote":   res.ReturnedQuote,
				"returned_base":    res.ReturnedBase,
				"final_pnl_quote":  res.Bot.PnlQuote,
			},
		})
		d.Log.Info("mmbot stopped", "bot_id", req.BotID, "return_inventory", ret,
			"returned_quote", res.ReturnedQuote, "returned_base", res.ReturnedBase,
			"pnl_quote", res.Bot.PnlQuote)
		writeJSON(w, http.StatusOK, map[string]any{
			"bot":            stateToJSON(res.Bot),
			"returned_quote": res.ReturnedQuote,
			"returned_base":  res.ReturnedBase,
		})
	}
}

// mmbotStatusHandler: GET /admin/mmbot/status?bot_id=...
func mmbotStatusHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		botID := r.URL.Query().Get("bot_id")
		if botID == "" {
			writeError(w, http.StatusBadRequest, "bot_id query param required")
			return
		}
		state, err := d.MMBotClient.Status(r.Context(), botID)
		if err != nil {
			httpStatus, httpMsg := mmbotGRPCStatus(err); writeError(w, httpStatus, httpMsg)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"bot": stateToJSON(state)})
	}
}

// mmbotListHandler: GET /admin/mmbot/list?pair=...&status=...
//
// Filters are both optional. Empty pair filter returns bots
// across all pairs; empty status filter returns all statuses.
// status is the proto enum name (e.g. "RUNNING"), not an int.
func mmbotListHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pairFilter := r.URL.Query().Get("pair")
		var statusFilter mmbot.Status
		if s := r.URL.Query().Get("status"); s != "" {
			statusFilter = mmbot.Status(s)
		}
		states, err := d.MMBotClient.List(r.Context(), pairFilter, statusFilter)
		if err != nil {
			httpStatus, httpMsg := mmbotGRPCStatus(err); writeError(w, httpStatus, httpMsg)
			return
		}
		bots := make([]map[string]any, 0, len(states))
		for _, s := range states {
			bots = append(bots, stateToJSON(s))
		}
		writeJSON(w, http.StatusOK, map[string]any{"bots": bots})
	}
}

// stateToJSON converts an internal mmbot.BotState into the
// public JSON shape admin dashboards consume. Decimal strings
// (mid_price, balances, pnl_quote) pass through unchanged; we
// never round or parse them here.
func stateToJSON(s mmbot.BotState) map[string]any {
	out := map[string]any{
		"bot_id":         s.BotID,
		"pair":           s.Pair,
		"status":         string(s.Status),
		"mid_price":      s.MidPrice,
		"spread_bps":     s.SpreadBps,
		"base_balance":   s.BaseBalance,
		"quote_balance":  s.QuoteBalance,
		"open_order_ids": s.OpenOrderIDs,
		"pnl_quote":      s.PnlQuote,
		"created_at":     s.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		"last_error":     s.LastError,
	}
	if s.StartedAt != nil {
		out["started_at"] = s.StartedAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	if s.StoppedAt != nil {
		out["stopped_at"] = s.StoppedAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	return out
}
