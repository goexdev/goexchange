// Package marketdata owns the live data plane for the public API:
// the in-process EventBus, the market-data WebSocket hub, and the
// forwarder that pumps matching-engine stream events into the bus.
//
// File purpose: bridge the matching-engine gRPC stream
// (matching.Client.StreamTrades) into the marketdata.EventBus so that
// the existing MarketWSHub forwards trade events to all subscribed
// WebSocket clients without further changes.
//
// Data-flow contract (see MATCHING_LICENSE_DESIGN §5.3):
//   - Trade events arriving here were *also* written to the trades
//     table by the API trading service when the originating order was
//     placed. We only use the stream for *real-time market data
//     dissemination*, not for additional DB writes.
//   - Order-update events are ignored by this forwarder; the API
//     already learns about order-status changes through the synchronous
//     PlaceOrder / AmendOrder / CancelOrder responses.
package marketdata

import (
	"context"
	"log/slog"

	"github.com/goexdev/goexchange/internal/matching"
)

// StreamForwarder subscribes to the matching engine stream and
// republishes trade events onto the given EventBus.
type StreamForwarder struct {
	client StreamSource
	bus    *EventBus
	log    *slog.Logger
}

// StreamSource is the subset of matching.Client the forwarder needs.
// Defined as an interface so tests can stub it without spinning up a
// real gRPC connection.
type StreamSource interface {
	StreamTrades(ctx context.Context) (<-chan matching.TradeEvent, error)
}

// NewStreamForwarder returns a forwarder that pumps StreamSource
// events onto bus. The forwarder does not start until Run is called.
func NewStreamForwarder(src StreamSource, bus *EventBus, log *slog.Logger) *StreamForwarder {
	return &StreamForwarder{client: src, bus: bus, log: log}
}

// Run blocks until ctx is canceled or the underlying stream errors
// out. On error it logs and returns; callers may restart the API
// process (or wrap this in a reconnect loop) to recover.
//
// The function is intentionally simple: connect once, drain until
// done. A retry loop would mask serious issues (matching engine
// unreachable, etc.) and is better surfaced as a process exit +
// supervisor restart.
func (f *StreamForwarder) Run(ctx context.Context) error {
	events, err := f.client.StreamTrades(ctx)
	if err != nil {
		f.log.Error("stream: connect failed", "error", err)
		return err
	}
	f.log.Info("stream: subscribed to matching engine")

	for {
		select {
		case <-ctx.Done():
			f.log.Info("stream: ctx canceled, exiting")
			return nil
		case ev, ok := <-events:
			if !ok {
				f.log.Warn("stream: channel closed by server")
				return nil
			}
			f.handle(ctx, ev)
		}
	}
}

// handle dispatches a single TradeEvent. ORDER_UPDATE is dropped here
// (see file header); TRADE is published to the bus.
func (f *StreamForwarder) handle(ctx context.Context, ev matching.TradeEvent) {
	switch ev.Type {
	case matching.TradeEventTypeTrade:
		if ev.Trade == nil {
			return
		}
		t := ev.Trade
		busEv := Event{
			Type: EventTrade,
			Trade: &TradePayload{
				ID:       t.ID.String(),
				Pair:     t.Pair,
				Side:     string(t.TakerSide),
				Price:    t.Price.String(),
				Quantity: t.Quantity.String(),
				BuyerID:  t.BuyUserID.String(),
				SellerID: t.SellUserID.String(),
			},
		}
		f.bus.Publish(busEv)
	case matching.TradeEventTypeOrderUpdate:
		// Intentionally ignored. Order status is propagated to clients
		// via the originating REST response + the per-user order WebSocket.
	default:
		f.log.Warn("stream: unknown event type", "type", ev.Type)
	}
}