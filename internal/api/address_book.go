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
		asset := r.URL.Query().Get("asset")
		addrs, err := d.UserSvc.ListAddresses(r.Context(), userID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if asset != "" {
			filtered := []user.AddressBookEntry{}
			for _, a := range addrs {
				if a.Asset == strings.ToUpper(asset) {
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
		entry, err := d.UserSvc.AddAddress(r.Context(), userID, user.AddAddressInput{
			Asset: in.Asset, Address: in.Address, Label: in.Label, Whitelisted: in.Whitelisted,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
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
			writeError(w, http.StatusBadRequest, err.Error())
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
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, entry)
	}
}
