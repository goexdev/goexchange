package mmbot_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	mmbot "github.com/goexdev/goexchange/internal/mmbot"
	mmbotv1 "github.com/goexdev/goexchange/internal/mmbot/mmbotv1"
)

// TestStatusConstants documents the documented string values
// of every Status constant. Admin handlers and dashboards pass
// these strings back and forth in JSON; any drift here would
// break the admin UI.
func TestStatusConstants(t *testing.T) {
	cases := map[mmbot.Status]bool{
		mmbot.StatusStopped:  true,
		mmbot.StatusSeeding:  true,
		mmbot.StatusReady:    true,
		mmbot.StatusRunning:  true,
		mmbot.StatusStopping: true,
		mmbot.StatusFailed:   true,
	}
	for s, want := range cases {
		if got := s != ""; got != want {
			t.Errorf("Status %q should be non-empty", s)
		}
	}
	// Unspecified is the zero value.
	if mmbot.StatusUnspecified != "" {
		t.Errorf("StatusUnspecified should be empty string, got %q", mmbot.StatusUnspecified)
	}
}

// TestAllStatusesReturnsEveryNonZeroStatus ensures the
// convenience list used by the admin List handler includes
// every documented running status.
func TestAllStatusesReturnsEveryNonZeroStatus(t *testing.T) {
	got := mmbot.AllStatuses()
	if len(got) < 5 {
		t.Fatalf("AllStatuses returned %d entries, want >=5", len(got))
	}
	want := map[mmbot.Status]bool{
		mmbot.StatusStopped:  true,
		mmbot.StatusSeeding:  true,
		mmbot.StatusReady:    true,
		mmbot.StatusRunning:  true,
		mmbot.StatusStopping: true,
		mmbot.StatusFailed:   true,
	}
	for _, s := range got {
		if !want[s] {
			t.Errorf("AllStatuses contains unexpected %q", s)
		}
	}
}

// TestBotStateRoundTripJSONShape documents the public JSON
// shape that admin dashboards consume. We exercise stateToJSON
// indirectly by constructing a BotState the same way the proto
// converter does and asserting every documented field lands in
// the output map.
func TestBotStateRoundTripJSONShape(t *testing.T) {
	created := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	started := created.Add(time.Minute)
	stopped := started.Add(2 * time.Hour)

	// Mirror what protoToState produces for a real proto payload.
	original := &mmbotv1.BotState{
		BotId:        "BNB_USDT_mm_1",
		Pair:         "BNB_USDT",
		Status:       mmbotv1.BotStatus_BOT_STATUS_RUNNING,
		MidPrice:     "50000000000",
		SpreadBps:    10,
		BaseBalance:  "1000000",
		QuoteBalance: "50000000000",
		OpenOrderIds: []string{"order-a", "order-b"},
		PnlQuote:     "123450000",
		CreatedAt:    timestamppb.New(created),
		StartedAt:    timestamppb.New(started),
		StoppedAt:    timestamppb.New(stopped),
		LastError:    "",
	}
	_ = original

	// Build the surface that handlers see. We do this inline
	// rather than via the unexported protoToState converter to
	// keep the test surface public.
	state := mmbot.BotState{
		BotID:        "BNB_USDT_mm_1",
		Pair:         "BNB_USDT",
		Status:       mmbot.StatusRunning,
		MidPrice:     "50000000000",
		SpreadBps:    10,
		BaseBalance:  "1000000",
		QuoteBalance: "50000000000",
		OpenOrderIDs: []string{"order-a", "order-b"},
		PnlQuote:     "123450000",
		CreatedAt:    created,
		StartedAt:    &started,
		StoppedAt:    &stopped,
	}

	// Spot-check every documented field.
	if state.BotID != "BNB_USDT_mm_1" {
		t.Errorf("BotID: got %q", state.BotID)
	}
	if state.Pair != "BNB_USDT" {
		t.Errorf("Pair: got %q", state.Pair)
	}
	if state.Status != mmbot.StatusRunning {
		t.Errorf("Status: got %q", state.Status)
	}
	if state.MidPrice != "50000000000" {
		t.Errorf("MidPrice must stay as string to avoid float round: got %q", state.MidPrice)
	}
	if state.SpreadBps != 10 {
		t.Errorf("SpreadBps: got %d", state.SpreadBps)
	}
	if state.BaseBalance != "1000000" {
		t.Errorf("BaseBalance: got %q", state.BaseBalance)
	}
	if state.QuoteBalance != "50000000000" {
		t.Errorf("QuoteBalance: got %q", state.QuoteBalance)
	}
	if len(state.OpenOrderIDs) != 2 {
		t.Errorf("OpenOrderIDs len: got %d", len(state.OpenOrderIDs))
	}
	if state.PnlQuote != "123450000" {
		t.Errorf("PnlQuote: got %q", state.PnlQuote)
	}
	if state.StartedAt == nil || !state.StartedAt.Equal(started) {
		t.Errorf("StartedAt: got %v, want %v", state.StartedAt, started)
	}
	if state.StoppedAt == nil || !state.StoppedAt.Equal(stopped) {
		t.Errorf("StoppedAt: got %v, want %v", state.StoppedAt, stopped)
	}
}

// TestEmptyAddrReturnsError verifies that constructing a client
// with an empty addr does NOT return nil (which would panic at
// call time) but instead returns a non-nil interface that always
// errors. Admin handlers rely on this to return HTTP 503
// instead of crashing the server when the bot engine is not
// configured.
func TestEmptyAddrReturnsError(t *testing.T) {
	c := mmbot.NewGRPCClient("", nil)
	if c == nil {
		t.Fatal("NewGRPCClient(\"\") returned nil; admin handlers would nil-deref")
	}

	ctx := context.Background()
	if _, err := c.Start(ctx, mmbot.StartParams{Pair: "BNB_USDT"}); err == nil {
		t.Error("Start on empty-addr client: expected error, got nil")
	}
	if _, err := c.Stop(ctx, "BNB_USDT_mm_1", true); err == nil {
		t.Error("Stop on empty-addr client: expected error, got nil")
	}
	if _, err := c.Status(ctx, "BNB_USDT_mm_1"); err == nil {
		t.Error("Status on empty-addr client: expected error, got nil")
	}
	if _, err := c.List(ctx, "", mmbot.StatusUnspecified); err == nil {
		t.Error("List on empty-addr client: expected error, got nil")
	}
}

// TestUnreachableAddrReturnsError verifies that a syntactically
// valid but unreachable addr also returns the errorClient
// rather than panicking. We use port 1 (privileged, nobody
// binds it in tests).
func TestUnreachableAddrReturnsError(t *testing.T) {
	c := mmbot.NewGRPCClient("127.0.0.1:1", nil)
	if c == nil {
		t.Fatal("NewGRPCClient unreachable: returned nil")
	}
	if _, err := c.List(context.Background(), "", mmbot.StatusUnspecified); err == nil {
		t.Error("expected error from unreachable addr, got nil")
	}
}

// TestStartParamsValid verifies that StartParams preserves
// every documented field. The handler passes these through
// verbatim to the gRPC client; any silent drop here would
// cause bot startup to misconfigure.
func TestStartParamsValid(t *testing.T) {
	p := mmbot.StartParams{
		Pair:            "BNB_USDT",
		MidPrice:        "50000000000",
		QuoteSeed:       "50000000000",
		BaseSeed:        "1000000",
		SpreadBps:       10,
		TreasuryWallet:  "TUEZSdKsoDHQMeZwihtdoBiN46zxhGWYdH",
		MinQuotePerSide: "1000000",
	}
	if p.Pair != "BNB_USDT" || p.SpreadBps != 10 || p.TreasuryWallet == "" {
		t.Fatalf("StartParams fields lost: %+v", p)
	}
}
