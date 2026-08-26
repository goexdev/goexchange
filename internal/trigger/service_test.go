package trigger

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

// TestTriggerTypeConstants verifies type constants
func TestTriggerTypeConstants(t *testing.T) {
	if StopLoss != "STOP_LOSS" {
		t.Errorf("StopLoss should be STOP_LOSS, got %s", StopLoss)
	}
	if TakeProfit != "TAKE_PROFIT" {
		t.Errorf("TakeProfit should be TAKE_PROFIT, got %s", TakeProfit)
	}
}

// TestTriggerStatusConstants verifies status constants
func TestTriggerStatusConstants(t *testing.T) {
	if StatusPending != "PENDING" {
		t.Errorf("StatusPending should be PENDING, got %s", StatusPending)
	}
	if StatusTriggered != "TRIGGERED" {
		t.Errorf("StatusTriggered should be TRIGGERED, got %s", StatusTriggered)
	}
	if StatusCancelled != "CANCELLED" {
		t.Errorf("StatusCancelled should be CANCELLED, got %s", StatusCancelled)
	}
	if StatusExpired != "EXPIRED" {
		t.Errorf("StatusExpired should be EXPIRED, got %s", StatusExpired)
	}
}

// TestTriggerInputValidation tests input validation
func TestTriggerInputValidation(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := &Service{pool: &pgxpool.Pool{}, log: log}

	ctx := context.Background()
	userID := uuid.New()

	// Invalid type
	_, err := svc.Create(ctx, CreateInput{
		UserID:       userID,
		Pair:         "BTC_USDT",
		Side:         "SELL",
		TriggerType:  "INVALID_TYPE",
		TriggerPrice: decimal.NewFromInt(50000),
		Quantity:     decimal.NewFromFloat(0.1),
	})
	if err == nil {
		t.Error("Expected error for invalid trigger type")
	}

	// Invalid side
	_, err = svc.Create(ctx, CreateInput{
		UserID:       userID,
		Pair:         "BTC_USDT",
		Side:         "INVALID",
		TriggerType:  StopLoss,
		TriggerPrice: decimal.NewFromInt(50000),
		Quantity:     decimal.NewFromFloat(0.1),
	})
	if err == nil {
		t.Error("Expected error for invalid side")
	}

	// Zero trigger price
	_, err = svc.Create(ctx, CreateInput{
		UserID:       userID,
		Pair:         "BTC_USDT",
		Side:         "SELL",
		TriggerType:  StopLoss,
		TriggerPrice: decimal.Zero,
		Quantity:     decimal.NewFromFloat(0.1),
	})
	if err == nil {
		t.Error("Expected error for zero trigger price")
	}

	// Zero quantity
	_, err = svc.Create(ctx, CreateInput{
		UserID:       userID,
		Pair:         "BTC_USDT",
		Side:         "SELL",
		TriggerType:  StopLoss,
		TriggerPrice: decimal.NewFromInt(50000),
		Quantity:     decimal.Zero,
	})
	if err == nil {
		t.Error("Expected error for zero quantity")
	}
}