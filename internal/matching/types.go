// Package matching defines the public interface and message types
// used to talk to the proprietary matching engine.
//
// The matching engine itself lives in the private goexchange-core
// repository and runs as a separate gRPC service (port 50051).
// This package exposes the contract that the public API/server code
// depends on, plus a Client interface for talking to it.
//
// See docs/MATCHING_LICENSE_DESIGN.md for the full architecture.
package matching

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Type is the order kind (LIMIT / MARKET / etc).
type Type string

const (
	TypeLimit  Type = "LIMIT"
	TypeMarket Type = "MARKET"
)

// Status of an order in the matching engine.
type Status string

const (
	StatusOpen     Status = "OPEN"
	StatusPartial  Status = "PARTIAL"
	StatusFilled   Status = "FILLED"
	StatusCanceled Status = "CANCELED"
	StatusRejected Status = "REJECTED"
)

// Side of an order (BUY or SELL).
type Side string

const (
	SideBuy  Side = "BUY"
	SideSell Side = "SELL"
)

// OrderType distinguishes market and limit orders.
// (Alias of Type for code that prefers the longer name.)
type OrderType = Type

const (
	OrderTypeLimit  = TypeLimit
	OrderTypeMarket = TypeMarket
)

// TimeInForce is the order's time-in-force policy.
type TimeInForce string

const (
	TIF_GTC TimeInForce = "GTC" // Good Till Cancel
	TIF_IOC TimeInForce = "IOC" // Immediate or Cancel
	TIF_FOK TimeInForce = "FOK" // Fill or Kill
)

// STPMode is the Self-Trade Prevention mode.
type STPMode string

const (
	STPNone          STPMode = "NONE"
	STP_CANCEL_TAKER STPMode = "CANCEL_TAKER"
	STP_CANCEL_MAKER STPMode = "CANCEL_MAKER"
	STP_CANCEL_BOTH  STPMode = "CANCEL_BOTH"
)

// Legacy aliases for code that uses unprefixed names.
const (
	STPRejectTaker  = STP_CANCEL_TAKER
	STPCancelMaker  = STP_CANCEL_MAKER
	STPCancelBoth   = STP_CANCEL_BOTH
	STPCancelTaker  = STP_CANCEL_TAKER
)

// Order is the in-flight representation of an order inside the
// matching engine. The matching engine mutates fields as the order
// progresses (Filled, Remaining, Status).
type Order struct {
	ID          uuid.UUID
	UserID      uuid.UUID
	Pair        string
	Base        string
	Quote       string
	Side        Side
	Type        Type
	Price       decimal.Decimal
	Quantity    decimal.Decimal
	FilledQty   decimal.Decimal
	RemainingQty decimal.Decimal
	Status      Status
	TimeInForce TimeInForce
	STPMode     STPMode
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ExpiresAt   *time.Time
	ClientID    string
	StopPrice   *decimal.Decimal
}

// Remaining returns the order's remaining quantity.
func (o *Order) Remaining() decimal.Decimal {
	return o.RemainingQty
}

// Trade is emitted whenever two orders match. Trades are immutable
// once written.
type Trade struct {
	ID          uuid.UUID
	Pair        string
	BuyOrderID  uuid.UUID
	SellOrderID uuid.UUID
	BuyUserID   uuid.UUID
	SellUserID  uuid.UUID
	Base        string
	Quote       string
	BaseAmt     decimal.Decimal
	QuoteAmt    decimal.Decimal
	Price       decimal.Decimal
	Quantity    decimal.Decimal
	TakerSide   Side
	Timestamp   time.Time
	// ExecutedAt is an alias for Timestamp, formatted as RFC3339.
	// The matching engine historically used this name; existing API
	// consumers rely on it.
	ExecutedAt string
}

// PlaceOrderRequest is sent to the matching engine when a user places
// a new order. The engine replies with a PlaceOrderResult that
// includes any trades produced by the match.
type PlaceOrderRequest struct {
	OrderID     uuid.UUID
	UserID      uuid.UUID
	Pair        string
	Side        Side
	Type        Type
	Price       decimal.Decimal
	Quantity    decimal.Decimal
	TimeInForce TimeInForce
	STPMode     STPMode
	ExpiresAt   *time.Time
	StopPrice   *decimal.Decimal
	ClientID    string
	Timestamp   time.Time
}

// PlaceOrderResult is the matching engine's response to PlaceOrder.
type PlaceOrderResult struct {
	OrderID      uuid.UUID
	Status       Status
	Filled       decimal.Decimal
	Remaining    decimal.Decimal
	Trades       []Trade
	RejectReason string
}

// CancelOrderRequest identifies an order to cancel.
type CancelOrderRequest struct {
	OrderID uuid.UUID
	UserID  uuid.UUID
}

// AmendOrderRequest describes changes to an existing order.
type AmendOrderRequest struct {
	OrderID  uuid.UUID
	UserID   uuid.UUID
	NewPrice *decimal.Decimal
	NewQty   *decimal.Decimal
}

// AmendOrderResult is the matching engine's response to AmendOrder.
type AmendOrderResult struct {
	Order        Order
	Trades       []Trade
	RejectReason string
}

// PriceLevel is one row in the order book snapshot.
type PriceLevel struct {
	Price    decimal.Decimal
	Quantity decimal.Decimal
}

// OrderBookSnapshot is a depth-limited view of one pair's book.
type OrderBookSnapshot struct {
	Pair string
	Bids []PriceLevel
	Asks []PriceLevel
}

// PairConfig describes the static parameters of one trading pair.
type PairConfig struct {
	Base        string
	Quote       string
	PricePrec   int
	QtyPrec     int
	MinQty      decimal.Decimal
	MaxQty      decimal.Decimal
	TickSize    decimal.Decimal
	Enabled     bool
	MakerFeeBps int
	TakerFeeBps int
}

// Ticker is a rolling 24h summary for one pair.
type Ticker struct {
	Pair      string
	LastPrice decimal.Decimal
	BestBid   decimal.Decimal
	BestAsk   decimal.Decimal
	Volume24h decimal.Decimal
	High24h   decimal.Decimal
	Low24h    decimal.Decimal
	Change24h decimal.Decimal
	Trades24h int64
	UpdatedAt time.Time
}

// Engine is the legacy in-process matching engine. In production
// this is replaced by a Client (gRPC). Kept here so existing code in
// internal/trading compiles against the public type set. New code
// should depend on Client.
type Engine interface {
	PlaceOrder(o *Order) ([]Trade, error)
	CancelOrder(pair string, orderID uuid.UUID, userID uuid.UUID) error
	AmendOrder(o *Order) ([]Trade, error)
	GetOrderBook(pair string, depth int) OrderBookSnapshot
}

// Client is the public-facing interface to the matching engine.
// The API server obtains a Client via cmd/matcher-wrapper and
// dispatches every place/cancel/amend/query through it.
type Client interface {
	// PlaceOrder submits a new order to the matching engine and
	// returns the resulting trades (if any) plus the order's new
	// status.
	PlaceOrder(ctx context.Context, req PlaceOrderRequest) (*PlaceOrderResult, error)

	// CancelOrder cancels an existing open order.
	CancelOrder(ctx context.Context, req CancelOrderRequest) error

	// AmendOrder modifies an open order's price or quantity. If the
	// amendment produces new matches the returned trades reflect them.
	AmendOrder(ctx context.Context, req AmendOrderRequest) (*AmendOrderResult, error)

	// GetOrderBook returns a snapshot of one pair's book at the
	// requested depth (each side).
	GetOrderBook(ctx context.Context, pair string, depth int) (*OrderBookSnapshot, error)

	// GetTicker returns the 24h ticker for one pair.
	GetTicker(ctx context.Context, pair string) (*Ticker, error)

	// GetOrder returns a single order's current state.
	GetOrder(ctx context.Context, orderID uuid.UUID) (*Order, error)

	// StreamTrades subscribes to a live feed of trade + order
	// updates. The returned channel is closed when ctx is canceled
	// or the underlying stream errors out.
	StreamTrades(ctx context.Context) (<-chan TradeEvent, error)
}

// TradeEvent is one item in the live trade feed produced by
// StreamTrades. Exactly one of Trade or Order is non-nil.
type TradeEvent struct {
	Type  string // "TRADE" | "ORDER_UPDATE"
	Trade *Trade
	Order *Order
}

// Trade event type constants used in TradeEvent.Type.
const (
	TradeEventTypeTrade       = "TRADE"
	TradeEventTypeOrderUpdate = "ORDER_UPDATE"
)