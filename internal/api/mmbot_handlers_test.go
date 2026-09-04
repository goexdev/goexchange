package api_test

// Integration tests for the per-pair market-making bot admin
// handlers in mmbot_handlers.go.
//
// Strategy: each handler depends on a mmbot.Client
// interface (not the concrete grpcClientImpl) plus a few
// fields of api.Deps (Log, MMBotClient, UserSvc, AuditSvc).
// We inject a fakeClient that returns canned BotState
// responses, and a minimal Deps that wires only those fields.
// auditLogAdmin no-ops when AuditSvc is nil, so we leave
// AuditSvc nil here -- tests cover the handler happy path
// and error path. A separate integration test against a real
// DB would exercise audit insertion.
//
// We do not test the adminMiddleware path (admin role
// enforcement) here -- that lives in router.go and is
// covered by the existing api-level integration tests.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/goexdev/goexchange/internal/api"
	"github.com/goexdev/goexchange/internal/mmbot"
)

// fakeClient is a hand-rolled mmbot.Client that returns canned
// values. We cannot mock the gRPC layer in the existing
// integration tests because they require a live bot engine;
// this fake lets us verify the handler logic in isolation.
type fakeClient struct {
	startBot  mmbot.BotState
	startErr  error
	stopRes   mmbot.StopResult
	stopErr   error
	statusBot mmbot.BotState
	statusErr error
	listBots  []mmbot.BotState
	listErr   error

	// captured records the last args each method was called
	// with so tests can assert wire-level detail.
	captured struct {
		startParams mmbot.StartParams
		stopBotID   string
		stopRet     bool
		statusBotID string
		listPair    string
		listStatus  mmbot.Status
	}
}

func (f *fakeClient) Start(_ context.Context, p mmbot.StartParams) (mmbot.BotState, error) {
	f.captured.startParams = p
	return f.startBot, f.startErr
}
func (f *fakeClient) Stop(_ context.Context, botID string, ret bool) (mmbot.StopResult, error) {
	f.captured.stopBotID = botID
	f.captured.stopRet = ret
	return f.stopRes, f.stopErr
}
func (f *fakeClient) Status(_ context.Context, botID string) (mmbot.BotState, error) {
	f.captured.statusBotID = botID
	return f.statusBot, f.statusErr
}
func (f *fakeClient) List(_ context.Context, pair string, st mmbot.Status) ([]mmbot.BotState, error) {
	f.captured.listPair = pair
	f.captured.listStatus = st
	return f.listBots, f.listErr
}

// compile-time check that fakeClient satisfies the interface.
var _ mmbot.Client = (*fakeClient)(nil)

// newTestDeps returns a Deps with only the fields our handlers
// touch. Log goes to io.Discard to keep test output clean.
// AuditSvc is nil: auditLogAdmin short-circuits on nil so the
// handlers still run.
func newTestDeps(c mmbot.Client) api.Deps {
	return api.Deps{
		Log:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		MMBotClient:  c,
		// UserSvc, AuditSvc, Pool etc intentionally nil: the
		// happy-path handlers we test do not touch them.
	}
}

// doReq is a small helper: builds a request through the given
// handler and returns the response recorder + decoded body.
func doReq(t *testing.T, h http.HandlerFunc, method, target string, body []byte) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h(w, req)
	var decoded map[string]any
	if w.Body.Len() > 0 {
		if err := json.Unmarshal(w.Body.Bytes(), &decoded); err != nil && w.Code < 400 {
			t.Fatalf("decode body %q: %v", w.Body.String(), err)
		}
	}
	return w, decoded
}

// TestStartHandler_Success verifies that a valid start request
// reaches the client with the documented field mapping.
func TestStartHandler_Success(t *testing.T) {
	started := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	want := mmbot.BotState{
		BotID:        "BNB_USDT_mm_1",
		Pair:         "BNB_USDT",
		Status:       mmbot.StatusRunning,
		MidPrice:     "50000000000",
		SpreadBps:    10,
		BaseBalance:  "1000000",
		QuoteBalance: "50000000000",
		OpenOrderIDs: []string{"a", "b"},
		PnlQuote:     "0",
		CreatedAt:    started,
	}
	fc := &fakeClient{startBot: want}
	d := newTestDeps(fc)

	body := []byte(`{
		"pair":"BNB_USDT",
		"mid_price":"50000000000",
		"quote_seed":"50000000000",
		"base_seed":"1000000",
		"spread_bps":10
	}`)
	w, decoded := doReq(t, api.StartMMBotHandlerForTest(d), http.MethodPost,
		"/api/v1/admin/mmbot/start", body)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if fc.captured.startParams.Pair != "BNB_USDT" {
		t.Errorf("Pair: got %q", fc.captured.startParams.Pair)
	}
	if fc.captured.startParams.MidPrice != "50000000000" {
		t.Errorf("MidPrice: got %q (must stay string to avoid float round)", fc.captured.startParams.MidPrice)
	}
	if fc.captured.startParams.SpreadBps != 10 {
		t.Errorf("SpreadBps: got %d", fc.captured.startParams.SpreadBps)
	}
	// Verify the JSON shape admin dashboards consume.
	if decoded["bot"] == nil {
		t.Fatalf("response missing bot: %+v", decoded)
	}
	botMap, ok := decoded["bot"].(map[string]any)
	if !ok {
		t.Fatalf("bot not a map: %T", decoded["bot"])
	}
	if botMap["bot_id"] != "BNB_USDT_mm_1" {
		t.Errorf("bot_id: got %v", botMap["bot_id"])
	}
	if botMap["status"] != "RUNNING" {
		t.Errorf("status: got %v", botMap["status"])
	}
	if botMap["mid_price"] != "50000000000" {
		t.Errorf("mid_price: got %v", botMap["mid_price"])
	}
}

// TestStartHandler_BadJSON verifies that a malformed body
// returns 400 and never reaches the client.
func TestStartHandler_BadJSON(t *testing.T) {
	fc := &fakeClient{}
	d := newTestDeps(fc)
	w, decoded := doReq(t, api.StartMMBotHandlerForTest(d), http.MethodPost,
		"/api/v1/admin/mmbot/start", []byte(`{not json}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
	if decoded["error"] == nil {
		t.Errorf("missing error in body: %s", w.Body.String())
	}
	// start must NOT have been called.
	if fc.captured.startParams.Pair != "" {
		t.Errorf("start was called despite bad JSON: %+v", fc.captured.startParams)
	}
}

// TestStartHandler_MissingFields verifies that the handler
// rejects requests missing required fields with 400 and does
// not call the client.
func TestStartHandler_MissingFields(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"empty body", `{}`},
		{"no pair", `{"mid_price":"1","quote_seed":"1","base_seed":"1"}`},
		{"no mid_price", `{"pair":"BNB_USDT","quote_seed":"1","base_seed":"1"}`},
		{"no quote_seed", `{"pair":"BNB_USDT","mid_price":"1","base_seed":"1"}`},
		{"no base_seed", `{"pair":"BNB_USDT","mid_price":"1","quote_seed":"1"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fc := &fakeClient{}
			d := newTestDeps(fc)
			w, decoded := doReq(t, api.StartMMBotHandlerForTest(d), http.MethodPost,
				"/api/v1/admin/mmbot/start", []byte(c.body))
			if w.Code != http.StatusBadRequest {
				t.Errorf("status: got %d, want 400; body=%s", w.Code, w.Body.String())
			}
			if decoded["error"] == nil {
				t.Errorf("missing error: %s", w.Body.String())
			}
			if fc.captured.startParams.Pair != "" {
				t.Errorf("start was called despite missing field: %+v", fc.captured.startParams)
			}
		})
	}
}

// TestStartHandler_EngineError verifies that a non-nil error
// from the client propagates as HTTP 503 (Service Unavailable).
// Admin dashboards interpret 503 as "core is down, retry".
func TestStartHandler_EngineError(t *testing.T) {
	fc := &fakeClient{startErr: errors.New("mmbot: dial localhost:50052: connection refused")}
	d := newTestDeps(fc)
	body := []byte(`{
		"pair":"BNB_USDT","mid_price":"50000000000",
		"quote_seed":"50000000000","base_seed":"1000000"
	}`)
	w, _ := doReq(t, api.StartMMBotHandlerForTest(d), http.MethodPost,
		"/api/v1/admin/mmbot/start", body)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503; body=%s", w.Code, w.Body.String())
	}
}

// TestStopHandler_Success verifies that return_inventory defaults
// to true when omitted, and the bool field round-trips to the
// client.
func TestStopHandler_Success(t *testing.T) {
	started := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	stopped := started.Add(time.Hour)
	fc := &fakeClient{
		stopRes: mmbot.StopResult{
			Bot: mmbot.BotState{
				BotID:    "BNB_USDT_mm_1",
				Status:   mmbot.StatusStopped,
				StartedAt: func() *time.Time { t := started; return &t }(),
				StoppedAt: func() *time.Time { t := stopped; return &t }(),
				PnlQuote:  "123450000",
			},
			ReturnedQuote: "50100000000",
			ReturnedBase:  "999000",
		},
	}
	d := newTestDeps(fc)

	body := []byte(`{"bot_id":"BNB_USDT_mm_1"}`)
	w, decoded := doReq(t, api.StopMMBotHandlerForTest(d), http.MethodPost,
		"/api/v1/admin/mmbot/stop", body)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	if !fc.captured.stopRet {
		t.Errorf("default return_inventory must be true; got false")
	}
	if fc.captured.stopBotID != "BNB_USDT_mm_1" {
		t.Errorf("bot_id: got %q", fc.captured.stopBotID)
	}
	if decoded["returned_quote"] != "50100000000" {
		t.Errorf("returned_quote: got %v", decoded["returned_quote"])
	}
}

// TestStopHandler_ReturnInventoryFalse verifies that an
// explicit return_inventory=false passes through to the
// client. Used by operators who want to "pause" a bot
// without moving funds.
func TestStopHandler_ReturnInventoryFalse(t *testing.T) {
	fc := &fakeClient{}
	d := newTestDeps(fc)
	body := []byte(`{"bot_id":"BNB_USDT_mm_1","return_inventory":false}`)
	w, _ := doReq(t, api.StopMMBotHandlerForTest(d), http.MethodPost,
		"/api/v1/admin/mmbot/stop", body)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d", w.Code)
	}
	if fc.captured.stopRet {
		t.Errorf("return_inventory=false should pass through; got true")
	}
}

// TestStopHandler_MissingBotID verifies that an empty bot_id
// returns 400 and never reaches the client.
func TestStopHandler_MissingBotID(t *testing.T) {
	fc := &fakeClient{}
	d := newTestDeps(fc)
	w, _ := doReq(t, api.StopMMBotHandlerForTest(d), http.MethodPost,
		"/api/v1/admin/mmbot/stop", []byte(`{}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

// TestStatusHandler_Success verifies the GET /status path
// returns the bot object as documented.
func TestStatusHandler_Success(t *testing.T) {
	fc := &fakeClient{statusBot: mmbot.BotState{
		BotID: "BNB_USDT_mm_1", Pair: "BNB_USDT",
		Status: mmbot.StatusRunning, MidPrice: "50000000000",
	}}
	d := newTestDeps(fc)
	w, decoded := doReq(t, api.MMBotStatusHandlerForTest(d), http.MethodGet,
		"/api/v1/admin/mmbot/status?bot_id=BNB_USDT_mm_1", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d; body=%s", w.Code, w.Body.String())
	}
	if fc.captured.statusBotID != "BNB_USDT_mm_1" {
		t.Errorf("bot_id: got %q", fc.captured.statusBotID)
	}
	botMap := decoded["bot"].(map[string]any)
	if botMap["pair"] != "BNB_USDT" {
		t.Errorf("pair: got %v", botMap["pair"])
	}
}

// TestStatusHandler_MissingBotID verifies that an empty bot_id
// query param returns 400.
func TestStatusHandler_MissingBotID(t *testing.T) {
	fc := &fakeClient{}
	d := newTestDeps(fc)
	w, _ := doReq(t, api.MMBotStatusHandlerForTest(d), http.MethodGet,
		"/api/v1/admin/mmbot/status", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

// TestListHandler_EmptyFilters verifies that an empty pair +
// empty status list call works (returns whatever the bot engine
// has).
func TestListHandler_EmptyFilters(t *testing.T) {
	fc := &fakeClient{listBots: []mmbot.BotState{
		{BotID: "BNB_USDT_mm_1", Pair: "BNB_USDT", Status: mmbot.StatusRunning},
		{BotID: "ETH_USDT_mm_1", Pair: "ETH_USDT", Status: mmbot.StatusStopped},
	}}
	d := newTestDeps(fc)
	w, decoded := doReq(t, api.MMBotListHandlerForTest(d), http.MethodGet,
		"/api/v1/admin/mmbot/list", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d", w.Code)
	}
	if fc.captured.listPair != "" || fc.captured.listStatus != mmbot.StatusUnspecified {
		t.Errorf("empty filter should pass zero values; got pair=%q status=%q",
			fc.captured.listPair, fc.captured.listStatus)
	}
	bots := decoded["bots"].([]any)
	if len(bots) != 2 {
		t.Errorf("expected 2 bots, got %d", len(bots))
	}
}

// TestListHandler_FilterByPair verifies that ?pair=BNB_USDT
// forwards to the client.
func TestListHandler_FilterByPair(t *testing.T) {
	fc := &fakeClient{}
	d := newTestDeps(fc)
	w, _ := doReq(t, api.MMBotListHandlerForTest(d), http.MethodGet,
		"/api/v1/admin/mmbot/list?pair=BNB_USDT", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d", w.Code)
	}
	if fc.captured.listPair != "BNB_USDT" {
		t.Errorf("pair filter: got %q", fc.captured.listPair)
	}
}

// TestListHandler_FilterByStatus verifies that ?status=RUNNING
// forwards the proto enum NAME (not int) to the client.
func TestListHandler_FilterByStatus(t *testing.T) {
	fc := &fakeClient{}
	d := newTestDeps(fc)
	w, _ := doReq(t, api.MMBotListHandlerForTest(d), http.MethodGet,
		"/api/v1/admin/mmbot/list?status=RUNNING", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d", w.Code)
	}
	if fc.captured.listStatus != mmbot.StatusRunning {
		t.Errorf("status filter: got %q, want RUNNING", fc.captured.listStatus)
	}
}

// TestListHandler_EngineError verifies that a non-nil error
// from the client returns 503.
func TestListHandler_EngineError(t *testing.T) {
	fc := &fakeClient{listErr: errors.New("mmbot: connection refused")}
	d := newTestDeps(fc)
	w, _ := doReq(t, api.MMBotListHandlerForTest(d), http.MethodGet,
		"/api/v1/admin/mmbot/list", nil)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}
}

// TestStateToJSON_Fields verifies that stateToJSON emits every
// documented field and does not silently drop nilable time
// pointers.
func TestStateToJSON_Fields(t *testing.T) {
	created := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	started := created.Add(time.Minute)
	state := mmbot.BotState{
		BotID:        "X",
		Pair:         "BNB_USDT",
		Status:       mmbot.StatusRunning,
		MidPrice:     "50000000000",
		SpreadBps:    10,
		BaseBalance:  "1",
		QuoteBalance: "2",
		OpenOrderIDs: []string{"o1"},
		PnlQuote:     "3",
		CreatedAt:    created,
		StartedAt:    func() *time.Time { t := started; return &t }(),
	}

	// Use the private handler helper via the dedicated test
	// entry point. See mmbot_handlers_export_test.go in api_test
	// package for the re-export.
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	api.RenderBotStateForTest(w, r, state)

	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v; body=%s", err, w.Body.String())
	}
	mustHave := []string{
		"bot_id", "pair", "status", "mid_price", "spread_bps",
		"base_balance", "quote_balance", "open_order_ids",
		"pnl_quote", "created_at", "started_at",
	}
	for _, k := range mustHave {
		if _, ok := got[k]; !ok {
			t.Errorf("missing field %q in JSON output: %s", k, w.Body.String())
		}
	}
	// Decimal fields must NOT be parsed to float64; we keep them
	// as strings throughout so the admin dashboard can format
	// without precision loss.
	if got["mid_price"] != "50000000000" {
		t.Errorf("mid_price must be string, got %T %v", got["mid_price"], got["mid_price"])
	}
}
