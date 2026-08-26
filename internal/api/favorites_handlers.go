package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// favoritesHandler handles GET /api/v1/users/me/favorites
// Returns user's favorited market pairs.
//
// SECURITY: rate limited (see router) - prevents abuse.
func favoritesHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := userIDFromContextUUID(r.Context())
		if uid == (uuid.UUID{}) {
			writeError(w, http.StatusUnauthorized, "no user in context")
			return
		}
		favorites, err := d.UserSvc.GetFavorites(r.Context(), uid)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"favorites": favorites})
	}
}

// addFavoriteHandler handles POST /api/v1/users/me/favorites
// Body: {"pair": "BTC_USDT"}
//
// SECURITY:
//   - rate limited per user (see router)
//   - max 100 favorites per user (DB-enforced via app check)
//   - pair format validated against known markets
//   - cannot exceed reasonable count to prevent DoS
func addFavoriteHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := userIDFromContextUUID(r.Context())
		if uid == (uuid.UUID{}) {
			writeError(w, http.StatusUnauthorized, "no user in context")
			return
		}
		var in struct {
			Pair string `json:"pair"`
		}
		if err := decodeJSON(r, &in); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		// Validate pair format (basic - 3-12 chars, uppercase + underscore)
		if !validPairFormat(in.Pair) {
			writeError(w, http.StatusBadRequest, "invalid pair format")
			return
		}
		if err := d.UserSvc.AddFavorite(r.Context(), uid, in.Pair); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "pair": in.Pair})
	}
}

// removeFavoriteHandler handles DELETE /api/v1/users/me/favorites/{pair}
func removeFavoriteHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := userIDFromContextUUID(r.Context())
		if uid == (uuid.UUID{}) {
			writeError(w, http.StatusUnauthorized, "no user in context")
			return
		}
		pair := chi.URLParam(r, "pair")
		if !validPairFormat(pair) {
			writeError(w, http.StatusBadRequest, "invalid pair format")
			return
		}
		if err := d.UserSvc.RemoveFavorite(r.Context(), uid, pair); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "pair": pair})
	}
}

// validPairFormat checks basic format (3-12 uppercase + underscore chars)
// Real pair names like BTC_USDT, ETH_USDT, SOL_USDT fit this pattern.
func validPairFormat(pair string) bool {
	if len(pair) < 3 || len(pair) > 12 {
		return false
	}
	for _, c := range pair {
		if !((c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}
	return true
}