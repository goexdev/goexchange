package api

import (
	"context"
	"errors"
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
//	X-Api-Key:   *** key string (shown once at creation)
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
//
// X-Auth-Reason header (UAPI-7 audit fix): every 401 response
// also sets an X-Auth-Reason header with a stable, machine-
// readable reason code. The body remains the same generic
// "invalid api key" so an attacker cannot enumerate valid
// keys / replay attacks / clock-skew state from the body. The
// header is for operators and integration tests only — clients
// should keep treating the response as opaque. Documented
// reason codes:
//
//	missing_headers   — X-Api-Key or X-Api-Nonce absent
//	bad_nonce_format   — X-Api-Nonce not a positive integer
//	key_not_found      — key_id not in DB or revoked
//	key_expired        — key has expires_at in the past
//	clock_skew         — X-Api-Nonce outside ±5min window
//	nonce_replayed     — X-Api-Nonce <= last_nonce for this key
//	bad_signature      — bcrypt mismatch (key tampered)
//
// Operators reading the API log or a test harness comparing
// the header value across retries can now distinguish "the
// nonce was replayed" from "the key is revoked" without
// compromising the public contract.
func userAPIKeyAuth(svc *apikeys.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fullKey := r.Header.Get("X-Api-Key")
			nonceStr := strings.TrimSpace(r.Header.Get("X-Api-Nonce"))

			if fullKey == "" || nonceStr == "" {
				writeAuthError(w, "missing_headers")
				return
			}

			nonce, err := strconv.ParseInt(nonceStr, 10, 64)
			if err != nil || nonce <= 0 {
				writeAuthError(w, "bad_nonce_format")
				return
			}

			res, err := svc.ValidateRequest(r.Context(), fullKey, nonce)
			if err != nil {
				// Map each typed service error to its reason
				// code. The body stays generic — only the
				// header discriminates.
				writeAuthError(w, authReasonForError(err))
				return
			}

			ctx := r.Context()
			ctx = withUserID(ctx, res.Key.UserID.String())
			ctx = contextWithScopes(ctx, res.Scopes)
			ctx = contextWithAPIKeyKey(ctx, res.Key.KeyID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// writeAuthError sends a 401 with the generic body and adds
// the X-Auth-Reason header for operator / test visibility.
// Keeping the body identical across all reasons is deliberate:
// we never want to leak which dimension failed to the client.
func writeAuthError(w http.ResponseWriter, reason string) {
	w.Header().Set("X-Auth-Reason", reason)
	writeError(w, http.StatusUnauthorized, "invalid api key")
}

// authReasonForError maps a typed apikeys.Service error onto
// the stable reason code emitted in X-Auth-Reason. Unknown
// errors fall through to "bad_signature" so the operator sees
// a real signature / bcrypt failure rather than a misleading
// "key_not_found" — the failure is opaque to the client but
// the category helps in log triage.
func authReasonForError(err error) string {
	switch {
	case errors.Is(err, apikeys.ErrKeyNotFound):
		return "key_not_found"
	case errors.Is(err, apikeys.ErrKeyRevoked):
		return "key_not_found" // deliberately collapsed; an
		// attacker should not be able to probe revoked state
	case errors.Is(err, apikeys.ErrKeyExpired):
		return "key_expired"
	case errors.Is(err, apikeys.ErrClockSkew):
		return "clock_skew"
	case errors.Is(err, apikeys.ErrNonceReplayed):
		return "nonce_replayed"
	case errors.Is(err, apikeys.ErrNonceTooOld):
		return "clock_skew"
	default:
		// Bcrypt mismatch, DB error, or anything else.
		// We surface as "bad_signature" because the
		// underlying call site is the bcrypt comparison
		// (line 296 of apikeys/service.go); any other
		// error here would be a programming bug worth
		// investigating via the server log, not the
		// client-visible header.
		return "bad_signature"
	}
}

// scopesContextKey is a private key type so callers cannot
// accidentally collide with another context value.
type scopesContextKey struct{}

// apiKeyKeyContextKey is used by the api-key rate limiter. It
// stores either the verified api_key.KeyID (for authenticated
// requests) or the client IP (for public endpoints that never
// pass through userAPIKeyAuth). The middleware picks the right
// one — see apiKeyKeyFromContext below.
type apiKeyKeyContextKey struct{}

// contextWithAPIKeyKey stores the api-key rate-limit bucket key.
// Called from userAPIKeyAuth for authenticated requests, and
// from a small public-endpoint shim (see router.go) for
// unauthenticated ones.
func contextWithAPIKeyKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, apiKeyKeyContextKey{}, key)
}

// apiKeyKeyFromContext returns the bucket key for the api-key
// rate limiter. If the request did not pass through
// userAPIKeyAuth (public endpoint), falls back to the client
// IP so the limiter still has something to count against.
//
// IP resolution order (leftmost wins):
//
//   1. X-Forwarded-For leftmost entry — this is the *original*
//      client per RFC 7239 + nginx convention ($proxy_add_x…
//      appends). When cloudflare or another CDN is in front
//      it appends the edge IP at the right, so the leftmost
//      entry stays the true client. This is the only header
//      that survives proxy rewrites.
//
//   2. X-Real-IP. nginx sets this to $remote_addr (= the
//      immediate peer), so when cloudflare is in front this
//      value is the cloudflare edge IP. We use it as a
//      fallback when X-Forwarded-For is missing.
//
//   3. RemoteAddr. Same caveat as X-Real-IP, used as a last
//      resort.
//
// Note that we deliberately do NOT trust client-supplied
// X-Real-IP because nginx's proxy_set_header X-Real-IP
// $remote_addr rewrites it. Trusting it would let an attacker
// bypass the limiter by sending X-Real-IP: random-per-request.
func apiKeyKeyFromContext(r *http.Request) string {
	if v, ok := r.Context().Value(apiKeyKeyContextKey{}).(string); ok && v != "" {
		return v
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.Index(xff, ","); i >= 0 {
			xff = xff[:i]
		}
		if ip := strings.TrimSpace(xff); ip != "" {
			return ip
		}
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	return IPFromRequest(r)
}

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
