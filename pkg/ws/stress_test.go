package ws

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/alpine-webadmin/alpine-webadmin/pkg/testutil"
)

// stubResponseWriter implements http.ResponseWriter and http.Hijacker for testing.
type stubHijacker struct {
	nc     net.Conn
	bufrw  *bufio.ReadWriter
	header http.Header
}

func (s *stubHijacker) Header() http.Header        { return s.header }
func (s *stubHijacker) Write([]byte) (int, error) { return 0, nil }
func (s *stubHijacker) WriteHeader(int)             {}
func (s *stubHijacker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return s.nc, s.bufrw, nil
}

// handshakeClient performs a minimal WebSocket client handshake.
func handshakeClient(nc net.Conn) error {
	fmt.Fprintf(nc, "GET /ws HTTP/1.1\r\n")
	fmt.Fprintf(nc, "Host: localhost\r\n")
	fmt.Fprintf(nc, "Upgrade: websocket\r\n")
	fmt.Fprintf(nc, "Connection: Upgrade\r\n")
	fmt.Fprintf(nc, "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n")
	fmt.Fprintf(nc, "Sec-WebSocket-Version: 13\r\n")
	fmt.Fprintf(nc, "\r\n")

	br := bufio.NewReader(nc)
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return err
		}
		if line == "\r\n" {
			return nil
		}
	}
}

func TestStressManyConnections(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	var wg sync.WaitGroup
	conns := make(chan *Conn, 100)

	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func(nc net.Conn) {
				defer wg.Done()
				br := bufio.NewReadWriter(bufio.NewReader(nc), bufio.NewWriter(nc))
				w := &stubHijacker{nc: nc, bufrw: br, header: make(http.Header)}
				r, _ := http.NewRequest("GET", "/ws", nil)
				r.Header.Set("Upgrade", "websocket")
				r.Header.Set("Connection", "Upgrade")
				r.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
				c, err := Upgrade(w, r)
				if err != nil {
					nc.Close()
					return
				}
				conns <- c
				// Echo loop
				for {
					op, data, err := c.ReadMessage()
					if err != nil {
						c.Close()
						return
					}
					if op == OpClose {
						c.Close()
						return
					}
					if err := c.WriteMessage(op, data); err != nil {
						c.Close()
						return
					}
				}
			}(nc)
		}
	}()

	// Stress: 50 concurrent clients, each sending 100 messages
	clientCount := 50
	msgsPerClient := 100
	var clientWg sync.WaitGroup

	for i := 0; i < clientCount; i++ {
		clientWg.Add(1)
		go func(id int) {
			defer clientWg.Done()
			nc, err := net.Dial("tcp", ln.Addr().String())
			if err != nil {
				t.Errorf("client %d dial: %v", id, err)
				return
			}
			defer nc.Close()
			if err := handshakeClient(nc); err != nil {
				t.Errorf("client %d handshake: %v", id, err)
				return
			}
			c := NewConn(nc)
			for j := 0; j < msgsPerClient; j++ {
				msg := fmt.Sprintf("client-%d-msg-%d", id, j)
				if err := c.WriteMessage(OpText, []byte(msg)); err != nil {
					t.Errorf("client %d write: %v", id, err)
					return
				}
				_, data, err := c.ReadMessage()
				if err != nil {
					t.Errorf("client %d read: %v", id, err)
					return
				}
				if string(data) != msg {
					t.Errorf("client %d echo mismatch", id)
					return
				}
			}
			c.WriteClose(1000, "done")
		}(i)
	}

	clientWg.Wait()
	ln.Close()
	wg.Wait()
	close(conns)
	for c := range conns {
		c.Close()
	}

	testutil.WaitForGoroutines()
	snap := testutil.SnapshotGoroutines()
	testutil.WaitForGoroutines()
	if snap.Leaked() {
		t.Fatalf("goroutine leak detected: %d goroutines remain", runtime.NumGoroutine())
	}
}

func TestStressRapidOpenClose(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go func(nc net.Conn) {
				br := bufio.NewReadWriter(bufio.NewReader(nc), bufio.NewWriter(nc))
				w := &stubHijacker{nc: nc, bufrw: br, header: make(http.Header)}
				r, _ := http.NewRequest("GET", "/ws", nil)
				r.Header.Set("Upgrade", "websocket")
				r.Header.Set("Connection", "Upgrade")
				r.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
				c, err := Upgrade(w, r)
				if err != nil {
					nc.Close()
					return
				}
				// Read one message then close
				c.ReadMessage()
				c.Close()
			}(nc)
		}
	}()

	// Rapid open/close 200 times
	for i := 0; i < 200; i++ {
		nc, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		handshakeClient(nc)
		c := NewConn(nc)
		c.WriteMessage(OpText, []byte("hello"))
		time.Sleep(5 * time.Millisecond)
		c.Close()
	}

	testutil.WaitForGoroutines()
	snap := testutil.SnapshotGoroutines()
	testutil.WaitForGoroutines()
	if snap.Leaked() {
		t.Fatalf("goroutine leak after rapid open/close: %d goroutines", runtime.NumGoroutine())
	}
}
