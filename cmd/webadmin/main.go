package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
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
	ws.MaxFrameSize = uint64(cfg.WSMaxFrameSize)

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

	mux.Handle("/api/system/hostname", csrf(authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body ipc.HostnameSetReq
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		payload, _ := json.Marshal(body)
		forwardIPC(w, r, ipcClient, ipc.TypeHostnameSet, payload)
	}))))

	mux.Handle("/api/packages", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		forwardIPCWithTimeout(w, r, ipcClient, ipc.TypePackageList, []byte("{}"), 60*time.Second)
	})))

	mux.Handle("/api/packages/search", csrf(authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		q := r.URL.Query().Get("q")
		payload, _ := json.Marshal(ipc.PackageSearchReq{Query: q})
		forwardIPCWithTimeout(w, r, ipcClient, ipc.TypePackageSearch, payload, 60*time.Second)
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
		dfBin := findBin("df", "/bin/df", "/usr/bin/df", "/sbin/df")
		cmd := exec.Command(dfBin, "-h")
		// Capture stdout and stderr separately; df commonly exits non-zero
		// when it cannot stat some mounts (e.g. Docker overlays) but its
		// stdout listing is still valid. Filter stderr permission-denied
		// noise rather than failing the request.
		var stdoutBuf, stderrBuf strings.Builder
		cmd.Stdout = &stdoutBuf
		cmd.Stderr = &stderrBuf
		err := cmd.Run()
		stdout := stdoutBuf.String()
		if err != nil {
			if stdout == "" {
				logger.Error("storage: df failed", map[string]interface{}{
					"bin": dfBin, "err": err.Error(), "stderr": stderrBuf.String(),
				})
				http.Error(w, fmt.Sprintf("df failed: %v", err), http.StatusServiceUnavailable)
				return
			}
			logger.Warn("storage: df partial failure", map[string]interface{}{
				"bin": dfBin, "err": err.Error(), "stderr": stderrBuf.String(),
			})
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(stdout))
	})))

	mux.Handle("/api/users", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			getentBin := findBin("getent", "/usr/bin/getent", "/bin/getent")
			out, err := exec.Command(getentBin, "passwd").Output()
			if err != nil {
				logger.Error("users: getent failed", map[string]interface{}{
					"bin": getentBin,
					"err": err.Error(),
				})
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

	mux.Handle("/api/mount", csrf(authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body ipc.MountReq
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		payload, _ := json.Marshal(body)
		forwardIPC(w, r, ipcClient, ipc.TypeMount, payload)
	}))))

	mux.Handle("/api/unmount", csrf(authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body ipc.UnmountReq
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		payload, _ := json.Marshal(body)
		forwardIPC(w, r, ipcClient, ipc.TypeUnmount, payload)
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
		// SSE streaming of system logs. We dial roothelper's privileged log
		// streaming socket so reading /var/log/messages / dmesg happens as
		// root, regardless of webadmin's group membership.
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
			return
		}

		conn, err := net.DialTimeout("unix", cfg.LogsSocket, 5*time.Second)
		if err != nil {
			logger.Error("logs: dial logstream socket", map[string]interface{}{
				"socket": cfg.LogsSocket, "err": err.Error(),
			})
			http.Error(w, fmt.Sprintf("logstream unavailable: %v", err), http.StatusServiceUnavailable)
			return
		}
		defer conn.Close()

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		fmt.Fprintf(w, ": connected\n\n")
		flusher.Flush()

		// Close the upstream socket when the client disconnects so roothelper
		// can clean up the child process.
		ctx := r.Context()
		go func() {
			<-ctx.Done()
			conn.Close()
		}()

		hb := time.NewTicker(15 * time.Second)
		defer hb.Stop()
		lines := make(chan string, 64)
		go func() {
			scanner := bufio.NewScanner(conn)
			scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
			for scanner.Scan() {
				lines <- scanner.Text()
			}
			close(lines)
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case <-hb.C:
				fmt.Fprintf(w, ": ping\n\n")
				flusher.Flush()
			case line, ok := <-lines:
				if !ok {
					return
				}
				fmt.Fprintf(w, "data: %s\n\n", line)
				flusher.Flush()
			}
		}
	})))

	mux.Handle("/api/alerts", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var alerts []map[string]interface{}
		now := time.Now().Unix()

		// Load average
		if data, err := os.ReadFile("/proc/loadavg"); err == nil {
			parts := strings.Fields(string(data))
			if len(parts) > 0 {
				if load, err := strconv.ParseFloat(parts[0], 64); err == nil {
					cores := float64(runtime.NumCPU())
					if load > cores*2 {
						alerts = append(alerts, map[string]interface{}{"level": "error", "message": fmt.Sprintf("High load average: %.2f (cores: %.0f)", load, cores), "timestamp": now})
					} else if load > cores {
						alerts = append(alerts, map[string]interface{}{"level": "warning", "message": fmt.Sprintf("Elevated load average: %.2f (cores: %.0f)", load, cores), "timestamp": now})
					}
				}
			}
		}

		// Memory
		if f, err := os.Open("/proc/meminfo"); err == nil {
			var label string
			var memTotal, memAvailable int64
			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				if n, _ := fmt.Sscanf(scanner.Text(), "%s %d", &label, &memTotal); n == 2 && label == "MemTotal:" {
					break
				}
			}
			_ = f.Close()
			if f2, err := os.Open("/proc/meminfo"); err == nil {
				scanner2 := bufio.NewScanner(f2)
				for scanner2.Scan() {
					if n, _ := fmt.Sscanf(scanner2.Text(), "%s %d", &label, &memAvailable); n == 2 && label == "MemAvailable:" {
						break
					}
				}
				_ = f2.Close()
			}
			if memTotal > 0 {
				used := memTotal - memAvailable
				pct := float64(used) / float64(memTotal) * 100
				if pct > 90 {
					alerts = append(alerts, map[string]interface{}{"level": "error", "message": fmt.Sprintf("Critical memory usage: %.0f%%", pct), "timestamp": now})
				} else if pct > 80 {
					alerts = append(alerts, map[string]interface{}{"level": "warning", "message": fmt.Sprintf("High memory usage: %.0f%%", pct), "timestamp": now})
				}
			}
		}

		// Disk
		dfBin := findBin("df", "/bin/df", "/usr/bin/df", "/sbin/df")
		if out, err := exec.Command(dfBin, "-h").Output(); err == nil {
			for _, line := range strings.Split(string(out), "\n")[1:] {
				fields := strings.Fields(line)
				if len(fields) < 6 {
					continue
				}
				useStr := strings.TrimSuffix(fields[4], "%")
				if pct, err := strconv.Atoi(useStr); err == nil {
					if pct > 95 {
						alerts = append(alerts, map[string]interface{}{"level": "error", "message": fmt.Sprintf("Critical disk usage on %s: %d%%", fields[5], pct), "timestamp": now})
					} else if pct > 85 {
						alerts = append(alerts, map[string]interface{}{"level": "warning", "message": fmt.Sprintf("High disk usage on %s: %d%%", fields[5], pct), "timestamp": now})
					}
				}
			}
		}

		if len(alerts) == 0 {
			alerts = append(alerts, map[string]interface{}{"level": "info", "message": "System operational", "timestamp": now})
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

		// Origin validation
		origin := r.Header.Get("Origin")
		if origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host != r.Host {
				http.Error(w, "Invalid origin", http.StatusForbidden)
				return
			}
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

// findBin returns the first existing path from candidates, falling back to
// exec.LookPath(name), then to the bare name as last resort.
func findBin(name string, candidates ...string) string {
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return name
}

func forwardIPC(w http.ResponseWriter, r *http.Request, client *ipc.Client, msgType string, payload []byte) {
	forwardIPCWithTimeout(w, r, client, msgType, payload, 5*time.Second)
}

func forwardIPCWithTimeout(w http.ResponseWriter, r *http.Request, client *ipc.Client, msgType string, payload []byte, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
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

// Hijack lets the WebSocket upgrade access the underlying connection.
// Without this, middleware-wrapped ResponseWriters fail the http.Hijacker
// type assertion and WebSocket handshakes are rejected.
func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := rw.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response writer does not support hijacking")
	}
	return h.Hijack()
}

// Flush passes through to the underlying writer for SSE (log streaming).
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
