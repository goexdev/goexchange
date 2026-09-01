// Multi-provider unit tests. These tests verify the failover and
// rate-limit logic without touching a live chain — we point the
// adapter at httptest servers so the assertions are deterministic.
//
// Run with `go test ./internal/blockchain/tron/`.

package tron

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestFailoverNextProvider verifies that when the primary returns
// 5xx the call lands on the backup, and that the active selector
// promotes the backup so subsequent calls hit it directly.
func TestFailoverNextProvider(t *testing.T) {
	primaryHits := int32(0)
	backupHits := int32(0)
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&primaryHits, 1)
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer primary.Close()
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&backupHits, 1)
		w.Write([]byte(`{"blockID":"deadbeef","raw_data":{"number":1,"timestamp":0}}`))
	}))
	defer backup.Close()

	a, err := NewAdapter(Config{
		Providers: []Provider{
			{Name: "p", BaseURL: primary.URL, Weight: 1},
			{Name: "b", BaseURL: backup.URL, Weight: 1},
		},
	})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}

	// First call: primary 500 → backup 200 → backup promoted.
	out := struct {
		Hash string `json:"blockID"`
	}{}
	if err := a.callWithFailover(context.Background(), MethodGetNowBlock, nil, &out); err != nil {
		t.Fatalf("call: %v", err)
	}
	if atomic.LoadInt32(&primaryHits) != 1 {
		t.Errorf("primary hits = %d, want 1", atomic.LoadInt32(&primaryHits))
	}
	if atomic.LoadInt32(&backupHits) != 1 {
		t.Errorf("backup hits = %d, want 1", atomic.LoadInt32(&backupHits))
	}
	if got := a.ActiveProvider().Name; got != "b" {
		t.Errorf("active = %q, want b (backup should be promoted)", got)
	}

	// Second call should go directly to backup (now active), no
	// extra primary hit.
	if err := a.callWithFailover(context.Background(), MethodGetNowBlock, nil, &out); err != nil {
		t.Fatalf("call2: %v", err)
	}
	if atomic.LoadInt32(&primaryHits) != 1 {
		t.Errorf("primary hits after promotion = %d, want 1 (no extra hit)", atomic.LoadInt32(&primaryHits))
	}
	if atomic.LoadInt32(&backupHits) != 2 {
		t.Errorf("backup hits = %d, want 2", atomic.LoadInt32(&backupHits))
	}
}

// TestFailoverRateLimit verifies that a 429 response marks the
// provider as rate-limited (Health=0 + 429 stamp) and the next
// call within the grace window skips it.
func TestFailoverRateLimit(t *testing.T) {
	primaryHits := int32(0)
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&primaryHits, 1)
		w.WriteHeader(429)
		io.WriteString(w, `{"error":"too many requests"}`)
	}))
	defer primary.Close()
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"blockID":"deadbeef","raw_data":{"number":1,"timestamp":0}}`))
	}))
	defer backup.Close()

	a, err := NewAdapter(Config{
		Providers: []Provider{
			{Name: "p", BaseURL: primary.URL, Weight: 1},
			{Name: "b", BaseURL: backup.URL, Weight: 1},
		},
	})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}

	out := struct {
		Hash string `json:"blockID"`
	}{}
	if err := a.callWithFailover(context.Background(), MethodGetNowBlock, nil, &out); err != nil {
		t.Fatalf("call: %v", err)
	}
	if atomic.LoadInt32(&primaryHits) != 1 {
		t.Errorf("primary hits = %d, want 1", atomic.LoadInt32(&primaryHits))
	}
	// After a 429 the primary should be unavailable.
	if a.providers[0].IsAvailable() {
		t.Error("primary should be unavailable after 429")
	}

	// Second call within the grace window should skip the rate-
	// limited primary entirely and land on the backup.
	primaryHitsBefore := atomic.LoadInt32(&primaryHits)
	if err := a.callWithFailover(context.Background(), MethodGetNowBlock, nil, &out); err != nil {
		t.Fatalf("call2: %v", err)
	}
	if atomic.LoadInt32(&primaryHits) != primaryHitsBefore {
		t.Errorf("primary should not have been retried inside grace window (got %d hits)", atomic.LoadInt32(&primaryHits))
	}
}

// TestSetActiveUnknownName verifies SetActive rejects unknown
// provider names without disturbing the active index.
func TestSetActiveUnknownName(t *testing.T) {
	a, err := NewAdapter(Config{
		Providers: []Provider{
			{Name: "primary", BaseURL: "http://localhost:1", Weight: 1},
			{Name: "backup", BaseURL: "http://localhost:2", Weight: 1},
		},
	})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	if err := a.SetActive("does-not-exist"); err == nil {
		t.Error("SetActive(unknown) should return an error")
	}
	if got := a.ActiveProvider().Name; got != "primary" {
		t.Errorf("active = %q, want primary (unchanged)", got)
	}
	if err := a.SetActive("backup"); err != nil {
		t.Errorf("SetActive(backup): %v", err)
	}
	if got := a.ActiveProvider().Name; got != "backup" {
		t.Errorf("active = %q, want backup", got)
	}
}

// TestRateLimitGraceExpiry verifies that once the 429 stamp is
// older than the grace window the provider becomes available
// again without any explicit MarkHealthy call.
func TestRateLimitGraceExpiry(t *testing.T) {
	p := Provider{Name: "p", BaseURL: "http://localhost"}
	p.Health = new(atomic.Int32)
	p.RateLimit429At = new(atomic.Int64)
	p.Health.Store(1)
	p.RateLimit429At.Store(time.Now().Add(-2 * RateLimitGrace).UnixNano())
	// Simulate a recent unhealthy result.
	p.Health.Store(0)
	if !p.IsAvailable() {
		t.Error("provider should recover after grace window")
	}
}

// TestIsAvailableRateLimit verifies the negative case: a fresh
// 429 with Health=0 makes the provider unavailable.
func TestIsAvailableRateLimit(t *testing.T) {
	p := Provider{Name: "p", BaseURL: "http://localhost"}
	p.Health = new(atomic.Int32)
	p.RateLimit429At = new(atomic.Int64)
	p.MarkRateLimited()
	if p.IsAvailable() {
		t.Error("provider should be unavailable within grace window after 429")
	}
	// Force the 429 stamp to long ago, leaving Health=0; provider
	// should still recover.
	p.RateLimit429At.Store(time.Now().Add(-2 * RateLimitGrace).UnixNano())
	if !p.IsAvailable() {
		t.Error("provider should recover after grace window")
	}
}

// TestIsRateLimitErrMatches verifies the heuristic catches the
// common 429 phrasings.
func TestIsRateLimitErrMatches(t *testing.T) {
	cases := []string{
		"status=429 from chainstack",
		"Too Many Requests",
		"upstream rate limit",
		"rate-limited by provider",
	}
	for _, s := range cases {
		if !isRateLimitErr(errString(s)) {
			t.Errorf("isRateLimitErr(%q) = false, want true", s)
		}
	}
	if isRateLimitErr(errString("status=500 from upstream")) {
		t.Error("isRateLimitErr should not match non-429 errors")
	}
	if isRateLimitErr(nil) {
		t.Error("isRateLimitErr(nil) should be false")
	}
}

// errString turns a string into an error for the helper tests.
func errString(s string) error {
	if s == "" {
		return nil
	}
	return &stringErr{s}
}

type stringErr struct{ s string }

func (e *stringErr) Error() string { return e.s }

// ensure strings used in tests match the package import path
var _ = strings.HasPrefix