package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"sync"
	"syscall"

	"github.com/alpine-webadmin/alpine-webadmin/pkg/log"
)

// serveLogStream listens on a dedicated Unix socket and, for each accepted
// connection from the expected UID (webadmin), spawns a privileged log source
// process (tail -F /var/log/messages, or dmesg -w as fallback) and copies its
// stdout to the connection.
//
// The child process is killed when the client disconnects.
func serveLogStream(socketPath string, expectedUID int, logger *log.Logger) error {
	_ = os.Remove(socketPath)

	l, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("logstream: listen %s: %w", socketPath, err)
	}

	// Set ownership root:webadmin so webadmin can connect.
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
			return fmt.Errorf("logstream: chown: %w", err)
		}
	}
	if err := os.Chmod(socketPath, 0660); err != nil {
		return fmt.Errorf("logstream: chmod: %w", err)
	}

	logger.Info("logstream listening", map[string]interface{}{
		"socket":      socketPath,
		"expectedUID": expectedUID,
	})

	for {
		conn, err := l.Accept()
		if err != nil {
			logger.Warn("logstream: accept failed", map[string]interface{}{"err": err.Error()})
			continue
		}
		go handleLogStream(conn, expectedUID, logger)
	}
}

func handleLogStream(conn net.Conn, expectedUID int, logger *log.Logger) {
	defer conn.Close()

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
		logger.Warn("logstream: rejected peer", map[string]interface{}{"uid": cred.Uid})
		return
	}

	cmd, source := pickLogSource()
	if cmd == nil {
		fmt.Fprintln(conn, "[logstream] no log source available")
		logger.Error("logstream: no log source available", nil)
		return
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		logger.Error("logstream: stdout pipe", map[string]interface{}{"err": err.Error()})
		return
	}
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		logger.Error("logstream: start", map[string]interface{}{"src": source, "err": err.Error()})
		fmt.Fprintf(conn, "[logstream] start failed: %v\n", err)
		return
	}

	logger.Info("logstream: client connected", map[string]interface{}{"src": source})

	// Kill child when connection drops or when we exit.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var once sync.Once
	killChild := func() {
		once.Do(func() {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
		})
	}
	defer killChild()

	// Drain stderr to log (non-fatal).
	if stderr != nil {
		go func() {
			data, _ := io.ReadAll(stderr)
			if len(data) > 0 {
				logger.Warn("logstream: stderr", map[string]interface{}{"src": source, "stderr": string(data)})
			}
		}()
	}

	// Watch for client disconnect.
	go func() {
		buf := make([]byte, 16)
		for {
			if _, err := conn.Read(buf); err != nil {
				cancel()
				killChild()
				return
			}
		}
	}()

	// Copy log lines to client. io.Copy returns when stdout closes or
	// the underlying conn is closed.
	go func() {
		_, _ = io.Copy(conn, stdout)
		cancel()
	}()

	<-ctx.Done()
	logger.Info("logstream: client disconnected", map[string]interface{}{"src": source})
}

// pickLogSource selects the best available log source. As root we can always
// read /var/log/messages if present; fall back to dmesg -w.
func pickLogSource() (*exec.Cmd, string) {
	if _, err := os.Stat("/var/log/messages"); err == nil {
		for _, p := range []string{"/usr/bin/tail", "/bin/tail"} {
			if _, err := os.Stat(p); err == nil {
				return exec.Command(p, "-n", "100", "-F", "/var/log/messages"), "/var/log/messages"
			}
		}
	}
	for _, p := range []string{"/bin/dmesg", "/sbin/dmesg", "/usr/bin/dmesg"} {
		if _, err := os.Stat(p); err == nil {
			return exec.Command(p, "-w"), "dmesg"
		}
	}
	return nil, ""
}
