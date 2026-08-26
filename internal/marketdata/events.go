package marketdata

import (
	"sync"
)

// EventType identifies the kind of market data event.
type EventType string

const (
	// EventPairToggled fires when a pair's enabled state changes.
	EventPairToggled EventType = "pair.toggled"
	// EventPairAdded fires when a new pair is added.
	EventPairAdded EventType = "pair.added"
	// EventPairRemoved fires when a pair is removed.
	EventPairRemoved EventType = "pair.removed"
	// EventPairsReloaded fires when the entire pair list is reloaded.
	EventPairsReloaded EventType = "pairs.reloaded"
	// EventTicker fires periodically with the current price of a pair.
	EventTicker EventType = "ticker.update"
	// EventTrade fires whenever the matching engine reports a fill.
	// Sourced from the matching.StreamTrades gRPC stream; see
	// MATCHING_LICENSE_DESIGN §5.3 for the data flow contract.
	EventTrade EventType = "trade.executed"
)

// TradePayload carries trade details when Event.Type == EventTrade.
// Other fields on Event are not used for this type.
type TradePayload struct {
	ID       string `json:"id"`
	Pair     string `json:"pair"`
	Side     string `json:"side"`
	Price    string `json:"price"`
	Quantity string `json:"quantity"`
	BuyerID  string `json:"buyer_user_id,omitempty"`
	SellerID string `json:"seller_user_id,omitempty"`
}

// Event is one market data change.
type Event struct {
	Type    EventType `json:"type"`
	Base    string    `json:"base,omitempty"`
	Quote   string    `json:"quote,omitempty"`
	Enabled bool      `json:"enabled,omitempty"`
	// Ticker fields (only set for EventTicker)
	Bid  string `json:"bid,omitempty"`
	Ask  string `json:"ask,omitempty"`
	Last string `json:"last,omitempty"`
	// Optional volume / change
	Volume24h string `json:"volume24h,omitempty"`
	Change24h string `json:"change24h,omitempty"`
	// Trade fields (only set for EventTrade)
	Trade *TradePayload `json:"trade,omitempty"`
}

// subscriber holds the (send-side) channel + a unique id for lookup.
type subscriber struct {
	id  int
	ch  chan Event
}

// EventBus is a pub/sub bus for market data events.
//
// It is safe for concurrent use. Subscribers receive events on their own
// channel; if the subscriber is slow, events are dropped (non-blocking).
type EventBus struct {
	mu          sync.RWMutex
	subscribers map[int]*subscriber
	nextID      int
	bufferSize  int
}

// NewEventBus creates an event bus.
func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[int]*subscriber),
		bufferSize:  64,
	}
}

// Subscribe returns a new channel that receives all events.
// Caller MUST call Unsubscribe with the returned ID when done.
func (b *EventBus) Subscribe() (int, <-chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.nextID
	b.nextID++
	sub := &subscriber{
		id: id,
		ch: make(chan Event, b.bufferSize),
	}
	b.subscribers[id] = sub
	return id, sub.ch
}

// Unsubscribe removes a subscriber by ID.
// Safe to call multiple times.
func (b *EventBus) Unsubscribe(id int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	sub, ok := b.subscribers[id]
	if !ok {
		return
	}
	delete(b.subscribers, id)
	close(sub.ch)
}

// Publish sends an event to all subscribers.
// Non-blocking: if a subscriber's channel is full, the event is dropped.
func (b *EventBus) Publish(e Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, sub := range b.subscribers {
		select {
		case sub.ch <- e:
		default:
			// Drop event for slow subscriber
		}
	}
}

// SubscriberCount returns the number of active subscribers.
func (b *EventBus) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}
