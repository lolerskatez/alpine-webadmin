package ipc

import (
	"fmt"
	"net"
	"os"
	"os/user"
	"strconv"
	"syscall"
	"time"
)

// Handler processes a request envelope and returns a response envelope.
type Handler func(req Envelope) Envelope

// Serve listens on a Unix domain socket and dispatches requests to handler.
// It enforces SO_PEERCRED: only connections from expectedUID are accepted.
func Serve(socketPath string, expectedUID int, handler Handler) error {
	// Remove stale socket
	_ = os.Remove(socketPath)

	l, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("ipc: listen %s: %w", socketPath, err)
	}
	defer l.Close()

	// Set ownership to root:webadmin so the unprivileged webadmin process can connect.
	// If the webadmin user/group doesn't resolve, fall back to expectedUID's primary group.
	gid := -1
	if g, err := user.LookupGroup("webadmin"); err == nil {
		if n, err := strconv.Atoi(g.Gid); err == nil {
			gid = n
		}
	}
	if gid < 0 && expectedUID > 0 {
		if u, err := user.LookupId(strconv.Itoa(expectedUID)); err == nil {
			if n, err := strconv.Atoi(u.Gid); err == nil {
				gid = n
			}
		}
	}
	if gid >= 0 {
		if err := os.Chown(socketPath, 0, gid); err != nil {
			return fmt.Errorf("ipc: chown socket: %w", err)
		}
	}

	// Set permissions so only root and webadmin group can connect
	if err := os.Chmod(socketPath, 0660); err != nil {
		return fmt.Errorf("ipc: chmod socket: %w", err)
	}

	for {
		conn, err := l.Accept()
		if err != nil {
			// Log and continue; don't crash the daemon
			continue
		}
		go handleConnection(conn, expectedUID, handler)
	}
}

func handleConnection(conn net.Conn, expectedUID int, handler Handler) {
	defer conn.Close()

	// SO_PEERCRED validation (Linux only)
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return
	}
	f, err := uc.File()
	if err != nil {
		return
	}
	defer f.Close()

	cred, err := syscall.GetsockoptUcred(int(f.Fd()), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	if err != nil {
		return
	}
	if int(cred.Uid) != expectedUID {
		// Unauthorized peer
		return
	}

	// Set read deadline
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	req, err := ReadMessage(conn)
	if err != nil {
		return
	}

	resp := handler(req)
	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_ = WriteMessage(conn, resp)
}
