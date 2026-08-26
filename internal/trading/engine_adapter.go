package trading

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/goexdev/goexchange/internal/matching"
)

// EngineAdapter wraps matching.Engine to satisfy OrderSource.
type EngineAdapter struct {
	Engine matching.Engine
}

// PlaceOrder converts PlaceOrderRequest to Order and calls engine.
func (a *EngineAdapter) PlaceOrder(ctx context.Context, req matching.PlaceOrderRequest) (*matching.PlaceOrderResult, error) {
	parts := strings.Split(req.Pair, "_")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid pair format: %s", req.Pair)
	}
	base, quote := strings.ToUpper(parts[0]), strings.ToUpper(parts[1])
	id := req.OrderID
	if id == uuid.Nil {
		id = uuid.New()
	}
	o := &matching.Order{
		ID:           id,
		UserID:       req.UserID,
		Pair:         req.Pair,
		Base:         base,
		Quote:        quote,
		Side:         req.Side,
		Type:         req.Type,
		Price:        req.Price,
		Quantity:     req.Quantity,
		RemainingQty: req.Quantity,
		Status:       matching.StatusOpen,
		CreatedAt:    time.Now().UTC(),
	}
	trades, err := a.Engine.PlaceOrder(o)
	if err != nil {
		return nil, err
	}
	return &matching.PlaceOrderResult{
		OrderID:   o.ID,
		Status:    o.Status,
		Trades:    trades,
		Filled:    o.FilledQty,
		Remaining: o.Remaining(),
	}, nil
}

// CancelOrder cancels an order via engine.
func (a *EngineAdapter) CancelOrder(ctx context.Context, pair string, orderID uuid.UUID, userID uuid.UUID) error {
	return a.Engine.CancelOrder(pair, orderID, userID)
}

////AmendOrder amends an order via engine.
func (a *EngineAdapter) AmendOrder(ctx context.Context, pair string, orderID uuid.UUID, userID uuid.UUID, side matching.Side, price, quantity decimal.Decimal) (*matching.PlaceOrderResult, error) {
	// Look up the existing order to get base/quote
	o := &matching.Order{
		ID:     orderID,
		UserID: userID,
		Pair:   pair,
		Side:   side,
		Price:  price,
		Quantity: quantity,
	}
	trades, err := a.Engine.AmendOrder(o)
	if err != nil {
		return nil, err
	}
	return &matching.PlaceOrderResult{
		OrderID:   o.ID,
		Status:    o.Status,
		Trades:    trades,
		Filled:    o.FilledQty,
		Remaining: o.Remaining(),
	}, nil
}
