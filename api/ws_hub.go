package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// Message is a real-time WebSocket message sent to all clients.
type Message struct {
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

// Hub manages WebSocket connections and broadcasts.
type Hub struct {
	mu      sync.RWMutex
	clients map[*wsClient]struct{}
}

type wsClient struct {
	conn *websocket.Conn
	send chan Message
}

// NewHub creates a new WebSocket hub.
func NewHub() *Hub {
	return &Hub{clients: make(map[*wsClient]struct{})}
}

// Broadcast sends a message to all connected clients.
func (h *Hub) Broadcast(msg Message) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for client := range h.clients {
		select {
		case client.send <- msg:
		default:
			// Client buffer full — skip (don't block)
		}
	}
}

// BroadcastJSON is a helper to broadcast any value as JSON.
func (h *Hub) BroadcastJSON(msgType string, payload any) {
	h.Broadcast(Message{Type: msgType, Payload: payload})
}

// ServeWS handles a new WebSocket connection.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // local-only server
	})
	if err != nil {
		log.Printf("ws accept error: %v", err)
		return
	}

	client := &wsClient{
		conn: conn,
		send: make(chan Message, 64),
	}

	h.mu.Lock()
	h.clients[client] = struct{}{}
	h.mu.Unlock()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Writer goroutine — cancels context on write error so the reader loop exits too.
	go func() {
		defer cancel()
		for {
			select {
			case msg, ok := <-client.send:
				if !ok {
					return
				}
				if err := wsjson.Write(ctx, conn, msg); err != nil {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Reader loop (handle ping/pong + detect disconnect)
	for {
		var raw json.RawMessage
		if err := wsjson.Read(ctx, conn, &raw); err != nil {
			break
		}
		// Echo back event type "ping" with "pong"
		var msg Message
		if err := json.Unmarshal(raw, &msg); err == nil && msg.Type == "ping" {
			_ = wsjson.Write(ctx, conn, Message{Type: "pong"})
		}
	}

	h.mu.Lock()
	delete(h.clients, client)
	h.mu.Unlock()
	conn.Close(websocket.StatusNormalClosure, "bye")
}

// ClientCount returns the number of connected WebSocket clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
