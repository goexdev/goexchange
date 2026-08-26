// Package trigger manages conditional orders (STOP_LOSS, TAKE_PROFIT).
//
// A trigger order is stored in the database and monitored by a background
// worker. When the market price reaches the trigger price, a corresponding
// market order is placed automatically.
//
// Trigger conditions:
//   - STOP_LOSS:   current price <= trigger_price (sell to limit loss / buy breakout)
//   - TAKE_PROFIT: current price >= trigger_price (sell to lock profit / buy dip)
package trigger

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

// Type is the trigger order type.
type Type string

const (
	StopLoss   Type = "STOP_LOSS"
	TakeProfit Type = "TAKE_PROFIT"
)

// Status of a trigger order.
type Status string

const (
	StatusPending   Status = "PENDING"
	StatusTriggered Status = "TRIGGERED"
	StatusCancelled Status = "CANCELLED"
	StatusExpired   Status = "EXPIRED"
)

// TriggerOrder is a conditional order that executes when price reaches trigger_price.
type TriggerOrder struct {
	ID               uuid.UUID  `json:"id"`
	UserID           uuid.UUID  `json:"user_id"`
	Pair             string     `json:"pair"`
	Side             string     `json:"side"`
	TriggerType      Type       `json:"trigger_type"`
	TriggerPrice     string     `json:"trigger_price"`
	Quantity         string     `json:"quantity"`
	Status           Status     `json:"status"`
	TriggeredAt      *time.Time `json:"triggered_at"`
	TriggeredOrderID *uuid.UUID `json:"triggered_order_id"`
	CancelledAt      *time.Time `json:"cancelled_at"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// CreateInput contains the data to create a trigger order.
type CreateInput struct {
	UserID       uuid.UUID
	Pair         string
	Side         string
	TriggerType  Type
	TriggerPrice decimal.Decimal
	Quantity     decimal.Decimal
}

// Service manages trigger orders.
type Service struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

// NewService creates a new trigger service.
func NewService(pool *pgxpool.Pool, log *slog.Logger) *Service {
	return &Service{pool: pool, log: log}
}

// Create creates a new trigger order.
func (s *Service) Create(ctx context.Context, in CreateInput) (*TriggerOrder, error) {
	if in.TriggerPrice.IsZero() || in.Quantity.IsZero() {
		return nil, fmt.Errorf("trigger_price and quantity required")
	}
	if in.TriggerType != StopLoss && in.TriggerType != TakeProfit {
		return nil, fmt.Errorf("invalid trigger_type: %s", in.TriggerType)
	}
	if in.Side != "BUY" && in.Side != "SELL" {
		return nil, fmt.Errorf("side must be BUY or SELL")
	}

	var t TriggerOrder
	err := s.pool.QueryRow(ctx,
		`INSERT INTO trigger_orders (user_id, pair, side, trigger_type, trigger_price, quantity)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, user_id, pair, side, trigger_type, trigger_price, quantity, status,
				   triggered_at, triggered_order_id, cancelled_at, created_at, updated_at`,
		in.UserID, in.Pair, in.Side, string(in.TriggerType),
		in.TriggerPrice.String(), in.Quantity.String(),
	).Scan(&t.ID, &t.UserID, &t.Pair, &t.Side, &t.TriggerType, &t.TriggerPrice,
		&t.Quantity, &t.Status, &t.TriggeredAt, &t.TriggeredOrderID,
		&t.CancelledAt, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// ListByUser returns user's trigger orders.
func (s *Service) ListByUser(ctx context.Context, userID uuid.UUID) ([]TriggerOrder, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, pair, side, trigger_type, trigger_price, quantity, status,
				triggered_at, triggered_order_id, cancelled_at, created_at, updated_at
		 FROM trigger_orders WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TriggerOrder{}
	for rows.Next() {
		var t TriggerOrder
		if err := rows.Scan(&t.ID, &t.UserID, &t.Pair, &t.Side, &t.TriggerType,
			&t.TriggerPrice, &t.Quantity, &t.Status, &t.TriggeredAt,
			&t.TriggeredOrderID, &t.CancelledAt, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// Cancel cancels a trigger order (must belong to user).
func (s *Service) Cancel(ctx context.Context, userID, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE trigger_orders SET status = 'CANCELLED', cancelled_at = NOW(), updated_at = NOW()
		 WHERE id = $1 AND user_id = $2 AND status = 'PENDING'`,
		id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("trigger order not found or not pending")
	}
	return nil
}

// CheckAndTrigger checks pending triggers against current price and triggers those that meet condition.
func (s *Service) CheckAndTrigger(ctx context.Context, pair string, currentPrice decimal.Decimal, placeOrder func(ctx context.Context, userID uuid.UUID, pair, side string, quantity decimal.Decimal) (uuid.UUID, error)) ([]TriggerOrder, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, pair, side, trigger_type, trigger_price, quantity, status,
				triggered_at, triggered_order_id, cancelled_at, created_at, updated_at
		 FROM trigger_orders
		 WHERE pair = $1 AND status = 'PENDING'
		   AND ((trigger_type = 'STOP_LOSS' AND $2::numeric <= trigger_price::numeric)
		     OR (trigger_type = 'TAKE_PROFIT' AND $2::numeric >= trigger_price::numeric))`,
		pair, currentPrice.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	triggered := []TriggerOrder{}
	for rows.Next() {
		var t TriggerOrder
		if err := rows.Scan(&t.ID, &t.UserID, &t.Pair, &t.Side, &t.TriggerType,
			&t.TriggerPrice, &t.Quantity, &t.Status, &t.TriggeredAt,
			&t.TriggeredOrderID, &t.CancelledAt, &t.CreatedAt, &t.UpdatedAt); err != nil {
			s.log.Warn("scan trigger order", "error", err)
			continue
		}

		qty, _ := decimal.NewFromString(t.Quantity)
		orderID, err := placeOrder(ctx, t.UserID, t.Pair, t.Side, qty)
		if err != nil {
			s.log.Warn("trigger execute failed", "trigger_id", t.ID, "error", err)
			continue
		}

		now := time.Now().UTC()
		_, err = s.pool.Exec(ctx,
			`UPDATE trigger_orders SET status = 'TRIGGERED', triggered_at = $2, triggered_order_id = $3, updated_at = $2
			 WHERE id = $1 AND status = 'PENDING'`,
			t.ID, now, orderID)
		if err != nil {
			s.log.Warn("mark trigger failed", "trigger_id", t.ID, "error", err)
		}
		t.Status = StatusTriggered
		t.TriggeredAt = &now
		t.TriggeredOrderID = &orderID
		triggered = append(triggered, t)
	}
	return triggered, nil
}

// CountPending returns the number of pending trigger orders (for monitoring).
func (s *Service) CountPending(ctx context.Context) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM trigger_orders WHERE status = 'PENDING'`).Scan(&count)
	return count, err
}

// RunMonitor runs a background worker that checks triggers every interval.
func (s *Service) RunMonitor(ctx context.Context, getPrices func(ctx context.Context) (map[string]decimal.Decimal, error), placeOrder func(ctx context.Context, userID uuid.UUID, pair, side string, quantity decimal.Decimal) (uuid.UUID, error), interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	s.log.Info("trigger monitor starting", "interval", interval)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.tick(ctx, getPrices, placeOrder)
		}
	}
}

func (s *Service) tick(ctx context.Context, getPrices func(ctx context.Context) (map[string]decimal.Decimal, error), placeOrder func(ctx context.Context, userID uuid.UUID, pair, side string, quantity decimal.Decimal) (uuid.UUID, error)) {
	prices, err := getPrices(ctx)
	if err != nil {
		s.log.Warn("trigger monitor getPrices failed", "error", err)
		return
	}
	for pair, price := range prices {
		if !price.IsPositive() {
			continue
		}
		triggered, err := s.CheckAndTrigger(ctx, pair, price, placeOrder)
		if err != nil {
			s.log.Warn("trigger check failed", "pair", pair, "error", err)
			continue
		}
		for _, t := range triggered {
			s.log.Info("trigger order executed",
				"trigger_id", t.ID, "pair", t.Pair, "side", t.Side,
				"type", t.TriggerType, "trigger_price", t.TriggerPrice,
				"current_price", price.String(), "quantity", t.Quantity)
		}
	}
}

// EnsureSchema verifies the trigger_orders table exists.
func (s *Service) EnsureSchema(ctx context.Context) error {
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'trigger_orders')`,
	).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("trigger_orders table does not exist")
	}
	return nil
}