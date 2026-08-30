package api

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

// RateLimiter implements a simple sliding window rate limiter per key.
//
// SECURITY: Protects against:
// - Brute force login attacks (limit per IP)
// - Account creation flood (limit per IP)
// - Cancel flood (limit per user)
// - Withdrawal flood (limit per user)
//
// Uses in-memory storage. For multi-instance deployments, replace
// with Redis-based implementation.
type RateLimiter struct {
	mu       sync.Mutex
	requests map[string][]time.Time
	limit    int
	window   time.Duration
}

// NewRateLimiter creates a new rate limiter.
// limit: max requests in window
// window: time period
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		requests: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
	}
	// Background cleanup
	go rl.cleanup()
	return rl
}

// Allow checks if a request from `key` is allowed.
// Returns true if allowed, false if rate limited.
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	// Get existing requests for this key
	requests, ok := rl.requests[key]
	if !ok {
		requests = []time.Time{}
	}

	// Filter out old requests (outside window)
	valid := []time.Time{}
	for _, t := range requests {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}

	// Check limit
	if len(valid) >= rl.limit {
		rl.requests[key] = valid
		return false
	}

	// Add current request
	valid = append(valid, now)
	rl.requests[key] = valid
	return true
}

// Reset clears the limiter state for a key (used on successful auth)
func (rl *RateLimiter) Reset(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.requests, key)
}

// cleanup periodically removes old entries to prevent memory leak.
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(rl.window)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		cutoff := now.Add(-rl.window)
		for key, requests := range rl.requests {
			valid := []time.Time{}
			for _, t := range requests {
				if t.After(cutoff) {
					valid = append(valid, t)
				}
			}
			if len(valid) == 0 {
				delete(rl.requests, key)
			} else {
				rl.requests[key] = valid
			}
		}
		rl.mu.Unlock()
	}
}

// =========================================================================
// Per-API-key rate limiting
//
// One bucket per API key (private endpoints) OR per source IP
// (public endpoints, where there is no API key yet). The
// userAPIKeyRateLimiter middleware uses the apiKeyKeyFromContext
// function to pick which: if the request passed through
// userAPIKeyAuth the api key id is in context; otherwise the
// client IP falls back.
//
// The fallback means a single attacker can still flood public
// market data endpoints (which is exactly the threat the limiter
// is meant to stop), without needing to register a key first.
// The error message is intentionally identical ("api key rate
// limit exceeded") for both paths so an attacker probing the
// limit does not learn whether they hit a per-IP or per-key
// bucket.
//
// We follow the same in-memory map + sliding-window pattern as
// the other RateLimiters in this file. Trade-off: in a multi-
// instance deployment each instance would have its own bucket
// and the effective limit is N times the per-instance number.
// For the single-process target (under 1000 users) this is
// fine. Future work: externalise to Redis. See the package doc
// at the top of this file.
// =========================================================================

// apiKeyRateLimiter implements a sliding window rate limiter
// keyed by either an api_key id (private endpoints) or a client
// IP (public endpoints). Both flow through the same middleware;
// the key is decided at request time by apiKeyKeyFromContext.
type apiKeyRateLimiter struct {
	mu       sync.Mutex
	requests map[string][]time.Time
	limit    int
	window   time.Duration
}

// NewAPIKeyRateLimiter constructs a per-key rate limiter. The
// default limit (60/min) matches the existing orderLimiter for
// non-trading endpoints and is permissive enough to allow
// monitoring scripts while still capping brute-force scan
// attempts.
func NewAPIKeyRateLimiter(limit int, window time.Duration) *apiKeyRateLimiter {
	rl := &apiKeyRateLimiter{
		requests: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
	}
	go rl.cleanup()
	return rl
}

// allow checks if a request from `key` is permitted. Returns
// true with retryAfter=0 on success, or false with the time
// until the oldest in-window request falls out.
func (l *apiKeyRateLimiter) allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-l.window)

	requests, ok := l.requests[key]
	if !ok {
		requests = []time.Time{}
	}

	valid := make([]time.Time, 0, len(requests))
	for _, t := range requests {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}

	if len(valid) >= l.limit {
		l.requests[key] = valid
		// Retry-after = time until the OLDEST in-window request
		// expires. That is the earliest moment the bucket will
		// have a free slot.
		return false, valid[0].Add(l.window).Sub(now)
	}

	valid = append(valid, now)
	l.requests[key] = valid
	return true, 0
}

// cleanup periodically prunes empty buckets to keep the map
// size proportional to the number of active keys/IPs.
func (l *apiKeyRateLimiter) cleanup() {
	ticker := time.NewTicker(l.window)
	defer ticker.Stop()
	for range ticker.C {
		l.mu.Lock()
		now := time.Now()
		cutoff := now.Add(-l.window)
		for key, requests := range l.requests {
			valid := []time.Time{}
			for _, t := range requests {
				if t.After(cutoff) {
					valid = append(valid, t)
				}
			}
			if len(valid) == 0 {
				delete(l.requests, key)
			} else {
				l.requests[key] = valid
			}
		}
		l.mu.Unlock()
	}
}

// Middleware applies the rate limit. The bucket key is whatever
// apiKeyKeyFromContext returns; if the request never passed
// through userAPIKeyAuth, that function falls back to the client
// IP, so public endpoints are also rate-limited (per IP).
//
// On 429 the response includes a Retry-After header so a polite
// client can back off without guessing.
func (l *apiKeyRateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := apiKeyKeyFromContext(r)
		if key == "" {
			// Should not happen for any route under
			// /user-api/v2 — they all pass through either
			// userAPIKeyAuth (private) or IPFromRequest (public).
			// Defensive: skip rather than 500.
			next.ServeHTTP(w, r)
			return
		}
		if ok, retryAfter := l.allow(key); !ok {
			w.Header().Set("Retry-After",
				strconv.Itoa(int(retryAfter.Seconds())+1))
			writeError(w, http.StatusTooManyRequests,
				"api key rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}
// Middleware returns a chi middleware that rate limits per IP.
// keyFn extracts the key from the request (defaults to IP).
func (rl *RateLimiter) Middleware(keyFn func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := keyFn(r)
			if !rl.Allow(key) {
				writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// IPFromRequest extracts the client IP from the request.
func IPFromRequest(r *http.Request) string {
	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.RemoteAddr
	}
	// Strip port: "1.2.3.4:5678" → "1.2.3.4"
	if idx := lastIndex(ip, ":"); idx > 0 {
		if ip[0] == '[' {
			ip = ip[1:idx-1]
		} else {
			ip = ip[:idx]
		}
	}
	return ip
}

// lastIndex is a simple wrapper for strings.LastIndex to avoid importing strings
func lastIndex(s, sub string) int {
	for i := len(s) - len(sub); i >= 0; i-- {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
