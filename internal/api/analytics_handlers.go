package api

import (
	"net/http"

	"github.com/google/uuid"
)

// pnlHandler handles GET /api/v1/users/me/pnl
func pnlHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContextUUID(r.Context())
		if userID == uuid.Nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		pnl, err := d.AnalyticsSvc.ComputeUserPnL(r.Context(), userID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, pnl)
	}
}

// statusHandler handles GET /api/v1/status (public)
func statusHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := d.AnalyticsSvc.ComputeStatus(r.Context())
		writeJSON(w, http.StatusOK, status)
	}
}