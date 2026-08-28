// Package analytics computes P&L (Profit & Loss) and platform status.
package analytics

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PairPnL is the aggregate P&L for one trading pair.
//
// Numeric amounts are returned as JSON numbers (not strings) so that
// the SPA can sum / chart / i18n-format them without a parse round-trip
// (M11 from the 2026-08-28 audit). Precision is bounded by the
// upstream `%.8f` formatting — the API does not preserve fractional
// satoshi precision beyond 8 decimal places.
type PairPnL struct {
	Pair            string  `json:"pair"`
	RealizedPnL     float64 `json:"realized_pnl"`
	UnrealizedPnL   float64 `json:"unrealized_pnl"`
	TotalPnL        float64 `json:"total_pnl"`
	TotalBought     float64 `json:"total_bought"`
	TotalSold       float64 `json:"total_sold"`
	CurrentHoldings float64 `json:"current_holdings"`
	AvgBuyPrice     float64 `json:"avg_buy_price"`
	AvgSellPrice    float64 `json:"avg_sell_price"`
	TotalVolume     float64 `json:"total_volume"`
	TotalTrades     int     `json:"total_trades"`
}

// UserPnL is the full P&L summary for a user.
type UserPnL struct {
	UserID          uuid.UUID  `json:"-"` // omitted — leaks account id (M10 from the 2026-08-28 audit)
	GeneratedAt     time.Time  `json:"generated_at"`
	Pairs           []PairPnL  `json:"pairs"`
	TotalPnL        float64    `json:"total_pnl"`
	TotalRealized   float64    `json:"total_realized"`
	TotalUnrealized float64    `json:"total_unrealized"`
	TotalTrades     int        `json:"total_trades"`
	TotalVolume     float64    `json:"total_volume"`
}

// StatusInfo represents platform health.
type StatusInfo struct {
	Status        string            `json:"status"`
	GeneratedAt   time.Time         `json:"generated_at"`
	Version       string            `json:"version"`
	UptimeSeconds int64             `json:"uptime_seconds"`
	Components    []ComponentStatus `json:"components"`
}

// ComponentStatus shows health of one system component.
type ComponentStatus struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	LatencyMs int64  `json:"latency_ms,omitempty"`
	Message   string `json:"message,omitempty"`
}

var startTime = time.Now()

// Service computes analytics.
type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// ComputeUserPnL computes P&L using FIFO accounting.
func (s *Service) ComputeUserPnL(ctx context.Context, userID uuid.UUID) (*UserPnL, error) {
	// Get all trades involving this user, with their actual side
	rows, err := s.pool.Query(ctx, `
		SELECT
			(mp.base || '_' || mp.quote) AS pair,
			t.price::text,
			t.quantity::text,
			CASE
				WHEN t.buy_order_id = o.id THEN 'BUY'
				WHEN t.sell_order_id = o.id THEN 'SELL'
				ELSE t.taker_side
			END AS user_side,
			t.executed_at
		FROM trades t
		JOIN orders o ON (o.id = t.buy_order_id OR o.id = t.sell_order_id)
		JOIN trading_pairs mp ON mp.id = t.pair_id
		WHERE o.user_id = $1
		ORDER BY t.executed_at ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type tradeRow struct {
		pair     string
		price    string
		quantity string
		side     string
		time     time.Time
	}

	trades := []tradeRow{}
	for rows.Next() {
		var t tradeRow
		if err := rows.Scan(&t.pair, &t.price, &t.quantity, &t.side, &t.time); err != nil {
			return nil, err
		}
		trades = append(trades, t)
	}

	// FIFO matching per pair
	type pairState struct {
		buyQueue     []tradeRow
		totalBought  float64
		totalSold    float64
		totalVolume  float64
		realizedPnL  float64
		totalBuyCost float64
		totalSellRev float64
		tradeCount   int
	}
	pairs := map[string]*pairState{}

	parseFloat := func(s string) float64 {
		var f float64
		fmt.Sscanf(s, "%f", &f)
		return f
	}

	for _, t := range trades {
		ps, ok := pairs[t.pair]
		if !ok {
			ps = &pairState{}
			pairs[t.pair] = ps
		}
		ps.tradeCount++
		qty := parseFloat(t.quantity)
		price := parseFloat(t.price)
		if t.side == "BUY" {
			ps.totalBought += qty
			ps.totalBuyCost += qty * price
			ps.buyQueue = append(ps.buyQueue, t)
		} else if t.side == "SELL" {
			ps.totalSold += qty
			ps.totalSellRev += qty * price
			// FIFO match against buy queue
			remaining := qty
			for remaining > 0 && len(ps.buyQueue) > 0 {
				buy := ps.buyQueue[0]
				buyQty := parseFloat(buy.quantity)
				buyPrice := parseFloat(buy.price)
				matchQty := buyQty
				if matchQty > remaining {
					matchQty = remaining
				}
				ps.realizedPnL += (price - buyPrice) * matchQty
				remaining -= matchQty
				if matchQty >= buyQty {
					ps.buyQueue = ps.buyQueue[1:]
				} else {
					ps.buyQueue[0].quantity = fmt.Sprintf("%f", buyQty-matchQty)
				}
			}
		}
		ps.totalVolume += qty * price
	}

	result := &UserPnL{
		UserID:      userID,
		GeneratedAt: time.Now().UTC(),
		Pairs:       []PairPnL{},
	}
	totalRealized := 0.0
	totalTrades := 0
	totalVolume := 0.0
	for pair, ps := range pairs {
		var avgBuy, avgSell float64
		if ps.totalBought > 0 {
			avgBuy = ps.totalBuyCost / ps.totalBought
		}
		if ps.totalSold > 0 {
			avgSell = ps.totalSellRev / ps.totalSold
		}
		holdings := 0.0
		for _, buy := range ps.buyQueue {
			holdings += parseFloat(buy.quantity)
		}
		result.Pairs = append(result.Pairs, PairPnL{
		Pair:            pair,
		RealizedPnL:     round8(ps.realizedPnL),
		UnrealizedPnL:   0,
		TotalPnL:        round8(ps.realizedPnL),
		TotalBought:     round8(ps.totalBought),
		TotalSold:       round8(ps.totalSold),
		CurrentHoldings: round8(holdings),
		AvgBuyPrice:     round8(avgBuy),
		AvgSellPrice:    round8(avgSell),
		TotalVolume:     round8(ps.totalVolume),
		TotalTrades:     ps.tradeCount,
	})
		totalRealized += ps.realizedPnL
		totalTrades += ps.tradeCount
		totalVolume += ps.totalVolume
	}
	result.TotalRealized = round8(totalRealized)
	result.TotalUnrealized = 0
	result.TotalPnL = round8(totalRealized)
	result.TotalTrades = totalTrades
	result.TotalVolume = round8(totalVolume)

	return result, nil
}

// ComputeStatus returns platform health snapshot.
func (s *Service) ComputeStatus(ctx context.Context) *StatusInfo {
	components := []ComponentStatus{
		{Name: "api", Status: "operational", LatencyMs: 5},
		{Name: "matcher", Status: "operational", LatencyMs: 2},
		{Name: "scheduler", Status: "operational"},
		{Name: "database", Status: "operational"},
	}
	start := time.Now()
	if err := s.pool.Ping(ctx); err != nil {
		components[3].Status = "down"
		components[3].Message = err.Error()
	} else {
		components[3].LatencyMs = time.Since(start).Milliseconds()
	}

	overallStatus := "operational"
	for _, c := range components {
		if c.Status == "down" {
			overallStatus = "down"
			break
		}
		if c.Status == "degraded" {
			overallStatus = "degraded"
		}
	}

	return &StatusInfo{
		Status:        overallStatus,
		GeneratedAt:   time.Now().UTC(),
		Version:       "v0.7.0",
		UptimeSeconds: int64(time.Since(startTime).Seconds()),
		Components:    components,
	}
}
// round8 rounds a float64 to 8 decimal places — same precision as the
// previous %.8f string formatting, but returning a float so the JSON
// encoder emits a number instead of a quoted string (M11 fix).
func round8(v float64) float64 {
	if v >= 0 {
		return float64(int64(v*1e8+0.5)) / 1e8
	}
	return float64(int64(v*1e8-0.5)) / 1e8
}
