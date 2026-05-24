package ws

import (
	"bufio"
	"io"
	"net"
	"sync"
	"time"
)

// Conn wraps a net.Conn with WebSocket framing.
type Conn struct {
	nc       net.Conn
	reader   *bufio.Reader
	mu       sync.Mutex
	closed   bool
	sendCh   chan []byte
	shutdown chan struct{}
}

// NewConn wraps an existing net.Conn for WebSocket use.
func NewConn(nc net.Conn) *Conn {
	return &Conn{
		nc:       nc,
		reader:   bufio.NewReaderSize(nc, 4096),
		sendCh:   make(chan []byte, 256),
		shutdown: make(chan struct{}),
	}
}

// NetConn returns the underlying net.Conn.
func (c *Conn) NetConn() net.Conn { return c.nc }

// SendCh returns the outgoing message channel.
func (c *Conn) SendCh() chan []byte { return c.sendCh }

// Close initiates a graceful close.
func (c *Conn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()
	close(c.shutdown)
	return c.nc.Close()
}

// ReadMessage blocks until a text or binary frame is received.
// It handles ping/pong and close frames automatically.
func (c *Conn) ReadMessage() (OpCode, []byte, error) {
	for {
		f, err := ReadFrame(c.reader)
		if err != nil {
			return 0, nil, err
		}
		switch f.OpCode {
		case OpClose:
			return OpClose, f.Payload, io.EOF
		case OpPing:
			if err := c.WritePong(f.Payload); err != nil {
				return 0, nil, err
			}
		case OpPong:
			// ignore
		case OpText, OpBinary:
			return f.OpCode, f.Payload, nil
		}
	}
}

// WriteMessage writes a text or binary frame.
func (c *Conn) WriteMessage(op OpCode, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return io.EOF
	}
	c.nc.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return WriteFrame(c.nc, Frame{Fin: true, OpCode: op, Payload: data})
}

// WritePong writes a pong frame in response to ping.
func (c *Conn) WritePong(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return io.EOF
	}
	return WriteFrame(c.nc, Frame{Fin: true, OpCode: OpPong, Payload: data})
}

// WriteClose writes a close frame and closes the underlying connection.
func (c *Conn) WriteClose(code uint16, reason string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	payload := make([]byte, 2+len(reason))
	payload[0] = byte(code >> 8)
	payload[1] = byte(code)
	copy(payload[2:], reason)
	_ = WriteFrame(c.nc, Frame{Fin: true, OpCode: OpClose, Payload: payload})
	return c.nc.Close()
}
