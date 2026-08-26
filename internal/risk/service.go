// Package risk provides risk scoring and control for user actions.
//
// Risk factors (each contributes points):
// - Failed login attempts in last 1h: +5 each (max 30)
// - New IP not seen before: +15
// - Account age < 24h: +10
// - Account age < 7 days: +5
// - Withdraw amount > 50% of total deposits: +20
// - Withdraw to NEW address: +10
// - Time of withdrawal (2-6am UTC): +10
//
// Score thresholds:
// - 0-30:  ALLOW (auto-approve)
// - 31-60: HOLD (manual review)
// - 61+:   BLOCK (auto-reject)
package risk

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

const (
	ActionAllow = "ALLOW"
	ActionHold  = "HOLD"
	ActionBlock = "BLOCK"
)

// Score represents a risk assessment.
type Score struct {
	Score   int
	Factors map[string]int
	Action  string
}

// Service provides risk scoring.
type Service struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

func New(pool *pgxpool.Pool, log *slog.Logger) *Service {
	return &Service{pool: pool, log: log}
}

// RecordLogin records a login attempt.
func (s *Service) RecordLogin(ctx context.Context, email string, userID *uuid.UUID, ip, userAgent string, success bool, failureReason string) error {
	const q = `
		INSERT INTO login_attempts (email, user_id, ip, user_agent, success, failure_reason)
		VALUES ($1, $2, $3::inet, $4, $5, $6)
	`
	var uid interface{}
	if userID != nil {
		uid = *userID
	}
	_, err := s.pool.Exec(ctx, q, email, uid, ip, userAgent, success, failureReason)
	return err
}

// RecordKnownIP records a known IP for user.
func (s *Service) RecordKnownIP(ctx context.Context, userID uuid.UUID, ip string) error {
	const q = `
		INSERT INTO user_known_ips (user_id, ip, login_count)
		VALUES ($1, $2, 1)
		ON CONFLICT (user_id, ip) DO UPDATE
		SET last_seen = NOW(), login_count = user_known_ips.login_count + 1
	`
	_, err := s.pool.Exec(ctx, q, userID, ip)
	return err
}

// IsKnownIP checks if IP is in user_known_ips.
func (s *Service) IsKnownIP(ctx context.Context, userID uuid.UUID, ip string) (bool, error) {
	var count int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM user_known_ips WHERE user_id = $1 AND ip = $2::inet`, userID, ip).Scan(&count)
	return count > 0, err
}

// CountFailedLogins in last N hours.
func (s *Service) CountFailedLogins(ctx context.Context, email string, hours int) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM login_attempts
		WHERE email = $1 AND success = false AND timestamp > NOW() - ($2 || ' hours')::interval
	`, email, fmt.Sprintf("%d", hours)).Scan(&count)
	return count, err
}

// ComputeLoginScore returns risk score for login.
func (s *Service) ComputeLoginScore(ctx context.Context, userID uuid.UUID, email, ip string) (*Score, error) {
	factors := map[string]int{}

	failed, err := s.CountFailedLogins(ctx, email, 1)
	if err != nil {
		return nil, err
	}
	if failed > 0 {
		pts := failed * 5
		if pts > 30 {
			pts = 30
		}
		factors["failed_logins_1h"] = pts
	}

	known, err := s.IsKnownIP(ctx, userID, ip)
	if err != nil {
		return nil, err
	}
	if !known {
		factors["new_ip"] = 15
	}

	var createdAt time.Time
	err = s.pool.QueryRow(ctx, `SELECT created_at FROM users WHERE id = $1`, userID).Scan(&createdAt)
	if err != nil {
		return nil, err
	}
	age := time.Since(createdAt)
	if age < 24*time.Hour {
		factors["account_age_lt_24h"] = 10
	} else if age < 7*24*time.Hour {
		factors["account_age_lt_7d"] = 5
	}

	score := 0
	for _, pts := range factors {
		score += pts
	}
	action := ActionForScore(score)
	return &Score{Score: score, Factors: factors, Action: action}, nil
}

// ComputeWithdrawScore returns risk score for withdrawal.
func (s *Service) ComputeWithdrawScore(ctx context.Context, userID uuid.UUID, amount decimal.Decimal, destAddress string) (*Score, error) {
	factors := map[string]int{}

	var email string
	_ = s.pool.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, userID).Scan(&email)
	failed, err := s.CountFailedLogins(ctx, email, 24)
	if err != nil {
		return nil, err
	}
	if failed > 0 {
		pts := failed * 3
		if pts > 25 {
			pts = 25
		}
		factors["failed_logins_24h"] = pts
	}

	var createdAt time.Time
	_ = s.pool.QueryRow(ctx, `SELECT created_at FROM users WHERE id = $1`, userID).Scan(&createdAt)
	age := time.Since(createdAt)
	if age < 24*time.Hour {
		factors["account_age_lt_24h"] = 15
	} else if age < 7*24*time.Hour {
		factors["account_age_lt_7d"] = 8
	}

	var totalDeposits decimal.Decimal
	_ = s.pool.QueryRow(ctx, `SELECT COALESCE(SUM(amount), 0) FROM deposits WHERE user_id = $1 AND status = 'DONE'`, userID).Scan(&totalDeposits)
	if totalDeposits.IsPositive() {
		ratio := amount.Div(totalDeposits)
		if ratio.GreaterThan(decimal.NewFromFloat(0.5)) {
			factors["amount_ratio_gt_50pct"] = 20
		} else if ratio.GreaterThan(decimal.NewFromFloat(0.2)) {
			factors["amount_ratio_gt_20pct"] = 10
		}
	}

	var sentBefore int
	_ = s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM withdrawals WHERE user_id = $1 AND dest_address = $2 AND status IN ('DONE', 'BROADCAST')`, userID, destAddress).Scan(&sentBefore)
	if sentBefore == 0 {
		factors["new_destination"] = 10
	}

	hour := time.Now().UTC().Hour()
	if hour >= 2 && hour < 6 {
		factors["unusual_hour"] = 10
	}

	score := 0
	for _, pts := range factors {
		score += pts
	}
	action := ActionForScore(score)
	return &Score{Score: score, Factors: factors, Action: action}, nil
}

// RecordEvent records a risk event.
func (s *Service) RecordEvent(ctx context.Context, userID uuid.UUID, eventType string, score *Score, contextMap map[string]interface{}) error {
	factorsJSON, _ := json.Marshal(score.Factors)
	contextJSON, _ := json.Marshal(contextMap)
	const q = `
		INSERT INTO risk_events (user_id, event_type, risk_score, factors, action, context)
		VALUES ($1, $2, $3, $4::jsonb, $5, $6::jsonb)
	`
	_, err := s.pool.Exec(ctx, q, userID, eventType, score.Score, factorsJSON, score.Action, contextJSON)
	return err
}

// GetUserRiskScore returns user's latest risk score.
func (s *Service) GetUserRiskScore(ctx context.Context, userID uuid.UUID) (int, error) {
	var score int
	err := s.pool.QueryRow(ctx, `SELECT last_risk_score FROM users WHERE id = $1`, userID).Scan(&score)
	return score, err
}

// UpdateUserRiskScore updates user's risk score.
func (s *Service) UpdateUserRiskScore(ctx context.Context, userID uuid.UUID, score int) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET last_risk_score = $2, risk_score_updated_at = NOW() WHERE id = $1`, userID, score)
	return err
}

// ListRiskEvents returns recent risk events.
func (s *Service) ListRiskEvents(ctx context.Context, limit int) ([]RiskEvent, error) {
	const q = `
		SELECT id, user_id, event_type, risk_score, factors, action, context, created_at
		FROM risk_events ORDER BY created_at DESC LIMIT $1
	`
	rows, err := s.pool.Query(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RiskEvent{}
	for rows.Next() {
		var e RiskEvent
		var factors, context []byte
		if err := rows.Scan(&e.ID, &e.UserID, &e.EventType, &e.Score, &factors, &e.Action, &context, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.Factors = string(factors)
		e.Context = string(context)
		out = append(out, e)
	}
	return out, nil
}

// RiskEvent is an audit event.
type RiskEvent struct {
	ID        uuid.UUID `json:"id"`
	UserID    uuid.UUID `json:"user_id"`
	EventType string    `json:"event_type"`
	Score     int       `json:"risk_score"`
	Factors   string    `json:"factors"`
	Action    string    `json:"action"`
	Context   string    `json:"context"`
	CreatedAt time.Time `json:"created_at"`
}

// ActionForScore returns the action for a given score.
func ActionForScore(score int) string {
	switch {
	case score <= 30:
		return ActionAllow
	case score <= 60:
		return ActionHold
	default:
		return ActionBlock
	}
}
