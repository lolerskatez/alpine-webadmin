package ipc

import (
	"bytes"
	"testing"
)

func TestReadWriteMessage(t *testing.T) {
	msg := Envelope{
		Version:     Version,
		RequestID:   "test-1",
		MessageType: TypeSystemInfo,
		Payload:     []byte(`{"hello":"world"}`),
	}

	var buf bytes.Buffer
	if err := WriteMessage(&buf, msg); err != nil {
		t.Fatalf("WriteMessage failed: %v", err)
	}

	got, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("ReadMessage failed: %v", err)
	}

	if got.Version != msg.Version {
		t.Errorf("Version = %d, want %d", got.Version, msg.Version)
	}
	if got.RequestID != msg.RequestID {
		t.Errorf("RequestID = %q, want %q", got.RequestID, msg.RequestID)
	}
	if got.MessageType != msg.MessageType {
		t.Errorf("MessageType = %q, want %q", got.MessageType, msg.MessageType)
	}
	if !bytes.Equal(got.Payload, msg.Payload) {
		t.Errorf("Payload = %q, want %q", got.Payload, msg.Payload)
	}
}

func TestReadMessageTooLarge(t *testing.T) {
	var buf bytes.Buffer
	// Write length header for 100KB (exceeds MaxMessageSize)
	length := make([]byte, 4)
	length[0] = 0
	length[1] = 1
	length[2] = 0x86
	length[3] = 0xA0
	buf.Write(length)
	_, err := ReadMessage(&buf)
	if err == nil {
		t.Fatal("expected error for oversized message")
	}
}

func BenchmarkReadWriteMessage(b *testing.B) {
	msg := Envelope{
		Version:     Version,
		RequestID:   "bench",
		MessageType: TypeSystemInfo,
		Payload:     []byte(`{"hello":"world","foo":"bar","num":42}`),
	}
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		WriteMessage(&buf, msg)
		ReadMessage(&buf)
	}
}
