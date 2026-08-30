package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/goexdev/goexchange/internal/apikeys"
)

// userAPIKeyAuth middleware verifies a user-api request and
// places the user_id in the request context. It is mounted
// under /user-api/v2 only — the regular /api/v1 routes use the
// Bearer-token authMiddleware for human sessions.
//
// Required request headers:
//
//	X-Api-Key:   full key string (shown once at creation)
//	X-Api-Nonce: unix ms timestamp, must be strictly greater than
//	             the last accepted nonce for this key
//
// On success the context carries the user_id (via withUserID,
// the same key JWT auth uses) plus the verified scopes so
// handlers can decide whether the requested operation is
// allowed.
//
// On failure the response is 401 with a generic "invalid api
// key" body; details are logged server-side but never leaked to
// the client.
func userAPIKeyAuth(svc *apikeys.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fullKey := r.Header.Get("X-Api-Key")
			nonceStr := strings.TrimSpace(r.Header.Get("X-Api-Nonce"))

			if fullKey == "" || nonceStr == "" {
				writeError(w, http.StatusUnauthorized, "invalid api key")
				return
			}

			nonce, err := strconv.ParseInt(nonceStr, 10, 64)
			if err != nil || nonce <= 0 {
				writeError(w, http.StatusUnauthorized, "invalid api key")
				return
			}

			res, err := svc.ValidateRequest(r.Context(), fullKey, nonce)
			if err != nil {
				// Map all errors to 401 — we never tell the
				// client whether the key was wrong vs the
				// nonce was replayed vs the clock was off.
				writeError(w, http.StatusUnauthorized, "invalid api key")
				return
			}

			ctx := r.Context()
			ctx = withUserID(ctx, res.Key.UserID.String())
			ctx = contextWithScopes(ctx, res.Scopes)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// scopesContextKey is a private key type so callers cannot
// accidentally collide with another context value.
type scopesContextKey struct{}

// contextWithScopes stores the api key's verified scopes in
// the request context. Handlers that need to check scope reach
// for this with ScopesFromContext.
func contextWithScopes(ctx context.Context, scopes []string) context.Context {
	return context.WithValue(ctx, scopesContextKey{}, scopes)
}

// ScopesFromContext returns the scopes stored by
// userAPIKeyAuth, or nil if the request did not pass through
// the middleware.
func ScopesFromContext(ctx context.Context) []string {
	if ctx == nil {
		return nil
	}
	v, _ := ctx.Value(scopesContextKey{}).([]string)
	return v
}

// HasScope reports whether the context's api key has been
// granted the named scope. Returns false if no scopes were
// stored (request did not pass userAPIKeyAuth).
func HasScope(ctx context.Context, scope string) bool {
	for _, s := range ScopesFromContext(ctx) {
		if s == scope {
			return true
		}
	}
	return false
}
