package matching

import (
	"context"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// OrderSourceAdapter wraps a Client to satisfy the OrderSource
// interface expected by trading.Service. The trading layer depends
// on a narrow OrderSource interface rather than the full Client,
// which keeps the dependency on the matching engine one-way.
type OrderSourceAdapter struct {
	Client Client
}

// NewOrderSourceAdapter returns an adapter for trading.OrderSource.
func NewOrderSourceAdapter(c Client) *OrderSourceAdapter {
	return &OrderSourceAdapter{Client: c}
}

// PlaceOrder satisfies OrderSource.PlaceOrder.
func (a *OrderSourceAdapter) PlaceOrder(ctx context.Context, req PlaceOrderRequest) (*PlaceOrderResult, error) {
	return a.Client.PlaceOrder(ctx, req)
}

// CancelOrder satisfies OrderSource.CancelOrder.
func (a *OrderSourceAdapter) CancelOrder(ctx context.Context, pair string, orderID uuid.UUID, userID uuid.UUID) error {
	return a.Client.CancelOrder(ctx, CancelOrderRequest{
		OrderID: orderID,
		UserID:  userID,
	})
}

// AmendOrder satisfies OrderSource.AmendOrder. The OrderSource
// signature takes a separate price/quantity/side rather than an
// AmendOrderRequest, so we adapt and return a PlaceOrderResult as
// expected by the trading layer.
func (a *OrderSourceAdapter) AmendOrder(
	ctx context.Context,
	pair string,
	orderID uuid.UUID,
	userID uuid.UUID,
	side Side,
	price, quantity decimal.Decimal,
) (*PlaceOrderResult, error) {
	res, err := a.Client.AmendOrder(ctx, AmendOrderRequest{
		OrderID:  orderID,
		UserID:   userID,
		NewPrice: &price,
		NewQty:   &quantity,
	})
	if err != nil {
		return nil, err
	}
	if res == nil {
		return &PlaceOrderResult{OrderID: orderID, Status: StatusRejected}, nil
	}
	// AmendOrderResult.Order is a value, not pointer; copy fields.
	return &PlaceOrderResult{
		OrderID:   res.Order.ID,
		Status:    res.Order.Status,
		Filled:    res.Order.FilledQty,
		Remaining: res.Order.RemainingQty,
		Trades:    res.Trades,
	}, nil
}