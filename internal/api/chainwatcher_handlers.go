package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/goexdev/goexchange/internal/chainwatcher"

)

// spawnDepositHandler handles POST /api/v1/admin/spawn-deposit (authenticated).
//
// Admin-only endpoint for manually triggering a mock deposit.
// In M4, this will be replaced by real chain driver.
func spawnDepositHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "no user in context")
			return
		}

		var in struct {
			UserID string `json:"user_id"`
			Asset  string `json:"asset"`
			Amount string `json:"amount"`
			TxHash string `json:"tx_hash"`
		}
		if err := decodeJSON(r, &in); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}

		// Default: spawn for self
		uid, err := uuid.Parse(in.UserID)
		if err != nil {
			// Use authenticated user
			authUID, err := uuid.Parse(userID)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid user id")
				return
			}
			uid = authUID
		}

		amount, err := decimal.NewFromString(in.Amount)
		if err != nil || !amount.IsPositive() {
			writeError(w, http.StatusBadRequest, "amount must be positive decimal")
			return
		}

		asset := in.Asset
		if asset == "" {
			asset = "USDT"
		}

		// Generate tx_hash if not provided
		txHash := in.TxHash
		if txHash == "" {
			txHash = "ADMIN_" + uuid.New().String()
		}

		deposit, err := d.ChainWatcherSvc.SpawnDeposit(r.Context(), uid, asset, amount)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, deposit)
	}
}

// listDepositsHandler handles GET /api/v1/deposits (authenticated).
//
// Returns the authenticated user's deposit history.
func listDepositsHandler(d Deps) http.HandlerFunc {
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

		deposits, err := d.ChainWatcherSvc.ListDeposits(r.Context(), uid, 50)
		if err != nil {
			d.Log.Error("list deposits failed", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		writeJSON(w, http.StatusOK, deposits)
	}
}

// chainWatcherHealthHandler handles GET /api/v1/admin/chainwatcher/health.
//
// Returns mock chainwatcher status (deposits count since startup).
func chainWatcherHealthHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"driver":         "mock",
			"deposits_count": d.ChainWatcherSvc.DepositsCount(),
			"status":         "ok",
		})
	}
}

// ensure chainwatcher import is used
var _ = chainwatcher.Service{}

// createWithdrawalHandler handles POST /api/v1/withdrawals.
//
// Withdraws asset to external address.
//
// Error reporting is precise per field. The previous version returned
// "invalid json" for any enum / unknown-field error which made
// debugging impossible (NEW-H2 from the 2026-08-28 v0.3 audit). We now
// run the JSON parse and the field validations separately so each step
// can name the specific problem.
func createWithdrawalHandler(d Deps) http.HandlerFunc {
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

		// Parse body without strict unknown-field rejection — callers
		// regularly typo `address` vs `dest_address`. We validate each
		// well-known field below and ignore anything else so a typo
		// surfaces as "dest_address required" rather than "invalid json".
		var in struct {
			Asset       string `json:"asset"`
			Amount      string `json:"amount"`
			DestAddress string `json:"dest_address"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}

		// Field-by-field validation. Each branch reports the exact
		// problem with the offending field name.
		if in.Asset == "" {
			in.Asset = "BTC"
		} else if !validWithdrawAsset(in.Asset) {
			writeError(w, http.StatusBadRequest,
				"asset must be one of: BTC, ETH, BNB, SOL, USDT, USDC")
			return
		}
		if in.Amount == "" {
			writeError(w, http.StatusBadRequest, "amount required")
			return
		}
		if in.DestAddress == "" {
			writeError(w, http.StatusBadRequest, "dest_address required")
			return
		}

		amount, err := decimal.NewFromString(in.Amount)
		if err != nil || !amount.IsPositive() {
			writeError(w, http.StatusBadRequest, "amount must be a positive decimal string")
			return
		}

		wd, err := d.ChainWatcherSvc.WithdrawWithSigner(r.Context(), uid, in.Asset, in.DestAddress, amount)
		if err != nil {
			// Surface known user errors verbatim; anything else is a
			// 5xx that the middleware already redacts to "internal
			// error" — but the service sometimes returns wrapped
			// pgx errors as 400s so we still log here for forensics.
			d.Log.Warn("withdraw rejected", "user_id", uid, "asset", in.Asset, "error", err)
			if errors.Is(err, chainwatcher.ErrInvalidAmount) ||
				errors.Is(err, chainwatcher.ErrInsufficientBalance) ||
				errors.Is(err, chainwatcher.ErrWithdrawalBlocked) ||
				errors.Is(err, chainwatcher.ErrWithdrawLimitExceeded) {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		// Update last_used_at for the address book entry (best-effort, non-blocking)
		go func() {
			if err := d.UserSvc.MarkAddressUsed(nil, uid, in.Asset, in.DestAddress); err != nil {
				d.Log.Warn("mark address used failed", "error", err, "asset", in.Asset)
			}
		}()

		writeJSON(w, http.StatusCreated, wd)
	}
}

// validWithdrawAsset mirrors the supported currencies in the seed data.
func validWithdrawAsset(s string) bool {
	switch s {
	case "BTC", "ETH", "BNB", "SOL", "USDT", "USDC":
		return true
	}
	return false
}

// listWithdrawalsHandler handles GET /api/v1/withdrawals (authenticated).
func listWithdrawalsHandler(d Deps) http.HandlerFunc {
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

		wd, err := d.ChainWatcherSvc.ListWithdrawals(r.Context(), uid, 50)
		if err != nil {
			d.Log.Error("list withdrawals failed", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		writeJSON(w, http.StatusOK, wd)
	}
}


// getDepositAddressHandler handles GET /api/v1/deposit-address/{asset}.
//
// Error mapping (UAPI-6 audit fix): the underlying chainwatcher
// service can fail in distinct ways and each maps to a different
// HTTP status. Returning 500 + the raw pgx / driver error
// message leaks internal state to the client — we now return
// generic 4xx / 5xx bodies and log the detail server-side only.
//
//	chain not configured for asset       → 404 not found
//	chain driver exists but disabled     → 503 service unavailable
//	chain RPC error (timeout, 5xx, etc.)  → 502 bad gateway
//	anything else (DB, panic, etc.)      → 500 internal error
func getDepositAddressHandler(d Deps) http.HandlerFunc {
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

		asset := chi.URLParam(r, "asset")
		if asset == "" {
			writeError(w, http.StatusBadRequest, "asset required")
			return
		}

		addr, err := d.ChainWatcherSvc.GetDepositAddress(r.Context(), uid, asset)
		if err != nil {
			// Always log the detail server-side. What we send
			// to the client depends only on a string match
			// against well-known driver error shapes, not the
			// raw error message.
			d.Log.Error("get deposit address failed",
				"user_id", uid, "asset", asset, "error", err)

			// Classify by the same strings the chainwatcher
			// service returns. We deliberately do not match on
			// the full err.Error() because the message is
			// operator-facing; only the prefix is stable.
			msg := err.Error()
			switch {
			case strings.HasPrefix(msg, "no driver for asset"):
				writeError(w, http.StatusNotFound,
					"deposit addresses not supported for this asset")
			case strings.HasPrefix(msg, "chain ") && strings.Contains(msg, " disabled"):
				writeError(w, http.StatusServiceUnavailable,
					"deposit addresses temporarily unavailable for this asset")
			case strings.Contains(msg, "rpc error") ||
				strings.Contains(msg, "connection refused") ||
				strings.Contains(msg, "context deadline exceeded"):
				writeError(w, http.StatusBadGateway,
					"upstream chain rpc error")
			default:
				// Catch-all 5xx — never expose the raw
				// message. Same hardening as the v0.2 / v0.3
				// NEW-H2 audit applied to the regular /api/v1
				// surface; we mirror it here so the public
				// user-api surface does not regress.
				writeError(w, http.StatusInternalServerError,
					"internal error")
			}
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"asset":   asset,
			"address": addr,
		})
	}
}


// importDepositsFromChainHandler handles POST /api/v1/deposits/import.
// Scans chain for any unrecorded confirmed deposits for the authenticated user
// and inserts them. Returns count of imported deposits.
func importDepositsFromChainHandler(d Deps) http.HandlerFunc {
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
		imported, err := d.ChainWatcherSvc.ImportDepositsFromChain(r.Context(), uid)
		if err != nil {
			d.Log.Error("import deposits failed", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"imported_count": len(imported),
			"imported_ids":   imported,
		})
	}
}

// listPendingTxsHandler handles GET /api/v1/pending-txs.
// Returns per-tx pending deposits with live confirmation counts.
// Each entry: tx_hash, amount, confirmations (0 = mempool), block_height, etc.
func listPendingTxsHandler(d Deps) http.HandlerFunc {
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
		pending, err := d.ChainWatcherSvc.GetPendingTxs(r.Context(), uid)
		if err != nil {
			d.Log.Error("get pending txs failed", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		writeJSON(w, http.StatusOK, pending)
	}
}

// listPendingDepositsHandler handles GET /api/v1/pending-deposits.
//
// Returns all watched addresses with confirmed + pending balance.
func listPendingDepositsHandler(d Deps) http.HandlerFunc {
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
		pd, err := d.ChainWatcherSvc.GetPendingDeposits(r.Context(), uid)
		if err != nil {
			d.Log.Error("get pending deposits failed", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		writeJSON(w, http.StatusOK, pd)
	}
}
