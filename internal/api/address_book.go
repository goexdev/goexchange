package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/goexdev/goexchange/internal/user"
)

// listAddressesHandler handles GET /api/v1/users/me/addresses
func listAddressesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContextUUID(r.Context())
		if userID == uuid.Nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		asset := strings.ToUpper(r.URL.Query().Get("asset"))
		addrs, err := d.UserSvc.ListAddresses(r.Context(), userID)
		if err != nil {
			d.Log.Error("list addresses failed", "user_id", userID, "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if asset != "" {
			filtered := []user.AddressBookEntry{}
			for _, a := range addrs {
				if a.Asset == asset {
					filtered = append(filtered, a)
				}
			}
			addrs = filtered
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"addresses": addrs,
		})
	}
}

// addAddressHandler handles POST /api/v1/users/me/addresses
func addAddressHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContextUUID(r.Context())
		if userID == uuid.Nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		var in struct {
			Asset       string `json:"asset"`
			Address     string `json:"address"`
			Label       string `json:"label"`
			Whitelisted bool   `json:"whitelisted"`
		}
		if err := decodeJSON(r, &in); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		// Field validation — return precise per-field error instead of
		// letting the DB layer return a raw SQL error (which the H2
		// audit of 2026-08-28 v0.2 + v0.3's NEW-H3 both flagged).
		in.Asset = strings.ToUpper(strings.TrimSpace(in.Asset))
		if in.Asset == "" || in.Address == "" {
			writeError(w, http.StatusBadRequest, "asset and address required")
			return
		}
		if !validAddressAsset(in.Asset) {
			writeError(w, http.StatusBadRequest,
				"asset must be one of: BTC, ETH, BNB, SOL, USDT, USDC")
			return
		}
		entry, err := d.UserSvc.AddAddress(r.Context(), userID, user.AddAddressInput{
			Asset: in.Asset, Address: in.Address, Label: in.Label, Whitelisted: in.Whitelisted,
		})
		if err != nil {
			// 5xx — never echo raw pgx errors to the client. Log
			// the detail for operators and return a generic message
			// (writeError already does the redaction; this comment
			// documents the policy for the next person who edits
			// this file).
			d.Log.Error("add address failed", "user_id", userID, "asset", in.Asset, "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		writeJSON(w, http.StatusCreated, entry)
	}
}

// deleteAddressHandler handles DELETE /api/v1/users/me/addresses/{id}
func deleteAddressHandler(d Deps) http.HandlerFunc {
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
		if err := d.UserSvc.DeleteAddress(r.Context(), userID, id); err != nil {
			d.Log.Error("delete address failed", "user_id", userID, "address_id", id, "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}

// updateAddressHandler handles PATCH /api/v1/users/me/addresses/{id}
func updateAddressHandler(d Deps) http.HandlerFunc {
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
		var in struct {
			Label       *string `json:"label"`
			Whitelisted  *bool   `json:"whitelisted"`
		}
		if err := decodeJSON(r, &in); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		entry, err := d.UserSvc.UpdateAddress(r.Context(), userID, id, user.UpdateAddressInput{
			Label: in.Label, Whitelisted: in.Whitelisted,
		})
		if err != nil {
			d.Log.Error("update address failed", "user_id", userID, "address_id", id, "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		writeJSON(w, http.StatusOK, entry)
	}
}

// validAddressAsset mirrors the supported withdrawal currencies. Kept
// in the API layer (rather than hitting the DB or currencies service)
// because the address book is a small enumeration and the user-facing
// error must use the same wording as the withdraw endpoint.
func validAddressAsset(s string) bool {
	switch s {
	case "BTC", "ETH", "BNB", "SOL", "USDT", "USDC":
		return true
	}
	return false
}
