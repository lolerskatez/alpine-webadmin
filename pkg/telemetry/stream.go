package telemetry

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/alpine-webadmin/alpine-webadmin/pkg/ws"
)

// StreamConfig controls telemetry WebSocket behavior.
type StreamConfig struct {
	MaxConns       int
	SendBuffer     int
	WriteTimeout   time.Duration
	ReadTimeout    time.Duration
	PingInterval   time.Duration
	ClientTimeout  time.Duration
}

// DefaultStreamConfig returns sensible defaults.
func DefaultStreamConfig() StreamConfig {
	return StreamConfig{
		MaxConns:      10,
		SendBuffer:    256,
		WriteTimeout:  10 * time.Second,
		ReadTimeout:   60 * time.Second,
		PingInterval:  30 * time.Second,
		ClientTimeout: 120 * time.Second,
	}
}

// Stream manages WebSocket connections for telemetry broadcasting.
type Stream struct {
	hub    *Hub
	cfg    StreamConfig
	mu     sync.Mutex
	conns  map[*streamConn]struct{}
	closed bool
	stopCh chan struct{}
	wg     sync.WaitGroup
}

type streamConn struct {
	wsConn   *ws.Conn
	teleConn *Conn
	lastPong time.Time
}

// NewStream creates a telemetry stream manager.
func NewStream(hub *Hub, cfg StreamConfig) *Stream {
	return &Stream{
		hub:    hub,
		cfg:    cfg,
		conns:  make(map[*streamConn]struct{}),
		stopCh: make(chan struct{}),
	}
}

// Accept upgrades an HTTP connection to a WebSocket and starts the telemetry stream.
// Returns true if the connection was accepted, false if at capacity.
func (s *Stream) Accept(nc net.Conn) bool {
	s.mu.Lock()
	if s.closed || len(s.conns) >= s.cfg.MaxConns {
		s.mu.Unlock()
		nc.Close()
		return false
	}

	wsc := ws.NewConn(nc)
	teleConn := NewConn(s.cfg.SendBuffer)
	sc := &streamConn{wsConn: wsc, teleConn: teleConn, lastPong: time.Now()}
	s.conns[sc] = struct{}{}
	s.mu.Unlock()

	s.hub.Register(teleConn)

	s.wg.Add(2)
	go s.readLoop(sc)
	go s.writeLoop(sc)

	return true
}

// readLoop handles incoming frames: ping/pong, close, and timeouts.
func (s *Stream) readLoop(sc *streamConn) {
	defer s.wg.Done()
	defer s.remove(sc)

	sc.wsConn.NetConn().SetReadDeadline(time.Now().Add(s.cfg.ReadTimeout))

	for {
		select {
		case <-s.stopCh:
			return
		default:
		}

		sc.wsConn.NetConn().SetReadDeadline(time.Now().Add(s.cfg.ReadTimeout))
		op, _, err := sc.wsConn.ReadMessage()
		if err != nil {
			return
		}
		if op == ws.OpPong {
			sc.lastPong = time.Now()
		}
	}
}

// writeLoop pumps messages from the Hub to the WebSocket client.
func (s *Stream) writeLoop(sc *streamConn) {
	defer s.wg.Done()
	defer s.remove(sc)

	// Periodic ping to detect dead clients
	ticker := time.NewTicker(s.cfg.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case msg := <-sc.teleConn.SendCh:
			if err := sc.wsConn.WriteMessage(ws.OpText, msg); err != nil {
				return
			}

		case <-ticker.C:
			// Check for client timeout
			if time.Since(sc.lastPong) > s.cfg.ClientTimeout {
				return
			}
			// Send ping frame
			if err := sc.wsConn.WritePing(); err != nil {
				return
			}

		case <-s.stopCh:
			return
		}
	}
}

func (s *Stream) remove(sc *streamConn) {
	s.mu.Lock()
	if _, ok := s.conns[sc]; ok {
		delete(s.conns, sc)
		s.mu.Unlock()
		s.hub.Unregister(sc.teleConn)
		sc.wsConn.WriteClose(1001, "server closing")
	} else {
		s.mu.Unlock()
	}
}

// Count returns active connections.
func (s *Stream) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.conns)
}

// Shutdown gracefully closes all connections and waits for goroutines.
func (s *Stream) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	s.closed = true
	// Copy connections to avoid holding lock during close writes
	toClose := make([]*streamConn, 0, len(s.conns))
	for sc := range s.conns {
		toClose = append(toClose, sc)
	}
	s.mu.Unlock()

	close(s.stopCh)

	// Force-close underlying TCP to unblock read/write goroutines
	for _, sc := range toClose {
		sc.wsConn.NetConn().Close()
	}

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
