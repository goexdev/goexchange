// Real gRPC client that talks to the proprietary matching engine
// running in goexchange-core. Generated stubs live in
// internal/matching/matchingv1 (see proto/matching.proto and
// scripts/gen_proto.sh).
package matching

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/protobuf/types/known/timestamppb"

	matchingv1 "github.com/goexdev/goexchange/internal/matching/matchingv1"
)

// newGRPCClient returns a Client that talks to the matching engine
// over gRPC. addr is the gRPC endpoint (e.g. "matching:50051").
//
// The connection uses insecure credentials by default because the
// matching lives on the internal Docker network. For external
// deployments, swap in TLS credentials.
func newGRPCClient(addr string, log *slog.Logger) Client {
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                30 * time.Second,
			Timeout:             10 * time.Second,
			PermitWithoutStream: true,
		}),
	)
	if err != nil {
		log.Error("matching: grpc dial failed", "addr", addr, "error", err)
		// Fall back to noop so the public binary still runs.
		return &noopClient{}
	}
	log.Info("matching: gRPC client connected", "addr", addr)
	return &grpcClientImpl{conn: conn, log: log, client: matchingv1.NewMatchingServiceClient(conn)}
}

// grpcClientImpl is the real Client backed by gRPC.
type grpcClientImpl struct {
	conn   *grpc.ClientConn
	log    *slog.Logger
	client matchingv1.MatchingServiceClient
}

func (g *grpcClientImpl) PlaceOrder(ctx context.Context, req PlaceOrderRequest) (*PlaceOrderResult, error) {
	resp, err := g.client.PlaceOrder(ctx, g.toProtoOrder(req))
	if err != nil {
		return nil, fmt.Errorf("matching.PlaceOrder: %w", err)
	}
	return g.fromProtoOrderResult(resp), nil
}

func (g *grpcClientImpl) CancelOrder(ctx context.Context, req CancelOrderRequest) error {
	_, err := g.client.CancelOrder(ctx, &matchingv1.CancelOrderRequest{
		OrderId: req.OrderID.String(),
		UserId:  req.UserID.String(),
	})
	if err != nil {
		return fmt.Errorf("matching.CancelOrder: %w", err)
	}
	return nil
}

func (g *grpcClientImpl) AmendOrder(ctx context.Context, req AmendOrderRequest) (*AmendOrderResult, error) {
	price := ""
	if req.NewPrice != nil {
		price = req.NewPrice.String()
	}
	qty := ""
	if req.NewQty != nil {
		qty = req.NewQty.String()
	}
	resp, err := g.client.AmendOrder(ctx, &matchingv1.AmendOrderRequest{
		OrderId:     req.OrderID.String(),
		UserId:      req.UserID.String(),
		NewPrice:    price,
		NewQuantity: qty,
	})
	if err != nil {
		return nil, fmt.Errorf("matching.AmendOrder: %w", err)
	}
	return g.fromProtoAmendResult(resp), nil
}

func (g *grpcClientImpl) GetOrderBook(ctx context.Context, pair string, depth int) (*OrderBookSnapshot, error) {
	resp, err := g.client.GetOrderBook(ctx, &matchingv1.GetOrderBookRequest{
		Pair:  pair,
		Depth: int32(depth),
	})
	if err != nil {
		return nil, fmt.Errorf("matching.GetOrderBook: %w", err)
	}
	return g.fromProtoOrderBook(resp), nil
}

func (g *grpcClientImpl) GetTicker(ctx context.Context, pair string) (*Ticker, error) {
	resp, err := g.client.GetTicker(ctx, &matchingv1.GetTickerRequest{Pair: pair})
	if err != nil {
		return nil, fmt.Errorf("matching.GetTicker: %w", err)
	}
	return g.fromProtoTicker(resp), nil
}

func (g *grpcClientImpl) GetOrder(ctx context.Context, orderID uuid.UUID) (*Order, error) {
	resp, err := g.client.GetOrder(ctx, &matchingv1.GetOrderRequest{OrderId: orderID.String()})
	if err != nil {
		return nil, fmt.Errorf("matching.GetOrder: %w", err)
	}
	return g.fromProtoOrder(resp), nil
}

func (g *grpcClientImpl) StreamTrades(ctx context.Context) (<-chan TradeEvent, error) {
	stream, err := g.client.StreamTrades(ctx, &matchingv1.StreamTradesRequest{})
	if err != nil {
		return nil, fmt.Errorf("matching.StreamTrades: %w", err)
	}
	ch := make(chan TradeEvent, 64)
	go func() {
		defer close(ch)
		for {
			ev, err := stream.Recv()
			if err != nil {
				return
			}
			ch <- g.fromProtoTradeEvent(ev)
		}
	}()
	return ch, nil
}

// --- proto <-> internal type conversions ---

func (g *grpcClientImpl) toProtoOrder(req PlaceOrderRequest) *matchingv1.PlaceOrderRequest {
	return &matchingv1.PlaceOrderRequest{
		OrderId:     req.OrderID.String(),
		UserId:      req.UserID.String(),
		Pair:        req.Pair,
		Side:        sideToProto(req.Side),
		Type:        orderTypeToProto(req.Type),
		Price:       req.Price.String(),
		Quantity:    req.Quantity.String(),
		TimeInForce: tifToProto(req.TimeInForce),
		StpMode:     stpToProto(req.STPMode),
		ExpiresAt:   timeToTS(req.ExpiresAt),
		StopPrice:   decimalToString(req.StopPrice),
		ClientId:    req.ClientID,
	}
}

func (g *grpcClientImpl) fromProtoOrderResult(r *matchingv1.PlaceOrderResponse) *PlaceOrderResult {
	out := &PlaceOrderResult{
		OrderID:      parseUUID(r.OrderId),
		Status:       statusFromProto(r.Status),
		RejectReason: r.RejectReason,
		Trades:       make([]Trade, 0, len(r.Trades)),
	}
	if d, err := decimal.NewFromString(r.Filled); err == nil {
		out.Filled = d
	}
	if d, err := decimal.NewFromString(r.Remaining); err == nil {
		out.Remaining = d
	}
	for _, t := range r.Trades {
		out.Trades = append(out.Trades, g.fromProtoTrade(t))
	}
	return out
}

func (g *grpcClientImpl) fromProtoAmendResult(r *matchingv1.AmendOrderResponse) *AmendOrderResult {
	out := &AmendOrderResult{
		Order:        *g.fromProtoOrder(r.Order),
		RejectReason: r.RejectReason,
		Trades:       make([]Trade, 0, len(r.Trades)),
	}
	for _, t := range r.Trades {
		out.Trades = append(out.Trades, g.fromProtoTrade(t))
	}
	return out
}

func (g *grpcClientImpl) fromProtoOrderBook(r *matchingv1.OrderBookSnapshot) *OrderBookSnapshot {
	out := &OrderBookSnapshot{Pair: r.Pair}
	for _, l := range r.Bids {
		p, _ := decimal.NewFromString(l.Price)
		q, _ := decimal.NewFromString(l.Quantity)
		out.Bids = append(out.Bids, PriceLevel{Price: p, Quantity: q})
	}
	for _, l := range r.Asks {
		p, _ := decimal.NewFromString(l.Price)
		q, _ := decimal.NewFromString(l.Quantity)
		out.Asks = append(out.Asks, PriceLevel{Price: p, Quantity: q})
	}
	return out
}

func (g *grpcClientImpl) fromProtoTicker(r *matchingv1.Ticker) *Ticker {
	out := &Ticker{Pair: r.Pair}
	out.LastPrice, _ = decimal.NewFromString(r.LastPrice)
	out.BestBid, _ = decimal.NewFromString(r.BestBid)
	out.BestAsk, _ = decimal.NewFromString(r.BestAsk)
	out.Volume24h, _ = decimal.NewFromString(r.Volume_24H)
	out.High24h, _ = decimal.NewFromString(r.High_24H)
	out.Low24h, _ = decimal.NewFromString(r.Low_24H)
	out.Change24h, _ = decimal.NewFromString(r.Change_24H)
	out.Trades24h = r.Trades_24H
	if r.UpdatedAt != nil {
		out.UpdatedAt = r.UpdatedAt.AsTime()
	}
	return out
}

func (g *grpcClientImpl) fromProtoOrder(o *matchingv1.Order) *Order {
	out := &Order{
		ID:     parseUUID(o.Id),
		UserID: parseUUID(o.UserId),
		Pair:   o.Pair,
		Side:   sideFromProto(o.Side),
		Type:   orderTypeFromProto(o.Type),
	}
	if o.Base != "" {
		out.Base = o.Base
	}
	if o.Quote != "" {
		out.Quote = o.Quote
	}
	out.Price, _ = decimal.NewFromString(o.Price)
	out.Quantity, _ = decimal.NewFromString(o.Quantity)
	out.FilledQty, _ = decimal.NewFromString(o.Filled)
	out.RemainingQty, _ = decimal.NewFromString(o.Remaining)
	out.Status = statusFromProto(o.Status)
	out.TimeInForce = tifFromProto(o.TimeInForce)
	out.STPMode = stpFromProto(o.StpMode)
	if o.ClientId != "" {
		out.ClientID = o.ClientId
	}
	if o.StopPrice != "" {
		if d, err := decimal.NewFromString(o.StopPrice); err == nil {
			out.StopPrice = &d
		}
	}
	if o.CreatedAt != nil {
		out.CreatedAt = o.CreatedAt.AsTime()
	}
	if o.UpdatedAt != nil {
		out.UpdatedAt = o.UpdatedAt.AsTime()
	}
	if o.ExpiresAt != nil {
		exp := o.ExpiresAt.AsTime()
		out.ExpiresAt = &exp
	}
	return out
}

func (g *grpcClientImpl) fromProtoTrade(t *matchingv1.Trade) Trade {
	out := Trade{
		ID:          parseUUID(t.Id),
		Pair:        t.Pair,
		BuyOrderID:  parseUUID(t.BuyOrderId),
		SellOrderID: parseUUID(t.SellOrderId),
		BuyUserID:   parseUUID(t.BuyUserId),
		SellUserID:  parseUUID(t.SellUserId),
		TakerSide:   sideFromProto(t.TakerSide),
		Base:        t.Base,
		Quote:       t.Quote,
	}
	out.Price, _ = decimal.NewFromString(t.Price)
	out.Quantity, _ = decimal.NewFromString(t.Quantity)
	if t.BaseAmount != "" {
		out.BaseAmt, _ = decimal.NewFromString(t.BaseAmount)
	}
	if t.QuoteAmount != "" {
		out.QuoteAmt, _ = decimal.NewFromString(t.QuoteAmount)
	}
	if t.ExecutedAt != nil {
		out.Timestamp = t.ExecutedAt.AsTime()
		out.ExecutedAt = out.Timestamp.Format(time.RFC3339)
	}
	return out
}

func (g *grpcClientImpl) fromProtoTradeEvent(ev *matchingv1.TradeEvent) TradeEvent {
	out := TradeEvent{Type: ev.Type.String()}
	if ev.Trade != nil {
		t := g.fromProtoTrade(ev.Trade)
		out.Trade = &t
	}
	if ev.Order != nil {
		o := g.fromProtoOrder(ev.Order)
		out.Order = o
	}
	return out
}

// --- proto-side enum helpers (mirrors internal enums) ---

func sideToProto(s Side) matchingv1.Side {
	switch s {
	case SideBuy:
		return matchingv1.Side_BUY
	case SideSell:
		return matchingv1.Side_SELL
	default:
		return matchingv1.Side_SIDE_UNSPECIFIED
	}
}

func sideFromProto(s matchingv1.Side) Side {
	switch s {
	case matchingv1.Side_BUY:
		return SideBuy
	case matchingv1.Side_SELL:
		return SideSell
	default:
		return ""
	}
}

func orderTypeToProto(t Type) matchingv1.OrderType {
	switch t {
	case TypeLimit:
		return matchingv1.OrderType_LIMIT
	case TypeMarket:
		return matchingv1.OrderType_MARKET
	default:
		return matchingv1.OrderType_ORDER_TYPE_UNSPECIFIED
	}
}

func orderTypeFromProto(t matchingv1.OrderType) Type {
	switch t {
	case matchingv1.OrderType_LIMIT:
		return TypeLimit
	case matchingv1.OrderType_MARKET:
		return TypeMarket
	default:
		return ""
	}
}

func statusFromProto(s matchingv1.Status) Status {
	switch s {
	case matchingv1.Status_OPEN:
		return StatusOpen
	case matchingv1.Status_PARTIAL:
		return StatusPartial
	case matchingv1.Status_FILLED:
		return StatusFilled
	case matchingv1.Status_CANCELED:
		return StatusCanceled
	case matchingv1.Status_REJECTED:
		return StatusRejected
	default:
		return ""
	}
}

func tifToProto(t TimeInForce) matchingv1.TimeInForce {
	switch t {
	case TIF_GTC:
		return matchingv1.TimeInForce_GTC
	case TIF_IOC:
		return matchingv1.TimeInForce_IOC
	case TIF_FOK:
		return matchingv1.TimeInForce_FOK
	default:
		return matchingv1.TimeInForce_TIF_UNSPECIFIED
	}
}

func tifFromProto(s matchingv1.TimeInForce) TimeInForce {
	switch s {
	case matchingv1.TimeInForce_GTC:
		return TIF_GTC
	case matchingv1.TimeInForce_IOC:
		return TIF_IOC
	case matchingv1.TimeInForce_FOK:
		return TIF_FOK
	default:
		return ""
	}
}

// stpToProto maps our internal STPMode string to the proto enum.
// The proto enum uses CANCEL_* (legacy REJECT_TAKER is a noop alias
// of CANCEL_TAKER on the core side).
func stpToProto(s STPMode) matchingv1.STPMode {
	if s == STPNone {
		return matchingv1.STPMode_NONE
	}
	if s == STPCancelMaker {
		return matchingv1.STPMode_CANCEL_MAKER
	}
	if s == STPCancelBoth {
		return matchingv1.STPMode_CANCEL_BOTH
	}
	// STPCancelTaker and STPRejectTaker are the same constant value
	// ("CANCEL_TAKER"); STPMode_underscore variants fall through here.
	return matchingv1.STPMode_CANCEL_TAKER
}

func stpFromProto(s matchingv1.STPMode) STPMode {
	switch s {
	case matchingv1.STPMode_NONE:
		return STPNone
	case matchingv1.STPMode_CANCEL_TAKER:
		return STPCancelTaker
	case matchingv1.STPMode_CANCEL_MAKER:
		return STPCancelMaker
	case matchingv1.STPMode_CANCEL_BOTH:
		return STPCancelBoth
	default:
		return ""
	}
}

// --- utility helpers ---

// parseUUID returns uuid.Nil on parse error so callers can
// gracefully handle malformed IDs without panicking.
func parseUUID(s string) uuid.UUID {
	id, _ := uuid.Parse(s)
	return id
}

// timeToTS converts an optional *time.Time to a protobuf Timestamp.
func timeToTS(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}

// decimalToString converts an optional *decimal.Decimal to a string.
func decimalToString(d *decimal.Decimal) string {
	if d == nil {
		return ""
	}
	return d.String()
}