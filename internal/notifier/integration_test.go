//go:build integration
// +build integration

package notifier

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) *pgxpool.Pool {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://exchange:exchange@localhost:5433/exchange?sslmode=disable"
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)

	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	require.NoError(t, err)

	return pool
}

func TestIntegration_SendNotification_InsertsRow(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	svc := NewService(pool, NewConsoleProvider(log), "test@goexchange.local", log)

	ctx := context.Background()

	// Register test user
	email := "notif-test-" + uuid.NewString()[:8] + "@test.local"
	var userID uuid.UUID
	err := pool.QueryRow(ctx,
		`INSERT INTO users (id, email, password_hash) VALUES (gen_random_uuid(), $1, $2) RETURNING id`,
		email, "$2a$10$dummy",
	).Scan(&userID)
	require.NoError(t, err)
	defer pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)

	// Send notification
	err = svc.SendNotification(ctx, userID, TypeKYCApproved,
		"Test KYC Approved",
		"Test notification body",
		map[string]any{"test": "value"})
	require.NoError(t, err)

	// Verify notification inserted
	var count int
	err = pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND type = $2`,
		userID, TypeKYCApproved).Scan(&count)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, 1)

	// Verify email queued
	err = pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM email_outbox WHERE to_email = $1`,
		email).Scan(&count)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, 1, "email should be queued")
}

func TestIntegration_ProcessOutbox_SendsPendingEmails(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	svc := NewService(pool, NewConsoleProvider(log), "test@goexchange.local", log) // empty cfg = console mode

	ctx := context.Background()

	// Insert test email
	testEmail := "outbox-test-" + uuid.NewString()[:8] + "@test.local"
	_, err := pool.Exec(ctx,
		`INSERT INTO email_outbox (to_email, subject, body) VALUES ($1, $2, $3)`,
		testEmail, "Test Subject", "Test Body")
	require.NoError(t, err)
	defer pool.Exec(ctx, `DELETE FROM email_outbox WHERE to_email = $1`, testEmail)

	// Process outbox
	sent, err := svc.ProcessOutbox(ctx, 10)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, sent, 1, "should send at least 1 email")

	// Verify status updated
	var status string
	err = pool.QueryRow(ctx,
		`SELECT status FROM email_outbox WHERE to_email = $1 LIMIT 1`,
		testEmail).Scan(&status)
	require.NoError(t, err)
	assert.Equal(t, "SENT", status)
}

func TestIntegration_ListForUser_ReturnsNewestFirst(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	svc := NewService(pool, NewConsoleProvider(log), "test@goexchange.local", log)

	ctx := context.Background()

	// Register test user
	email := "list-test-" + uuid.NewString()[:8] + "@test.local"
	var userID uuid.UUID
	err := pool.QueryRow(ctx,
		`INSERT INTO users (id, email, password_hash) VALUES (gen_random_uuid(), $1, $2) RETURNING id`,
		email, "$2a$10$dummy").Scan(&userID)
	require.NoError(t, err)
	defer pool.Exec(ctx, `DELETE FROM notifications WHERE user_id = $1`, userID)
	defer pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)

	// Insert 3 notifications
	for i := 0; i < 3; i++ {
		_, err = pool.Exec(ctx,
			`INSERT INTO notifications (user_id, type, title, body) VALUES ($1, $2, $3, $4)`,
			userID, "TEST", "Title "+string(rune('A'+i)), "Body")
		require.NoError(t, err)
		time.Sleep(10 * time.Millisecond)
	}

	notifs, err := svc.ListForUser(ctx, userID, 10)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(notifs), 3)
	assert.Equal(t, "Title C", notifs[0].Title)
}

func TestIntegration_MarkRead_UpdatesTimestamp(t *testing.T) {
	pool := setupTestDB(t)
	defer pool.Close()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	svc := NewService(pool, NewConsoleProvider(log), "test@goexchange.local", log)

	ctx := context.Background()

	email := "mark-test-" + uuid.NewString()[:8] + "@test.local"
	var userID uuid.UUID
	err := pool.QueryRow(ctx,
		`INSERT INTO users (id, email, password_hash) VALUES (gen_random_uuid(), $1, $2) RETURNING id`,
		email, "$2a$10$dummy").Scan(&userID)
	require.NoError(t, err)
	defer pool.Exec(ctx, `DELETE FROM notifications WHERE user_id = $1`, userID)
	defer pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)

	var notifID uuid.UUID
	err = pool.QueryRow(ctx,
		`INSERT INTO notifications (user_id, type, title, body) VALUES ($1, $2, $3, $4) RETURNING id`,
		userID, "TEST", "Title", "Body").Scan(&notifID)
	require.NoError(t, err)

	err = svc.MarkRead(ctx, userID, notifID)
	require.NoError(t, err)

	var readAt *time.Time
	err = pool.QueryRow(ctx,
		`SELECT read_at FROM notifications WHERE id = $1`, notifID).Scan(&readAt)
	require.NoError(t, err)
	assert.NotNil(t, readAt)
}

func TestIntegration_SMTP_DeliverToMailHog(t *testing.T) {
	if os.Getenv("TEST_SMTP") == "" {
		t.Skip("skipping SMTP test (set TEST_SMTP=1 to run)")
	}

	pool := setupTestDB(t)
	defer pool.Close()

	// Configure MailHog SMTP (localhost:1025)
	cfg := SMTPConfig{
		Host:     "127.0.0.1",
		Port:     1025,
		User:     "",
		Password: "",
		From:     "test@goexchange.local",
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	provider := NewSMTPProvider(cfg)
	svc := NewService(pool, provider, cfg.From, log)

	ctx := context.Background()

	// Queue a test email
	to := "smtp-recipient-" + uuid.NewString()[:8] + "@test.local"
	subject := "Test SMTP " + uuid.NewString()[:8]
	body := "Hello from goexchange SMTP test!"

	err := svc.Send(ctx, to, subject, body)
	require.NoError(t, err)
	defer pool.Exec(ctx, `DELETE FROM email_outbox WHERE to_email = $1`, to)

	// Process outbox - should send via SMTP
	sent, err := svc.ProcessOutbox(ctx, 10)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, sent, 1, "should have sent at least 1 email")

	// Verify status updated
	var status string
	err = pool.QueryRow(ctx,
		`SELECT status FROM email_outbox WHERE to_email = $1 LIMIT 1`, to).Scan(&status)
	require.NoError(t, err)
	assert.Equal(t, "SENT", status, "email should be SENT (MailHog accepted it)")
}
