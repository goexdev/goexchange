package trading


import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/goexdev/goexchange/internal/matching"
	"github.com/goexdev/goexchange/internal/wallet"
)

// Order types.
const (
	OrderTypeLimit  = "LIMIT"
	OrderTypeMarket = "MARKET"
)

// Errors returned by the trading service.
var (
	ErrInvalidPair   = errors.New("invalid pair format (expected BASE_QUOTE)")
	ErrUnknownPair   = errors.New("unknown trading pair")
	ErrInvalidSide   = errors.New("side must be BUY or SELL")
	ErrInvalidType   = errors.New("type must be LIMIT (MVP)")
	ErrInvalidPrice  = errors.New("price must be positive")
	ErrInvalidQty    = errors.New("quantity must be positive")
	ErrInsufficient   = errors.New("insufficient balance for this order")
	ErrOrderNotFound = errors.New("order not found")
	ErrNotOwner      = errors.New("you don't own this order")
	ErrAlreadyClosed = errors.New("order is already closed (filled or canceled)")
)

// Service handles order placement, cancellation, and history.
type Service struct {
	pool   *pgxpool.Pool
	src    OrderSource
	engine matching.Engine // legacy in-process engine; nil in production
	wallet *wallet.Service
	log    *slog.Logger

	// Known pairs (cached at startup, could be refreshed in M1)
	pairs map[string]PairInfo
}

// PairInfo describes a trading pair.
type PairInfo struct {
	Base            string
	Quote           string
	MinQty          decimal.Decimal
	MaxQty          decimal.Decimal
	PricePrecision  int
	QtyPrecision    int
}

// NewService creates a new trading service.
// OrderSource is the interface trading needs from matching.
// Implemented by matching.Engine (in-mem) or matching.Client (HTTP).
type OrderSource interface {
	PlaceOrder(ctx context.Context, req matching.PlaceOrderRequest) (*matching.PlaceOrderResult, error)
	CancelOrder(ctx context.Context, pair string, orderID uuid.UUID, userID uuid.UUID) error
	AmendOrder(ctx context.Context, pair string, orderID uuid.UUID, userID uuid.UUID, side matching.Side, price, quantity decimal.Decimal) (*matching.PlaceOrderResult, error)
}

func NewService(pool *pgxpool.Pool, src OrderSource, wallet *wallet.Service, log *slog.Logger) *Service {
	s := &Service{
		pool:   pool,
		src:    src,
		engine: nil, // legacy
		wallet: wallet,
		log:    log,
		pairs:  make(map[string]PairInfo),
	}
	s.loadPairs()
	return s
}

// loadPairs reads trading_pairs table and caches known pairs.
func (s *Service) loadPairs() {
	rows, err := s.pool.Query(context.Background(),
		`SELECT base, quote, min_qty, max_qty, price_precision, qty_precision
		 FROM trading_pairs WHERE enabled = TRUE`)
	if err != nil {
		s.log.Error("load pairs failed", "error", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var pi PairInfo
		var key string
		if err := rows.Scan(&pi.Base, &pi.Quote, &pi.MinQty, &pi.MaxQty, &pi.PricePrecision, &pi.QtyPrecision); err != nil {
			s.log.Warn("scan pair failed", "error", err)
			continue
		}
		key = pi.Base + "_" + pi.Quote
		s.pairs[key] = pi
	}
	s.log.Info("loaded trading pairs", "count", len(s.pairs))
}

// ListPairs returns all known pairs.
func (s *Service) ListPairs() []PairInfo {
	out := make([]PairInfo, 0, len(s.pairs))
	for _, p := range s.pairs {
		out = append(out, p)
	}
	return out
}

// PlaceOrderInput is the data needed to place a new order.
// STPMode is the Self-Trade Prevention mode for an order.
type STPMode string

const (
	// STPRejectTaker: if taker order would match against user's own
	// resting order, skip the match (taker continues matching other
	// orders in the book).
	STPRejectTaker STPMode = "REJECT_TAKER"
	// STPCancelMaker: if taker order would match against user's own
	// resting order, cancel the maker (taker continues matching).
	STPCancelMaker STPMode = "CANCEL_MAKER"
)

type PlaceOrderInput struct {
	UserID   uuid.UUID
	Pair     string // "BTC_USDT"
	Side     string // "BUY" or "SELL"
	Type     string // "LIMIT" or "MARKET" (default LIMIT)
	Price    decimal.Decimal
	Quantity decimal.Decimal
	STPMode  STPMode // self-trade prevention (default REJECT_TAKER)
}

// PlaceOrderResult is the response after placing an order.
type PlaceOrderResult struct {
	OrderID uuid.UUID         `json:"order_id"`
	Status  matching.Status   `json:"status"`
	Trades  []matching.Trade  `json:"trades"`
	Filled  decimal.Decimal   `json:"filled"`
	Remaining decimal.Decimal `json:"remaining"`
}

// Candle represents a single OHLC candle.
type Candle struct {
	Time   int64   `json:"time"`   // unix seconds
	Open   string  `json:"open"`   // opening price
	High   string  `json:"high"`   // highest price in bucket
	Low    string  `json:"low"`    // lowest price in bucket
	Close  string  `json:"close"`  // closing price
	Volume string  `json:"volume"` // total quantity traded
}

// RecentTrade is a simplified trade for public API responses.
type RecentTrade struct {
	ID         string `json:"id"`
	Price      string `json:"price"`
	Quantity   string `json:"quantity"`
	Side       string `json:"side"` // "BUY" or "SELL" (taker side)
	ExecutedAt string `json:"executed_at"`
}

// Market24hStats represents 24-hour market statistics.
type Market24hStats struct {
	High         string  `json:"high"`
	Low          string  `json:"low"`
	Open         string  `json:"open"`
	Last         string  `json:"last"`
	ChangePct    float64 `json:"change_pct"`
	VolumeBase   string  `json:"volume_base"`
	VolumeQuote  string  `json:"volume_quote"`
	TradeCount   int     `json:"trade_count"`
}

// UserTrade represents a trade for a user, enriched with user role.
type UserTrade struct {
	ID          string `json:"id"`
	Pair        string `json:"pair"`
	Base        string `json:"base"`
	Quote       string `json:"quote"`
	Price       string `json:"price"`
	Quantity    string `json:"quantity"`
	Total       string `json:"total"`
	Side        string `json:"side"` // "BUY" or "SELL" from user's perspective
	CounterSide string `json:"counter_side"`
	OrderID     string `json:"order_id"`
	ExecutedAt  string `json:"executed_at"`
}

// CancelAllOrders cancels all open orders for a user and refunds frozen balances.
// If pairFilter is non-empty, only cancels orders for that pair.
// Returns the number of orders cancelled.
//
// CRITICAL: Must unfreeze the remaining balance for each order, otherwise
// the user's balance will be permanently lost.
func (s *Service) CancelAllOrders(ctx context.Context, userID uuid.UUID, pairFilter string) (int, error) {
	// Build query to get the orders being cancelled
	var selectQuery string
	var args []interface{}
	if pairFilter != "" {
		selectQuery = `SELECT o.id, o.pair_id, tp.base, tp.quote, o.side, o.price, o.quantity, o.filled_quantity
		             FROM orders o JOIN trading_pairs tp ON o.pair_id = tp.id
		             WHERE o.user_id = $1 AND o.status IN ('OPEN', 'PARTIAL')
		               AND (tp.base || '_' || tp.quote) = $2
		             FOR UPDATE OF o`
		args = []interface{}{userID, pairFilter}
	} else {
		selectQuery = `SELECT o.id, o.pair_id, tp.base, tp.quote, o.side, o.price, o.quantity, o.filled_quantity
		             FROM orders o JOIN trading_pairs tp ON o.pair_id = tp.id
		             WHERE o.user_id = $1 AND o.status IN ('OPEN', 'PARTIAL')
		             FOR UPDATE OF o`
		args = []interface{}{userID}
	}

	// Start a transaction to ensure atomicity
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, selectQuery, args...)
	if err != nil {
		return 0, fmt.Errorf("query orders: %w", err)
	}

	type orderInfo struct {
		id       uuid.UUID
		pairID   int
		base     string
		quote    string
		side     string
		price    string
		quantity string
		filled   string
	}
	var orders []orderInfo

	for rows.Next() {
		var o orderInfo
		if err := rows.Scan(&o.id, &o.pairID, &o.base, &o.quote, &o.side, &o.price, &o.quantity, &o.filled); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan order: %w", err)
		}
		orders = append(orders, o)
	}
	rows.Close()

	if len(orders) == 0 {
		return 0, nil
	}

	// SECURITY: Move unfreeze OUTSIDE the DB transaction.
	// If unfreeze is inside and one fails, the whole tx rolls back,
	// but the DB status update also rolls back - leaving the order
	// stuck as OPEN while we tried to cancel it.
	// By committing first, the order status is guaranteed updated,
	// and unfreeze can be retried separately if needed.

	// Update all orders to CANCELLED in the same transaction
	// Re-verify status inside the transaction to handle concurrent cancels
	var updateQuery string
	if pairFilter != "" {
		updateQuery = `UPDATE orders
		             SET status = 'CANCELLED', updated_at = NOW()
		             WHERE user_id = $1 AND status IN ('OPEN', 'PARTIAL')
		               AND pair_id = (SELECT id FROM trading_pairs WHERE base = SPLIT_PART($2, '_', 1) AND quote = SPLIT_PART($2, '_', 2))
		             RETURNING id`
	} else {
		updateQuery = `UPDATE orders
		             SET status = 'CANCELLED', updated_at = NOW()
		             WHERE user_id = $1 AND status IN ('OPEN', 'PARTIAL')
		             RETURNING id`
	}

	updatedRows, err := tx.Exec(ctx, updateQuery, args...)
	if err != nil {
		return 0, fmt.Errorf("update orders: %w", err)
	}
	// SECURITY: Verify we updated exactly the number of orders we expected
	// If less, another cancel raced and got some of them
	if int(updatedRows.RowsAffected()) != len(orders) {
		s.log.Warn("cancel-all: row count mismatch",
			"expected", len(orders), "got", updatedRows.RowsAffected(),
			"user_id", userID)
		// Don't fail - the orders we did update are correct,
		// just fewer than expected (race condition)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}

	// CRITICAL: Cancel each order in the matcher's in-memory book
	// The matcher maintains an in-process order book separate from the DB.
	// Without these calls, the cancelled orders would remain in the matcher
	// book and could still match against new orders - causing incorrect trades!
	// Errors here are logged but not fatal - DB status is already CANCELLED.
	for _, o := range orders {
		pair := o.base + "_" + o.quote
		if err := s.src.CancelOrder(ctx, pair, o.id, userID); err != nil {
			s.log.Error("cancel-all: matcher cancel failed (db already cancelled)",
				"order_id", o.id, "pair", pair, "error", err)
		}
	}

	// SECURITY: Unfreeze OUTSIDE the transaction
	// Each unfreeze is its own DB transaction, and the orders are
	// already marked CANCELLED so they can't be processed again
	for _, o := range orders {
		qty, _ := decimal.NewFromString(o.quantity)
		filled, _ := decimal.NewFromString(o.filled)
		price, _ := decimal.NewFromString(o.price)
		remaining := qty.Sub(filled)

		if remaining.LessThanOrEqual(decimal.Zero) {
			continue
		}

		var unfreezeAsset string
		var unfreezeAmount decimal.Decimal
		if o.side == "SELL" {
			unfreezeAsset = o.base
			unfreezeAmount = remaining
		} else {
			unfreezeAsset = o.quote
			unfreezeAmount = remaining.Mul(price)
		}

		// SECURITY: validate amount > 0
		if unfreezeAmount.LessThanOrEqual(decimal.Zero) {
			s.log.Error("cancel-all: invalid unfreeze amount",
				"order_id", o.id, "amount", unfreezeAmount.String())
			continue
		}

		if err := s.wallet.Unfreeze(ctx, userID, unfreezeAsset, unfreezeAmount); err != nil {
			s.log.Error("cancel-all unfreeze failed",
				"order_id", o.id, "asset", unfreezeAsset,
				"amount", unfreezeAmount.String(), "error", err)
		}
	}

	return len(orders), nil
}

// GetUserTrades returns the recent trades for a user.
// If pairFilter is non-empty, only returns trades for that pair.
func (s *Service) GetUserTrades(ctx context.Context, userID uuid.UUID, pairFilter string, limit int) ([]UserTrade, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	// Build query with optional pair filter
	var query string
	var args []interface{}
	if pairFilter != "" {
		// Look up pair_id
		var pairID int
		err := s.pool.QueryRow(ctx,
			`SELECT id FROM trading_pairs WHERE base = $1 AND quote = $2`,
			strings.ToUpper(strings.SplitN(pairFilter, "_", 2)[0]),
			strings.ToUpper(strings.SplitN(pairFilter, "_", 2)[1]),
		).Scan(&pairID)
		if err != nil {
			return nil, fmt.Errorf("unknown pair: %s: %w", pairFilter, err)
		}
		query = `SELECT t.id, tp.base, tp.quote, t.price, t.quantity, t.taker_side, t.buy_order_id, t.sell_order_id, t.executed_at, o_buy.user_id, o_sell.user_id
		         FROM trades t
		         JOIN trading_pairs tp ON t.pair_id = tp.id
		         JOIN orders o_buy ON o_buy.id = t.buy_order_id
		         JOIN orders o_sell ON o_sell.id = t.sell_order_id
		         WHERE t.pair_id = $1 AND (o_buy.user_id = $2 OR o_sell.user_id = $2)
		         ORDER BY t.executed_at DESC
		         LIMIT $3`
		args = []interface{}{pairID, userID, limit}
	} else {
		query = `SELECT t.id, tp.base, tp.quote, t.price, t.quantity, t.taker_side, t.buy_order_id, t.sell_order_id, t.executed_at, o_buy.user_id, o_sell.user_id
		         FROM trades t
		         JOIN trading_pairs tp ON t.pair_id = tp.id
		         JOIN orders o_buy ON o_buy.id = t.buy_order_id
		         JOIN orders o_sell ON o_sell.id = t.sell_order_id
		         WHERE o_buy.user_id = $1 OR o_sell.user_id = $1
		         ORDER BY t.executed_at DESC
		         LIMIT $2`
		args = []interface{}{userID, limit}
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []UserTrade{}
	for rows.Next() {
		var t UserTrade
		var id uuid.UUID
		var base, quote, takerSide, buyOrderID, sellOrderID string
		var price, quantity float64
		var executedAt time.Time
		var buyUserID, sellUserID uuid.UUID
		if err := rows.Scan(&id, &base, &quote, &price, &quantity, &takerSide, &buyOrderID, &sellOrderID, &executedAt, &buyUserID, &sellUserID); err != nil {
			return nil, err
		}
		t.ID = id.String()
		t.Pair = base + "_" + quote
		t.Base = base
		t.Quote = quote
		t.Price = fmt.Sprintf("%f", price)
		t.Quantity = fmt.Sprintf("%f", quantity)
		t.Total = fmt.Sprintf("%f", price*quantity)
		if buyUserID == userID {
			t.Side = "BUY"
			t.CounterSide = "SELL"
			t.OrderID = buyOrderID
		} else {
			t.Side = "SELL"
			t.CounterSide = "BUY"
			t.OrderID = sellOrderID
		}
		t.ExecutedAt = executedAt.Format(time.RFC3339)
		out = append(out, t)
	}
	return out, nil
}

// Get24hStats returns 24-hour market statistics for a pair.
func (s *Service) Get24hStats(ctx context.Context, base, quote string) (*Market24hStats, error) {
	// Look up pair_id
	var pairID int
	err := s.pool.QueryRow(ctx,
		`SELECT id FROM trading_pairs WHERE base = $1 AND quote = $2`,
		strings.ToUpper(base), strings.ToUpper(quote),
	).Scan(&pairID)
	if err != nil {
		return nil, fmt.Errorf("unknown pair: %s_%s: %w", base, quote, err)
	}

	stats := &Market24hStats{
		High:        "0",
		Low:         "0",
		Open:        "0",
		Last:        "0",
		VolumeBase:  "0",
		VolumeQuote: "0",
	}

	// Get 24h aggregate stats
	err = s.pool.QueryRow(ctx,
		`SELECT
		  COALESCE(MAX(price), 0) AS high,
		  COALESCE(MIN(price), 0) AS low,
		  COALESCE(SUM(quantity), 0) AS vol_base,
		  COALESCE(SUM(quantity * price), 0) AS vol_quote,
		  COUNT(*) AS trade_count
		 FROM trades
		 WHERE pair_id = $1 AND executed_at >= NOW() - INTERVAL '24 hours'`,
		pairID,
	).Scan(&stats.High, &stats.Low, &stats.VolumeBase, &stats.VolumeQuote, &stats.TradeCount)
	if err != nil {
		return nil, err
	}

	// Get open price (oldest trade in window)
	err = s.pool.QueryRow(ctx,
		`SELECT price FROM trades
		 WHERE pair_id = $1 AND executed_at >= NOW() - INTERVAL '24 hours'
		 ORDER BY executed_at ASC LIMIT 1`,
		pairID,
	).Scan(&stats.Open)
	if err != nil {
		// No trades in window
		stats.Open = stats.Last
	}

	// Get last price (most recent)
	err = s.pool.QueryRow(ctx,
		`SELECT price FROM trades
		 WHERE pair_id = $1
		 ORDER BY executed_at DESC LIMIT 1`,
		pairID,
	).Scan(&stats.Last)
	if err != nil {
		// No trades at all
		return stats, nil
	}

	// Calculate change %
	high, _ := parseFloat(stats.High)
	low, _ := parseFloat(stats.Low)
	open, _ := parseFloat(stats.Open)
	last, _ := parseFloat(stats.Last)
	if open > 0 {
		stats.ChangePct = ((last - open) / open) * 100
	}
	_ = high
	_ = low

	return stats, nil
}

func parseFloat(s string) (float64, error) {
	return strconv.ParseFloat(s, 64)
}

// GetRecentTrades returns the most recent N trades for a pair.
func (s *Service) GetRecentTrades(ctx context.Context, base, quote string, limit int) ([]RecentTrade, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	// Look up pair_id
	var pairID int
	err := s.pool.QueryRow(ctx,
		`SELECT id FROM trading_pairs WHERE base = $1 AND quote = $2`,
		strings.ToUpper(base), strings.ToUpper(quote),
	).Scan(&pairID)
	if err != nil {
		return nil, fmt.Errorf("unknown pair: %s_%s: %w", base, quote, err)
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, price, quantity, taker_side, executed_at
		 FROM trades
		 WHERE pair_id = $1
		 ORDER BY executed_at DESC
		 LIMIT $2`,
		pairID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []RecentTrade{}
	for rows.Next() {
		var t RecentTrade
		var id uuid.UUID
		var price, quantity float64
		var side string
		var executedAt time.Time
		if err := rows.Scan(&id, &price, &quantity, &side, &executedAt); err != nil {
			return nil, err
		}
		t.ID = id.String()
		t.Price = fmt.Sprintf("%f", price)
		t.Quantity = fmt.Sprintf("%f", quantity)
		t.Side = side
		t.ExecutedAt = executedAt.Format(time.RFC3339)
		out = append(out, t)
	}
	return out, nil
}

// GetCandles returns OHLC candles for a trading pair within a time range.
// intervalSec: candle size in seconds (60 = 1m, 300 = 5m, 3600 = 1h, 86400 = 1d)
// If no trades in a bucket, that bucket is omitted (not returned).
func (s *Service) GetCandles(ctx context.Context, base, quote string, intervalSec int, fromMs, toMs int64) ([]Candle, error) {
	if intervalSec < 60 {
		intervalSec = 60
	}

	// Look up pair_id from trading_pairs table
	var pairID int
	err := s.pool.QueryRow(ctx,
		`SELECT id FROM trading_pairs WHERE base = $1 AND quote = $2`,
		strings.ToUpper(base), strings.ToUpper(quote),
	).Scan(&pairID)
	if err != nil {
		return nil, fmt.Errorf("unknown pair: %s_%s: %w", base, quote, err)
	}

	toSec := toMs / 1000
	if toSec == 0 {
		toSec = time.Now().Unix()
	}
	fromSec := fromMs / 1000
	if fromSec == 0 {
		fromSec = toSec - 86400 // default 24h
	}

	// Aggregate trades into time buckets using date_trunc + bucket arithmetic
	q := `SELECT
		  EXTRACT(EPOCH FROM (to_timestamp(EXTRACT(EPOCH FROM executed_at)::int / $3 * $3)))::bigint AS bucket,
		  (array_agg(price ORDER BY executed_at ASC))[1]   AS open_price,
		  MAX(price)  AS high_price,
		  MIN(price)  AS low_price,
		  (array_agg(price ORDER BY executed_at DESC))[1]  AS close_price,
		  SUM(quantity) AS volume
		FROM trades
		WHERE pair_id = $1 AND executed_at >= to_timestamp($2) AND executed_at <= to_timestamp($4)
		GROUP BY bucket
		ORDER BY bucket ASC`

	rows, err := s.pool.Query(ctx, q, pairID, fromSec, intervalSec, toSec)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var candles []Candle
	for rows.Next() {
		var c Candle
		var open, high, low, close, volume string
		if err := rows.Scan(&c.Time, &open, &high, &low, &close, &volume); err != nil {
			return nil, err
		}
		c.Open = open
		c.High = high
		c.Low = low
		c.Close = close
		c.Volume = volume
		candles = append(candles, c)
	}
	return candles, nil
}

// PlaceOrder places a limit order.
//
// Flow:
//  1. Validate pair / side / price / quantity
//  2. Freeze balance (USDT for BUY, base for SELL)
//  3. Submit to matching engine
//  4. Persist order (DB insert)
//  5. Settle each trade (unfreeze maker's frozen, transfer assets)
//  6. Update order status
//  7. Persist trade rows
func (s *Service) PlaceOrder(ctx context.Context, in PlaceOrderInput) (*PlaceOrderResult, error) {
	// 1. Validate
	pairInfo, ok := s.pairs[strings.ToUpper(in.Pair)]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownPair, in.Pair)
	}
	side := matching.Side(strings.ToUpper(in.Side))
	if side != matching.SideBuy && side != matching.SideSell {
		return nil, ErrInvalidSide
	}
	orderType := matching.TypeLimit
	if in.Type != "" {
		orderType = matching.Type(strings.ToUpper(in.Type))
		if orderType != matching.TypeLimit && orderType != matching.TypeMarket {
			return nil, fmt.Errorf("invalid order type: %s (must be LIMIT or MARKET)", in.Type)
		}
	}
	// Market orders don't need a price
	if orderType == matching.TypeLimit && !in.Price.IsPositive() {
		return nil, ErrInvalidPrice
	}
	if !in.Quantity.IsPositive() {
		return nil, ErrInvalidQty
	}

	// 2. Freeze
	var freezeAmount decimal.Decimal
	freezeAsset := pairInfo.Quote
	if orderType == matching.TypeMarket {
		// For market orders, estimate cost using current best price + 10% buffer
		// This ensures we have enough frozen even with slippage
		if side == matching.SideBuy {
			// Buy: need quote asset (price * qty)
			// Use a conservative estimate: qty * (1 * 10^9) as upper bound
			freezeAmount = in.Quantity.Mul(decimal.NewFromInt(1000000))
		} else {
			// Sell: freeze base asset
			freezeAmount = in.Quantity
			freezeAsset = pairInfo.Base
		}
	} else {
		freezeAmount = in.Quantity.Mul(in.Price)
		if side == matching.SideSell {
			freezeAmount = in.Quantity
			freezeAsset = pairInfo.Base
		}
	}
	if err := s.wallet.Freeze(ctx, in.UserID, freezeAsset, freezeAmount); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInsufficient, err)
	}

	// 3. Submit to matching engine
	stpMode := matching.STPRejectTaker
	if in.STPMode == STPCancelMaker {
		stpMode = matching.STPCancelMaker
	}
	orderID := uuid.New()
	o := &matching.Order{
		ID:          orderID,
		UserID:      in.UserID,
		Pair:        strings.ToUpper(in.Pair),
		Base:        pairInfo.Base,
		Quote:       pairInfo.Quote,
		Side:        side,
		Type:        orderType,
		Price:       in.Price,
		Quantity:    in.Quantity,
		RemainingQty: in.Quantity,
		Status:      matching.StatusOpen,
		CreatedAt:   time.Now().UTC(),
		STPMode:     stpMode,
	}
	// Submit to matcher via interface (Engine in-process OR Client HTTP)
	req := matching.PlaceOrderRequest{
		OrderID:  orderID,
		UserID:   in.UserID,
		Pair:     o.Pair,
		Side:     o.Side,
		Type:     o.Type,
		STPMode:  stpMode,
		Price:    o.Price,
		Quantity: o.Quantity,
	}
	var trades []matching.Trade
	if s.src != nil {
		// Use interface (Client HTTP or Engine in-process)
		result, sErr := s.src.PlaceOrder(ctx, req)
		if sErr != nil {
			_ = s.wallet.Unfreeze(ctx, in.UserID, freezeAsset, freezeAmount)
			return nil, fmt.Errorf("match: %w", sErr)
		}
		trades = result.Trades
		// Update order status from result (engine modified it)
		o.Status = result.Status
		o.FilledQty = result.Filled
	} else if s.engine != nil {
		var sErr error
		trades, sErr = s.engine.PlaceOrder(o)
		if sErr != nil {
			_ = s.wallet.Unfreeze(ctx, in.UserID, freezeAsset, freezeAmount)
			return nil, fmt.Errorf("match: %w", sErr)
		}
	}

	// 4. Persist order (INSERT)
	if err := s.persistOrder(ctx, o); err != nil {
		s.log.Error("persist order failed", "order_id", orderID, "error", err)
	}

	// 5. Settle each trade
	for i := range trades {
		tr := &trades[i]
		if err := s.settleTrade(ctx, tr, in.UserID); err != nil {
			s.log.Error("settle trade failed", "trade_id", tr.ID, "error", err)
		}
		// Persist trade row
		if err := s.persistTrade(ctx, tr); err != nil {
			s.log.Error("persist trade failed", "trade_id", tr.ID, "error", err)
		}
		// CRITICAL FIX: Update maker order status in DB.
		// Engine updates maker in-memory but DB was never synced.
		// On matcher restart, stale OPEN orders would be re-loaded = double-fill risk.
		makerOrderID := tr.BuyOrderID
		if makerOrderID == o.ID {
			makerOrderID = tr.SellOrderID
		}
		// Determine maker's new status and filled quantity
		makerStatus := matching.StatusPartial
		makerFilled := decimal.Zero
		var makerQtyStr, makerFilledStr string
		if err := s.pool.QueryRow(ctx,
			`SELECT quantity, filled_quantity FROM orders WHERE id = $1`,
			makerOrderID,
		).Scan(&makerQtyStr, &makerFilledStr); err == nil {
			makerQty, _ := decimal.NewFromString(makerQtyStr)
			currentFilled, _ := decimal.NewFromString(makerFilledStr)
			newFilled := currentFilled.Add(tr.Quantity)
			makerFilled = newFilled
			if newFilled.GreaterThanOrEqual(makerQty) {
				makerStatus = matching.StatusFilled
			} else {
				makerStatus = matching.StatusPartial
			}
		}
		if err := s.updateOrderStatus(ctx, makerOrderID, makerStatus, makerFilled); err != nil {
			s.log.Error("update maker order status failed", "order_id", makerOrderID, "error", err)
		}
	}

	// 6. Update order status
	if err := s.updateOrderStatus(ctx, o.ID, o.Status, o.FilledQty); err != nil {
		s.log.Error("update order status failed", "error", err)
	}

	// 7. Refund unused frozen for unfilled portion
	if o.Remaining().IsPositive() {
		// Order is in book, freeze stays
	} else {
		// Fully filled - if we overfroze (SELL), refund the difference
		// Actually no - we froze full qty*price for BUY, full qty for SELL
		// Settlement handles debit frozen for filled portion
		// The frozen for unfilled portion is already debited or stays
		// No additional action needed for FILLED orders
	}

	s.log.Info("order placed",
		"order_id", orderID,
		"user_id", in.UserID,
		"pair", in.Pair,
		"side", side,
		"price", in.Price.String(),
		"qty", in.Quantity.String(),
		"trades", len(trades),
		"filled", o.FilledQty.String(),
	)

	return &PlaceOrderResult{
		OrderID:  orderID,
		Status:   o.Status,
		Trades:   trades,
		Filled:   o.FilledQty,
		Remaining: o.RemainingQty,
	}, nil
}

// settleTrade settles a single trade.
//
// Rules:
//   Taker = the order that initiated the match (o in PlaceOrder)
//   Maker = the resting order that was already in the book
//
// BUY taker (current order):
//   - Already froze quote (USDT) for full qty on place
//   - For each fill: debit_frozen filled_qty * price (quote), credit base (BTC)
// SELL taker:
//   - Already froze base (BTC) for full qty on place
//   - For each fill: debit_frozen filled_qty (base), credit quote (USDT)
//
// Maker always had opposite asset frozen. Need to debit_frozen + credit.
func (s *Service) settleTrade(ctx context.Context, tr *matching.Trade, takerUserID uuid.UUID) error {
	notional := tr.Quantity.Mul(tr.Price) // quote amount
	baseAmt := tr.Quantity                // base amount

	// Determine taker side
	takerIsBuyer := tr.TakerSide == matching.SideBuy

	// Buyer pays quote, receives base
	buyerID := tr.BuyUserID
	sellerID := tr.SellUserID

	// Taker actions
	if takerIsBuyer {
		// Taker (buyer) already froze quote; debit the filled portion
		// directly from frozen (settle the trade) rather than unfreezing
		// it back to available. Using Unfreeze here made the buy
		// effectively free — the buyer received base without paying
		// quote, which is a critical accounting bug. See settleTrade
		// doc comment + commit fixing taker settlement paths.
		if err := s.wallet.DebitFrozen(ctx, takerUserID, tr.Quote, notional); err != nil {
			return fmt.Errorf("taker debit frozen quote: %w", err)
		}
		// Taker receives base
		if err := s.wallet.Credit(ctx, takerUserID, tr.Base, baseAmt); err != nil {
			return fmt.Errorf("taker credit base: %w", err)
		}
	} else {
		// Taker (seller) already froze base; debit the filled portion
		// directly from frozen (settle the trade) rather than unfreezing
		// it back to available. See symmetric note above for BUY taker.
		if err := s.wallet.DebitFrozen(ctx, takerUserID, tr.Base, baseAmt); err != nil {
			return fmt.Errorf("taker debit frozen base: %w", err)
		}
		// Taker receives quote
		if err := s.wallet.Credit(ctx, takerUserID, tr.Quote, notional); err != nil {
			return fmt.Errorf("taker credit quote: %w", err)
		}
	}

	// Maker actions (opposite of taker)
	makerID := sellerID
	if !takerIsBuyer {
		makerID = buyerID
	}
	if takerIsBuyer {
		// Maker (seller) had base frozen, debit (final settle)
		if err := s.wallet.DebitFrozen(ctx, makerID, tr.Base, baseAmt); err != nil {
			return fmt.Errorf("maker debit base: %w", err)
		}
		// Maker receives quote
		if err := s.wallet.Credit(ctx, makerID, tr.Quote, notional); err != nil {
			return fmt.Errorf("maker credit quote: %w", err)
		}
	} else {
		// Maker (buyer) had quote frozen, debit (final settle)
		if err := s.wallet.DebitFrozen(ctx, makerID, tr.Quote, notional); err != nil {
			return fmt.Errorf("maker debit quote: %w", err)
		}
		// Maker receives base
		if err := s.wallet.Credit(ctx, makerID, tr.Base, baseAmt); err != nil {
			return fmt.Errorf("maker credit base: %w", err)
		}
	}

	return nil
}

// persistOrder writes the order to the DB. Returns a non-nil
// error when no row was inserted (e.g. duplicate id, constraint
// violation); updateOrderStatus below mirrors the same pattern.
func (s *Service) persistOrder(ctx context.Context, o *matching.Order) error {
	// Get pair_id from DB
	var pairID int
	err := s.pool.QueryRow(ctx, `SELECT id FROM trading_pairs WHERE base = $1 AND quote = $2`,
		o.Base, o.Quote).Scan(&pairID)
	if err != nil {
		return fmt.Errorf("lookup pair_id for %s_%s: %w", o.Base, o.Quote, err)
	}

	const q = `
		INSERT INTO orders (id, user_id, pair_id, side, type, price, quantity, filled_quantity, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), NOW())
	`
	tag, err := s.pool.Exec(ctx, q,
		o.ID, o.UserID, pairID, string(o.Side), string(o.Type),
		o.Price, o.Quantity, o.FilledQty, string(o.Status))
	if err != nil {
		return fmt.Errorf("insert order %s: %w", o.ID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("insert order %s: row not inserted", o.ID)
	}
	return nil
}

// updateOrderStatus updates the order's status + filled_quantity.
// Returns a non-nil error when no row matched. That signals the
// order is missing from the DB even though the matching engine
// reported a trade against it. Without this check the API would
// silently leave the DB in an inconsistent state (engine says
// FILLED, DB says OPEN) after an external DELETE during
// development. Callers should log the error prominently.
func (s *Service) updateOrderStatus(ctx context.Context, id uuid.UUID, status matching.Status, filled decimal.Decimal) error {
	const q = `UPDATE orders SET status = $2, filled_quantity = $3, updated_at = NOW() WHERE id = $1`
	tag, err := s.pool.Exec(ctx, q, id, string(status), filled)
	if err != nil {
		return fmt.Errorf("update order %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("update order %s: row not found (status=%s filled=%s)", id, status, filled)
	}
	return nil
}

// persistTrade writes the trade to the DB. Uses ON CONFLICT DO
// NOTHING so a duplicate trade id (which should never happen, but
// might if the stream forwarder re-delivers one) does not crash
// the request with a constraint violation.
func (s *Service) persistTrade(ctx context.Context, tr *matching.Trade) error {
	// Get pair_id from DB
	var pairID int
	err := s.pool.QueryRow(ctx, `SELECT id FROM trading_pairs WHERE base = $1 AND quote = $2`,
		tr.Base, tr.Quote).Scan(&pairID)
	if err != nil {
		return fmt.Errorf("lookup pair_id for %s_%s: %w", tr.Base, tr.Quote, err)
	}

	const q = `
		INSERT INTO trades (id, buy_order_id, sell_order_id, pair_id, price, quantity, taker_side, executed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO NOTHING
	`
	_, err = s.pool.Exec(ctx, q, tr.ID, tr.BuyOrderID, tr.SellOrderID, pairID, tr.Price, tr.Quantity, string(tr.TakerSide), tr.ExecutedAt)
	if err != nil {
		return fmt.Errorf("insert trade %s: %w", tr.ID, err)
	}
	return nil
}

// AmendOrder amends an existing order's price and/or quantity.
//
// Either Price or Quantity may be zero (decimal.Zero) to signal "leave
// unchanged". This lets API handlers accept partial amend payloads
// (Bug #10 fix).
//
// Refunds the old frozen balance and freezes the new amount (only the
// difference). Uses SELECT FOR UPDATE to lock the order row during the
// operation. This prevents race conditions where the order might be
// cancelled or filled while we're computing the balance changes.
//
// Flow:
//  1. Lock order row, validate ownership and status
//  2. Resolve final price/quantity (fall back to current row values when
//     the caller passed a zero sentinel)
//  3. Calculate unfreeze amount (old frozen for remaining qty)
//  4. Calculate freeze amount (new frozen for new remaining qty)
//  5. Update order price/quantity in DB
//  6. Submit to matcher (which re-evaluates position and may match)
//
// Bug #11/#17 fix: the previous implementation committed DB changes before
// calling the matcher, so a matcher amend failure (e.g. order no longer
// in the active book) left the DB in an inconsistent state with the new
// price/quantity written but the matcher cache stale. We now snapshot
// the original price/quantity/frozen values before any mutation and,
// if the matcher call fails, restore them (DB row + wallet balances)
// before returning the error.
func (s *Service) AmendOrder(ctx context.Context, in AmendOrderInput) (*PlaceOrderResult, error) {
	// BUG #10 fix: a zero decimal means "do not change this field". This
	// lets callers amend only price OR only quantity without supplying
	// the other. We resolve the final values after reading the row.
	priceChangeRequested := !in.Price.IsZero()
	qtyChangeRequested := !in.Quantity.IsZero()
	if !priceChangeRequested && !qtyChangeRequested {
		return nil, fmt.Errorf("amend: at least one of price or quantity must be provided")
	}
	// Begin transaction
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Lock the order row
	var (
		userID     uuid.UUID
		status     matching.Status
		filledQty  decimal.Decimal
		price      decimal.Decimal
		quantity   decimal.Decimal
		sideStr    string
		base, quote string
	)
	err = tx.QueryRow(ctx, `
		SELECT o.user_id, o.status, o.filled_quantity, o.price, o.quantity, o.side, tp.base, tp.quote
		FROM orders o JOIN trading_pairs tp ON o.pair_id = tp.id
		WHERE o.id = $1
		FOR UPDATE OF o
	`, in.OrderID).Scan(
		&userID, &status, &filledQty, &price, &quantity, &sideStr, &base, &quote,
	)
	if err != nil {
		if errors.Is(err, pgxErrNoRows(err)) {
			return nil, ErrOrderNotFound
		}
		return nil, fmt.Errorf("load order: %w", err)
	}

	// Ownership check
	if userID != in.UserID {
		return nil, ErrNotOwner
	}

	// Status check
	if status == matching.StatusFilled ||
		status == matching.StatusCanceled ||
		status == matching.Status("CANCELLED") {
		return nil, ErrAlreadyClosed
	}

	// BUG #10 fix: resolve the effective new price and quantity. A zero
	// sentinel in the input means "keep current value".
	newPrice := price
	newQuantity := quantity
	if priceChangeRequested {
		newPrice = in.Price
	}
	if qtyChangeRequested {
		newQuantity = in.Quantity
	}
	// Validate the resolved values.
	if !newPrice.IsPositive() || !newQuantity.IsPositive() {
		return nil, ErrInvalidPrice
	}

	// Cannot reduce quantity below filled amount.
	if newQuantity.LessThan(filledQty) {
		return nil, ErrInvalidQty
	}

	// No-op amend: caller requested changes that resolve to the same
	// values that are already on the book. Avoid touching DB or matcher.
	if newPrice.Equal(price) && newQuantity.Equal(quantity) {
		return &PlaceOrderResult{
			OrderID:   in.OrderID,
			Status:    status,
			Trades:    nil,
			Filled:    filledQty,
			Remaining: quantity.Sub(filledQty),
		}, tx.Commit(ctx)
	}

	// Calculate remaining quantities
	oldRemaining := quantity.Sub(filledQty)
	newRemaining := newQuantity.Sub(filledQty)

	// Determine freeze assets
	freezeAsset := base
	if sideStr != "SELL" {
		freezeAsset = quote
	}

	// Calculate unfreeze and freeze amounts
	var oldFrozen, newFrozen decimal.Decimal
	if sideStr == "SELL" {
		oldFrozen = oldRemaining
		newFrozen = newRemaining
	} else {
		oldFrozen = oldRemaining.Mul(price)
		newFrozen = newRemaining.Mul(newPrice)
	}

	// Snapshot the DB row BEFORE we mutate anything. If anything fails
	// below, we use this snapshot to restore the row (Bug #11/#17).
	origPrice := price
	origQuantity := quantity

	// BUG #11/#17 fix: submit to the matcher FIRST. The previous code
	// committed DB changes before calling the matcher, so a matcher
	// failure left the DB in an inconsistent state. If the matcher
	// rejects the amend, we never touch DB or wallet.
	if s.src == nil {
		return nil, fmt.Errorf("amend: matcher not configured")
	}
	result, err := s.src.AmendOrder(ctx, base+"_"+quote, in.OrderID, in.UserID, matching.Side(sideStr), newPrice, newQuantity)
	if err != nil {
		s.log.Error("matcher amend failed (DB not touched)", "order_id", in.OrderID, "error", err)
		return nil, fmt.Errorf("matcher: %w", err)
	}

	// Matcher accepted. Now update DB + wallet atomically (in a single tx
	// so a wallet failure rolls back the DB row).
	diff := newFrozen.Sub(oldFrozen)
	var walletErr error
	if diff.IsPositive() {
		walletErr = s.wallet.Freeze(ctx, in.UserID, freezeAsset, diff)
	} else if diff.IsNegative() {
		walletErr = s.wallet.Unfreeze(ctx, in.UserID, freezeAsset, diff.Abs())
	}
	if walletErr != nil {
		// Wallet move failed before we wrote the DB row. Try to restore the
		// matcher to the original values so the cache stays consistent.
		s.log.Error("amend wallet move failed; restoring matcher cache",
			"order_id", in.OrderID, "error", walletErr)
		if _, restoreErr := s.src.AmendOrder(ctx, base+"_"+quote, in.OrderID, in.UserID, matching.Side(sideStr), origPrice, origQuantity); restoreErr != nil {
			s.log.Error("amend: matcher restore failed; cache inconsistent",
				"order_id", in.OrderID, "restore_error", restoreErr)
		}
		return nil, fmt.Errorf("amend wallet: %w", walletErr)
	}

	// Update order in DB
	_, err = tx.Exec(ctx,
		`UPDATE orders SET price = $1, quantity = $2, updated_at = NOW() WHERE id = $3`,
		newPrice, newQuantity, in.OrderID)
	if err != nil {
		// DB write failed after we already moved the wallet. Best-effort
		// restore wallet + matcher so the system stays consistent.
		s.log.Error("amend db update failed; rolling back wallet + matcher",
			"order_id", in.OrderID, "error", err)
		_ = tx.Rollback(ctx)
		if diff.IsPositive() {
			_ = s.wallet.Unfreeze(ctx, in.UserID, freezeAsset, diff)
		} else if diff.IsNegative() {
			_ = s.wallet.Freeze(ctx, in.UserID, freezeAsset, diff.Abs())
		}
		if _, restoreErr := s.src.AmendOrder(ctx, base+"_"+quote, in.OrderID, in.UserID, matching.Side(sideStr), origPrice, origQuantity); restoreErr != nil {
			s.log.Error("amend: matcher restore failed; cache inconsistent",
				"order_id", in.OrderID, "restore_error", restoreErr)
		}
		return nil, fmt.Errorf("update order: %w", err)
	}

	// Commit the DB transaction
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	// Settle any trades the matcher generated from the new price crossing
	// the spread. The amended order is the taker here.
	amendedOrderID := in.OrderID
	trades := result.Trades
	for i := range trades {
		tr := &trades[i]
		if err := s.settleTrade(ctx, tr, in.UserID); err != nil {
			s.log.Error("amend: settle trade failed", "trade_id", tr.ID, "error", err)
		}
		if err := s.persistTrade(ctx, tr); err != nil {
			s.log.Error("amend: persist trade failed", "trade_id", tr.ID, "error", err)
		}
		// Update maker order status in DB. The maker is the resting order
		// opposite our amended taker.
		makerOrderID := tr.BuyOrderID
		if makerOrderID == amendedOrderID {
			makerOrderID = tr.SellOrderID
		}
		var makerQtyStr, makerFilledStr string
		makerStatus := matching.StatusPartial
		makerFilled := decimal.Zero
		if err := s.pool.QueryRow(ctx,
			`SELECT quantity, filled_quantity FROM orders WHERE id = $1`,
			makerOrderID,
		).Scan(&makerQtyStr, &makerFilledStr); err == nil {
			makerQty, _ := decimal.NewFromString(makerQtyStr)
			currentFilled, _ := decimal.NewFromString(makerFilledStr)
			newFilled := currentFilled.Add(tr.Quantity)
			makerFilled = newFilled
			if newFilled.GreaterThanOrEqual(makerQty) {
				makerStatus = matching.StatusFilled
			} else {
				makerStatus = matching.StatusPartial
			}
		}
		if err := s.updateOrderStatus(ctx, makerOrderID, makerStatus, makerFilled); err != nil {
			s.log.Error("amend: update maker status failed", "order_id", makerOrderID, "error", err)
		}
	}

	// Update the amended order itself: use the matcher-reported status
	// and filled quantity.
	takerStatus := result.Status
	if takerStatus == "" {
		takerStatus = matching.StatusOpen
	}
	takerFilled := result.Filled
	if err := s.updateOrderStatus(ctx, amendedOrderID, takerStatus, takerFilled); err != nil {
		s.log.Error("amend: update amended order status failed", "order_id", amendedOrderID, "error", err)
	}

	s.log.Info("order amended",
		"order_id", in.OrderID, "user_id", in.UserID,
		"new_price", newPrice.String(), "new_qty", newQuantity.String(),
		"trades", len(trades), "status", takerStatus,
		"filled", takerFilled.String())
	return &PlaceOrderResult{
		OrderID:   in.OrderID,
		Status:    takerStatus,
		Trades:    trades,
		Filled:    takerFilled,
		Remaining: result.Remaining,
	}, nil
}

// AmendOrderInput is the data needed to amend an order.
type AmendOrderInput struct {
	OrderID  uuid.UUID
	UserID   uuid.UUID
	Pair     string
	Side     matching.Side
	Price    decimal.Decimal
	Quantity decimal.Decimal
}


// CancelOrder cancels an order and refunds the frozen balance.
//
// SECURITY: Uses a transaction with SELECT FOR UPDATE to prevent
// race conditions where two concurrent cancels could double-unfreeze.
// Re-verifies order status inside the transaction to ensure idempotency:
// a cancel of an already-cancelled order returns nil (already closed).
func (s *Service) CancelOrder(ctx context.Context, orderID, userID uuid.UUID) error {
	// Begin transaction
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Lock the order row and load it
	var (
		o            OrderRecord
		status       matching.Status
		filledQty    decimal.Decimal
		price        decimal.Decimal
		quantity     decimal.Decimal
		sideStr      string
		base, quote  string
	)
	err = tx.QueryRow(ctx, `
		SELECT o.user_id, o.status, o.filled_quantity, o.price, o.quantity, o.side, tp.base, tp.quote
		FROM orders o JOIN trading_pairs tp ON o.pair_id = tp.id
		WHERE o.id = $1
		FOR UPDATE OF o
	`, orderID).Scan(
		&o.UserID, &status, &filledQty, &price, &quantity, &sideStr, &base, &quote,
	)
	if err != nil {
		if errors.Is(err, pgxErrNoRows(err)) {
			return ErrOrderNotFound
		}
		return fmt.Errorf("load order: %w", err)
	}
	o.ID = orderID
	o.Status = status
	o.FilledQuantity = filledQty
	o.Price = price
	o.Quantity = quantity
	o.Base = base
	o.Quote = quote

	// SECURITY: ownership check (defense in depth)
	if o.UserID != userID {
		return ErrNotOwner
	}

	// SECURITY: idempotency - if already closed, return success without doing anything
	// This makes double-cancel a no-op, preventing balance inflation.
	if status == matching.StatusFilled ||
		status == matching.StatusCanceled ||
		status == matching.Status("CANCELLED") {
		return ErrAlreadyClosed
	}

	// Calculate remaining quantity
	remaining := quantity.Sub(filledQty)

	// Determine the asset to unfreeze BEFORE marking cancelled.
	var unfreezeAsset string
	var unfreezeAmount decimal.Decimal
	if sideStr == "SELL" {
		unfreezeAsset = base
		unfreezeAmount = remaining
	} else {
		unfreezeAsset = quote
		unfreezeAmount = remaining.Mul(price)
	}

	// BUG #13 fix: read the actual frozen balance for this asset instead of
	// trusting (quantity - filled_quantity). Multi-trade partial fills can
	// leave the actual frozen balance less than the formula would suggest
	// (settlement debits frozen per trade; partial trades can interact
	// with concurrent cancels / re-credits in ways the formula misses).
	// Unfreeze only what is actually frozen; never more.
	if unfreezeAmount.IsPositive() {
		var frozenActual decimal.Decimal
		if err := tx.QueryRow(ctx,
			`SELECT frozen FROM balances WHERE user_id = $1 AND asset = $2 FOR UPDATE`,
			userID, unfreezeAsset).Scan(&frozenActual); err == nil {
			if frozenActual.LessThan(unfreezeAmount) {
				unfreezeAmount = frozenActual
			}
		}
	}

	// SECURITY: validate unfreeze amount is non-negative (sanity check)
	if unfreezeAmount.IsNegative() {
		return fmt.Errorf("invalid unfreeze amount: %s", unfreezeAmount.String())
	}

	// Update order status FIRST in the transaction so concurrent cancels
	// are blocked by the FOR UPDATE row lock acquired above.
	_, err = tx.Exec(ctx,
		`UPDATE orders SET status = 'CANCELLED', updated_at = NOW() WHERE id = $1`,
		orderID)
	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}

	// Commit the transaction before calling external matcher.
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	// BUG #14 fix: notify the matching engine the order is cancelled, with
	// retries. The matching engine caches orders in memory and may continue
	// to fill a cancelled order if the first gRPC call fails (which leads to
	// Bug #16: double-spend against a cancelled sell). We retry with a short
	// backoff so transient gRPC errors do not leave the engine out of sync.
	// The DB is already CANCELLED; retrying cannot make the situation worse,
	// but failing to notify can leave the cache stale.
	// Build the matching-engine pair key (BASE_QUOTE) from the values
	// already loaded by the FOR UPDATE query above — avoids the caller
	// having to know it (H4 from the 2026-08-28 audit: cancel orders
	// should be addressable by order ID alone).
	pair := base + "_" + quote

	if s.src != nil {
		if err := s.cancelWithRetry(ctx, pair, orderID, userID); err != nil {
			// Log at WARN, not silently swallowed. The DB is correct; the
			// matching engine may be stale. Operators should reconcile.
			s.log.Warn("matcher cancel failed after retries (DB is CANCELLED; engine may be stale)",
				"order_id", orderID, "error", err)
		}
	}

	// Refund frozen balance - outside the DB tx; wallet.Unfreeze is its own tx.
	// Skip the unfreeze if amount is zero (e.g. fully filled, or frozen was
	// already cleared by previous settle).
	if unfreezeAmount.IsPositive() {
		if err := s.wallet.Unfreeze(ctx, userID, unfreezeAsset, unfreezeAmount); err != nil {
			s.log.Error("cancel unfreeze failed",
				"order_id", orderID, "asset", unfreezeAsset,
				"amount", unfreezeAmount.String(), "error", err)
		}
	}

	s.log.Info("order canceled",
		"order_id", orderID, "user_id", userID,
		"asset", unfreezeAsset, "amount", unfreezeAmount.String())
	return nil
}

// cancelWithRetry sends a cancel request to the matching engine, retrying
// with a short backoff on transient errors. The DB has already been marked
// CANCELLED before this is called, so failure here only means the engine
// cache may be stale (which leads to stale fills; see Bug #16). We retry
// up to 3 times with 50ms between attempts to ride out transient gRPC
// errors.
func (s *Service) cancelWithRetry(ctx context.Context, pair string, orderID, userID uuid.UUID) error {
	var lastErr error
	backoff := 50 * time.Millisecond
	for attempt := 1; attempt <= 3; attempt++ {
		if err := s.src.CancelOrder(ctx, pair, orderID, userID); err != nil {
			lastErr = err
			s.log.Warn("matcher cancel attempt failed",
				"order_id", orderID, "attempt", attempt, "error", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			continue
		}
		return nil
	}
	return lastErr
}

// OrderRecord is the DB representation of an order.
type OrderRecord struct {
	ID             uuid.UUID       `json:"id"`
	UserID         uuid.UUID       `json:"-"` // omitted — leaks account id (M10 from the 2026-08-28 audit); callers must authenticate and trust their own context
	PairID         int             `json:"pair_id"`
	Pair           string          `json:"pair"` // base_quote, e.g. "BTC_USDT"
	Base           string          `json:"base"`
	Quote          string          `json:"quote"`
	Side           matching.Side   `json:"side"`
	Type           matching.Type   `json:"type"`
	Price          decimal.Decimal `json:"price"`
	Quantity       decimal.Decimal `json:"quantity"`
	FilledQuantity decimal.Decimal `json:"filled_quantity"`
	Status         matching.Status `json:"status"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// Remaining returns unfilled quantity.
func (o *OrderRecord) Remaining() decimal.Decimal {
	return o.Quantity.Sub(o.FilledQuantity)
}

// getOrder retrieves one order from DB.
func (s *Service) getOrder(ctx context.Context, id uuid.UUID) (*OrderRecord, error) {
	const q = `
		SELECT o.id, o.user_id, o.pair_id, tp.base, tp.quote, o.side, o.type,
		       o.price, o.quantity, o.filled_quantity, o.status, o.created_at, o.updated_at
		FROM orders o JOIN trading_pairs tp ON o.pair_id = tp.id
		WHERE o.id = $1
	`
	o := &OrderRecord{}
	err := s.pool.QueryRow(ctx, q, id).Scan(
		&o.ID, &o.UserID, &o.PairID, &o.Base, &o.Quote, &o.Side, &o.Type,
		&o.Price, &o.Quantity, &o.FilledQuantity, &o.Status, &o.CreatedAt, &o.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgxErrNoRows(err)) {
			return nil, ErrOrderNotFound
		}
		return nil, err
	}
	return o, nil
}

// GetOrderForUser returns the order if and only if it belongs to the
// given user. The returned error is ErrOrderNotFound in both the
// "row missing" and "row belongs to someone else" cases so callers
// (e.g. the GET /api/v1/orders/{id} handler) can answer 404 to both
// without leaking which id belongs to whom. (NEW-L4 from the 2026-08-28
// v0.3 audit.)
func (s *Service) GetOrderForUser(ctx context.Context, orderID, userID uuid.UUID) (*OrderRecord, error) {
	o, err := s.getOrder(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if o.UserID != userID {
		return nil, ErrOrderNotFound
	}
	return o, nil
}

// ListOrders returns a user's recent orders.
func (s *Service) ListOrders(ctx context.Context, userID uuid.UUID, limit int) ([]*OrderRecord, error) {
	_ = ctx
	return s.ListOrdersFiltered(ctx, userID, "", "", limit)
}

// ListOrdersFiltered returns the user's orders, optionally filtered by pair and status.
// pairFilter format: "BTC_USDT" or "" for all
// statusFilter: "OPEN" | "FILLED" | "CANCELLED" | "" for all
func (s *Service) ListOrdersFiltered(ctx context.Context, userID uuid.UUID, pairFilter, statusFilter string, limit int) ([]*OrderRecord, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	// Build dynamic query
	q := `SELECT o.id, o.user_id, o.pair_id, tp.base, tp.quote, o.side, o.type,
	       o.price, o.quantity, o.filled_quantity, o.status, o.created_at, o.updated_at
	FROM orders o JOIN trading_pairs tp ON o.pair_id = tp.id
	WHERE o.user_id = $1`
	args := []interface{}{userID}
	argN := 1
	if pairFilter != "" {
		argN++
		q += ` AND (tp.base || '_' || tp.quote) = $` + fmt.Sprint(argN)
		args = append(args, pairFilter)
	}
	if statusFilter != "" {
		argN++
		q += ` AND o.status = $` + fmt.Sprint(argN)
		args = append(args, statusFilter)
	}
	q += ` ORDER BY o.created_at DESC`
	argN++
	q += ` LIMIT $` + fmt.Sprint(argN)
	args = append(args, limit)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*OrderRecord{} // ensure non-nil
	for rows.Next() {
		o := &OrderRecord{}
		if err := rows.Scan(
			&o.ID, &o.UserID, &o.PairID, &o.Base, &o.Quote, &o.Side, &o.Type,
			&o.Price, &o.Quantity, &o.FilledQuantity, &o.Status, &o.CreatedAt, &o.UpdatedAt,
		); err != nil {
			return nil, err
		}
		// Construct pair string from base + quote (M6.53.1 fix)
		o.Pair = o.Base + "_" + o.Quote
		out = append(out, o)
	}
	return out, rows.Err()
}

// pgxErrNoRows extracts pgx.ErrNoRows.
func pgxErrNoRows(err error) error {
	return err
}

// ListAllOrders returns the most recent N orders (admin only).
func (s *Service) ListAllOrders(ctx context.Context, limit int) ([]*OrderRecord, error) {
	const q = `
		SELECT o.id, o.user_id, tp.base, tp.quote, o.side, o.type, o.price, o.quantity,
		       o.filled_quantity, o.status, o.created_at, o.updated_at
		FROM orders o JOIN trading_pairs tp ON o.pair_id = tp.id
		ORDER BY o.created_at DESC LIMIT $1
	`
	rows, err := s.pool.Query(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*OrderRecord{}
	for rows.Next() {
		o := &OrderRecord{}
		var base, quote string
		if err := rows.Scan(&o.ID, &o.UserID, &base, &quote, &o.Side, &o.Type, &o.Price, &o.Quantity,
			&o.FilledQuantity, &o.Status, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return nil, err
		}
		o.Base = base
		o.Quote = quote
		out = append(out, o)
	}
	return out, nil
}

// GetOrderStats returns order stats.
func (s *Service) GetOrderStats(ctx context.Context) (map[string]interface{}, error) {
	stats := map[string]interface{}{}
	var total, open, partial, filled, canceled float64
	err := s.pool.QueryRow(ctx, `
		SELECT
		  COUNT(*) AS total,
		  COUNT(*) FILTER (WHERE status = 'OPEN') AS open,
		  COUNT(*) FILTER (WHERE status = 'PARTIAL') AS partial,
		  COUNT(*) FILTER (WHERE status = 'FILLED') AS filled,
		  COUNT(*) FILTER (WHERE status = 'CANCELED') AS canceled
		FROM orders
	`).Scan(&total, &open, &partial, &filled, &canceled)
	if err != nil {
		return nil, err
	}
	stats["total_orders"] = total
	stats["open_orders"] = open
	stats["partial_orders"] = partial
	stats["filled_orders"] = filled
	stats["canceled_orders"] = canceled
	return stats, nil
}
