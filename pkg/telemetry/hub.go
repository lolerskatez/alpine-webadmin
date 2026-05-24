package telemetry

import "sync"

// Conn represents a telemetry subscriber connection.
type Conn struct {
	SendCh chan []byte // buffered send channel
}

// NewConn creates a subscriber with a buffered send channel.
func NewConn(buffer int) *Conn {
	return &Conn{SendCh: make(chan []byte, buffer)}
}

// Hub manages subscriber connections and fans out telemetry broadcasts.
type Hub struct {
	mu    sync.RWMutex
	conns map[*Conn]struct{}
}

// NewHub creates a new broadcast hub.
func NewHub() *Hub {
	return &Hub{conns: make(map[*Conn]struct{})}
}

// Register adds a connection to the hub.
func (h *Hub) Register(c *Conn) {
	h.mu.Lock()
	h.conns[c] = struct{}{}
	h.mu.Unlock()
}

// Unregister removes a connection from the hub.
func (h *Hub) Unregister(c *Conn) {
	h.mu.Lock()
	delete(h.conns, c)
	h.mu.Unlock()
}

// Count returns the number of registered connections.
func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.conns)
}

// Broadcast sends msg to all registered connections. Slow consumers drop messages.
func (h *Hub) Broadcast(msg []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.conns {
		select {
		case c.SendCh <- msg:
		default:
			// slow consumer: drop message
		}
	}
}
