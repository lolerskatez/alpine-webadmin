package ws

import "time"

// WritePing writes a WebSocket ping frame.
func (c *Conn) WritePing() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.nc.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return WriteFrame(c.nc, Frame{Fin: true, OpCode: OpPing, Payload: nil})
}
