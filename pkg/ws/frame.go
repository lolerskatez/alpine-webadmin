package ws

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
)

// OpCode represents a WebSocket frame opcode.
type OpCode byte

const (
	OpContinuation OpCode = 0x0
	OpText         OpCode = 0x1
	OpBinary       OpCode = 0x2
	OpClose        OpCode = 0x8
	OpPing         OpCode = 0x9
	OpPong         OpCode = 0xA
)

// Frame represents a single WebSocket frame.
type Frame struct {
	Fin    bool
	OpCode OpCode
	Mask   bool
	MaskKey [4]byte
	Payload []byte
}

// MaxFrameSize is the maximum allowed payload length for a single WebSocket frame.
var MaxFrameSize uint64 = 65536

// ReadFrame reads a single WebSocket frame from r.
func ReadFrame(r io.Reader) (Frame, error) {
	var h [2]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return Frame{}, fmt.Errorf("ws: read header: %w", err)
	}

	fin := h[0]&0x80 != 0
	opcode := OpCode(h[0] & 0x0F)
	masked := h[1]&0x80 != 0
	length := uint64(h[1] & 0x7F)

	if length == 126 {
		var ext [2]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return Frame{}, fmt.Errorf("ws: read extended length: %w", err)
		}
		length = uint64(binary.BigEndian.Uint16(ext[:]))
	} else if length == 127 {
		var ext [8]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return Frame{}, fmt.Errorf("ws: read extended length: %w", err)
		}
		length = binary.BigEndian.Uint64(ext[:])
	}

	if length > MaxFrameSize {
		return Frame{}, fmt.Errorf("ws: frame too large: %d", length)
	}

	var maskKey [4]byte
	if masked {
		if _, err := io.ReadFull(r, maskKey[:]); err != nil {
			return Frame{}, fmt.Errorf("ws: read mask key: %w", err)
		}
	}

	payload := make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			return Frame{}, fmt.Errorf("ws: read payload: %w", err)
		}
	}

	if masked {
		for i := range payload {
			payload[i] ^= maskKey[i%4]
		}
	}

	return Frame{Fin: fin, OpCode: opcode, Mask: masked, MaskKey: maskKey, Payload: payload}, nil
}

// WriteFrame writes a single WebSocket frame to w.
// Server-to-client frames are never masked per RFC 6455.
func WriteFrame(w io.Writer, f Frame) error {
	var header []byte
	b0 := byte(f.OpCode)
	if f.Fin {
		b0 |= 0x80
	}
	header = append(header, b0)

	length := len(f.Payload)
	if length <= 125 {
		header = append(header, byte(length))
	} else if length <= 65535 {
		header = append(header, 126)
		header = binary.BigEndian.AppendUint16(header, uint16(length))
	} else {
		header = append(header, 127)
		header = binary.BigEndian.AppendUint64(header, uint64(length))
	}

	if _, err := w.Write(header); err != nil {
		return fmt.Errorf("ws: write header: %w", err)
	}
	if _, err := w.Write(f.Payload); err != nil {
		return fmt.Errorf("ws: write payload: %w", err)
	}
	return nil
}

// MaskFrame masks the payload in-place and sets the mask bit.
func MaskFrame(f *Frame) {
	if f.Mask {
		return
	}
	rand.Read(f.MaskKey[:])
	for i := range f.Payload {
		f.Payload[i] ^= f.MaskKey[i%4]
	}
	f.Mask = true
}
