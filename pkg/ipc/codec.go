package ipc

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// WriteMessage serializes an Envelope with a 4-byte big-endian length prefix.
func WriteMessage(w io.Writer, env Envelope) error {
	data, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("ipc: marshal envelope: %w", err)
	}
	if len(data) > MaxMessageSize {
		return fmt.Errorf("ipc: message exceeds max size (%d > %d)", len(data), MaxMessageSize)
	}

	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, uint32(len(data)))
	if _, err := w.Write(header); err != nil {
		return fmt.Errorf("ipc: write header: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("ipc: write payload: %w", err)
	}
	return nil
}

// ReadMessage deserializes an Envelope from a length-prefixed stream.
func ReadMessage(r io.Reader) (Envelope, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(r, header); err != nil {
		return Envelope{}, fmt.Errorf("ipc: read header: %w", err)
	}

	length := binary.BigEndian.Uint32(header)
	if length == 0 {
		return Envelope{}, io.EOF
	}
	if length > MaxMessageSize {
		return Envelope{}, fmt.Errorf("ipc: message too large (%d)", length)
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return Envelope{}, fmt.Errorf("ipc: read payload: %w", err)
	}

	var env Envelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return Envelope{}, fmt.Errorf("ipc: unmarshal envelope: %w", err)
	}
	return env, nil
}
