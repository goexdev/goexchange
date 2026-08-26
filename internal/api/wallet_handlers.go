package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/goexdev/goexchange/internal/wallet"
)

// getWalletHandler handles GET /api/v1/wallets (authenticated).
//
// Returns all balances for the authenticated user (zero balances included).
func getWalletHandler(d Deps) http.HandlerFunc {
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

		balances, err := d.WalletSvc.GetAll(r.Context(), uid)
		if err != nil {
			d.Log.Error("get balances failed", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		writeJSON(w, http.StatusOK, balances)
	}
}

// getOneWalletHandler handles GET /api/v1/wallets/{asset}.
func getOneWalletHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "no user in context")
			return
		}

		// Extract asset from URL via chi URLParam
		asset := chi.URLParam(r, "asset")
		if asset == "" {
			writeError(w, http.StatusBadRequest, "missing asset")
			return
		}

		uid, err := uuid.Parse(userID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid user id")
			return
		}

		bal, err := d.WalletSvc.GetOne(r.Context(), uid, asset)
		if err != nil {
			if errors.Is(err, wallet.ErrAssetNotSupported) {
				writeError(w, http.StatusNotFound, "asset not supported")
				return
			}
			d.Log.Error("get balance failed", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		writeJSON(w, http.StatusOK, bal)
	}
}

// ensureDecimal is a helper for handlers (currently unused but exported for future).
func ensureDecimal(s string) (decimal.Decimal, error) {
	return decimal.NewFromString(s)
}