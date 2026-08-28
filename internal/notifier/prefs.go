package notifier

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Prefs represents a user's notification preferences.
type Prefs struct {
	UserID                uuid.UUID `json:"-"` // omitted — leaks account id (NEW-M4 from the 2026-08-28 v0.3 audit)
	Notify2FAEnabled      bool      `json:"notify_2fa_enabled"`
	Notify2FADisabled     bool      `json:"notify_2fa_disabled"`
	Notify2FABackupUsed   bool      `json:"notify_2fa_backup_used"`
	Notify2FAFailed       bool      `json:"notify_2fa_failed"`
	Notify2FALoginSuccess bool      `json:"notify_2fa_login_success"`
	NotifyLogin           bool      `json:"notify_login"`
	NotifyWithdrawal      bool      `json:"notify_withdrawal"`
	NotifyLargeWithdraw   bool      `json:"notify_large_withdraw"`

	// Email preferences (separate from in-app)
	Email2FAEnabled      bool `json:"email_2fa_enabled"`
	Email2FADisabled     bool `json:"email_2fa_disabled"`
	Email2FABackupUsed   bool `json:"email_2fa_backup_used"`
	Email2FAFailed       bool `json:"email_2fa_failed"`
	Email2FALoginSuccess bool `json:"email_2fa_login_success"`
	EmailLogin           bool `json:"email_login"`
	EmailWithdrawal      bool `json:"email_withdrawal"`
	EmailLargeWithdraw   bool `json:"email_large_withdraw"`

	UpdatedAt time.Time `json:"updated_at"`
}

// PrefsService manages user notification preferences.
type PrefsService struct {
	pool *pgxpool.Pool
}

// NewPrefsService creates a new preferences service.
func NewPrefsService(pool *pgxpool.Pool) *PrefsService {
	return &PrefsService{pool: pool}
}

// ErrPrefsNotFound is returned when no preferences exist for a user.
var ErrPrefsNotFound = errors.New("notification preferences not found")

// Get retrieves preferences for a user. Creates default if not exists.
func (s *PrefsService) Get(ctx context.Context, userID uuid.UUID) (*Prefs, error) {
	p := &Prefs{}
	err := s.pool.QueryRow(ctx,
		`SELECT user_id, notify_2fa_enabled, notify_2fa_disabled,
		        notify_2fa_backup_used, notify_2fa_failed,
		        notify_2fa_login_success, notify_login,
		        notify_withdrawal, notify_large_withdraw,
		        email_2fa_enabled, email_2fa_disabled,
		        email_2fa_backup_used, email_2fa_failed,
		        email_2fa_login_success, email_login,
		        email_withdrawal, email_large_withdraw, updated_at
		 FROM user_notification_prefs WHERE user_id = $1`,
		userID).Scan(
		&p.UserID, &p.Notify2FAEnabled, &p.Notify2FADisabled,
		&p.Notify2FABackupUsed, &p.Notify2FAFailed,
		&p.Notify2FALoginSuccess, &p.NotifyLogin,
		&p.NotifyWithdrawal, &p.NotifyLargeWithdraw,
		&p.Email2FAEnabled, &p.Email2FADisabled,
		&p.Email2FABackupUsed, &p.Email2FAFailed,
		&p.Email2FALoginSuccess, &p.EmailLogin,
		&p.EmailWithdrawal, &p.EmailLargeWithdraw,
		&p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Auto-create with defaults
			if _, err := s.pool.Exec(ctx,
				`INSERT INTO user_notification_prefs (user_id) VALUES ($1)
				 ON CONFLICT (user_id) DO NOTHING`,
				userID); err != nil {
				return nil, err
			}
			// Retry read
			return s.Get(ctx, userID)
		}
		return nil, err
	}
	return p, nil
}

// Update modifies user preferences (only non-zero fields).
func (s *PrefsService) Update(ctx context.Context, userID uuid.UUID, updates map[string]bool) (*Prefs, error) {
	// First ensure prefs exist
	if _, err := s.Get(ctx, userID); err != nil {
		return nil, err
	}

	// Build dynamic update
	setClauses := []string{}
	args := []interface{}{}
	argIdx := 1
	for field, val := range updates {
		setClauses = append(setClauses, field+" = $"+itoa(argIdx))
		args = append(args, val)
		argIdx++
	}
	if len(setClauses) == 0 {
		return s.Get(ctx, userID)
	}
	setClauses = append(setClauses, "updated_at = NOW()")
	args = append(args, userID)

	query := "UPDATE user_notification_prefs SET "
	for i, clause := range setClauses {
		if i > 0 {
			query += ", "
		}
		query += clause
	}
	query += " WHERE user_id = $" + itoa(argIdx)

	if _, err := s.pool.Exec(ctx, query, args...); err != nil {
		return nil, err
	}
	return s.Get(ctx, userID)
}

// ShouldSend checks if a notification type should be sent to a user.
// Returns true if user has preference enabled or if no preference exists (default).
func (s *PrefsService) ShouldSend(ctx context.Context, userID uuid.UUID, notificationType string) bool {
	prefs, err := s.Get(ctx, userID)
	if err != nil {
		// Default: send notifications on error (fail-open for availability)
		return true
	}

	switch notificationType {
	case Type2FAEnabled:
		return prefs.Notify2FAEnabled
	case Type2FADisabled:
		return prefs.Notify2FADisabled
	case Type2FABackupUsed:
		return prefs.Notify2FABackupUsed
	case Type2FAFailed:
		return prefs.Notify2FAFailed
	case Type2FALoginSuccess:
		return prefs.Notify2FALoginSuccess
	case TypeWithdrawalDone:
		return prefs.NotifyWithdrawal
	case TypeLargeWithdraw:
		return prefs.NotifyLargeWithdraw
	default:
		return true
	}
}

// ShouldSendEmail checks if the user wants email for the given notification type.
// Returns true by default (no preference or fail-open).
func (s *PrefsService) ShouldSendEmail(ctx context.Context, userID uuid.UUID, notificationType string) bool {
	prefs, err := s.Get(ctx, userID)
	if err != nil {
		return true
	}

	switch notificationType {
	case Type2FAEnabled:
		return prefs.Email2FAEnabled
	case Type2FADisabled:
		return prefs.Email2FADisabled
	case Type2FABackupUsed:
		return prefs.Email2FABackupUsed
	case Type2FAFailed:
		return prefs.Email2FAFailed
	case Type2FALoginSuccess:
		return prefs.Email2FALoginSuccess
	case TypeWithdrawalDone:
		return prefs.EmailWithdrawal
	case TypeLargeWithdraw:
		return prefs.EmailLargeWithdraw
	default:
		return true
	}
}

// itoa is a simple int to string conversion without importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := "0123456789"
	result := ""
	for n > 0 {
		result = string(digits[n%10]) + result
		n /= 10
	}
	return result
}
