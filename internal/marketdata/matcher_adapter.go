// Package marketdata - adapter for matching.Client to marketdata.DataSource.
package marketdata

import (
	"context"
	"errors"

	"github.com/goexdev/goexchange/internal/matching"
)

// MatcherAdapter wraps matching.Client to satisfy the DataSource interface.
type MatcherAdapter struct {
	Client matching.Client
}

// NewMatcherAdapter creates an adapter for matching.Client.
func NewMatcherAdapter(c matching.Client) *MatcherAdapter {
	return &MatcherAdapter{Client: c}
}

// GetOrderBook converts matching.OrderBookSnapshot to (bids, asks) slice.
func (a *MatcherAdapter) GetOrderBook(ctx context.Context, base, quote string, depth int) ([]OrderBookLevel, []OrderBookLevel, error) {
	pair := base + "_" + quote
	snap, err := a.Client.GetOrderBook(ctx, pair, depth)
	if err != nil {
		return nil, nil, err
	}
	bids := make([]OrderBookLevel, 0, len(snap.Bids))
	for _, l := range snap.Bids {
		bids = append(bids, OrderBookLevel{
			Price:    l.Price.String(),
			Quantity: l.Quantity.String(),
		})
	}
	asks := make([]OrderBookLevel, 0, len(snap.Asks))
	for _, l := range snap.Asks {
		asks = append(asks, OrderBookLevel{
			Price:    l.Price.String(),
			Quantity: l.Quantity.String(),
		})
	}
	return bids, asks, nil
}

// GetTicker adapts the matching.Ticker return into the legacy 2-string
// return shape (best bid, best ask) expected by older callers.
func (a *MatcherAdapter) GetTicker(ctx context.Context, base, quote string) (bid, ask string, err error) {
	pair := base + "_" + quote
	t, err := a.Client.GetTicker(ctx, pair)
	if err != nil {
		return "", "", err
	}
	if t == nil {
		return "", "", errors.New("nil ticker from matching")
	}
	return t.BestBid.String(), t.BestAsk.String(), nil
}