package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/goexdev/goexchange/internal/auth"
	"github.com/goexdev/goexchange/internal/notifier"
	"github.com/gorilla/websocket"
)

// NotifWSHub manages WebSocket clients subscribed to per-user notifications.
type NotifWSHub struct {
	mu       sync.RWMutex
	clients  map[*notifWSClient]struct{}
	notifier *notifier.Service
	authSvc  *auth.Service
	log      *slog.Logger
	upgrader websocket.Upgrader
}

// notifWSClient is one connected WebSocket client.
type notifWSClient struct {
	hub    *NotifWSHub
	conn   *websocket.Conn
	userID uuid.UUID
	send   chan []byte
	subCh  <-chan notifier.Notification
	ctx    context.Context
	cancel context.CancelFunc
}

// notifWSMsg is the WS message payload.
type notifWSMsg struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

// NewNotifWSHub creates a hub and starts the stats broadcaster.
func NewNotifWSHub(notifier *notifier.Service, authSvc *auth.Service, log *slog.Logger) *NotifWSHub {
	h := &NotifWSHub{
		clients:  make(map[*notifWSClient]struct{}),
		notifier: notifier,
		authSvc:  authSvc,
		log:      log,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 4096,
			CheckOrigin:     func(r *http.Request) bool { return true },
		},
	}
	go h.statsLoop()
	return h
}

// statsLoop broadcasts server time + online user count every 10s.
func (h *NotifWSHub) statsLoop() {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for range t.C {
		h.broadcastStats()
	}
}

// broadcastStats sends current stats to all connected clients.
func (h *NotifWSHub) broadcastStats() {
	h.mu.RLock()
	count := len(h.clients)
	clients := make([]*notifWSClient, 0, count)
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.RUnlock()

	msg, _ := json.Marshal(notifWSMsg{
		Type: "stats",
		Payload: map[string]any{
			"ts":           time.Now().UTC().Format(time.RFC3339Nano),
			"online_users": count,
		},
	})
	for _, c := range clients {
		select {
		case c.send <- msg:
		default:
			// drop slow clients
		}
	}
}

// ServeWS handles a WebSocket upgrade.
// Auth via:
//  1. Authorization header (set by chi middleware, used by Python/curl tests)
//  2. ?token=<jwt> query param (browser WebSocket can't set headers)
func (h *NotifWSHub) ServeWS(w http.ResponseWriter, r *http.Request) {
	var uidStr string

	// Try header first (set by middleware for HTTP clients)
	if headerUID := userIDFromContext(r.Context()); headerUID != "" {
		uidStr = headerUID
	} else if tok := r.URL.Query().Get("token"); tok != "" {
		// Fallback: validate JWT from query param
		claims, err := h.authSvc.VerifyToken(tok)
		if err != nil {
			h.log.Warn("ws auth failed: invalid token", "error", err)
			http.Error(w, "unauthorized: invalid token", http.StatusUnauthorized)
			return
		}
		uidStr = claims.UserID
	} else {
		http.Error(w, "unauthorized: no token", http.StatusUnauthorized)
		return
	}

	userID, err := uuid.Parse(uidStr)
	if err != nil {
		http.Error(w, "unauthorized: invalid user id", http.StatusUnauthorized)
		return
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Warn("ws upgrade failed", "error", err)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	client := &notifWSClient{
		hub:    h,
		conn:   conn,
		userID: userID,
		send:   make(chan []byte, 32),
		subCh:  h.notifier.Subscribe(userID),
		ctx:    ctx,
		cancel: cancel,
	}

	h.register(client)
	go client.writePump()
	go client.readPump()
}

func (h *NotifWSHub) register(c *notifWSClient) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	h.log.Info("notif ws client connected", "user_id", c.userID, "total", len(h.clients))
}

func (h *NotifWSHub) unregister(c *notifWSClient) {
	h.mu.Lock()
	if _, ok := h.clients[c]; !ok {
		h.mu.Unlock()
		return
	}
	delete(h.clients, c)
	close(c.send)
	c.cancel()
	h.notifier.Unsubscribe(c.userID, c.subCh)
	h.mu.Unlock()
	h.log.Info("notif ws client disconnected", "user_id", c.userID, "total", len(h.clients))
}

func (c *notifWSClient) readPump() {
	defer func() {
		c.conn.Close()
		c.hub.unregister(c)
	}()
	c.conn.SetReadLimit(512)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})
	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
	}
}

func (c *notifWSClient) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	// Initial hello message
	hello, _ := json.Marshal(notifWSMsg{
		Type: "hello",
		Payload: map[string]any{
			"user_id": c.userID.String(),
			"ts":      time.Now().UTC().Format(time.RFC3339Nano),
		},
	})
	c.send <- hello

	for {
		select {
		case msg, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case n, ok := <-c.subCh:
			if !ok {
				return
			}
			data, _ := json.Marshal(notifWSMsg{
				Type:    "notification",
				Payload: n,
			})
			select {
			case c.send <- data:
			default:
				// slow client, drop
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-c.ctx.Done():
			return
		}
	}
}