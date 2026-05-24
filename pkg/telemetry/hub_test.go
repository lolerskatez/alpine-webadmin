package telemetry

import (
	"testing"
	"time"
)

func TestHubRegisterUnregister(t *testing.T) {
	h := NewHub()
	c1 := NewConn(4)
	c2 := NewConn(4)

	h.Register(c1)
	h.Register(c2)
	if h.Count() != 2 {
		t.Fatalf("Count = %d, want 2", h.Count())
	}

	h.Unregister(c1)
	if h.Count() != 1 {
		t.Fatalf("Count = %d, want 1", h.Count())
	}

	h.Unregister(c2)
	if h.Count() != 0 {
		t.Fatalf("Count = %d, want 0", h.Count())
	}
}

func TestHubBroadcast(t *testing.T) {
	h := NewHub()
	c := NewConn(4)
	h.Register(c)
	defer h.Unregister(c)

	msg := []byte(`{"type":"telemetry"}`)
	h.Broadcast(msg)

	select {
	case got := <-c.SendCh:
		if string(got) != string(msg) {
			t.Errorf("got %q, want %q", got, msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for broadcast")
	}
}

func TestHubBroadcastSlowConsumerDrop(t *testing.T) {
	h := NewHub()
	// buffer of 1, do not read from channel
	c := NewConn(1)
	h.Register(c)
	defer h.Unregister(c)

	// First message fills buffer
	h.Broadcast([]byte("msg1"))
	// Second message should be dropped (non-blocking)
	h.Broadcast([]byte("msg2"))

	// Only msg1 should be available
	select {
	case got := <-c.SendCh:
		if string(got) != "msg1" {
			t.Errorf("got %q, want msg1", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for first message")
	}

	// No second message
	select {
	case <-c.SendCh:
		t.Fatal("slow consumer should have dropped second message")
	case <-time.After(50 * time.Millisecond):
		// expected: no message
	}
}

func BenchmarkHubBroadcast1(b *testing.B) {
	h := NewHub()
	c := NewConn(256)
	h.Register(c)
	defer h.Unregister(c)

	go func() {
		for range c.SendCh {
		}
	}()

	msg := []byte(`{"type":"telemetry","payload":{}}`)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.Broadcast(msg)
	}
}

func BenchmarkHubBroadcast10(b *testing.B) {
	h := NewHub()
	for i := 0; i < 10; i++ {
		c := NewConn(256)
		h.Register(c)
		go func(ch chan []byte) {
			for range ch {
			}
		}(c.SendCh)
	}

	msg := []byte(`{"type":"telemetry","payload":{}}`)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.Broadcast(msg)
	}
}
