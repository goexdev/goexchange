package marketdata

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/goexdev/goexchange/internal/config"
	"github.com/goexdev/goexchange/internal/metrics"
)

// DataSource is the upstream data provider for market data.
type DataSource interface {
	GetOrderBook(ctx context.Context, base, quote string, depth int) (bids, asks []OrderBookLevel, err error)
	GetTicker(ctx context.Context, base, quote string) (bid, ask string, err error)
}

// ErrPairDisabled is returned by GetTicker / GetOrderBook when
// the pair exists in config but enabled=false. The HTTP handler
// maps it to 404 so disabled pairs do not leak cached price data
// to clients polling old endpoints.
var ErrPairDisabled = errors.New("pair disabled")

// OrderBookLevel is one price/qty in the order book.
type OrderBookLevel struct {
	Price    string `json:"price"`
	Quantity string `json:"quantity"`
}

// OrderBook is the full L2 order book for one pair.
type OrderBook struct {
	Pair   string           `json:"pair"`
	Bids   []OrderBookLevel `json:"bids"`
	Asks   []OrderBookLevel `json:"asks"`
	Time   time.Time        `json:"time"`
}

// Trade is one executed trade.
type Trade struct {
	ID        string    `json:"id"`
	Pair      string    `json:"pair"`
	Side      string    `json:"side"` // "buy" or "sell"
	Price     string    `json:"price"`
	Quantity  string    `json:"quantity"`
	Timestamp time.Time `json:"timestamp"`
}

// Candle is one OHLCV candle.
type Candle struct {
	Timestamp time.Time `json:"timestamp"`
	Open      string    `json:"open"`
	High      string    `json:"high"`
	Low       string    `json:"low"`
	Close     string    `json:"close"`
	Volume    string    `json:"volume"`
}

// Market represents a trading pair.
type Market struct {
	Base    string `json:"base"`
	Quote   string `json:"quote"`
	Pair    string `json:"pair"`
	Enabled bool   `json:"enabled"`
}

// Service provides market data + cached list of enabled pairs.
type Service struct {
	src   DataSource
	log   *slog.Logger
	mu    sync.RWMutex
	pairs []config.PairConfig
	events *EventBus // public bus for client subscriptions
}

// NewService creates a new market data service.
func NewService(src DataSource, log *slog.Logger) *Service {
	return &Service{
		src:    src,
		log:    log,
		events: NewEventBus(),
	}
}

// Events returns the public event bus for client subscriptions.
func (s *Service) Events() *EventBus {
	return s.events
}

// SetPairs updates the list of enabled trading pairs from config.
// Safe to call at runtime to hot-reload pairs.
func (s *Service) SetPairs(pairs []config.PairConfig) {
	s.mu.Lock()
	s.pairs = pairs
	s.mu.Unlock()
	s.events.Publish(Event{Type: EventPairsReloaded})

	// Update Prometheus gauges
	enabled := 0
	for _, p := range pairs {
		if p.Enabled {
			enabled++
		}
	}
	metrics.MarketPairsTotal.Set(float64(len(pairs)))
	metrics.MarketPairsEnabled.Set(float64(enabled))
}

// ListMarkets returns all enabled markets dynamically from config.
// Returns all configured pairs (enabled or not) - the frontend can filter.
func (s *Service) ListMarkets() []*Market {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*Market, 0, len(s.pairs))
	for _, p := range s.pairs {
		out = append(out, &Market{
			Base:    p.Base,
			Quote:   p.Quote,
			Pair:    p.Base + "_" + p.Quote,
			Enabled: p.Enabled,
		})
	}
	return out
}

// ListEnabledMarkets returns only enabled markets.
func (s *Service) ListEnabledMarkets() []*Market {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*Market, 0, len(s.pairs))
	for _, p := range s.pairs {
		if !p.Enabled {
			continue
		}
		out = append(out, &Market{
			Base:    p.Base,
			Quote:   p.Quote,
			Pair:    p.Base + "_" + p.Quote,
			Enabled: true,
		})
	}
	return out
}

// GetPair returns a specific pair config.
func (s *Service) GetPair(base, quote string) (*config.PairConfig, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.pairs {
		if p.Base == base && p.Quote == quote {
			return &p, true
		}
	}
	return nil, false
}

// SetPairEnabled updates the enabled state of a single pair.
// Used by admin API to toggle pairs at runtime.
func (s *Service) SetPairEnabled(base, quote string, enabled bool) bool {
	s.mu.Lock()
	found := false
	for i := range s.pairs {
		if s.pairs[i].Base == base && s.pairs[i].Quote == quote {
			s.pairs[i].Enabled = enabled
			found = true
			break
		}
	}
	s.mu.Unlock()
	if found {
		s.events.Publish(Event{
			Type:    EventPairToggled,
			Base:    base,
			Quote:   quote,
			Enabled: enabled,
		})
	}
	return found
}

// Ticker is the current price state of a pair.
type Ticker struct {
	Pair string `json:"pair"`
	Bid  string `json:"bid"`
	Ask  string `json:"ask"`
	Last string `json:"last"`
}

// GetTicker returns ticker for a market. Returns ErrPairDisabled
// (or ErrUnknownPair) when the pair is not in the enabled list,
// which the HTTP handler maps to 404 so disabled pairs do not
// leak cached price data.
func (s *Service) GetTicker(ctx context.Context, base, quote string) (*Ticker, error) {
	if !s.pairEnabled(base, quote) {
		return nil, ErrPairDisabled
	}
	bid, ask, err := s.src.GetTicker(ctx, base, quote)
	if err != nil {
		return nil, err
	}
	return &Ticker{
		Pair: base + "_" + quote,
		Bid:  bid,
		Ask:  ask,
		Last: bid, // simplified
	}, nil
}

// GetOrderBook returns the order book for a pair. Same
// enabled-only gate as GetTicker.
func (s *Service) GetOrderBook(ctx context.Context, base, quote string, depth int) (*OrderBook, error) {
	if !s.pairEnabled(base, quote) {
		return nil, ErrPairDisabled
	}
	bids, asks, err := s.src.GetOrderBook(ctx, base, quote, depth)
	if err != nil {
		return nil, err
	}
	return &OrderBook{
		Pair: base + "_" + quote,
		Bids: bids,
		Asks: asks,
		Time: time.Now(),
	}, nil
}

// pairEnabled returns true when the (base, quote) pair exists in
// the loaded config and is enabled. Used as a gate on every
// public data endpoint so a disabled pair disappears from the
// UI immediately on restart.
func (s *Service) pairEnabled(base, quote string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.pairs {
		if p.Base == base && p.Quote == quote {
			return p.Enabled
		}
	}
	return false
}

// (GetRecentTrades and GetCandles are handled by TradingSvc, not MarketDataSvc)
