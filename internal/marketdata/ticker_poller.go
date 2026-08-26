package marketdata

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/goexdev/goexchange/internal/metrics"
)

// TickerSource provides the current ticker for a pair.
// Implemented by matching.Client (production).
type TickerSource interface {
	GetTicker(ctx context.Context, base, quote string) (bestBid, bestAsk string, err error)
}

// TickerPoller periodically fetches tickers for all enabled pairs
// and publishes them to the event bus.
//
// Run via: go poller.Run(ctx) - blocks until ctx is cancelled.
type TickerPoller struct {
	mu       sync.RWMutex
	svc      *Service          // for listing enabled pairs
	src      TickerSource      // for fetching ticker prices
	events   *EventBus         // publish events here
	log      *slog.Logger
	interval time.Duration
	lastTick map[string]Event  // pair -> last ticker
}

// NewTickerPoller creates a poller.
func NewTickerPoller(svc *Service, src TickerSource, log *slog.Logger, interval time.Duration) *TickerPoller {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	return &TickerPoller{
		svc:      svc,
		src:      src,
		events:   svc.Events(),
		log:      log,
		interval: interval,
		lastTick: make(map[string]Event),
	}
}

// Run starts the polling loop. Blocks until ctx is cancelled.
func (p *TickerPoller) Run(ctx context.Context) {
	p.log.Info("ticker poller starting", "interval", p.interval)
	t := time.NewTicker(p.interval)
	defer t.Stop()

	// Tick once immediately
	p.tick(ctx)

	for {
		select {
		case <-ctx.Done():
			p.log.Info("ticker poller stopped")
			return
		case <-t.C:
			p.tick(ctx)
		}
	}
}

// tick fetches tickers for all enabled pairs and publishes events.
func (p *TickerPoller) tick(ctx context.Context) {
	pairs := p.svc.ListEnabledMarkets()
	if len(pairs) == 0 {
		return
	}

	for _, m := range pairs {
		bid, ask, err := p.src.GetTicker(ctx, m.Base, m.Quote)
		p.log.Debug("polled ticker", "pair", m.Base+"_"+m.Quote, "bid", bid, "ask", ask, "err", err)
		if err != nil {
			// Skip individual errors - one bad pair shouldn't break all
			continue
		}

		// Skip if both empty (no data)
		if bid == "" && ask == "" {
			continue
		}

		ev := Event{
			Type: EventTicker,
			Base: m.Base,
			Quote: m.Quote,
			Bid: bid,
			Ask: ask,
			Last: bid, // simplified
		}

		p.events.Publish(ev)
		metrics.TickerUpdatesTotal.Inc()

		p.mu.Lock()
		p.lastTick[m.Base+"_"+m.Quote] = ev
		p.mu.Unlock()
	}
}

// GetLastTick returns the last known ticker for a pair.
func (p *TickerPoller) GetLastTick(pair string) (Event, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	e, ok := p.lastTick[pair]
	return e, ok
}
