package audit_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"
	"os"

	"github.com/goexdev/goexchange/internal/audit"
)

var (
	testPool *pgxpool.Pool
	testSvc  *audit.Service
)

func TestMain(m *testing.M) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://exchange:exchange@localhost:5433/exchange?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		panic(err)
	}
	testPool = pool
	defer pool.Close()
	testSvc = audit.NewService(pool, slog.New(slog.NewTextHandler(os.Stdout, nil)))
	m.Run()
}

func TestLogAndQuery(t *testing.T) {
	ctx := context.Background()
	adminID := uuid.New()
	// Cleanup before
	_, _ = testPool.Exec(ctx, "DELETE FROM audit_log WHERE admin_email = $1", "audit-test@goexchange.local")

	// Log a success
	testSvc.Log(ctx, audit.LogEntry{
		AdminUserID: &adminID,
		AdminEmail:  "audit-test@goexchange.local",
		Action:      "user.set_role",
		TargetType:  "user",
		TargetLabel: "test-target",
		Details:     map[string]any{"old_role": "user", "new_role": "admin"},
		IP:          "127.0.0.1",
		UserAgent:   "Test/1.0",
		Status:      "success",
	})

	// Log a failure
	testSvc.Log(ctx, audit.LogEntry{
		AdminUserID: &adminID,
		AdminEmail:  "audit-test@goexchange.local",
		Action:      "kyc.approve",
		TargetType:  "kyc",
		TargetLabel: "test-kyc",
		Status:      "failure",
		ErrorMsg:    "test error",
	})

	// Query
	since := time.Now().Add(-1 * time.Hour)
	entries, err := testSvc.Query(ctx, audit.QueryFilter{
		AdminUserID: &adminID,
		Since:       &since,
		Limit:       10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 2 {
		t.Fatalf("expected at least 2 entries, got %d", len(entries))
	}
	if entries[0].Action != "kyc.approve" {
		t.Errorf("expected kyc.approve first (newest), got %s", entries[0].Action)
	}
	if entries[0].Status != "failure" {
		t.Errorf("expected status failure, got %s", entries[0].Status)
	}
	if entries[0].ErrorMsg != "test error" {
		t.Errorf("expected error msg, got %s", entries[0].ErrorMsg)
	}

	// Cleanup
	_, _ = testPool.Exec(ctx, "DELETE FROM audit_log WHERE admin_email = $1", "audit-test@goexchange.local")
}
