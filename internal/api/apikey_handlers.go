package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/goexdev/goexchange/internal/apikeys"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// userAPIKeysHandler handles GET /api/v1/users/me/api-keys
func userAPIKeysHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		uid, _ := uuid.Parse(userID)
		keys, err := d.APIKeys.List(r.Context(), uid)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if keys == nil {
			keys = []*apikeys.Key{}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"keys":  keys,
			"count": len(keys),
		})
	}
}

// createAPIKeyHandler handles POST /api/v1/users/me/api-keys
// Body: { "name": "...", "scopes": ["read", "trade"], "expires_in_days": 30 }
func createAPIKeyHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		uid, _ := uuid.Parse(userID)

		var body struct {
			Name          string   `json:"name"`
			Scopes        []string `json:"scopes"`
			ExpiresInDays int      `json:"expires_in_days"`
		}
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if body.Name == "" {
			writeError(w, http.StatusBadRequest, "name required")
			return
		}

		// Validate scopes
		validScopes := map[string]bool{
			apikeys.ScopeRead:     true,
			apikeys.ScopeTrade:    true,
			apikeys.ScopeWithdraw: true,
		}
		for _, s := range body.Scopes {
			if !validScopes[s] {
				writeError(w, http.StatusBadRequest, "invalid scope: "+s)
				return
			}
		}
		if len(body.Scopes) == 0 {
			body.Scopes = []string{apikeys.ScopeRead}
		}

		// Generate key without expiry first
		key, secret, err := d.APIKeys.Generate(r.Context(), uid, body.Name, body.Scopes, nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// If expires_in_days requested, update
		if body.ExpiresInDays > 0 {
			if body.ExpiresInDays > 365 {
				writeError(w, http.StatusBadRequest, "expires_in_days must be <= 365")
				return
			}
			_, err := d.Pool.Exec(r.Context(),
				`UPDATE api_keys SET expires_at = NOW() + ($1 || ' days')::interval WHERE id = $2`,
				strconv.Itoa(body.ExpiresInDays), key.ID,
			)
			if err == nil {
				exp := time.Now().AddDate(0, 0, body.ExpiresInDays)
				key.ExpiresAt = &exp
			}
		}

		// Return key + secret (one-time view)
		writeJSON(w, http.StatusOK, map[string]any{
			"key":     key,
			"secret":  secret,
			"warning": "Save this secret now. It will not be shown again.",
		})
	}
}

// revokeAPIKeyHandler handles DELETE /api/v1/users/me/api-keys/{id}
func revokeAPIKeyHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		uid, _ := uuid.Parse(userID)
		idStr := chi.URLParam(r, "id")
		keyID, err := uuid.Parse(idStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid id")
			return
		}
		if err := d.APIKeys.Revoke(r.Context(), uid, keyID); err != nil {
			if err == apikeys.ErrKeyNotFound {
				writeError(w, http.StatusNotFound, "key not found")
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
	}
}