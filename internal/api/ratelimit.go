package api

import (
	"net/http"
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
