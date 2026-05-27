package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// Hub manages the set of active WebSocket connections and broadcasts messages.
type Hub struct {
	mu        sync.RWMutex
	clients   map[*wsClient]struct{}
	broadcast chan interface{}
}

type wsClient struct {
	conn   *websocket.Conn
	send   chan interface{}
	cancel context.CancelFunc
}

// NewHub creates a new Hub.
func NewHub() *Hub {
	return &Hub{
		clients:   make(map[*wsClient]struct{}),
		broadcast: make(chan interface{}, 256),
	}
}

// Run reads from the broadcast channel and fans out to all clients.
// Must be called in a goroutine.
func (h *Hub) Run() {
	for evt := range h.broadcast {
		h.mu.RLock()
		for c := range h.clients {
			select {
			case c.send <- evt:
			default:
				// Slow client — drop the message rather than blocking.
			}
		}
		h.mu.RUnlock()
	}
}

// Broadcast sends an event to all connected clients.
func (h *Hub) Broadcast(evt interface{}) {
	select {
	case h.broadcast <- evt:
	default:
		slog.Warn("ws hub broadcast channel full, dropping event")
	}
}

func (h *Hub) register(c *wsClient) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) unregister(c *wsClient) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
}

// handleWS upgrades the connection and registers the client with the hub.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: false,
	})
	if err != nil {
		slog.Error("ws accept", "err", err)
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	c := &wsClient{
		conn:   conn,
		send:   make(chan interface{}, 32),
		cancel: cancel,
	}
	s.hub.register(c)

	// Writer goroutine: send queued events to the client.
	go func() {
		defer func() {
			s.hub.unregister(c)
			conn.Close(websocket.StatusNormalClosure, "")
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-c.send:
				if !ok {
					return
				}
				if err := wsjson.Write(ctx, conn, evt); err != nil {
					slog.Debug("ws write error", "err", err)
					return
				}
			}
		}
	}()

	// Reader: consume pings and detect disconnects.
	for {
		_, msg, err := conn.Read(ctx)
		if err != nil {
			break
		}
		// Accept ping messages; ignore anything else.
		var ping map[string]json.RawMessage
		_ = json.Unmarshal(msg, &ping)
	}

	cancel()
	s.hub.unregister(c)
}
