package api

import (
	"github.com/go-chi/chi/v5"
	"net/http"

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

		var in struct {
			Asset       string `json:"asset"`
			Amount      string `json:"amount"`
			DestAddress string `json:"dest_address"`
		}
		if err := decodeJSON(r, &in); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if in.Asset == "" {
			in.Asset = "BTC"
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
			writeError(w, http.StatusBadRequest, "invalid amount")
			return
		}

		wd, err := d.ChainWatcherSvc.WithdrawWithSigner(r.Context(), uid, in.Asset, in.DestAddress, amount)
		if err != nil {
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
// Returns the user's deposit address for the asset. If none exists,
// generates a new one via the chain driver.
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
			d.Log.Error("get deposit address failed", "error", err)
			writeError(w, http.StatusInternalServerError, err.Error())
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
