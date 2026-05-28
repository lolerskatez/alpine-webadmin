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
	"os/user"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alpine-webadmin/alpine-webadmin/internal/frontend"
	"github.com/alpine-webadmin/alpine-webadmin/pkg/auth"
	"github.com/alpine-webadmin/alpine-webadmin/pkg/config"
	"github.com/alpine-webadmin/alpine-webadmin/pkg/ipc"
	"github.com/alpine-webadmin/alpine-webadmin/pkg/log"
	"github.com/alpine-webadmin/alpine-webadmin/pkg/security"
	"github.com/alpine-webadmin/alpine-webadmin/pkg/telemetry"
	"github.com/alpine-webadmin/alpine-webadmin/pkg/version"
	"github.com/alpine-webadmin/alpine-webadmin/pkg/ws"
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
		"go":      runtime.Version(),
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
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		// Try system user authentication first if username is provided
		if req.Username != "" {
			payload, _ := json.Marshal(ipc.LoginVerifyReq{Username: req.Username, Password: req.Password})
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			resp, err := ipcClient.Call(ctx, ipc.Envelope{
				Version:     ipc.Version,
				RequestID:   fmt.Sprintf("%d", time.Now().UnixNano()),
				MessageType: ipc.TypeLoginVerify,
				Payload:     payload,
			})
			cancel()
			if err != nil {
				logger.Warn("login verify IPC error", map[string]interface{}{"ip": ip, "user": req.Username, "err": err.Error()})
			} else if resp.MessageType != ipc.TypeResponseOK {
				logger.Warn("login verify IPC unexpected response", map[string]interface{}{"ip": ip, "user": req.Username, "msgType": resp.MessageType})
			} else {
				var verifyResp ipc.LoginVerifyResp
				if unmarshalErr := json.Unmarshal(resp.Payload, &verifyResp); unmarshalErr != nil {
					logger.Warn("login verify IPC unmarshal error", map[string]interface{}{"ip": ip, "user": req.Username, "err": unmarshalErr.Error()})
				} else if !verifyResp.Valid {
					logger.Warn("login verify IPC invalid credentials", map[string]interface{}{"ip": ip, "user": req.Username})
				} else if verifyResp.Role == "admin" {
					failedTracker.RecordSuccess(ip)
					sid, err := sessionStore.CreateWithMetadata(req.Username, auth.RoleAdmin, ip, r.UserAgent())
					if err != nil {
						http.Error(w, "Internal error", http.StatusInternalServerError)
						return
					}
					auth.SetSessionCookie(w, sid, cfg.SessionTTL)
					csrfToken, _ := security.GenerateCSRFToken()
					security.SetCSRFCookie(w, csrfToken)
					logger.Info("login success", map[string]interface{}{"ip": ip, "user": req.Username})
					w.WriteHeader(http.StatusNoContent)
					return
				} else {
					// Valid system user but not in admin group
					failedTracker.RecordFailure(ip)
					logger.Warn("login failed: not in admin group", map[string]interface{}{"ip": ip, "user": req.Username, "groups": verifyResp.Groups})
					http.Error(w, "Unauthorized", http.StatusUnauthorized)
					return
				}
			}
		}

		// Legacy fallback: config password hash (only for empty username or "admin")
		if req.Username == "" || req.Username == "admin" {
			if auth.CheckPassword(req.Password, passwordHash) {
				failedTracker.RecordSuccess(ip)
				sid, err := sessionStore.CreateWithMetadata("admin", auth.RoleAdmin, ip, r.UserAgent())
				if err != nil {
					http.Error(w, "Internal error", http.StatusInternalServerError)
					return
				}
				auth.SetSessionCookie(w, sid, cfg.SessionTTL)
				csrfToken, _ := security.GenerateCSRFToken()
				security.SetCSRFCookie(w, csrfToken)
				logger.Info("login success (legacy)", map[string]interface{}{"ip": ip})
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}

		failedTracker.RecordFailure(ip)
		logger.Warn("login failed", map[string]interface{}{"ip": ip, "user": req.Username})
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
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

	// Running services status detail (read-only)
	mux.Handle("/api/services/status", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var output []byte
		var err error
		rcStatusBin := findBin("rc-status", "/sbin/rc-status", "/usr/sbin/rc-status")
		if _, statErr := os.Stat(rcStatusBin); statErr == nil {
			output, err = exec.Command(rcStatusBin, "-a").Output()
		}
		if err != nil || len(output) == 0 {
			output = []byte("rc-status not available")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"content": string(output)})
	})))

	mux.Handle("/api/services/", csrf(authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/services/")
		parts := strings.Split(path, "/")
		if len(parts) == 0 || parts[0] == "" {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		name := parts[0]

		if r.Method == http.MethodGet {
			if len(parts) == 2 && parts[1] == "log" {
				payload, _ := json.Marshal(ipc.ServiceLogReadReq{Name: name})
				forwardIPC(w, r, ipcClient, ipc.TypeServiceLogRead, payload)
				return
			}
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

	mux.Handle("/api/system/timezone", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			data, _ := os.ReadFile("/etc/timezone")
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"timezone": strings.TrimSpace(string(data))})
			return
		}
		if r.Method == http.MethodPost {
			var body ipc.TimezoneSetReq
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "Bad request", http.StatusBadRequest)
				return
			}
			payload, _ := json.Marshal(body)
			forwardIPC(w, r, ipcClient, ipc.TypeTimezoneSet, payload)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	})))

	mux.Handle("/api/system/ntp", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			enabled := false
			for _, svc := range []string{"chronyd", "ntpd"} {
				if out, err := exec.Command("/sbin/rc-service", svc, "status").Output(); err == nil {
					if strings.Contains(string(out), "started") {
						enabled = true
						break
					}
				}
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]bool{"enabled": enabled})
			return
		}
		if r.Method == http.MethodPost {
			var body ipc.NtpSetReq
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "Bad request", http.StatusBadRequest)
				return
			}
			payload, _ := json.Marshal(body)
			forwardIPC(w, r, ipcClient, ipc.TypeNtpSet, payload)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	})))

	mux.Handle("/api/system/updates", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		count := 0
		if out, err := exec.Command("apk", "list", "--upgradable").Output(); err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				if strings.TrimSpace(line) != "" {
					count++
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"count": count, "upgradable": count > 0})
	})))

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

	// Package dependency info
	mux.Handle("/api/packages/depends", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := r.URL.Query().Get("name")
		if name == "" {
			http.Error(w, "name required", http.StatusBadRequest)
			return
		}
		var depends, revDepends string
		apkBin := findBin("apk", "/sbin/apk")
		if out, err := exec.Command(apkBin, "info", "-R", name).Output(); err == nil {
			depends = string(out)
		}
		if out, err := exec.Command(apkBin, "info", "-r", name).Output(); err == nil {
			revDepends = string(out)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"depends": depends, "reverse_depends": revDepends})
	})))

	// APK world (explicitly installed packages)
	mux.Handle("/api/packages/world", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		data, _ := os.ReadFile("/etc/apk/world")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"content": string(data)})
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

	mux.Handle("/api/network/interfaces", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		type ifaceInfo struct {
			Name  string `json:"name"`
			State string `json:"state"`
		}
		var interfaces []ifaceInfo
		entries, _ := os.ReadDir("/sys/class/net")
		for _, entry := range entries {
			if !entry.IsDir() && entry.Name() != "lo" {
				stateData, _ := os.ReadFile("/sys/class/net/" + entry.Name() + "/operstate")
				state := strings.TrimSpace(string(stateData))
				if state == "" {
					state = "unknown"
				}
				interfaces = append(interfaces, ifaceInfo{Name: entry.Name(), State: state})
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(interfaces)
	})))

	mux.Handle("/api/network/", csrf(authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/api/network/")
		parts := strings.SplitN(path, "/", 2)
		if len(parts) != 2 {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		iface := parts[0]
		action := parts[1]
		if action == "up" {
			payload, _ := json.Marshal(ipc.NetIfReq{Interface: iface})
			forwardIPC(w, r, ipcClient, ipc.TypeNetIfUp, payload)
			return
		}
		if action == "down" {
			payload, _ := json.Marshal(ipc.NetIfReq{Interface: iface})
			forwardIPC(w, r, ipcClient, ipc.TypeNetIfDown, payload)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}))))

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

	// Disk partitions (read-only)
	mux.Handle("/api/storage/partitions", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var output []byte
		var err error
		fdiskBin := findBin("fdisk", "/sbin/fdisk", "/usr/sbin/fdisk")
		if _, statErr := os.Stat(fdiskBin); statErr == nil {
			output, err = exec.Command(fdiskBin, "-l").Output()
		}
		if err != nil || len(output) == 0 {
			data, _ := os.ReadFile("/proc/partitions")
			output = data
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"content": string(output)})
	})))

	// Disk I/O statistics (read-only)
	mux.Handle("/api/storage/diskstats", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		data, _ := os.ReadFile("/proc/diskstats")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"content": string(data)})
	})))

	mux.Handle("/api/fstab", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			forwardIPC(w, r, ipcClient, ipc.TypeFstabRead, []byte("{}"))
			return
		}
		if r.Method == http.MethodPost {
			var body ipc.FstabWriteReq
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "Bad request", http.StatusBadRequest)
				return
			}
			payload, _ := json.Marshal(body)
			forwardIPC(w, r, ipcClient, ipc.TypeFstabWrite, payload)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	})))

	// Mounted filesystems (read-only)
	mux.Handle("/api/storage/mounts", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		data, _ := os.ReadFile("/proc/mounts")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"content": string(data)})
	})))

	// USB devices (read-only)
	mux.Handle("/api/hardware/usb", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var output []byte
		var err error
		lsusbBin := findBin("lsusb", "/usr/bin/lsusb", "/bin/lsusb")
		if _, statErr := os.Stat(lsusbBin); statErr == nil {
			output, err = exec.Command(lsusbBin).Output()
		}
		if err != nil || len(output) == 0 {
			var lines []string
			entries, _ := os.ReadDir("/sys/bus/usb/devices")
			for _, entry := range entries {
				if strings.Contains(entry.Name(), ":") {
					continue
				}
				vendor, _ := os.ReadFile(filepath.Join("/sys/bus/usb/devices", entry.Name(), "idVendor"))
				product, _ := os.ReadFile(filepath.Join("/sys/bus/usb/devices", entry.Name(), "idProduct"))
				manuf, _ := os.ReadFile(filepath.Join("/sys/bus/usb/devices", entry.Name(), "manufacturer"))
				prod, _ := os.ReadFile(filepath.Join("/sys/bus/usb/devices", entry.Name(), "product"))
				if len(vendor) > 0 || len(manuf) > 0 {
					lines = append(lines, fmt.Sprintf("Bus %s Device %s: ID %s:%s %s %s",
						entry.Name(), entry.Name(), strings.TrimSpace(string(vendor)), strings.TrimSpace(string(product)),
						strings.TrimSpace(string(manuf)), strings.TrimSpace(string(prod))))
				}
			}
			output = []byte(strings.Join(lines, "\n"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"content": string(output)})
	})))

	// PCI devices (read-only)
	mux.Handle("/api/hardware/pci", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var output []byte
		var err error
		lspciBin := findBin("lspci", "/usr/bin/lspci", "/bin/lspci")
		if _, statErr := os.Stat(lspciBin); statErr == nil {
			output, err = exec.Command(lspciBin).Output()
		}
		if err != nil || len(output) == 0 {
			data, _ := os.ReadFile("/proc/bus/pci/devices")
			output = data
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"content": string(output)})
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
		if r.Method == http.MethodPost && action == "delete" {
			payload, _ := json.Marshal(ipc.UserDeleteReq{Username: username})
			forwardIPC(w, r, ipcClient, ipc.TypeUserDelete, payload)
			return
		}
		if r.Method == http.MethodPost && action == "lock" {
			payload, _ := json.Marshal(ipc.UserLockReq{Username: username})
			forwardIPC(w, r, ipcClient, ipc.TypeUserLock, payload)
			return
		}
		if r.Method == http.MethodPost && action == "unlock" {
			payload, _ := json.Marshal(ipc.UserUnlockReq{Username: username})
			forwardIPC(w, r, ipcClient, ipc.TypeUserUnlock, payload)
			return
		}
		if action == "groups" {
			if r.Method == http.MethodGet {
				idBin := findBin("id", "/usr/bin/id", "/bin/id")
				out, err := exec.Command(idBin, "-Gn", username).Output()
				if err != nil {
					http.Error(w, "Not available", http.StatusServiceUnavailable)
					return
				}
				groups := strings.Fields(string(out))
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{"username": username, "groups": groups})
				return
			}
			if r.Method == http.MethodPost {
				var body ipc.UserGroupReq
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					http.Error(w, "Bad request", http.StatusBadRequest)
					return
				}
				payload, _ := json.Marshal(ipc.UserGroupReq{Username: username, Group: body.Group})
				forwardIPC(w, r, ipcClient, ipc.TypeUserGroupAdd, payload)
				return
			}
		}
		if strings.HasPrefix(action, "groups/") {
			group := strings.TrimPrefix(action, "groups/")
			action := r.URL.Query().Get("action")
			if r.Method == http.MethodPost && action == "remove" {
				payload, _ := json.Marshal(ipc.UserGroupReq{Username: username, Group: group})
				forwardIPC(w, r, ipcClient, ipc.TypeUserGroupRemove, payload)
				return
			}
		}
		if r.Method == http.MethodPost && action == "shell" {
			var body ipc.ShellChangeReq
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "Bad request", http.StatusBadRequest)
				return
			}
			payload, _ := json.Marshal(ipc.ShellChangeReq{Username: username, Shell: body.Shell})
			forwardIPC(w, r, ipcClient, ipc.TypeShellChange, payload)
			return
		}
		if action == "ssh-keys" {
			if r.Method == http.MethodGet {
				payload, _ := json.Marshal(ipc.SshKeysReadReq{Username: username})
				forwardIPC(w, r, ipcClient, ipc.TypeSshKeysRead, payload)
				return
			}
			if r.Method == http.MethodPost {
				var body ipc.SshKeysWriteReq
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					http.Error(w, "Bad request", http.StatusBadRequest)
					return
				}
				payload, _ := json.Marshal(ipc.SshKeysWriteReq{Username: username, Content: body.Content})
				forwardIPC(w, r, ipcClient, ipc.TypeSshKeysWrite, payload)
				return
			}
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}))))

	mux.Handle("/api/groups", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		getentBin := findBin("getent", "/usr/bin/getent", "/bin/getent")
		out, err := exec.Command(getentBin, "group").Output()
		if err != nil {
			http.Error(w, "Not available", http.StatusServiceUnavailable)
			return
		}
		var groups []map[string]string
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			parts := strings.Split(line, ":")
			if len(parts) >= 1 {
				groups = append(groups, map[string]string{"name": parts[0]})
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(groups)
	})))

	mux.Handle("/api/storage/devices", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		lsblkBin := findBin("lsblk", "/bin/lsblk", "/usr/bin/lsblk", "/sbin/lsblk")
		out, err := exec.Command(lsblkBin, "-J", "-o", "NAME,SIZE,TYPE,MOUNTPOINT,MODEL").Output()
		if err != nil {
			// fallback to plain text
			out, err = exec.Command(lsblkBin, "-o", "NAME,SIZE,TYPE,MOUNTPOINT,MODEL").Output()
			if err != nil {
				http.Error(w, "lsblk failed", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "text/plain")
			w.Write(out)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(out)
	})))

	mux.Handle("/api/lbu/commit", csrf(authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		forwardIPC(w, r, ipcClient, ipc.TypeLbuCommit, []byte("{}"))
	}))))

	mux.Handle("/api/lbu/status", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		forwardIPC(w, r, ipcClient, ipc.TypeLbuStatus, []byte("{}"))
	})))

	mux.Handle("/api/lbu/list", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		forwardIPC(w, r, ipcClient, ipc.TypeLbuList, []byte("{}"))
	})))

	mux.Handle("/api/lbu/restore", csrf(authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body ipc.LbuRestoreReq
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		payload, _ := json.Marshal(body)
		forwardIPC(w, r, ipcClient, ipc.TypeLbuRestore, payload)
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

	mux.Handle("/api/kmod/list", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var modules []string
		if data, err := os.ReadFile("/proc/modules"); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				fields := strings.Fields(line)
				if len(fields) > 0 && fields[0] != "" {
					modules = append(modules, fields[0])
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(modules)
	})))

	mux.Handle("/api/kmod/load", csrf(authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body ipc.KModLoadReq
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		payload, _ := json.Marshal(body)
		forwardIPC(w, r, ipcClient, ipc.TypeKModLoad, payload)
	}))))

	mux.Handle("/api/kmod/unload", csrf(authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body ipc.KModUnloadReq
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		payload, _ := json.Marshal(body)
		forwardIPC(w, r, ipcClient, ipc.TypeKModUnload, payload)
	}))))

	mux.Handle("/api/apk/repositories", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			forwardIPC(w, r, ipcClient, ipc.TypeApkRepoRead, []byte("{}"))
			return
		}
		if r.Method == http.MethodPost {
			var body ipc.ApkRepoWriteReq
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "Bad request", http.StatusBadRequest)
				return
			}
			payload, _ := json.Marshal(body)
			forwardIPC(w, r, ipcClient, ipc.TypeApkRepoWrite, payload)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	})))

	mux.Handle("/api/ssh/config", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			forwardIPC(w, r, ipcClient, ipc.TypeSshConfigRead, []byte("{}"))
			return
		}
		if r.Method == http.MethodPost {
			var body ipc.SshConfigWriteReq
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "Bad request", http.StatusBadRequest)
				return
			}
			payload, _ := json.Marshal(body)
			forwardIPC(w, r, ipcClient, ipc.TypeSshConfigWrite, payload)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	})))

	mux.Handle("/api/cron", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			forwardIPC(w, r, ipcClient, ipc.TypeCronRead, []byte("{}"))
			return
		}
		if r.Method == http.MethodPost {
			var body ipc.CronWriteReq
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "Bad request", http.StatusBadRequest)
				return
			}
			payload, _ := json.Marshal(body)
			forwardIPC(w, r, ipcClient, ipc.TypeCronWrite, payload)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	})))

	mux.Handle("/api/processes", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		type procInfo struct {
			PID     int     `json:"pid"`
			Name    string  `json:"name"`
			User    string  `json:"user"`
			CPU     float64 `json:"cpu"`
			Mem     float64 `json:"mem"`
			State   string  `json:"state"`
			Command string  `json:"command"`
		}
		var processes []procInfo
		entries, _ := os.ReadDir("/proc")
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			pid, err := strconv.Atoi(entry.Name())
			if err != nil {
				continue
			}
			statusPath := fmt.Sprintf("/proc/%d/status", pid)
			statusData, err := os.ReadFile(statusPath)
			if err != nil {
				continue
			}
			p := procInfo{PID: pid}
			var uid int
			for _, line := range strings.Split(string(statusData), "\n") {
				if strings.HasPrefix(line, "Name:") {
					p.Name = strings.TrimSpace(strings.TrimPrefix(line, "Name:"))
				}
				if strings.HasPrefix(line, "State:") {
					p.State = strings.TrimSpace(strings.TrimPrefix(line, "State:"))
				}
				if strings.HasPrefix(line, "Uid:") {
					parts := strings.Fields(line)
					if len(parts) >= 2 {
						uid, _ = strconv.Atoi(parts[1])
					}
				}
			}
			if u, err := user.LookupId(fmt.Sprintf("%d", uid)); err == nil {
				p.User = u.Username
			} else {
				p.User = fmt.Sprintf("%d", uid)
			}
			cmdline, _ := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
			p.Command = strings.ReplaceAll(string(cmdline), "\x00", " ")
			if p.Command == "" {
				p.Command = p.Name
			}
			processes = append(processes, p)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(processes)
	})))

	mux.Handle("/api/processes/", csrf(authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/api/processes/")
		pidStr := strings.Split(path, "/")[0]
		pid, err := strconv.Atoi(pidStr)
		if err != nil || pid <= 0 {
			http.Error(w, "Invalid PID", http.StatusBadRequest)
			return
		}
		payload, _ := json.Marshal(ipc.ProcessKillReq{PID: pid})
		forwardIPC(w, r, ipcClient, ipc.TypeProcessKill, payload)
	}))))

	mux.Handle("/api/logs/history", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		filter := r.URL.Query().Get("filter")
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		if limit <= 0 || limit > 10000 {
			limit = 500
		}
		paths := []string{"/var/log/messages", "/var/log/syslog", "/var/log/kern.log"}
		var data []byte
		for _, p := range paths {
			if d, err := os.ReadFile(p); err == nil {
				data = d
				break
			}
		}
		lines := []string{}
		for _, line := range strings.Split(string(data), "\n") {
			if filter != "" && !strings.Contains(line, filter) {
				continue
			}
			lines = append(lines, line)
		}
		// reverse order (newest first)
		for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
			lines[i], lines[j] = lines[j], lines[i]
		}
		start := offset
		if start < 0 || start > len(lines) {
			start = 0
		}
		end := start + limit
		if end > len(lines) {
			end = len(lines)
		}
		result := lines[start:end]
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"lines":  result,
			"total":  len(lines),
			"offset": start,
			"limit":  limit,
		})
	})))

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

	// Firewall status (read-only)
	mux.Handle("/api/firewall", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var output []byte
		var err error
		nftBin := findBin("nft", "/usr/sbin/nft", "/sbin/nft")
		if _, statErr := os.Stat(nftBin); statErr == nil {
			output, err = exec.Command(nftBin, "list", "ruleset").Output()
		}
		if err != nil || len(output) == 0 {
			iptablesBin := findBin("iptables", "/usr/sbin/iptables", "/sbin/iptables")
			if _, statErr := os.Stat(iptablesBin); statErr == nil {
				output, err = exec.Command(iptablesBin, "-L", "-n", "-v").Output()
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			json.NewEncoder(w).Encode(map[string]string{"content": "", "error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"content": string(output)})
	})))

	// Network routes (read-only)
	mux.Handle("/api/network/routes", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ipBin := findBin("ip", "/usr/sbin/ip", "/sbin/ip")
		var output []byte
		var err error
		if _, statErr := os.Stat(ipBin); statErr == nil {
			output, err = exec.Command(ipBin, "route").Output()
		}
		if err != nil || len(output) == 0 {
			routeBin := findBin("route", "/usr/sbin/route", "/sbin/route")
			if _, statErr := os.Stat(routeBin); statErr == nil {
				output, err = exec.Command(routeBin, "-n").Output()
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			json.NewEncoder(w).Encode(map[string]string{"content": "", "error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"content": string(output)})
	})))

	// WiFi scan (read-only)
	mux.Handle("/api/network/wifi", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		iwBin := findBin("iw", "/usr/sbin/iw", "/sbin/iw")
		var output []byte
		var err error
		if _, statErr := os.Stat(iwBin); statErr == nil {
			output, err = exec.Command(iwBin, "dev", "scan").Output()
		}
		if err != nil || len(output) == 0 {
			iwlistBin := findBin("iwlist", "/usr/sbin/iwlist", "/sbin/iwlist")
			if _, statErr := os.Stat(iwlistBin); statErr == nil {
				output, err = exec.Command(iwlistBin, "scan").Output()
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			json.NewEncoder(w).Encode(map[string]string{"content": "", "error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"content": string(output)})
	})))

	// Generic file viewer (read-only, whitelist)
	mux.Handle("/api/files", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		path := r.URL.Query().Get("path")
		if path == "" {
			http.Error(w, "path required", http.StatusBadRequest)
			return
		}
		// Normalize and whitelist
		path = filepath.Clean(path)
		allowed := false
		for _, prefix := range []string{"/etc/", "/var/log/", "/proc/", "/sys/"} {
			if strings.HasPrefix(path, prefix) {
				allowed = true
				break
			}
		}
		if !allowed {
			http.Error(w, "path not allowed", http.StatusForbidden)
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			http.Error(w, "not available", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"content": string(data)})
	})))

	// Historical log search (grep-like)
	mux.Handle("/api/logs/search", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		query := r.URL.Query().Get("query")
		if query == "" {
			http.Error(w, "query required", http.StatusBadRequest)
			return
		}
		limitStr := r.URL.Query().Get("limit")
		limit := 100
		if limitStr != "" {
			if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 1000 {
				limit = n
			}
		}
		logPath := "/var/log/messages"
		if p := r.URL.Query().Get("file"); p != "" {
			clean := filepath.Clean(p)
			if strings.HasPrefix(clean, "/var/log/") || strings.HasPrefix(clean, "/etc/") {
				logPath = clean
			}
		}
		var lines []string
		if data, err := os.ReadFile(logPath); err == nil {
			all := strings.Split(string(data), "\n")
			queryLower := strings.ToLower(query)
			for i := len(all) - 1; i >= 0 && len(lines) < limit; i-- {
				line := all[i]
				if strings.Contains(strings.ToLower(line), queryLower) {
					lines = append([]string{line}, lines...)
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"lines": lines, "count": len(lines)})
	})))

	// Package cache clean
	mux.Handle("/api/packages/cache-clean", csrf(authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		forwardIPC(w, r, ipcClient, ipc.TypePackageCacheClean, []byte("{}"))
	}))))

	// CPU info (read-only)
	mux.Handle("/api/system/cpuinfo", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		info := map[string]string{}
		if data, err := os.ReadFile("/proc/cpuinfo"); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					info[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(info)
	})))

	// Memory info (read-only)
	mux.Handle("/api/system/meminfo", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		info := map[string]string{}
		if data, err := os.ReadFile("/proc/meminfo"); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					info[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(info)
	})))

	// dmesg / boot log (read-only)
	mux.Handle("/api/system/dmesg", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var output []byte
		var err error
		dmesgBin := findBin("dmesg", "/usr/sbin/dmesg", "/sbin/dmesg", "/bin/dmesg")
		if _, statErr := os.Stat(dmesgBin); statErr == nil {
			output, err = exec.Command(dmesgBin).Output()
		}
		if err != nil || len(output) == 0 {
			output, err = os.ReadFile("/var/log/dmesg")
		}
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			json.NewEncoder(w).Encode(map[string]string{"content": "", "error": err.Error()})
			return
		}
		content := string(output)
		// Optional severity filter: error, warn, info
		if level := r.URL.Query().Get("level"); level != "" {
			lines := strings.Split(content, "\n")
			var filtered []string
			for _, line := range lines {
				upper := strings.ToUpper(line)
				switch level {
				case "error":
					if strings.Contains(upper, "ERROR") || strings.Contains(upper, "ERR") || strings.Contains(upper, "FATAL") || strings.Contains(upper, "PANIC") {
						filtered = append(filtered, line)
					}
				case "warn":
					if strings.Contains(upper, "WARN") || strings.Contains(upper, "WARNING") {
						filtered = append(filtered, line)
					}
				case "info":
					if strings.Contains(upper, "INFO") || strings.Contains(upper, "NOTICE") {
						filtered = append(filtered, line)
					}
				default:
					filtered = append(filtered, line)
				}
			}
			content = strings.Join(filtered, "\n")
		}
		json.NewEncoder(w).Encode(map[string]string{"content": content})
	})))

	// Network addresses (read-only)
	mux.Handle("/api/network/addr", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ipBin := findBin("ip", "/usr/sbin/ip", "/sbin/ip")
		var output []byte
		var err error
		if _, statErr := os.Stat(ipBin); statErr == nil {
			output, err = exec.Command(ipBin, "addr", "show").Output()
		}
		if err != nil || len(output) == 0 {
			ifconfigBin := findBin("ifconfig", "/usr/sbin/ifconfig", "/sbin/ifconfig")
			if _, statErr := os.Stat(ifconfigBin); statErr == nil {
				output, err = exec.Command(ifconfigBin).Output()
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			json.NewEncoder(w).Encode(map[string]string{"content": "", "error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"content": string(output)})
	})))

	// Network interface statistics (read-only)
	mux.Handle("/api/network/dev", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		data, _ := os.ReadFile("/proc/net/dev")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"content": string(data)})
	})))

	// Listening sockets (read-only)
	mux.Handle("/api/network/listeners", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var output []byte
		var err error
		ssBin := findBin("ss", "/usr/sbin/ss", "/sbin/ss", "/bin/ss")
		if _, statErr := os.Stat(ssBin); statErr == nil {
			output, err = exec.Command(ssBin, "-tlnp").Output()
		}
		if err != nil || len(output) == 0 {
			netstatBin := findBin("netstat", "/usr/bin/netstat", "/bin/netstat")
			if _, statErr := os.Stat(netstatBin); statErr == nil {
				output, err = exec.Command(netstatBin, "-tlnp").Output()
			}
		}
		if err != nil || len(output) == 0 {
			output = []byte("ss/netstat not available")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"content": string(output)})
	})))

	// System load average (read-only)
	mux.Handle("/api/system/loadavg", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		data, _ := os.ReadFile("/proc/loadavg")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"content": strings.TrimSpace(string(data))})
	})))

	// Open files overview (read-only)
	mux.Handle("/api/system/openfiles", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var output []byte
		var err error
		lsofBin := findBin("lsof", "/usr/bin/lsof", "/usr/sbin/lsof", "/sbin/lsof")
		if _, statErr := os.Stat(lsofBin); statErr == nil {
			output, err = exec.Command(lsofBin, "-n", "-P").Output()
		}
		if err != nil || len(output) == 0 {
			output = []byte("lsof not available")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"content": string(output)})
	})))

	// Disk SMART status (read-only)
	mux.Handle("/api/storage/smart", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		dev := r.URL.Query().Get("dev")
		if dev == "" {
			dev = "/dev/sda"
		}
		dev = filepath.Clean(dev)
		if !strings.HasPrefix(dev, "/dev/") {
			http.Error(w, "invalid device", http.StatusBadRequest)
			return
		}
		var output []byte
		var err error
		smartctlBin := findBin("smartctl", "/usr/sbin/smartctl", "/sbin/smartctl")
		if _, statErr := os.Stat(smartctlBin); statErr == nil {
			output, err = exec.Command(smartctlBin, "-H", dev).Output()
			if err != nil || len(output) == 0 {
				output, _ = exec.Command(smartctlBin, "-a", dev).Output()
			}
		}
		if len(output) == 0 {
			hdparmBin := findBin("hdparm", "/usr/sbin/hdparm", "/sbin/hdparm")
			if _, statErr := os.Stat(hdparmBin); statErr == nil {
				output, err = exec.Command(hdparmBin, "-i", dev).Output()
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if err != nil && len(output) == 0 {
			json.NewEncoder(w).Encode(map[string]string{"content": "", "error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"content": string(output)})
	})))

	// SSH host keys (read-only)
	mux.Handle("/api/system/ssh-host-keys", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		keys := []map[string]string{}
		entries, err := os.ReadDir("/etc/ssh")
		if err == nil {
			for _, entry := range entries {
				name := entry.Name()
				if strings.HasPrefix(name, "ssh_host_") && strings.HasSuffix(name, "_key.pub") {
					data, err := os.ReadFile(filepath.Join("/etc/ssh", name))
					if err == nil {
						keys = append(keys, map[string]string{"file": name, "content": strings.TrimSpace(string(data))})
					}
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(keys)
	})))

	// MOTD / issue (read-only)
	mux.Handle("/api/system/motd", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		motd, _ := os.ReadFile("/etc/motd")
		issue, _ := os.ReadFile("/etc/issue")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"motd":  strings.TrimSpace(string(motd)),
			"issue": strings.TrimSpace(string(issue)),
		})
	})))

	// Kernel boot parameters (read-only)
	mux.Handle("/api/system/cmdline", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		data, _ := os.ReadFile("/proc/cmdline")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"cmdline": strings.TrimSpace(string(data))})
	})))

	// Swap info (read-only)
	mux.Handle("/api/system/swaps", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		data, _ := os.ReadFile("/proc/swaps")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"content": string(data)})
	})))

	// Init environment variables (read-only)
	mux.Handle("/api/system/environ", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		data, _ := os.ReadFile("/proc/1/environ")
		env := map[string]string{}
		for _, kv := range strings.Split(string(data), "\x00") {
			if kv == "" {
				continue
			}
			parts := strings.SplitN(kv, "=", 2)
			if len(parts) == 2 {
				env[parts[0]] = parts[1]
			} else {
				env[kv] = ""
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(env)
	})))

	// Kernel version detail (read-only)
	mux.Handle("/api/system/version", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		data, _ := os.ReadFile("/proc/version")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"content": strings.TrimSpace(string(data))})
	})))

	// DHCP toggle
	mux.Handle("/api/network/dhcp", csrf(authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		iface := body["iface"]
		action := body["action"]
		if iface == "" || (action != "start" && action != "stop") {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		payload, _ := json.Marshal(map[string]string{"iface": iface, "action": action})
		forwardIPC(w, r, ipcClient, ipc.TypeDhcpToggle, payload)
	}))))

	// Network connections (read-only)
	mux.Handle("/api/network/connections", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var output []byte
		var err error
		ssBin := findBin("ss", "/usr/sbin/ss", "/sbin/ss", "/bin/ss")
		if _, statErr := os.Stat(ssBin); statErr == nil {
			output, err = exec.Command(ssBin, "-tulpn").Output()
		}
		if err != nil || len(output) == 0 {
			netstatBin := findBin("netstat", "/usr/bin/netstat", "/bin/netstat")
			if _, statErr := os.Stat(netstatBin); statErr == nil {
				output, err = exec.Command(netstatBin, "-tulpn").Output()
			}
		}
		if err != nil || len(output) == 0 {
			lsofBin := findBin("lsof", "/usr/bin/lsof", "/bin/lsof")
			if _, statErr := os.Stat(lsofBin); statErr == nil {
				output, err = exec.Command(lsofBin, "-i", "-P", "-n").Output()
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if err != nil && len(output) == 0 {
			json.NewEncoder(w).Encode(map[string]string{"content": "", "error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"content": string(output)})
	})))

	// ARP table (read-only)
	mux.Handle("/api/network/arp", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var output []byte
		var err error
		ipBin := findBin("ip", "/sbin/ip", "/usr/sbin/ip", "/bin/ip")
		if _, statErr := os.Stat(ipBin); statErr == nil {
			output, err = exec.Command(ipBin, "neigh").Output()
		}
		if err != nil || len(output) == 0 {
			data, _ := os.ReadFile("/proc/net/arp")
			output = data
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"content": string(output)})
	})))

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

	// Audit log endpoint (reads last N lines from configured audit log)
	mux.Handle("/api/audit", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		limitStr := r.URL.Query().Get("limit")
		limit := 500
		if limitStr != "" {
			if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 5000 {
				limit = n
			}
		}
		data, err := os.ReadFile(cfg.AuditLogPath)
		if err != nil {
			http.Error(w, "Audit log unavailable", http.StatusServiceUnavailable)
			return
		}
		lines := strings.Split(string(data), "\n")
		var entries []map[string]interface{}
		start := 0
		if len(lines) > limit {
			start = len(lines) - limit
		}
		for i := start; i < len(lines); i++ {
			line := strings.TrimSpace(lines[i])
			if line == "" {
				continue
			}
			var entry map[string]interface{}
			if err := json.Unmarshal([]byte(line), &entry); err == nil {
				entries = append(entries, entry)
			} else {
				entries = append(entries, map[string]interface{}{"raw": line})
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(entries)
	})))

	// resolv.conf editor
	mux.Handle("/api/system/resolv", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			data, err := os.ReadFile("/etc/resolv.conf")
			if err != nil {
				http.Error(w, "Not available", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"content": string(data)})
			return
		}
		if r.Method == http.MethodPost {
			var body ipc.ResolvWriteReq
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "Bad request", http.StatusBadRequest)
				return
			}
			if err := os.WriteFile("/etc/resolv.conf", []byte(body.Content), 0644); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	})))

	// System clock setter
	mux.Handle("/api/system/clock", csrf(authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body ipc.ClockSetReq
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		payload, _ := json.Marshal(body)
		forwardIPC(w, r, ipcClient, ipc.TypeClockSet, payload)
	}))))

	// Config export
	mux.Handle("/api/config/export", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		data, err := os.ReadFile(*configPath)
		if err != nil {
			http.Error(w, "Config unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", "attachment; filename=\"config.json\"")
		w.Write(data)
	})))

	// Config import
	mux.Handle("/api/config/import", csrf(authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var incoming config.Config
		if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
			http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := incoming.Validate(); err != nil {
			http.Error(w, "Validation failed: "+err.Error(), http.StatusBadRequest)
			return
		}
		data, err := json.MarshalIndent(incoming, "", "  ")
		if err != nil {
			http.Error(w, "Serialization failed", http.StatusInternalServerError)
			return
		}
		if err := os.WriteFile(*configPath, data, 0640); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))))

	// Prometheus /metrics endpoint
	mux.Handle("/metrics", authenticated(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		var out strings.Builder
		now := time.Now().Unix()

		// CPU load
		if data, err := os.ReadFile("/proc/loadavg"); err == nil {
			parts := strings.Fields(string(data))
			if len(parts) > 0 {
				out.WriteString(fmt.Sprintf("# HELP node_load1 1m load average\n# TYPE node_load1 gauge\nnode_load1 %s\n", parts[0]))
			}
			if len(parts) > 1 {
				out.WriteString(fmt.Sprintf("# HELP node_load5 5m load average\n# TYPE node_load5 gauge\nnode_load5 %s\n", parts[1]))
			}
			if len(parts) > 2 {
				out.WriteString(fmt.Sprintf("# HELP node_load15 15m load average\n# TYPE node_load15 gauge\nnode_load15 %s\n", parts[2]))
			}
		}
		out.WriteString(fmt.Sprintf("# HELP node_cpu_count Number of logical CPUs\n# TYPE node_cpu_count gauge\nnode_cpu_count %d\n", runtime.NumCPU()))

		// Memory
		var memTotal, memFree, memAvailable, memBuffers, memCached int64
		if data, err := os.ReadFile("/proc/meminfo"); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				var name string
				var val int64
				if n, _ := fmt.Sscanf(line, "%s %d", &name, &val); n == 2 {
					switch strings.TrimSuffix(name, ":") {
					case "MemTotal":
						memTotal = val * 1024
					case "MemFree":
						memFree = val * 1024
					case "MemAvailable":
						memAvailable = val * 1024
					case "Buffers":
						memBuffers = val * 1024
					case "Cached":
						memCached = val * 1024
					}
				}
			}
		}
		if memTotal > 0 {
			out.WriteString(fmt.Sprintf("# HELP node_memory_MemTotal_bytes Total memory\n# TYPE node_memory_MemTotal_bytes gauge\nnode_memory_MemTotal_bytes %d\n", memTotal))
			out.WriteString(fmt.Sprintf("# HELP node_memory_MemFree_bytes Free memory\n# TYPE node_memory_MemFree_bytes gauge\nnode_memory_MemFree_bytes %d\n", memFree))
			if memAvailable > 0 {
				out.WriteString(fmt.Sprintf("# HELP node_memory_MemAvailable_bytes Available memory\n# TYPE node_memory_MemAvailable_bytes gauge\nnode_memory_MemAvailable_bytes %d\n", memAvailable))
			}
			out.WriteString(fmt.Sprintf("# HELP node_memory_Buffers_bytes Buffer cache\n# TYPE node_memory_Buffers_bytes gauge\nnode_memory_Buffers_bytes %d\n", memBuffers))
			out.WriteString(fmt.Sprintf("# HELP node_memory_Cached_bytes Cached memory\n# TYPE node_memory_Cached_bytes gauge\nnode_memory_Cached_bytes %d\n", memCached))
		}

		// Disk usage
		dfBin := findBin("df", "/bin/df", "/usr/bin/df", "/sbin/df")
		if output, err := exec.Command(dfBin, "-P").Output(); err == nil {
			for i, line := range strings.Split(string(output), "\n") {
				if i == 0 {
					continue // skip header
				}
				fields := strings.Fields(line)
				if len(fields) < 6 {
					continue
				}
				device := fields[0]
				mount := fields[5]
				if device == "tmpfs" || strings.HasPrefix(device, "devtmpfs") {
					continue
				}
				var size, used int64
				fmt.Sscanf(fields[1], "%d", &size)
				fmt.Sscanf(fields[2], "%d", &used)
				usePct := strings.TrimSuffix(fields[4], "%")
				out.WriteString(fmt.Sprintf("# HELP node_filesystem_size_bytes Filesystem size in bytes\n# TYPE node_filesystem_size_bytes gauge\nnode_filesystem_size_bytes{device=%q,mountpoint=%q} %d\n", device, mount, size*1024))
				out.WriteString(fmt.Sprintf("# HELP node_filesystem_free_bytes Filesystem free in bytes\n# TYPE node_filesystem_free_bytes gauge\nnode_filesystem_free_bytes{device=%q,mountpoint=%q} %d\n", device, mount, (size-used)*1024))
				out.WriteString(fmt.Sprintf("# HELP node_filesystem_avail_bytes Filesystem available in bytes\n# TYPE node_filesystem_avail_bytes gauge\nnode_filesystem_avail_bytes{device=%q,mountpoint=%q} %d\n", device, mount, (size-used)*1024))
				out.WriteString(fmt.Sprintf("# HELP node_filesystem_used_percent Filesystem used percent\n# TYPE node_filesystem_used_percent gauge\nnode_filesystem_used_percent{device=%q,mountpoint=%q} %s\n", device, mount, usePct))
			}
		}

		// Network stats
		if data, err := os.ReadFile("/proc/net/dev"); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				parts := strings.SplitN(strings.TrimSpace(line), ":", 2)
				if len(parts) != 2 {
					continue
				}
				iface := strings.TrimSpace(parts[0])
				if iface == "lo" || iface == "" {
					continue
				}
				fields := strings.Fields(parts[1])
				if len(fields) < 9 {
					continue
				}
				var rxBytes, rxPackets, txBytes, txPackets int64
				fmt.Sscanf(fields[0], "%d", &rxBytes)
				fmt.Sscanf(fields[1], "%d", &rxPackets)
				fmt.Sscanf(fields[8], "%d", &txBytes)
				fmt.Sscanf(fields[9], "%d", &txPackets)
				out.WriteString(fmt.Sprintf("# HELP node_network_receive_bytes_total Network received bytes\n# TYPE node_network_receive_bytes_total counter\nnode_network_receive_bytes_total{device=%q} %d\n", iface, rxBytes))
				out.WriteString(fmt.Sprintf("# HELP node_network_receive_packets_total Network received packets\n# TYPE node_network_receive_packets_total counter\nnode_network_receive_packets_total{device=%q} %d\n", iface, rxPackets))
				out.WriteString(fmt.Sprintf("# HELP node_network_transmit_bytes_total Network transmitted bytes\n# TYPE node_network_transmit_bytes_total counter\nnode_network_transmit_bytes_total{device=%q} %d\n", iface, txBytes))
				out.WriteString(fmt.Sprintf("# HELP node_network_transmit_packets_total Network transmitted packets\n# TYPE node_network_transmit_packets_total counter\nnode_network_transmit_packets_total{device=%q} %d\n", iface, txPackets))
			}
		}

		// Uptime
		if data, err := os.ReadFile("/proc/uptime"); err == nil {
			fields := strings.Fields(string(data))
			if len(fields) > 0 {
				if up, err := strconv.ParseFloat(fields[0], 64); err == nil {
					out.WriteString(fmt.Sprintf("# HELP node_boot_time_seconds System boot time\n# TYPE node_boot_time_seconds gauge\nnode_boot_time_seconds %d\n", now-int64(up)))
				}
			}
		}

		w.Write([]byte(out.String()))
	})))

	// ── Server with middleware ─────────────────────────
	handler := requestLogger(logger)(mux)
	handler = securityHeaders(handler)

	server := &http.Server{
		Addr:           cfg.Listen,
		Handler:        handler,
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

	// HTTP -> HTTPS redirect when TLS is configured
	if cfg.TLSCert != "" {
		go func() {
			redirect := &http.Server{
				Addr: ":80",
				Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					target := "https://" + r.Host + r.URL.RequestURI()
					if r.Host == "" {
						target = "https://" + cfg.Listen + r.URL.RequestURI()
					}
					http.Redirect(w, r, target, http.StatusMovedPermanently)
				}),
				ReadTimeout:  5 * time.Second,
				WriteTimeout: 5 * time.Second,
			}
			logger.Info("http redirect listening", map[string]interface{}{"addr": ":80"})
			if err := redirect.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.Warn("http redirect server error", map[string]interface{}{"error": err.Error()})
			}
		}()
	}

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
