package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/goexdev/goexchange/internal/trigger"
)

// listTriggerOrdersHandler handles GET /api/v1/users/me/triggers
func listTriggerOrdersHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContextUUID(r.Context())
		if userID == uuid.Nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		orders, err := d.TriggerSvc.ListByUser(r.Context(), userID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"triggers": orders})
	}
}

// createTriggerOrderHandler handles POST /api/v1/users/me/triggers
func createTriggerOrderHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContextUUID(r.Context())
		if userID == uuid.Nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		var in struct {
			Pair         string `json:"pair"`
			Side         string `json:"side"`
			TriggerType  string `json:"trigger_type"`
			TriggerPrice string `json:"trigger_price"`
			Quantity     string `json:"quantity"`
		}
		if err := decodeJSON(r, &in); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		tp, err := decimal.NewFromString(in.TriggerPrice)
		if err != nil || !tp.IsPositive() {
			writeError(w, http.StatusBadRequest, "invalid trigger_price")
			return
		}
		qty, err := decimal.NewFromString(in.Quantity)
		if err != nil || !qty.IsPositive() {
			writeError(w, http.StatusBadRequest, "invalid quantity")
			return
		}
		t, err := d.TriggerSvc.Create(r.Context(), trigger.CreateInput{
			UserID:       userID,
			Pair:         in.Pair,
			Side:         in.Side,
			TriggerType:  trigger.Type(in.TriggerType),
			TriggerPrice: tp,
			Quantity:     qty,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, t)
	}
}

// cancelTriggerOrderHandler handles DELETE /api/v1/users/me/triggers/{id}
func cancelTriggerOrderHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContextUUID(r.Context())
		if userID == uuid.Nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		idStr := chi.URLParam(r, "id")
		id, err := uuid.Parse(idStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid id")
			return
		}
		if err := d.TriggerSvc.Cancel(r.Context(), userID, id); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
	}
}