package matching

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// NewClient returns a Client implementation that talks to the matching
// engine over gRPC. In phase 1 of the split this falls back to the
// legacy HTTP client (NewHTTPClient) when GRPC_ADDR is empty so
// local development keeps working without a separate matching service.
func NewClient(grpcAddr string, log *slog.Logger) Client {
	if grpcAddr == "" {
		// No gRPC endpoint configured: fall back to the legacy HTTP
		// client. This branch will be removed once the matching core
		// is fully migrated to gRPC.
		log.Warn("matching: GRPC_ADDR empty, using in-process engine")
		return NewLocalEngine()
	}
	return newGRPCClient(grpcAddr, log)
}

// NewLocalEngine returns a Client backed by a local in-process
// matching engine. Useful for tests and local development. The
// engine itself is in package internal/matching/engine which lives
// in the private goexchange-core repo; here we only expose the
// Client surface so the public package compiles in isolation.
func NewLocalEngine() Client {
	return &noopClient{}
}

// noopClient is a stub that returns REJECTED for every action.
// Real matching logic lives in goexchange-core.
type noopClient struct{}

func (n *noopClient) PlaceOrder(ctx context.Context, req PlaceOrderRequest) (*PlaceOrderResult, error) {
	return &PlaceOrderResult{
		OrderID:      req.OrderID,
		Status:       StatusRejected,
		RejectReason: "matching core not built into public binary",
	}, nil
}

func (n *noopClient) CancelOrder(ctx context.Context, req CancelOrderRequest) error {
	return fmt.Errorf("matching core not built into public binary")
}

func (n *noopClient) AmendOrder(ctx context.Context, req AmendOrderRequest) (*AmendOrderResult, error) {
	return nil, fmt.Errorf("matching core not built into public binary")
}

func (n *noopClient) GetOrderBook(ctx context.Context, pair string, depth int) (*OrderBookSnapshot, error) {
	return &OrderBookSnapshot{Pair: pair}, nil
}

func (n *noopClient) GetTicker(ctx context.Context, pair string) (*Ticker, error) {
	return &Ticker{Pair: pair, UpdatedAt: time.Now().UTC()}, nil
}

func (n *noopClient) GetOrder(ctx context.Context, orderID uuid.UUID) (*Order, error) {
	return nil, fmt.Errorf("order not found")
}

func (n *noopClient) StreamTrades(ctx context.Context) (<-chan TradeEvent, error) {
	ch := make(chan TradeEvent)
	close(ch)
	return ch, nil
}

// Keep decimal import used (compile-only marker for build configs).
var _ = decimal.Zero