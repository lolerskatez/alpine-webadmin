package ws

import (
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
)

var wsGUID = []byte("258EAFA5-E914-47DA-95CA-C5AB0DC85B11")

// IsWebSocketUpgrade reports whether the request is a WebSocket upgrade.
func IsWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

// Upgrade hijacks the HTTP connection and performs the WebSocket handshake.
func Upgrade(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	if !IsWebSocketUpgrade(r) {
		return nil, fmt.Errorf("ws: missing upgrade headers")
	}

	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return nil, fmt.Errorf("ws: missing Sec-WebSocket-Key")
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return nil, fmt.Errorf("ws: response writer does not support hijacking")
	}

	// Write handshake response before hijack
	accept := computeAccept(key)
	w.Header().Set("Upgrade", "websocket")
	w.Header().Set("Connection", "Upgrade")
	w.Header().Set("Sec-WebSocket-Accept", accept)
	w.WriteHeader(http.StatusSwitchingProtocols)

	// Hijack
	nc, bufrw, err := hijacker.Hijack()
	if err != nil {
		return nil, fmt.Errorf("ws: hijack failed: %w", err)
	}
	if err := bufrw.Flush(); err != nil {
		nc.Close()
		return nil, fmt.Errorf("ws: flush failed: %w", err)
	}

	return NewConn(nc), nil
}

func computeAccept(key string) string {
	h := sha1.New()
	h.Write([]byte(key))
	h.Write(wsGUID)
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}
