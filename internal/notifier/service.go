// Package notifier handles email notifications + in-app notifications.
package notifier

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Notification type constants.
const (
	TypeKYCApproved     = "KYC_APPROVED"
	TypeKYCRejected     = "KYC_REJECTED"
	TypeWithdrawalHeld  = "WITHDRAWAL_HELD"
	TypeWithdrawalDone  = "WITHDRAWAL_DONE"
	TypeLargeWithdraw   = "LARGE_WITHDRAW"
	TypeLoginRisk       = "LOGIN_RISK"

	// 2FA notification types
	Type2FAEnabled    = "2FA_ENABLED"      // User enabled 2FA
	Type2FADisabled   = "2FA_DISABLED"     // User disabled 2FA
	Type2FABackupUsed = "2FA_BACKUP_USED"  // Backup code used for login
	Type2FAFailed     = "2FA_FAILED"
	Type2FALoginSuccess = "2FA_LOGIN_SUCCESS"       // Failed 2FA attempt
)

// Service is the notification service.
type Service struct {
	pool        *pgxpool.Pool
	provider    EmailProvider
	fromAddr    string
	log         *slog.Logger
	subscribers map[uuid.UUID]map[*chan Notification]struct{} // userID -> set of channel pointers
	subMu       sync.RWMutex
}

// NewService creates a new notifier service with a custom provider.
func NewService(pool *pgxpool.Pool, provider EmailProvider, fromAddr string, log *slog.Logger) *Service {
	return &Service{
		pool:        pool,
		provider:    provider,
		fromAddr:    fromAddr,
		log:         log,
		subscribers: make(map[uuid.UUID]map[*chan Notification]struct{}),
	}
}

// NotificationEvent is sent to WebSocket subscribers when a notification is created.
type NotificationEvent struct {
	ID        uuid.UUID `json:"id"`
	Type      string    `json:"type"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

// Close releases provider resources.
func (s *Service) Close() error {
	return s.provider.Close()
}

// Notification represents an in-app notification.
type Notification struct {
	ID        uuid.UUID
	Type      string
	Title     string
	Body      string
	ReadAt    *time.Time
	CreatedAt time.Time
}

// Send queues an email in the outbox.
func (s *Service) Send(ctx context.Context, to, subject, body string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO email_outbox (to_email, subject, body) VALUES ($1, $2, $3)`,
		to, subject, body,
	)
	return err
}

// SendNotification persists an in-app notification and (optionally) queues email.
// Email is sent only if caller provides an email via SendNotificationWithEmail
// or if the in-app notification is sent without email preference check.
//
// RECOMMENDED: Use SendNotificationWithEmail(ctx, userID, ntype, title, body, metadata, prefsSvc)
// which automatically respects email preferences.
func (s *Service) SendNotification(ctx context.Context, userID uuid.UUID, ntype, title, body string, metadata map[string]any) error {
	return s.sendNotificationInternal(ctx, userID, ntype, title, body, metadata, false)
}

// SendNotificationWithEmail sends in-app + email (if user preference allows).
// This is the preferred method for new code.
func (s *Service) SendNotificationWithEmail(ctx context.Context, userID uuid.UUID, ntype, title, body string, metadata map[string]any, prefsSvc *PrefsService) error {
	return s.sendNotificationInternal(ctx, userID, ntype, title, body, metadata, true, prefsSvc)
}

func (s *Service) sendNotificationInternal(ctx context.Context, userID uuid.UUID, ntype, title, body string, metadata map[string]any, sendEmail bool, prefsSvc ...*PrefsService) error {
	// 1. Insert in-app notification
	metaJSON := []byte("{}")
	if metadata != nil {
		b, err := json.Marshal(metadata)
		if err == nil {
			metaJSON = b
		}
	}
	if s.pool == nil {
		return fmt.Errorf("notifier pool is nil")
	}
	var notifID uuid.UUID
	var createdAt time.Time
	err := s.pool.QueryRow(ctx,
		`INSERT INTO notifications (user_id, type, title, body, metadata) VALUES ($1, $2, $3, $4, $5::jsonb)
		 RETURNING id, created_at`,
		userID, ntype, title, body, string(metaJSON),
	).Scan(&notifID, &createdAt)
	if err != nil {
		return fmt.Errorf("insert notification: %w", err)
	}

	// 1b. Publish to WebSocket subscribers (real-time push)
	s.publishNotification(userID, Notification{
		ID:        notifID,
		Type:      ntype,
		Title:     title,
		Body:      body,
		CreatedAt: createdAt,
	})

	// 2. Email (if requested and user has preference enabled)
	if !sendEmail {
		return nil
	}
	var ps *PrefsService
	if len(prefsSvc) > 0 {
		ps = prefsSvc[0]
	}
	if ps != nil && !ps.ShouldSendEmail(ctx, userID, ntype) {
		s.log.Debug("email notification skipped (user disabled)", "user_id", userID, "type", ntype)
		return nil
	}

	var email string
	err = s.pool.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, userID).Scan(&email)
	if err != nil {
		return fmt.Errorf("lookup user: %w", err)
	}
	return s.Send(ctx, email, title, body)
}

// ListForUser returns notifications for a user (newest first).
func (s *Service) ListForUser(ctx context.Context, userID uuid.UUID, limit int) ([]Notification, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, type, title, body, read_at, created_at
		 FROM notifications
		 WHERE user_id = $1
		 ORDER BY created_at DESC
		 LIMIT $2`,
		userID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Notification
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.Type, &n.Title, &n.Body, &n.ReadAt, &n.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// MarkRead marks a notification as read.
func (s *Service) MarkRead(ctx context.Context, userID, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE notifications SET read_at = NOW() WHERE id = $1 AND user_id = $2 AND read_at IS NULL`,
		id, userID,
	)
	return err
}

// MarkAllRead marks all of the user's unread notifications as read.
// Returns the number of notifications marked as read.
func (s *Service) MarkAllRead(ctx context.Context, userID uuid.UUID) (int64, error) {
	result, err := s.pool.Exec(ctx,
		`UPDATE notifications SET read_at = NOW() WHERE user_id = $1 AND read_at IS NULL`,
		userID,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

// ProcessOutbox runs one batch of the outbox.
func (s *Service) ProcessOutbox(ctx context.Context, batch int) (int, error) {
	if batch <= 0 || batch > 50 {
		batch = 10
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, to_email, subject, body, attempts FROM email_outbox
		 WHERE status = 'PENDING' AND attempts < 10
		 ORDER BY created_at ASC LIMIT $1
		 FOR UPDATE SKIP LOCKED`,
		batch,
	)
	if err != nil {
		return 0, err
	}
	type job struct {
		id             uuid.UUID
		to, subj, body string
		attempts       int
	}
	var jobs []job
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.id, &j.to, &j.subj, &j.body, &j.attempts); err != nil {
			rows.Close()
			return 0, err
		}
		jobs = append(jobs, j)
	}
	rows.Close()

	sent := 0
	for _, j := range jobs {
		err := s.deliver(ctx, j.to, j.subj, j.body)
		if err == nil {
			_, _ = s.pool.Exec(ctx, `UPDATE email_outbox SET status = 'SENT', sent_at = NOW(), attempts = attempts + 1 WHERE id = $1`, j.id)
			sent++
		} else {
			_, _ = s.pool.Exec(ctx, `UPDATE email_outbox SET status = 'PENDING', attempts = attempts + 1, last_error = $2 WHERE id = $1`, j.id, err.Error())
			s.log.Warn("email send failed", "id", j.id, "attempts", j.attempts+1, "error", err)
		}
	}
	return sent, nil
}

// deliver sends a single email via the configured provider.
func (s *Service) deliver(ctx context.Context, to, subject, body string) error {
	msg := EmailMessage{
		From:    s.fromAddr,
		To:      to,
		Subject: subject,
		Body:    body,
	}
	s.log.Info("delivering email",
		"provider", s.provider.Name(),
		"to", to,
		"subject", subject,
	)
	return s.provider.Send(ctx, msg)
}

// RunWorker runs the outbox worker in a loop. Cancel ctx to stop.
func (s *Service) RunWorker(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	s.log.Info("notifier worker started", "interval", interval.String(), "provider", s.provider.Name())
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n, err := s.ProcessOutbox(ctx, 20)
			if err != nil {
				s.log.Error("outbox processing failed", "error", err)
				continue
			}
			if n > 0 {
				s.log.Info("emails sent", "count", n, "provider", s.provider.Name())
			}
		}
	}
}


// Subscribe returns a channel that receives all notifications for the given user.
// Caller MUST call Unsubscribe to release resources.
func (s *Service) Subscribe(userID uuid.UUID) <-chan Notification {
	ch := make(chan Notification, 16)
	chPtr := &ch
	s.subMu.Lock()
	if _, ok := s.subscribers[userID]; !ok {
		s.subscribers[userID] = make(map[*chan Notification]struct{})
	}
	s.subscribers[userID][chPtr] = struct{}{}
	s.subMu.Unlock()
	return ch
}

// Unsubscribe removes a subscriber channel.
func (s *Service) Unsubscribe(userID uuid.UUID, ch <-chan Notification) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	if set, ok := s.subscribers[userID]; ok {
		// Find and remove by channel pointer comparison
		for chPtr := range set {
			if *chPtr == ch {
				delete(set, chPtr)
				break
			}
		}
		if len(set) == 0 {
			delete(s.subscribers, userID)
		}
	}
}

// publishNotification fans out a notification to all subscribers for that user.
func (s *Service) publishNotification(userID uuid.UUID, n Notification) {
	s.subMu.RLock()
	set, ok := s.subscribers[userID]
	s.subMu.RUnlock()
	if !ok {
		return
	}
	for chPtr := range set {
		select {
		case *chPtr <- n:
		default:
			// slow subscriber, drop
		}
	}
}
