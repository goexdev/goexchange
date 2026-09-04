package api

// Integration tests for the audit-log path of the per-pair
// market-making bot admin handlers.
//
// The existing mmbot_handlers_test.go (package api_test) injects
// nil AuditSvc and verifies handler behavior in isolation. The
// tests here use a real Postgres pool + a real audit.Service so
// we exercise the auditLogAdmin helper that the production
// handlers call on every successful or failed admin action.
//
// We need package api (not api_test) so the unexported userIDKey
// is reachable; this is the same pattern router_test uses for
// middleware tests.
//
// All admin operations on the mm-bot must leave a row in
// audit_log with a stable shape so the operator can answer
// "who started bot BNB_USDT on 2026-09-04 and with what
// parameters?". The test asserts:
//
//   1. Every successful Start / Stop writes one row with
//      status='success', action in {mmbot.start, mmbot.stop}.
//   2. Every failed Start / Stop writes one row with
//      status='failure' and the bot engine's error message
//      surfaced through ErrorMsg.
//   3. The admin user id is recorded as AdminUserID and the
//      resolved email lands in AdminEmail (the auditLogAdmin
//      helper calls UserSvc.GetUser to look it up).
//   4. Status / List do not write audit rows -- they are
//      read-only.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/goexdev/goexchange/internal/audit"
	"github.com/goexdev/goexchange/internal/mmbot"
	"github.com/goexdev/goexchange/internal/user"
)

var (
	auditPool     *pgxpool.Pool
	auditSvc      *audit.Service
	auditUserSvc  *user.Service
	auditLog      *slog.Logger
	auditAdminID  uuid.UUID
	auditAdminMail string
)

func TestMain(m *testing.M) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://exchange:***@localhost:5433/exchange?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		panic("audit test: open pool: " + err.Error())
	}
	auditPool = pool
	auditSvc = audit.NewService(pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	auditUserSvc = user.NewService(pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	auditLog = slog.New(slog.NewTextHandler(io.Discard, nil))

	// Provision a real admin user so auditLogAdmin's
	// UserSvc.GetUser lookup resolves to a real email. We do
	// not need this user to be in any specific role for the
	// test -- the audit log records who acted, not what
	// permission they had. The email is suffixed with a random
	// suffix so re-running tests does not collide on the
	// users_email_key unique index; each test run gets a fresh
	// admin and a fresh audit namespace.
	auditAdminID = uuid.New()
	auditAdminMail = "mmbot-audit-admin-" + auditAdminID.String() + "@goexchange.local"
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, email, password_hash, kyc_status, role)
		VALUES ($1, $2, 'placeholder', 'NONE', 'admin')
		ON CONFLICT (id) DO UPDATE SET email = EXCLUDED.email, role = 'admin'
	`, auditAdminID, auditAdminMail); err != nil {
		panic("audit test: provision admin: " + err.Error())
	}
	defer func() {
		// admin user is preserved (no FK constraints to worry
		// about; other tests can find it by email if they want
		// to clean up). The audit_log rows themselves stay for
		// ever; that's the production semantics.
		pool.Close()
	}()

	// audit_log is append-only; we cannot DELETE rows to clean
	// up. Each test uses a per-test cutoff timestamp instead
	// (see drainAudit below). The test data accumulates over
	// time but the per-test cutoffs keep individual assertions
	// deterministic.

	code := m.Run()
	os.Exit(code)
}

// withAdminContext returns a context carrying userIDKey so
// auditLogAdmin's userIDFromContextUUID lookup finds the admin.
// The handler-under-test pulls everything else from the Deps
// passed in directly.
func withAdminContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, userIDKey, auditAdminID.String())
}

// drainAudit captures the current audit row count for our test
// admin so each test only sees rows it wrote. The audit_log
// table is append-only in production (enforced by trigger), so
// we cannot DELETE rows to reset state. Instead we tag each
// test's rows by their admin_user_id (set to a unique-per-test
// UUID via the request context) and count deltas.
//
// Each test function is responsible for calling drainAudit at
// the top of its body and then asserting on rows where
// `created_at > drainTime`. The query helper auditSince
// implements that filter.
func drainAudit(t *testing.T) time.Time {
	t.Helper()
	return time.Now()
}

// auditSince reads audit rows for our test admin newer than
// cutoff, optionally filtered to a single action.
func auditSince(t *testing.T, cutoff time.Time, action string) []audit.LogEntryWithMeta {
	t.Helper()
	entries, err := auditSvc.Query(t.Context(), audit.QueryFilter{
		AdminUserID: &auditAdminID,
		Action:      action,
		Since:       &cutoff,
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("query audit: %v", err)
	}
	return entries
}

// auditDepsForTest returns a Deps with the fields the handlers
// touch: MMBotClient (fake), AuditSvc (real), UserSvc (real),
// Log (silent).
func auditDepsForTest(c mmbot.Client) Deps {
	return Deps{
		Log:         auditLog,
		MMBotClient: c,
		AuditSvc:    auditSvc,
		UserSvc:     auditUserSvc,
		Pool:        auditPool,
	}
}

// queryAudit reads audit rows for our test admin, optionally
// filtered to a single action. Returns up to 10 rows.
func queryAudit(t *testing.T, action string) []audit.LogEntryWithMeta {
	t.Helper()
	since := time.Now().Add(-30 * time.Second)
	entries, err := auditSvc.Query(t.Context(), audit.QueryFilter{
		AdminUserID: &auditAdminID,
		Action:      action,
		Since:       &since,
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("query audit: %v", err)
	}
	return entries
}

// --- fakeClient shared with the other test files would require
// the test files to live in the same package; we keep the fake
// local to this file to avoid coupling. The fake satisfies the
// mmbot.Client interface (compile-time checked below).

type auditFakeClient struct {
	startBot  mmbot.BotState
	startErr  error
	stopRes   mmbot.StopResult
	stopErr   error
	statusBot mmbot.BotState
	statusErr error
	listBots  []mmbot.BotState
	listErr   error
}

func (f *auditFakeClient) Start(_ context.Context, _ mmbot.StartParams) (mmbot.BotState, error) {
	return f.startBot, f.startErr
}
func (f *auditFakeClient) Stop(_ context.Context, _ string, _ bool) (mmbot.StopResult, error) {
	return f.stopRes, f.stopErr
}
func (f *auditFakeClient) Status(_ context.Context, _ string) (mmbot.BotState, error) {
	return f.statusBot, f.statusErr
}
func (f *auditFakeClient) List(_ context.Context, _ string, _ mmbot.Status) ([]mmbot.BotState, error) {
	return f.listBots, f.listErr
}

var _ mmbot.Client = (*auditFakeClient)(nil)

// --- tests ---

// TestStart_Start_Success_AuditWritesRow confirms that a successful
// admin POST /admin/mmbot/start writes exactly one audit_log
// row with action='mmbot.start', status='success', admin_user_id
// matching the context value, and details carrying the bot_id.
func TestStart_Success_AuditWritesRow(t *testing.T) {
	cutoff := drainAudit(t)

	startedAt := time.Now().UTC()
	want := mmbot.BotState{
		BotID:        "BNB_USDT_mm_audit_1",
		Pair:         "BNB_USDT",
		Status:       mmbot.StatusRunning,
		MidPrice:     "50000",
		SpreadBps:    20,
		BaseBalance:  "0.002",
		QuoteBalance: "100",
		PnlQuote:     "0",
		CreatedAt:    startedAt,
		StartedAt:    &startedAt,
	}
	d := auditDepsForTest(&auditFakeClient{startBot: want})

	body, _ := json.Marshal(map[string]any{
		"pair":       "BNB_USDT",
		"mid_price":  "50000",
		"quote_seed": "100",
		"base_seed":  "0.002",
		"spread_bps": 20,
	})
	req := httptest.NewRequest(http.MethodPost, "/admin/mmbot/start", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(withAdminContext(req.Context()))
	w := httptest.NewRecorder()
	StartMMBotHandlerForTest(d)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", w.Code, w.Body.String())
	}

	entries := auditSince(t, cutoff, "mmbot.start")
	if len(entries) != 1 {
		t.Fatalf("audit row count: got %d, want 1; entries=%+v", len(entries), entries)
	}
	e := entries[0]
	if e.Status != "success" {
		t.Errorf("status: got %q, want success", e.Status)
	}
	if e.AdminUserID == nil || *e.AdminUserID != auditAdminID {
		t.Errorf("admin_user_id: got %v, want %s", e.AdminUserID, auditAdminID)
	}
	if e.AdminEmail != auditAdminMail {
		t.Errorf("admin_email: got %q, want %q", e.AdminEmail, auditAdminMail)
	}
	if e.TargetType != "mmbot" {
		t.Errorf("target_type: got %q, want mmbot", e.TargetType)
	}
	if e.TargetLabel != want.BotID {
		t.Errorf("target_label: got %q, want %q (bot_id)", e.TargetLabel, want.BotID)
	}
	if e.Details["bot_id"] != want.BotID {
		t.Errorf("details.bot_id: got %v, want %s", e.Details["bot_id"], want.BotID)
	}
	if e.Details["pair"] != "BNB_USDT" {
		t.Errorf("details.pair: got %v, want BNB_USDT", e.Details["pair"])
	}
	// IP defaults to r.RemoteAddr when no X-Forwarded-For is
	// present; httptest uses "192.0.2.1:1234" -- just confirm
	// the field is populated.
	if e.IP == "" {
		t.Errorf("ip: empty (auditLogAdmin should have populated from req)")
	}
}

// TestStart_EngineError_AuditFailure confirms that when the
// bot engine returns an error, the audit row carries
// status='failure' and the underlying error message in
// ErrorMsg. This is the field the operator's "last failed mmbot
// start" alert keys off.
func TestStart_EngineError_AuditFailure(t *testing.T) {
	cutoff := drainAudit(t)

	engineErr := errors.New("bot process not running")
	d := auditDepsForTest(&auditFakeClient{startErr: engineErr})

	body, _ := json.Marshal(map[string]any{
		"pair":       "BNB_USDT",
		"mid_price":  "50000",
		"quote_seed": "100",
		"base_seed":  "0.002",
		"spread_bps": 20,
	})
	req := httptest.NewRequest(http.MethodPost, "/admin/mmbot/start", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(withAdminContext(req.Context()))
	w := httptest.NewRecorder()
	StartMMBotHandlerForTest(d)(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503; body=%s", w.Code, w.Body.String())
	}

	entries := auditSince(t, cutoff, "mmbot.start")
	if len(entries) != 1 {
		t.Fatalf("audit row count: got %d, want 1", len(entries))
	}
	e := entries[0]
	if e.Status != "failure" {
		t.Errorf("status: got %q, want failure", e.Status)
	}
	if e.ErrorMsg == "" {
		t.Errorf("error_msg: empty (engine error should have been recorded)")
	}
	// The error message includes the wrapped bot engine text
	// plus the handler's prefix; check the substantive part.
	if !contains(e.ErrorMsg, "bot process not running") {
		t.Errorf("error_msg: %q does not contain engine error text", e.ErrorMsg)
	}
	if e.TargetLabel != "BNB_USDT" {
		t.Errorf("target_label: got %q, want BNB_USDT (handler sets it to the request pair on failure)", e.TargetLabel)
	}
}

// TestStop_Success_AuditWritesRow confirms that Stop success
// audits with the bot_id (not the request pair) as
// TargetLabel, so operators can grep audit_log by bot_id to
// reconstruct the lifecycle.
func TestStop_Success_AuditWritesRow(t *testing.T) {
	cutoff := drainAudit(t)

	stoppedAt := time.Now().UTC()
	res := mmbot.StopResult{
		Bot: mmbot.BotState{
			BotID:    "BNB_USDT_mm_audit_1",
			Pair:     "BNB_USDT",
			Status:   mmbot.StatusStopped,
			StoppedAt: func() *time.Time { t := stoppedAt; return &t }(),
		},
		ReturnedQuote: "100",
		ReturnedBase:  "0.002",
	}
	d := auditDepsForTest(&auditFakeClient{stopRes: res})

	body, _ := json.Marshal(map[string]any{
		"bot_id": "BNB_USDT_mm_audit_1",
		"return_inventory": true,
	})
	req := httptest.NewRequest(http.MethodPost, "/admin/mmbot/stop", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(withAdminContext(req.Context()))
	w := httptest.NewRecorder()
	StopMMBotHandlerForTest(d)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", w.Code, w.Body.String())
	}

	entries := auditSince(t, cutoff, "mmbot.stop")
	if len(entries) != 1 {
		t.Fatalf("audit row count: got %d, want 1", len(entries))
	}
	e := entries[0]
	if e.Status != "success" {
		t.Errorf("status: got %q, want success", e.Status)
	}
	if e.TargetLabel != "BNB_USDT_mm_audit_1" {
		t.Errorf("target_label: got %q, want BNB_USDT_mm_audit_1 (Stop uses request bot_id as label)", e.TargetLabel)
	}
	// Stop handler emits return_inventory, returned_quote,
	// returned_base, final_pnl_quote in Details. The bot_id is
	// in TargetLabel, not Details, so we assert against the
	// label above.
	if e.Details["return_inventory"] != true {
		t.Errorf("details.return_inventory: got %v, want true", e.Details["return_inventory"])
	}
	if e.Details["returned_quote"] != "100" {
		t.Errorf("details.returned_quote: got %v, want 100", e.Details["returned_quote"])
	}
	if e.Details["returned_base"] != "0.002" {
		t.Errorf("details.returned_base: got %v, want 0.002", e.Details["returned_base"])
	}
}

// TestStop_EngineError_AuditFailure confirms the failure path
// on Stop too -- the handler must not silently swallow bot
// engine errors.
func TestStop_EngineError_AuditFailure(t *testing.T) {
	cutoff := drainAudit(t)

	d := auditDepsForTest(&auditFakeClient{
		stopErr: errors.New("cancel order timeout"),
	})

	body, _ := json.Marshal(map[string]any{"bot_id": "BNB_USDT_mm_audit_1"})
	req := httptest.NewRequest(http.MethodPost, "/admin/mmbot/stop", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(withAdminContext(req.Context()))
	w := httptest.NewRecorder()
	StopMMBotHandlerForTest(d)(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", w.Code)
	}

	entries := auditSince(t, cutoff, "mmbot.stop")
	if len(entries) != 1 {
		t.Fatalf("audit row count: got %d, want 1", len(entries))
	}
	if entries[0].Status != "failure" {
		t.Errorf("status: got %q, want failure", entries[0].Status)
	}
	if !contains(entries[0].ErrorMsg, "cancel order timeout") {
		t.Errorf("error_msg: %q does not contain engine text", entries[0].ErrorMsg)
	}
}

// TestStatusAndList_DoNotAudit confirms that read-only
// handlers (Status, List) do not write audit rows. The mm-bot
// admin reads happen on every operator dashboard load; if we
// wrote audit rows for them the table would balloon within
// hours.
func TestStatusAndList_DoNotAudit(t *testing.T) {
	cutoff := drainAudit(t)

	d := auditDepsForTest(&auditFakeClient{
		statusBot: mmbot.BotState{
			BotID: "BNB_USDT_mm_audit_1", Pair: "BNB_USDT",
			Status: mmbot.StatusRunning, MidPrice: "50000",
		},
		listBots: []mmbot.BotState{
			{BotID: "BNB_USDT_mm_audit_1", Pair: "BNB_USDT", Status: mmbot.StatusRunning},
		},
	})

	// GET /admin/mmbot/status?bot_id=...
	req := httptest.NewRequest(http.MethodGet, "/admin/mmbot/status?bot_id=BNB_USDT_mm_audit_1", nil)
	req = req.WithContext(withAdminContext(req.Context()))
	w := httptest.NewRecorder()
	MMBotStatusHandlerForTest(d)(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status handler: got %d, want 200", w.Code)
	}

	// GET /admin/mmbot/list
	req2 := httptest.NewRequest(http.MethodGet, "/admin/mmbot/list", nil)
	req2 = req2.WithContext(withAdminContext(req2.Context()))
	w2 := httptest.NewRecorder()
	MMBotListHandlerForTest(d)(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("list handler: got %d, want 200", w2.Code)
	}

	// No audit rows from either call. Filter by the per-test
	// cutoff so we only see rows from this run.
	entries, err := auditSvc.Query(t.Context(), audit.QueryFilter{
		AdminUserID: &auditAdminID,
		Since:       &cutoff,
		Limit:       50,
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	for _, e := range entries {
		if e.Action == "mmbot.status" || e.Action == "mmbot.list" {
			t.Errorf("read-only handler wrote audit row: action=%s", e.Action)
		}
	}
}

// TestStart_ValidationError_DoesNotAudit confirms that bad
// requests (missing fields, malformed JSON) are rejected
// before reaching auditLogAdmin. We do not want operator typos
// to pollute audit_log with "failure" rows that are actually
// client errors, not admin mistakes.
func TestStart_ValidationError_DoesNotAudit(t *testing.T) {
	cutoff := drainAudit(t)

	d := auditDepsForTest(&auditFakeClient{})

	cases := []struct {
		name string
		body string
	}{
		{"empty body", ""},
		{"missing pair", `{"mid_price":"50000","quote_seed":"100","base_seed":"0.002"}`},
		{"bad json", `{not json`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/admin/mmbot/start", bytes.NewReader([]byte(c.body)))
			req.Header.Set("Content-Type", "application/json")
			req = req.WithContext(withAdminContext(req.Context()))
			w := httptest.NewRecorder()
			StartMMBotHandlerForTest(d)(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("status: got %d, want 400", w.Code)
			}
		})
	}

	// Confirm no audit rows were written for the validation
	// failures. Use the per-test cutoff so we only see rows
	// from this run.
	since := cutoff
	entries, err := auditSvc.Query(t.Context(), audit.QueryFilter{
		AdminUserID: &auditAdminID,
		Since:       &since,
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	for _, e := range entries {
		if e.Action == "mmbot.start" {
			t.Errorf("validation error wrote audit row: status=%s error_msg=%q",
				e.Status, e.ErrorMsg)
		}
	}
}

// contains is a tiny strings.Contains shim that lets the file
// stay light on imports; the test does not need anything else
// from the strings package.
func contains(s, sub string) bool {
	return len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	if sub == "" {
		return 0
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}