package ipc

import (
	"context"
	"fmt"
	"net"
	"time"
)

// Client connects to the roothelper Unix socket.
type Client struct {
	SocketPath string
}

// NewClient creates an IPC client.
func NewClient(socketPath string) *Client {
	return &Client{SocketPath: socketPath}
}

// Call sends a request to roothelper and returns the response.
// Each call opens a fresh connection (one request/response per connection model).
func (c *Client) Call(ctx context.Context, req Envelope) (Envelope, error) {
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "unix", c.SocketPath)
	if err != nil {
		return Envelope{}, fmt.Errorf("ipc: dial roothelper: %w", err)
	}
	defer conn.Close()

	// Set overall deadline from context
	deadline, ok := ctx.Deadline()
	if ok {
		conn.SetDeadline(deadline)
	} else {
		conn.SetDeadline(time.Now().Add(10 * time.Second))
	}

	if err := WriteMessage(conn, req); err != nil {
		return Envelope{}, fmt.Errorf("ipc: send request: %w", err)
	}

	resp, err := ReadMessage(conn)
	if err != nil {
		return Envelope{}, fmt.Errorf("ipc: read response: %w", err)
	}
	return resp, nil
}
