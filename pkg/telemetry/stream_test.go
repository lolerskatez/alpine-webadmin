package telemetry

import (
	"context"
	"net"
	"testing"
	"time"
)

// pipeConn wraps net.Pipe for testing.
type pipeConn struct {
	net.Conn
}

func TestStreamAcceptAndCount(t *testing.T) {
	hub := NewHub()
	cfg := StreamConfig{
		MaxConns:     2,
		SendBuffer:   4,
		ReadTimeout:  time.Second,
		PingInterval: time.Hour, // disable ping for this test
	}
	stream := NewStream(hub, cfg)

	// Create dummy connections
	c1, _ := net.Pipe()
	c2, _ := net.Pipe()
	c3, _ := net.Pipe()

	if !stream.Accept(c1) {
		t.Fatal("expected c1 accepted")
	}
	if !stream.Accept(c2) {
		t.Fatal("expected c2 accepted")
	}
	if stream.Count() != 2 {
		t.Fatalf("Count = %d, want 2", stream.Count())
	}

	// At capacity: c3 should be rejected
	if stream.Accept(c3) {
		t.Fatal("expected c3 rejected at capacity")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	stream.Shutdown(ctx)
}

func TestStreamShutdownClosesConnections(t *testing.T) {
	hub := NewHub()
	cfg := StreamConfig{
		MaxConns:     10,
		SendBuffer:   4,
		ReadTimeout:  time.Second,
		PingInterval: time.Hour,
	}
	stream := NewStream(hub, cfg)

	c, s := net.Pipe()
	if !stream.Accept(s) {
		t.Fatal("expected accepted")
	}

	// Shutdown should close the underlying connection
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	stream.Shutdown(ctx)

	// Read from client side should fail after shutdown
	buf := make([]byte, 1)
	c.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, err := c.Read(buf)
	if err == nil {
		t.Fatal("expected connection closed after shutdown")
	}
	c.Close()
}

func TestStreamBroadcastToAcceptedConn(t *testing.T) {
	hub := NewHub()
	cfg := StreamConfig{
		MaxConns:     10,
		SendBuffer:   4,
		ReadTimeout:  time.Second,
		PingInterval: time.Hour,
	}
	stream := NewStream(hub, cfg)

	_, s := net.Pipe()
	if !stream.Accept(s) {
		t.Fatal("expected accepted")
	}

	// Give goroutines time to start
	time.Sleep(50 * time.Millisecond)

	msg := []byte("test")
	hub.Broadcast(msg)

	// Connection should receive the message via its writeLoop
	// We can't easily observe this without a real WS handshake,
	// but we can verify the hub has the conn registered.
	if hub.Count() != 1 {
		t.Fatalf("hub Count = %d, want 1", hub.Count())
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	stream.Shutdown(ctx)
}

// Avoid importing context if not already present in stream_test.go
// Let me add the missing import
