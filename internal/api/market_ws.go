package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/goexdev/goexchange/internal/marketdata"
	"github.com/gorilla/websocket"
)

// MarketWSHub broadcasts market data events to all connected clients.
//
// Clients connect to /ws/markets (public, no auth) and receive JSON messages:
//   {"type": "pair.toggled", "base": "BTC", "quote": "USDT", "enabled": true}
//   {"type": "pair.added", ...}
//   {"type": "pair.removed", ...}
//   {"type": "pairs.reloaded"}
type MarketWSHub struct {
	mu        sync.RWMutex
	clients   map[*marketWSClient]struct{}
	bus       *marketdata.EventBus
	subID     int // our subscription id on the bus
	upgrader  websocket.Upgrader
	log       *slog.Logger
}

// marketWSClient is one connected WebSocket client.
type marketWSClient struct {
	hub    *MarketWSHub
	conn   *websocket.Conn
	send   chan []byte
	ctx    context.Context
	cancel context.CancelFunc
}

// marketWSMsg is the message format sent to clients.
type marketWSMsg struct {
	Type    string `json:"type"`
	Base    string `json:"base,omitempty"`
	Quote   string `json:"quote,omitempty"`
	Enabled bool   `json:"enabled,omitempty"`
	Bid     string `json:"bid,omitempty"`
	Ask     string `json:"ask,omitempty"`
	Last    string `json:"last,omitempty"`
	Time    string `json:"time"`
}

// NewMarketWSHub creates a hub and subscribes to the marketdata event bus.
func NewMarketWSHub(bus *marketdata.EventBus, log *slog.Logger) *MarketWSHub {
	h := &MarketWSHub{
		clients: make(map[*marketWSClient]struct{}),
		bus:     bus,
		log:     log,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 4096,
			CheckOrigin:     func(r *http.Request) bool { return true },
		},
	}
	// Subscribe to event bus
	subID, ch := bus.Subscribe()
	h.subID = subID
	go h.forward(ch)
	return h
}

// forward relays events from the bus to all connected clients.
func (h *MarketWSHub) forward(events <-chan marketdata.Event) {
	for e := range events {
		msg := marketWSMsg{
			Type:    string(e.Type),
			Base:    e.Base,
			Quote:   e.Quote,
			Enabled: e.Enabled,
			Bid:     e.Bid,
			Ask:     e.Ask,
			Last:    e.Last,
			Time:    time.Now().UTC().Format(time.RFC3339),
		}
		data, err := json.Marshal(msg)
		if err != nil {
			h.log.Error("ws marshal failed", "error", err)
			continue
		}
		h.broadcast(data)
	}
}

// broadcast sends data to all connected clients (non-blocking).
func (h *MarketWSHub) broadcast(data []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		select {
		case c.send <- data:
		default:
			// Drop if client is slow
		}
	}
}

// ServeWS handles a WebSocket upgrade for /ws/markets.
func (h *MarketWSHub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Warn("ws upgrade failed", "error", err)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	c := &marketWSClient{
		hub:    h,
		conn:   conn,
		send:   make(chan []byte, 32),
		ctx:    ctx,
		cancel: cancel,
	}

	h.register(c)
	go c.writePump()
	go c.readPump()
}

func (h *MarketWSHub) register(c *marketWSClient) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	count := len(h.clients)
	h.mu.Unlock()
	h.log.Info("market ws client connected", "total", count)
}

func (h *MarketWSHub) unregister(c *marketWSClient) {
	h.mu.Lock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.send)
	}
	count := len(h.clients)
	h.mu.Unlock()
	h.log.Info("market ws client disconnected", "total", count)
}

// writePump sends messages from the hub to the client connection.
func (c *marketWSClient) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
		c.cancel()
	}()

	for {
		select {
		case <-c.ctx.Done():
			return
		case msg, ok := <-c.send:
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, nil)
				return
			}
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// readPump handles incoming messages (mostly pings/pongs).
func (c *marketWSClient) readPump() {
	defer c.hub.unregister(c)
	c.conn.SetReadLimit(512)
	_ = c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
	}
}
