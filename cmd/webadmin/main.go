package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alpine-webadmin/alpine-webadmin/pkg/auth"
	"github.com/alpine-webadmin/alpine-webadmin/pkg/config"
	"github.com/alpine-webadmin/alpine-webadmin/pkg/ipc"
	"github.com/alpine-webadmin/alpine-webadmin/pkg/log"
	"github.com/alpine-webadmin/alpine-webadmin/pkg/security"
	"github.com/alpine-webadmin/alpine-webadmin/pkg/telemetry"
	"github.com/alpine-webadmin/alpine-webadmin/pkg/version"
	"github.com/alpine-webadmin/alpine-webadmin/pkg/ws"
	"github.com/alpine-webadmin/alpine-webadmin/internal/frontend"
)

var (
	configPath = flag.String("config", "/etc/webadmin/config.json", "path to config file")
	verbose    = flag.Bool("v", false, "verbose logging")
)

func main() {
	flag.Parse()

	logger := log.New(log.Info)
	if *verbose {
		logger = log.New(log.Debug)
	}

	logger.Info("webadmin starting", map[string]interface{}{
		"version": version.Version,
		"go":     runtime.Version(),
	})

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("failed to load config", map[string]interface{}{"error": err.Error()})
		os.Exit(1)
	}

	// ── Subsystems ───────────────────────────────────
	sessionStore := auth.NewStore(cfg.SessionTTL)
	rateLimiter := security.NewLimiter(cfg.RateLimitRPS)
	failedTracker := auth.NewFailedLoginTracker(5, 15*time.Minute)
	ipcClient := ipc.NewClient(cfg.IPCSocket)
	teleHub := telemetry.NewHub()
	teleCollector := telemetry.NewCollector(teleHub, 2*time.Second)

	// ── Background goroutines ────────────────────────
	var wg sync.WaitGroup
	stopCh := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		teleCollector.Run(stopCh)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if n := sessionStore.Sweep(); n > 0 {
					logger.Debug("swept sessions", map[string]interface{}{"count": n})
				}
			case <-stopCh:
				return
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				rateLimiter.Sweep(5 * time.Minute)
				failedTracker.Sweep(30 * time.Minute)
			case <-stopCh:
				return
			}
		}
	}()

	// ── Password hash ──────────────────────────────────
	passwordHash := loadPasswordHash()

	// ── HTTP Router ──────────────────────────────────
	mux := http.NewServeMux()

	// Static assets
	mux.Handle("/assets/", http.FileServer(http.FS(frontend.Assets)))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		data, _ := frontend.Assets.ReadFile("assets/index.html")
		w.Header().Set("Content-Type", "text/html")
		w.Write(data)
	})

	// Unauthenticated API
	mux.HandleFunc("/api/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ip := security.ClientIP(r)
		if !rateLimiter.Allow(ip) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "Rate limited", http.StatusTooManyRequests)
			return
		}
		if failedTracker.IsLocked(ip) {
			w.Header().Set("Retry-After", "900")
			http.Error(w, "Account locked", http.StatusTooManyRequests)
			return
		}

		var req struct {
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		if !auth.CheckPassword(req.Password, passwordHash) {
			failedTracker.RecordFailure(ip)
			logger.Warn("login failed", map[string]interface{}{"ip": ip})
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		failedTracker.RecordSuccess(ip)
		sid, err := sessionStore.CreateWithMetadata("admin", auth.RoleAdmin, ip, r.UserAgent())
		if err != nil {
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}

		auth.SetSessionCookie(w, sid, cfg.SessionTTL)
		csrfToken, _ := security.GenerateCSRFToken()
		security.SetCSRFCookie(w, csrfToken)

		logger.Info("login success", map[string]interface{}{"ip": ip})
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		// Quick IPC ping
		_, _ = ipcClient.Call(ctx, ipc.Envelope{
			Version:     ipc.Version,
			RequestID:   "health",
			MessageType: ipc.TypeSystemInfo,
			Payload:     []byte("{}"),
		})

		resp := map[string]interface{}{
			"status":    "up",
			"version":   version.Version,
			"timestamp": time.Now().Unix(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// Authenticated API
	authenticated := auth.RequireAuth(sessionStore)
	csrf := security.CSRFMiddleware

	mux.Handle("/api/logout", csrf(authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		sid := auth.SessionFromRequest(r)
		sessionStore.Delete(sid)
		auth.ClearSessionCookie(w)
		security.SetCSRFCookie(w, "")
		w.WriteHeader(http.StatusNoContent)
	}))))

	mux.Handle("/api/session", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		sid := auth.SessionFromRequest(r)
		sess, ok := sessionStore.Get(sid)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"username": sess.Username,
		})
	})))

	// IPC-backed API routes
	mux.Handle("/api/services", csrf(authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		forwardIPC(w, r, ipcClient, ipc.TypeServiceList, []byte("{}"))
	}))))

	mux.Handle("/api/services/", csrf(authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/services/")
		parts := strings.Split(path, "/")
		if len(parts) == 0 || parts[0] == "" {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		name := parts[0]

		if r.Method == http.MethodGet {
			payload, _ := json.Marshal(ipc.ServiceStatusReq{Name: name})
			forwardIPC(w, r, ipcClient, ipc.TypeServiceStatus, payload)
			return
		}
		if r.Method == http.MethodPost && len(parts) == 2 {
			action := parts[1]
			payload, _ := json.Marshal(ipc.ServiceControlReq{Name: name, Action: action})
			forwardIPC(w, r, ipcClient, ipc.TypeServiceControl, payload)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}))))

	mux.Handle("/api/system", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		forwardIPC(w, r, ipcClient, ipc.TypeSystemInfo, []byte("{}"))
	})))

	mux.Handle("/api/packages", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		forwardIPC(w, r, ipcClient, ipc.TypePackageList, []byte("{}"))
	})))

	mux.Handle("/api/packages/search", csrf(authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		q := r.URL.Query().Get("q")
		payload, _ := json.Marshal(ipc.PackageSearchReq{Query: q})
		forwardIPC(w, r, ipcClient, ipc.TypePackageSearch, payload)
	}))))

	mux.Handle("/api/packages/info", csrf(authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := r.URL.Query().Get("name")
		payload, _ := json.Marshal(ipc.PackageInfoReq{Name: name})
		forwardIPC(w, r, ipcClient, ipc.TypePackageInfo, payload)
	}))))

	mux.Handle("/api/packages/install", csrf(authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body ipc.PackageInstallReq
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		payload, _ := json.Marshal(body)
		forwardIPC(w, r, ipcClient, ipc.TypePackageInstall, payload)
	}))))

	mux.Handle("/api/packages/remove", csrf(authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body ipc.PackageRemoveReq
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		payload, _ := json.Marshal(body)
		forwardIPC(w, r, ipcClient, ipc.TypePackageRemove, payload)
	}))))

	mux.Handle("/api/packages/update", csrf(authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		forwardIPC(w, r, ipcClient, ipc.TypePackageUpdate, []byte("{}"))
	}))))

	mux.Handle("/api/packages/upgrade", csrf(authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		forwardIPC(w, r, ipcClient, ipc.TypePackageUpgrade, []byte("{}"))
	}))))

	mux.Handle("/api/packages/op", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		opID := r.URL.Query().Get("op_id")
		payload, _ := json.Marshal(ipc.PackageOpQueryReq{OpID: opID})
		forwardIPC(w, r, ipcClient, ipc.TypePackageOpQuery, payload)
	})))

	mux.Handle("/api/services/enable", csrf(authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body ipc.ServiceEnableReq
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		payload, _ := json.Marshal(body)
		forwardIPC(w, r, ipcClient, ipc.TypeServiceEnable, payload)
	}))))

	mux.Handle("/api/services/disable", csrf(authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body ipc.ServiceDisableReq
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		payload, _ := json.Marshal(body)
		forwardIPC(w, r, ipcClient, ipc.TypeServiceDisable, payload)
	}))))

	mux.Handle("/api/network", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Read network interfaces from /proc/net/dev for now
		data, err := os.ReadFile("/proc/net/dev")
		if err != nil {
			http.Error(w, "Not available", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write(data)
	})))

	mux.Handle("/api/storage", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		out, err := exec.Command("df", "-h").Output()
		if err != nil {
			http.Error(w, "Not available", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write(out)
	})))

	mux.Handle("/api/users", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			out, err := exec.Command("getent", "passwd").Output()
			if err != nil {
				http.Error(w, "Not available", http.StatusServiceUnavailable)
				return
			}
			var users []map[string]string
			for _, line := range strings.Split(string(out), "\n") {
				if line == "" {
					continue
				}
				parts := strings.Split(line, ":")
				if len(parts) >= 3 {
					users = append(users, map[string]string{
						"username": parts[0],
						"uid":      parts[2],
						"gid":      parts[3],
						"home":     parts[5],
						"shell":    parts[6],
					})
				}
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(users)
			return
		}
		if r.Method == http.MethodPost {
			var body ipc.UserCreateReq
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "Bad request", http.StatusBadRequest)
				return
			}
			payload, _ := json.Marshal(body)
			forwardIPC(w, r, ipcClient, ipc.TypeUserCreate, payload)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	})))

	mux.Handle("/api/users/", csrf(authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/users/")
		parts := strings.Split(path, "/")
		if len(parts) < 2 || parts[0] == "" {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		username := parts[0]
		action := ""
		if len(parts) >= 2 {
			action = parts[1]
		}
		if r.Method == http.MethodPost && action == "password" {
			payload, _ := json.Marshal(ipc.PasswordResetReq{Username: username})
			forwardIPC(w, r, ipcClient, ipc.TypePasswordReset, payload)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}))))

	mux.Handle("/api/sessions", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var sessions []map[string]interface{}
		for id, s := range sessionStore.All() {
			sessions = append(sessions, map[string]interface{}{
				"id":         id,
				"username":   s.Username,
				"created_at": s.Created.Unix(),
				"ip":         s.ClientIP,
				"user_agent": s.UserAgent,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(sessions)
	})))

	mux.Handle("/api/sessions/", csrf(authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
		sessionStore.Delete(id)
		w.WriteHeader(http.StatusNoContent)
	}))))

	mux.Handle("/api/reboot", csrf(authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body ipc.RebootReq
		json.NewDecoder(r.Body).Decode(&body)
		payload, _ := json.Marshal(body)
		forwardIPC(w, r, ipcClient, ipc.TypeReboot, payload)
	}))))

	mux.Handle("/api/shutdown", csrf(authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body ipc.ShutdownReq
		json.NewDecoder(r.Body).Decode(&body)
		payload, _ := json.Marshal(body)
		forwardIPC(w, r, ipcClient, ipc.TypeShutdown, payload)
	}))))

	mux.Handle("/api/logs", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// SSE streaming of dmesg tail
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		cmd := exec.Command("dmesg", "-w")
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			http.Error(w, "Unavailable", http.StatusServiceUnavailable)
			return
		}
		if err := cmd.Start(); err != nil {
			http.Error(w, "Unavailable", http.StatusServiceUnavailable)
			return
		}
		defer cmd.Process.Kill()
		scanner := bufio.NewScanner(stdout)
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
			return
		}
		for scanner.Scan() {
			fmt.Fprintf(w, "data: %s\n\n", scanner.Text())
			flusher.Flush()
		}
	})))

	mux.Handle("/api/alerts", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// TODO: implement real alert system; return placeholder for now
		alerts := []map[string]interface{}{
			{"level": "info", "message": "System operational", "timestamp": time.Now().Unix()},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(alerts)
	})))

	// WebSocket endpoint
	mux.Handle("/ws", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ws.IsWebSocketUpgrade(r) {
			http.Error(w, "Not a websocket upgrade", http.StatusBadRequest)
			return
		}

		// Connection cap
		if teleHub.Count() >= cfg.WSMaxConns {
			http.Error(w, "Too many websocket connections", http.StatusServiceUnavailable)
			return
		}

		conn, err := ws.Upgrade(w, r)
		if err != nil {
			logger.Warn("websocket upgrade failed", map[string]interface{}{"error": err.Error()})
			return
		}

		// Bridge ws.Conn to telemetry.Conn
		teleConn := telemetry.NewConn(256)
		teleHub.Register(teleConn)
		defer teleHub.Unregister(teleConn)

		// Read goroutine (handles ping/pong/close)
		done := make(chan struct{})
		go func() {
			defer close(done)
			for {
				op, _, err := conn.ReadMessage()
				if err != nil {
					return
				}
				if op == ws.OpClose {
					return
				}
			}
		}()

		// Write goroutine (reads from teleConn.SendCh)
		go func() {
			defer conn.Close()
			for {
				select {
				case msg := <-teleConn.SendCh:
					if err := conn.WriteMessage(ws.OpText, msg); err != nil {
						return
					}
				case <-done:
					return
				}
			}
		}()

		<-done
		conn.Close()
	})))

	// ── Server with middleware ─────────────────────────
	handler := requestLogger(logger)(mux)
	handler = securityHeaders(handler)

	server := &http.Server{
		Addr:         cfg.Listen,
		Handler:      handler,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	// TLS or plain
	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		logger.Error("failed to listen", map[string]interface{}{"error": err.Error()})
		os.Exit(1)
	}

	// ── Graceful shutdown ──────────────────────────────
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		<-sigCh
		logger.Info("shutting down", nil)
		close(stopCh)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}()

	logger.Info("webadmin listening", map[string]interface{}{"addr": cfg.Listen})
	if cfg.TLSCert != "" {
		err = server.ServeTLS(listener, cfg.TLSCert, cfg.TLSKey)
	} else {
		err = server.Serve(listener)
	}
	if err != nil && err != http.ErrServerClosed {
		logger.Error("server error", map[string]interface{}{"error": err.Error()})
		os.Exit(1)
	}

	wg.Wait()
	logger.Info("webadmin stopped", nil)
}

// ── Middleware ───────────────────────────────────────

func requestLogger(logger *log.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &responseWriter{ResponseWriter: w, status: 200}
			next.ServeHTTP(rw, r)
			logger.Info("http request", map[string]interface{}{
				"method":   r.Method,
				"path":     r.URL.Path,
				"status":   rw.status,
				"duration": time.Since(start).String(),
				"ip":       security.ClientIP(r),
			})
		})
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; connect-src 'self' ws: wss:;")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		// HSTS only when TLS is active (detected via TLS connection state)
		if r.TLS != nil {
			w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

// ── Helpers ────────────────────────────────────────

func forwardIPC(w http.ResponseWriter, r *http.Request, client *ipc.Client, msgType string, payload []byte) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	resp, err := client.Call(ctx, ipc.Envelope{
		Version:     ipc.Version,
		RequestID:   fmt.Sprintf("%d", time.Now().UnixNano()),
		MessageType: msgType,
		Payload:     payload,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("IPC error: %v", err), http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if resp.MessageType == ipc.TypeResponseError {
		w.WriteHeader(http.StatusInternalServerError)
	}
	w.Write(resp.Payload)
}

func loadPasswordHash() string {
	data, err := os.ReadFile("/etc/webadmin/passwd")
	if err != nil {
		// Fallback: bcrypt of "admin" for first boot
		h, _ := auth.HashPassword("admin")
		return h
	}
	return strings.TrimSpace(string(data))
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}
