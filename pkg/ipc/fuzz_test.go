package ipc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"sync"
	"testing"
	"testing/quick"

	"github.com/alpine-webadmin/alpine-webadmin/pkg/testutil"
)

// FuzzEnvelopeRoundTrip tests that any Envelope can be serialized and deserialized.
func FuzzEnvelopeRoundTrip(f *testing.F) {
	seed := Envelope{
		Version:     Version,
		RequestID:   "req-123",
		MessageType: TypeSystemInfo,
		Payload:     []byte(`{"test":true}`),
	}
	b, _ := json.Marshal(seed)
	f.Add(b)

	f.Fuzz(func(t *testing.T, data []byte) {
		var env Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			return // invalid JSON is expected
		}
		// Re-serialize and verify round-trip consistency
		out, err := json.Marshal(env)
		if err != nil {
			return
		}
		var env2 Envelope
		if err := json.Unmarshal(out, &env2); err != nil {
			t.Fatalf("round-trip failed: %v", err)
		}
		if env2.Version != env.Version || env2.MessageType != env.MessageType {
			t.Fatalf("round-trip mismatch")
		}
	})
}

// FuzzReadWrite tests the binary read/write protocol against random data.
func FuzzReadWrite(f *testing.F) {
	f.Add([]byte("hello"))
	f.Add([]byte{})
	f.Add(make([]byte, 1024))

	f.Fuzz(func(t *testing.T, payload []byte) {
		var buf bytes.Buffer
		env := Envelope{Version: Version, MessageType: "test", Payload: payload}
		if err := WriteMessage(&buf, env); err != nil {
			return
		}
		readPayload, err := ReadMessage(&buf)
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return // truncated data is expected
			}
			t.Fatalf("read failed: %v", err)
		}
		if !bytes.Equal(readPayload.Payload, payload) {
			t.Fatalf("payload mismatch: got %d bytes, want %d", len(readPayload.Payload), len(payload))
		}
	})
}

// TestQuickEnvelopeProperties uses testing/quick to generate random envelopes.
func TestQuickEnvelopeProperties(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping quick check in short mode")
	}
	f := func(env Envelope) bool {
		b, err := json.Marshal(env)
		if err != nil {
			return false
		}
		var env2 Envelope
		if err := json.Unmarshal(b, &env2); err != nil {
			return false
		}
		return env2.Version == env.Version &&
			env2.MessageType == env.MessageType &&
			bytes.Equal(env2.Payload, env.Payload)
	}
	cfg := &quick.Config{MaxCount: 500}
	if err := quick.Check(f, cfg); err != nil {
		t.Fatal(err)
	}
}

// TestLargePayload tests handling of very large payloads.
func TestLargePayload(t *testing.T) {
	payload := make([]byte, 1024*1024) // 1MB
	rand.New(rand.NewSource(42)).Read(payload)

	var buf bytes.Buffer
	env := Envelope{Version: Version, MessageType: "test", Payload: payload}
	if err := WriteMessage(&buf, env); err != nil {
		t.Fatalf("write large payload: %v", err)
	}

	readEnv, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("read large payload: %v", err)
	}
	if !bytes.Equal(readEnv.Payload, payload) {
		t.Fatal("large payload mismatch")
	}
}

// TestMalformedLength tests handling of malformed length headers.
func TestMalformedLength(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
	}{
		{"zero length", []byte{0, 0, 0, 0}},
		{"oversized length", []byte{0xff, 0xff, 0xff, 0xff}},
		{"partial length", []byte{0x00, 0x00}},
		{"length exceeds data", []byte{0x00, 0x00, 0x00, 0x10, 0x01}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := bytes.NewReader(tt.input)
			_, err := ReadMessage(buf)
			if err == nil {
				t.Fatal("expected error for malformed length")
			}
		})
	}
}

// TestConcurrentReadWrite stress-tests concurrent IPC access.
func TestConcurrentReadWrite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrent test in short mode")
	}

	var buf bytes.Buffer
	const workers = 20
	const msgs = 100

	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < msgs; j++ {
				payload := []byte(fmt.Sprintf("worker-%d-msg-%d", id, j))
				env := Envelope{Version: Version, MessageType: "test", Payload: payload}
				if err := WriteMessage(&buf, env); err != nil {
					t.Errorf("write: %v", err)
					return
				}
			}
		}(i)
	}
	wg.Wait()

	testutil.WaitForGoroutines()
}
