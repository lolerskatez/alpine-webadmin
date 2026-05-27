package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/alpine-webadmin/alpine-webadmin/pkg/apk"
	"github.com/alpine-webadmin/alpine-webadmin/pkg/config"
	"github.com/alpine-webadmin/alpine-webadmin/pkg/ipc"
	"github.com/alpine-webadmin/alpine-webadmin/pkg/log"
	"github.com/alpine-webadmin/alpine-webadmin/pkg/openrc"
	"github.com/alpine-webadmin/alpine-webadmin/pkg/version"
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

	logger.Info("roothelper starting", map[string]interface{}{
		"version": version.Version,
		"go":     runtime.Version(),
	})

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("failed to load config", map[string]interface{}{"error": err.Error()})
		os.Exit(1)
	}

	expectedUID := 0
	if u, err := user.Lookup("webadmin"); err == nil {
		fmt.Sscanf(u.Uid, "%d", &expectedUID)
	}

	broker := newBroker(cfg, logger)

	logger.Info("roothelper listening", map[string]interface{}{
		"socket":      cfg.IPCSocket,
		"expectedUID": expectedUID,
	})

	// Start the log streaming server in the background. Failure here is
	// non-fatal: the rest of the API should still function.
	go func() {
		if err := serveLogStream(cfg.LogsSocket, expectedUID, logger); err != nil {
			logger.Error("logstream server failed", map[string]interface{}{"error": err.Error()})
		}
	}()

	if err := ipc.Serve(cfg.IPCSocket, expectedUID, broker.handle); err != nil {
		logger.Error("ipc server failed", map[string]interface{}{"error": err.Error()})
		os.Exit(1)
	}
}

// ── Broker ─────────────────────────────────────────

type broker struct {
	cfg     *config.Config
	logger  *log.Logger
	allowed map[string]bool
	svcMgr  *openrc.Manager
	apkMgr  *apk.Manager
}

func newBroker(cfg *config.Config, logger *log.Logger) *broker {
	allowed := make(map[string]bool)
	for _, h := range cfg.AllowedHelpers {
		allowed[h] = true
	}
	return &broker{
		cfg:     cfg,
		logger:  logger,
		allowed: allowed,
		svcMgr:  openrc.NewManager(logger),
		apkMgr:  apk.NewManager(logger),
	}
}

func (b *broker) handle(req ipc.Envelope) ipc.Envelope {
	if req.Version != ipc.Version {
		return b.error(req, ipc.ErrInvalidRequest, "unsupported protocol version")
	}

	start := time.Now()
	b.audit("request", req)

	var resp ipc.Envelope
	switch req.MessageType {
	case ipc.TypeServiceList:
		resp = b.handleServiceList(req)
	case ipc.TypeServiceStatus:
		resp = b.handleServiceStatus(req)
	case ipc.TypeServiceControl:
		resp = b.handleServiceControl(req)
	case ipc.TypeServiceEnable:
		resp = b.handleServiceEnable(req)
	case ipc.TypeServiceDisable:
		resp = b.handleServiceDisable(req)
	case ipc.TypeSystemInfo:
		resp = b.handleSystemInfo(req)
	case ipc.TypePackageList:
		resp = b.handlePackageList(req)
	case ipc.TypePackageSearch:
		resp = b.handlePackageSearch(req)
	case ipc.TypePackageInfo:
		resp = b.handlePackageInfo(req)
	case ipc.TypePackageInstall:
		resp = b.handlePackageInstall(req)
	case ipc.TypePackageRemove:
		resp = b.handlePackageRemove(req)
	case ipc.TypePackageUpdate:
		resp = b.handlePackageUpdate(req)
	case ipc.TypePackageUpgrade:
		resp = b.handlePackageUpgrade(req)
	case ipc.TypePackageOpQuery:
		resp = b.handlePackageOpQuery(req)
	case ipc.TypePackageCacheClean:
		resp = b.handlePackageCacheClean(req)
	case ipc.TypeDhcpToggle:
		resp = b.handleDhcpToggle(req)
	case ipc.TypeReboot:
		resp = b.handleReboot(req)
	case ipc.TypeShutdown:
		resp = b.handleShutdown(req)
	case ipc.TypeUserCreate:
		resp = b.handleUserCreate(req)
	case ipc.TypePasswordReset:
		resp = b.handlePasswordReset(req)
	case ipc.TypeMount:
		resp = b.handleMount(req)
	case ipc.TypeUnmount:
		resp = b.handleUnmount(req)
	case ipc.TypeKModLoad:
		resp = b.handleKModLoad(req)
	case ipc.TypeKModUnload:
		resp = b.handleKModUnload(req)
	case ipc.TypeHostnameSet:
		resp = b.handleHostnameSet(req)
	case ipc.TypeUserDelete:
		resp = b.handleUserDelete(req)
	case ipc.TypeApkRepoRead:
		resp = b.handleApkRepoRead(req)
	case ipc.TypeApkRepoWrite:
		resp = b.handleApkRepoWrite(req)
	case ipc.TypeUserLock:
		resp = b.handleUserLock(req)
	case ipc.TypeUserUnlock:
		resp = b.handleUserUnlock(req)
	case ipc.TypeSshConfigRead:
		resp = b.handleSshConfigRead(req)
	case ipc.TypeSshConfigWrite:
		resp = b.handleSshConfigWrite(req)
	case ipc.TypeCronRead:
		resp = b.handleCronRead(req)
	case ipc.TypeCronWrite:
		resp = b.handleCronWrite(req)
	case ipc.TypeProcessKill:
		resp = b.handleProcessKill(req)
	case ipc.TypeLogRead:
		resp = b.handleLogRead(req)
	case ipc.TypeNetIfUp:
		resp = b.handleNetIfUp(req)
	case ipc.TypeNetIfDown:
		resp = b.handleNetIfDown(req)
	case ipc.TypeFstabRead:
		resp = b.handleFstabRead(req)
	case ipc.TypeFstabWrite:
		resp = b.handleFstabWrite(req)
	case ipc.TypeTimezoneSet:
		resp = b.handleTimezoneSet(req)
	case ipc.TypeNtpSet:
		resp = b.handleNtpSet(req)
	case ipc.TypeServiceLogRead:
		resp = b.handleServiceLogRead(req)
	case ipc.TypeUserGroupAdd:
		resp = b.handleUserGroupAdd(req)
	case ipc.TypeUserGroupRemove:
		resp = b.handleUserGroupRemove(req)
	case ipc.TypeShellChange:
		resp = b.handleShellChange(req)
	case ipc.TypeLbuCommit:
		resp = b.handleLbuCommit(req)
	case ipc.TypeLbuStatus:
		resp = b.handleLbuStatus(req)
	case ipc.TypeLbuList:
		resp = b.handleLbuList(req)
	case ipc.TypeLbuRestore:
		resp = b.handleLbuRestore(req)
	case ipc.TypeSshKeysRead:
		resp = b.handleSshKeysRead(req)
	case ipc.TypeSshKeysWrite:
		resp = b.handleSshKeysWrite(req)
	case ipc.TypeClockSet:
		resp = b.handleClockSet(req)
	default:
		resp = b.error(req, ipc.ErrInvalidRequest, "unknown message type")
	}

	b.audit("response", req, "duration", time.Since(start).String(), "status", resp.MessageType)
	return resp
}

func (b *broker) audit(event string, req ipc.Envelope, kv ...string) {
	fields := map[string]interface{}{
		"event": event,
		"rid":   req.RequestID,
		"type":  req.MessageType,
	}
	for i := 0; i+1 < len(kv); i += 2 {
		fields[kv[i]] = kv[i+1]
	}
	b.logger.Info("audit", fields)
}

func (b *broker) ok(req ipc.Envelope, payload interface{}) ipc.Envelope {
	data, _ := json.Marshal(payload)
	return ipc.Envelope{
		Version:     ipc.Version,
		RequestID:   req.RequestID,
		MessageType: ipc.TypeResponseOK,
		Payload:     data,
	}
}

func (b *broker) error(req ipc.Envelope, code, message string) ipc.Envelope {
	payload, _ := json.Marshal(ipc.ResponseError{Code: code, Message: message})
	return ipc.Envelope{
		Version:     ipc.Version,
		RequestID:   req.RequestID,
		MessageType: ipc.TypeResponseError,
		Payload:     payload,
	}
}

// ── Validation ─────────────────────────────────────

var (
	reServiceName = regexp.MustCompile(`^[a-zA-Z0-9_\-]+$`)
	reUsername    = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`) // POSIX
	reModuleName  = regexp.MustCompile(`^[a-zA-Z0-9_\-]+$`)
)

func (b *broker) checkAllow(path string) bool {
	return b.allowed[path]
}

// resolveAllowed returns the first candidate path that both exists on disk
// AND is whitelisted in allowed_helpers. Returns ("", false) if none match.
func (b *broker) resolveAllowed(candidates ...string) (string, bool) {
	for _, p := range candidates {
		if !b.allowed[p] {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			return p, true
		}
	}
	return "", false
}

func (b *broker) validateService(name string) error {
	if name == "" || !reServiceName.MatchString(name) {
		return fmt.Errorf("invalid service name")
	}
	return nil
}

func (b *broker) validateUsername(name string) error {
	if name == "" || !reUsername.MatchString(name) {
		return fmt.Errorf("invalid username")
	}
	return nil
}

func (b *broker) validateModule(name string) error {
	if name == "" || !reModuleName.MatchString(name) {
		return fmt.Errorf("invalid module name")
	}
	return nil
}

func (b *broker) validateMountpoint(mp string) error {
	if !strings.HasPrefix(mp, "/") || strings.Contains(mp, "..") {
		return fmt.Errorf("invalid mountpoint")
	}
	return nil
}

func (b *broker) validateDevice(dev string) error {
	if !strings.HasPrefix(dev, "/dev/") || strings.Contains(dev, "..") {
		return fmt.Errorf("invalid device path")
	}
	return nil
}

func (b *broker) validateAction(action string, allowed ...string) error {
	for _, a := range allowed {
		if action == a {
			return nil
		}
	}
	return fmt.Errorf("action not allowed")
}

// ── Command Execution ──────────────────────────────

func (b *broker) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid: uint32(os.Getuid()),
			Gid: uint32(os.Getgid()),
		},
	}
	out, err := cmd.CombinedOutput()
	return out, err
}

// ── Handlers ───────────────────────────────────────

func (b *broker) handleServiceList(req ipc.Envelope) ipc.Envelope {
	if !b.checkAllow("/sbin/rc-status") {
		return b.error(req, ipc.ErrCapabilityDenied, "/sbin/rc-status not allowed")
	}
	svcs, err := b.svcMgr.List()
	if err != nil {
		return b.error(req, ipc.ErrExecutionFailed, err.Error())
	}
	var entries []ipc.ServiceEntry
	for _, s := range svcs {
		entries = append(entries, ipc.ServiceEntry{
			Name:     s.Name,
			Status:   string(s.State),
			Runlevel: s.Runlevel,
		})
	}
	return b.ok(req, ipc.ServiceListData{Services: entries})
}

func (b *broker) handleServiceStatus(req ipc.Envelope) ipc.Envelope {
	if !b.checkAllow("/sbin/rc-service") {
		return b.error(req, ipc.ErrCapabilityDenied, "/sbin/rc-service not allowed")
	}
	var body ipc.ServiceStatusReq
	if err := json.Unmarshal(req.Payload, &body); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	svc, err := b.svcMgr.Status(body.Name)
	if err != nil {
		return b.error(req, ipc.ErrExecutionFailed, err.Error())
	}
	return b.ok(req, ipc.ServiceStatusData{
		Name:     svc.Name,
		Status:   string(svc.State),
		PID:      svc.PID,
		Enabled:  svc.Enabled,
		Runlevel: svc.Runlevel,
	})
}

func (b *broker) handleServiceControl(req ipc.Envelope) ipc.Envelope {
	if !b.checkAllow("/sbin/rc-service") {
		return b.error(req, ipc.ErrCapabilityDenied, "/sbin/rc-service not allowed")
	}
	var body ipc.ServiceControlReq
	if err := json.Unmarshal(req.Payload, &body); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	if err := b.validateAction(body.Action, "start", "stop", "restart", "status"); err != nil {
		return b.error(req, ipc.ErrActionNotAllowed, err.Error())
	}
	var err error
	switch body.Action {
	case "start":
		err = b.svcMgr.Start(body.Name)
	case "stop":
		err = b.svcMgr.Stop(body.Name)
	case "restart":
		err = b.svcMgr.Restart(body.Name)
	case "status":
		_, err = b.svcMgr.Status(body.Name)
	}
	if err != nil {
		return b.error(req, ipc.ErrExecutionFailed, err.Error())
	}
	status := body.Action + "ed"
	if body.Action == "status" {
		st, _ := b.svcMgr.Status(body.Name)
		if st != nil {
			status = string(st.State)
		}
	}
	return b.ok(req, ipc.ServiceStatusData{Name: body.Name, Status: status})
}

func (b *broker) handleServiceEnable(req ipc.Envelope) ipc.Envelope {
	if !b.checkAllow("/sbin/rc-update") {
		return b.error(req, ipc.ErrCapabilityDenied, "/sbin/rc-update not allowed")
	}
	var body ipc.ServiceEnableReq
	if err := json.Unmarshal(req.Payload, &body); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	if err := b.svcMgr.Enable(body.Name); err != nil {
		return b.error(req, ipc.ErrExecutionFailed, err.Error())
	}
	return b.ok(req, ipc.ServiceStatusData{Name: body.Name, Status: "enabled"})
}

func (b *broker) handleServiceDisable(req ipc.Envelope) ipc.Envelope {
	if !b.checkAllow("/sbin/rc-update") {
		return b.error(req, ipc.ErrCapabilityDenied, "/sbin/rc-update not allowed")
	}
	var body ipc.ServiceDisableReq
	if err := json.Unmarshal(req.Payload, &body); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	if err := b.svcMgr.Disable(body.Name); err != nil {
		return b.error(req, ipc.ErrExecutionFailed, err.Error())
	}
	return b.ok(req, ipc.ServiceStatusData{Name: body.Name, Status: "disabled"})
}

func (b *broker) handleSystemInfo(req ipc.Envelope) ipc.Envelope {
	hostname, _ := os.Hostname()
	kernel := "unknown"
	if data, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		kernel = strings.TrimSpace(string(data))
	}
	var uptimeSec int64
	if f, err := os.Open("/proc/uptime"); err == nil {
		var up float64
		fmt.Fscanf(f, "%f", &up)
		f.Close()
		uptimeSec = int64(up)
	}
	var totalMem int64
	if f, err := os.Open("/proc/meminfo"); err == nil {
		var label string
		for {
			if _, err := fmt.Fscanf(f, "%s %d", &label, &totalMem); err != nil {
				break
			}
			if label == "MemTotal:" {
				break
			}
		}
		f.Close()
	}
	return b.ok(req, ipc.SystemInfoData{
		Hostname:   hostname,
		OS:         "Alpine Linux",
		Kernel:     kernel,
		UptimeSec:  uptimeSec,
		TotalMemKB: totalMem,
		CpuCount:   runtime.NumCPU(),
	})
}

func (b *broker) handleHostnameSet(req ipc.Envelope) ipc.Envelope {
	var body ipc.HostnameSetReq
	if err := json.Unmarshal(req.Payload, &body); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	if body.Hostname == "" || strings.Contains(body.Hostname, "..") || strings.Contains(body.Hostname, "/") {
		return b.error(req, ipc.ErrInvalidRequest, "invalid hostname")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Set transient hostname
	out, err := b.run(ctx, "/bin/hostname", body.Hostname)
	if err != nil {
		return b.error(req, ipc.ErrExecutionFailed, string(out))
	}
	// Persist to /etc/hostname
	if err := os.WriteFile("/etc/hostname", []byte(body.Hostname+"\n"), 0644); err != nil {
		return b.error(req, ipc.ErrExecutionFailed, err.Error())
	}
	return b.ok(req, map[string]string{"hostname": body.Hostname, "status": "set"})
}

func (b *broker) handlePackageList(req ipc.Envelope) ipc.Envelope {
	if !b.checkAllow("/sbin/apk") {
		return b.error(req, ipc.ErrCapabilityDenied, "/sbin/apk not allowed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pkgs, err := b.apkMgr.List(ctx)
	if err != nil {
		return b.error(req, ipc.ErrExecutionFailed, err.Error())
	}
	return b.ok(req, ipc.PackageListData{Packages: b.toIPCPackages(pkgs)})
}

func (b *broker) handlePackageSearch(req ipc.Envelope) ipc.Envelope {
	if !b.checkAllow("/sbin/apk") {
		return b.error(req, ipc.ErrCapabilityDenied, "/sbin/apk not allowed")
	}
	var body ipc.PackageSearchReq
	if err := json.Unmarshal(req.Payload, &body); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
	defer cancel()
	pkgs, err := b.apkMgr.Search(ctx, body.Query)
	if err != nil {
		return b.error(req, ipc.ErrExecutionFailed, err.Error())
	}
	return b.ok(req, ipc.PackageSearchData{Packages: b.toIPCPackages(pkgs)})
}

func (b *broker) handlePackageInfo(req ipc.Envelope) ipc.Envelope {
	if !b.checkAllow("/sbin/apk") {
		return b.error(req, ipc.ErrCapabilityDenied, "/sbin/apk not allowed")
	}
	var body ipc.PackageInfoReq
	if err := json.Unmarshal(req.Payload, &body); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	info, err := b.apkMgr.Info(ctx, body.Name)
	if err != nil {
		return b.error(req, ipc.ErrExecutionFailed, err.Error())
	}
	return b.ok(req, b.toIPCPackage(*info))
}

func (b *broker) handlePackageInstall(req ipc.Envelope) ipc.Envelope {
	if !b.checkAllow("/sbin/apk") {
		return b.error(req, ipc.ErrCapabilityDenied, "/sbin/apk not allowed")
	}
	var body ipc.PackageInstallReq
	if err := json.Unmarshal(req.Payload, &body); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	op, err := b.apkMgr.Install(ctx, body.Packages...)
	if err != nil && op == nil {
		return b.error(req, ipc.ErrExecutionFailed, err.Error())
	}
	return b.ok(req, b.opToIPC(op))
}

func (b *broker) handlePackageRemove(req ipc.Envelope) ipc.Envelope {
	if !b.checkAllow("/sbin/apk") {
		return b.error(req, ipc.ErrCapabilityDenied, "/sbin/apk not allowed")
	}
	var body ipc.PackageRemoveReq
	if err := json.Unmarshal(req.Payload, &body); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	op, err := b.apkMgr.Remove(ctx, body.Packages...)
	if err != nil && op == nil {
		return b.error(req, ipc.ErrExecutionFailed, err.Error())
	}
	return b.ok(req, b.opToIPC(op))
}

func (b *broker) handlePackageUpdate(req ipc.Envelope) ipc.Envelope {
	if !b.checkAllow("/sbin/apk") {
		return b.error(req, ipc.ErrCapabilityDenied, "/sbin/apk not allowed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	op, err := b.apkMgr.Update(ctx)
	if err != nil && op == nil {
		return b.error(req, ipc.ErrExecutionFailed, err.Error())
	}
	return b.ok(req, b.opToIPC(op))
}

func (b *broker) handlePackageUpgrade(req ipc.Envelope) ipc.Envelope {
	if !b.checkAllow("/sbin/apk") {
		return b.error(req, ipc.ErrCapabilityDenied, "/sbin/apk not allowed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	op, err := b.apkMgr.Upgrade(ctx)
	if err != nil && op == nil {
		return b.error(req, ipc.ErrExecutionFailed, err.Error())
	}
	return b.ok(req, b.opToIPC(op))
}

func (b *broker) handlePackageOpQuery(req ipc.Envelope) ipc.Envelope {
	var body ipc.PackageOpQueryReq
	if err := json.Unmarshal(req.Payload, &body); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	op := b.apkMgr.GetOperation(body.OpID)
	if op == nil {
		return b.error(req, ipc.ErrServiceNotFound, "operation not found")
	}
	return b.ok(req, b.opToIPC(op))
}

func (b *broker) handlePackageCacheClean(req ipc.Envelope) ipc.Envelope {
	if !b.checkAllow("/sbin/apk") {
		return b.error(req, ipc.ErrCapabilityDenied, "/sbin/apk not allowed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := b.run(ctx, "/sbin/apk", "cache", "clean")
	if err != nil {
		return b.error(req, ipc.ErrExecutionFailed, string(out))
	}
	return b.ok(req, map[string]string{"status": "cache cleaned"})
}

func (b *broker) handleDhcpToggle(req ipc.Envelope) ipc.Envelope {
	var body map[string]string
	if err := json.Unmarshal(req.Payload, &body); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	iface := body["iface"]
	action := body["action"]
	if iface == "" || (action != "start" && action != "stop") {
		return b.error(req, ipc.ErrInvalidRequest, "iface and action required")
	}
	if action == "start" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		out, err := b.run(ctx, "/sbin/udhcpc", "-i", iface, "-b")
		if err != nil {
			return b.error(req, ipc.ErrExecutionFailed, string(out))
		}
		return b.ok(req, map[string]string{"status": "dhcp started", "iface": iface})
	}
	// Stop: find udhcpc PID for this interface and kill it
	pidFile := fmt.Sprintf("/var/run/udhcpc.%s.pid", iface)
	if data, err := os.ReadFile(pidFile); err == nil {
		pid := strings.TrimSpace(string(data))
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		b.run(ctx, "/bin/kill", pid)
		os.Remove(pidFile)
		return b.ok(req, map[string]string{"status": "dhcp stopped", "iface": iface})
	}
	// Fallback: try to find and kill via pgrep or ps
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, _ := b.run(ctx, "/bin/sh", "-c", fmt.Sprintf("ps | grep 'udhcpc.*-i %s' | grep -v grep | awk '{print $1}'", iface))
	pid := strings.TrimSpace(string(out))
	if pid != "" {
		ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel2()
		b.run(ctx2, "/bin/kill", pid)
		return b.ok(req, map[string]string{"status": "dhcp stopped", "iface": iface})
	}
	return b.ok(req, map[string]string{"status": "no dhcp process found", "iface": iface})
}

func (b *broker) toIPCPackages(pkgs []apk.PackageInfo) []ipc.PackageInfo {
	var out []ipc.PackageInfo
	for _, p := range pkgs {
		out = append(out, b.toIPCPackage(p))
	}
	return out
}

func (b *broker) toIPCPackage(p apk.PackageInfo) ipc.PackageInfo {
	return ipc.PackageInfo{
		Name:        p.Name,
		Version:     p.Version,
		Description: p.Description,
		Size:        p.Size,
		Installed:   p.Installed,
	}
}

func (b *broker) opToIPC(op *apk.Operation) ipc.PackageOpData {
	var started, ended *int64
	if op.StartedAt != nil {
		s := op.StartedAt.Unix()
		started = &s
	}
	if op.EndedAt != nil {
		e := op.EndedAt.Unix()
		ended = &e
	}
	var prog []ipc.ProgressLine
	for _, p := range op.ProgressSnapshot() {
		prog = append(prog, ipc.ProgressLine{Timestamp: p.Timestamp.Unix(), Line: p.Line})
	}
	return ipc.PackageOpData{
		OpID:      op.ID,
		Type:      string(op.Type),
		State:     string(op.State),
		Progress:  prog,
		Error:     op.Error,
		CreatedAt: op.CreatedAt.Unix(),
		StartedAt: started,
		EndedAt:   ended,
	}
}

// ── Reboot / Shutdown ──────────────────────────────

func (b *broker) handleReboot(req ipc.Envelope) ipc.Envelope {
	bin, ok := b.resolveAllowed("/sbin/reboot", "/bin/reboot", "/usr/sbin/reboot", "/usr/bin/reboot")
	if !ok {
		return b.error(req, ipc.ErrCapabilityDenied, "reboot binary not allowed or not found")
	}
	var body ipc.RebootReq
	json.Unmarshal(req.Payload, &body)
	delay := body.DelaySec
	if delay < 0 || delay > 300 {
		delay = 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if delay > 0 {
		_, _ = b.run(ctx, bin, "-d", fmt.Sprintf("%d", delay))
	} else {
		_, _ = b.run(ctx, bin)
	}
	return b.ok(req, map[string]string{"status": "reboot scheduled"})
}

func (b *broker) handleShutdown(req ipc.Envelope) ipc.Envelope {
	bin, ok := b.resolveAllowed("/sbin/poweroff", "/bin/poweroff", "/usr/sbin/poweroff", "/usr/bin/poweroff")
	if !ok {
		return b.error(req, ipc.ErrCapabilityDenied, "poweroff binary not allowed or not found")
	}
	var body ipc.ShutdownReq
	json.Unmarshal(req.Payload, &body)
	delay := body.DelaySec
	if delay < 0 || delay > 300 {
		delay = 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if delay > 0 {
		_, _ = b.run(ctx, bin, "-d", fmt.Sprintf("%d", delay))
	} else {
		_, _ = b.run(ctx, bin)
	}
	return b.ok(req, map[string]string{"status": "shutdown scheduled"})
}

// ── User Management ────────────────────────────────

func (b *broker) handleUserCreate(req ipc.Envelope) ipc.Envelope {
	if !b.checkAllow("/sbin/adduser") {
		return b.error(req, ipc.ErrCapabilityDenied, "/sbin/adduser not allowed")
	}
	var body ipc.UserCreateReq
	if err := json.Unmarshal(req.Payload, &body); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	if err := b.validateUsername(body.Username); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	args := []string{"-D", body.Username}
	if body.Home != "" {
		if err := b.validateMountpoint(body.Home); err != nil {
			return b.error(req, ipc.ErrInvalidRequest, err.Error())
		}
		args = append(args, "-h", body.Home)
	}
	if body.Shell != "" {
		if !strings.HasPrefix(body.Shell, "/") || strings.Contains(body.Shell, "..") {
			return b.error(req, ipc.ErrInvalidRequest, "invalid shell path")
		}
		args = append(args, "-s", body.Shell)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := b.run(ctx, "/sbin/adduser", args...)
	if err != nil {
		return b.error(req, ipc.ErrExecutionFailed, string(out))
	}
	return b.ok(req, map[string]string{"username": body.Username, "status": "created"})
}

func (b *broker) handlePasswordReset(req ipc.Envelope) ipc.Envelope {
	if !b.checkAllow("/usr/bin/passwd") {
		return b.error(req, ipc.ErrCapabilityDenied, "/usr/bin/passwd not allowed")
	}
	var body ipc.PasswordResetReq
	if err := json.Unmarshal(req.Payload, &body); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	if err := b.validateUsername(body.Username); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Use stdin to set password; generate random password
	pass := generateRandomPassword(16)
	cmd := exec.CommandContext(ctx, "/usr/bin/passwd", body.Username)
	cmd.Stdin = strings.NewReader(pass + "\n" + pass + "\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return b.error(req, ipc.ErrExecutionFailed, string(out))
	}
	return b.ok(req, map[string]string{"username": body.Username, "status": "password reset", "temporary": pass})
}

func (b *broker) handleUserDelete(req ipc.Envelope) ipc.Envelope {
	if !b.checkAllow("/sbin/deluser") {
		return b.error(req, ipc.ErrCapabilityDenied, "/sbin/deluser not allowed")
	}
	var body ipc.UserDeleteReq
	if err := json.Unmarshal(req.Payload, &body); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	if err := b.validateUsername(body.Username); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := b.run(ctx, "/sbin/deluser", body.Username)
	if err != nil {
		return b.error(req, ipc.ErrExecutionFailed, string(out))
	}
	return b.ok(req, map[string]string{"username": body.Username, "status": "deleted"})
}

func (b *broker) handleApkRepoRead(req ipc.Envelope) ipc.Envelope {
	data, err := os.ReadFile("/etc/apk/repositories")
	if err != nil {
		return b.error(req, ipc.ErrExecutionFailed, err.Error())
	}
	return b.ok(req, map[string]string{"content": string(data)})
}

func (b *broker) handleApkRepoWrite(req ipc.Envelope) ipc.Envelope {
	var body ipc.ApkRepoWriteReq
	if err := json.Unmarshal(req.Payload, &body); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	if err := os.WriteFile("/etc/apk/repositories", []byte(body.Content), 0644); err != nil {
		return b.error(req, ipc.ErrExecutionFailed, err.Error())
	}
	return b.ok(req, map[string]string{"status": "written"})
}

func (b *broker) handleUserLock(req ipc.Envelope) ipc.Envelope {
	if !b.checkAllow("/usr/bin/passwd") {
		return b.error(req, ipc.ErrCapabilityDenied, "/usr/bin/passwd not allowed")
	}
	var body ipc.UserLockReq
	if err := json.Unmarshal(req.Payload, &body); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	if err := b.validateUsername(body.Username); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := b.run(ctx, "/usr/bin/passwd", "-l", body.Username)
	if err != nil {
		return b.error(req, ipc.ErrExecutionFailed, string(out))
	}
	return b.ok(req, map[string]string{"username": body.Username, "status": "locked"})
}

func (b *broker) handleUserUnlock(req ipc.Envelope) ipc.Envelope {
	if !b.checkAllow("/usr/bin/passwd") {
		return b.error(req, ipc.ErrCapabilityDenied, "/usr/bin/passwd not allowed")
	}
	var body ipc.UserUnlockReq
	if err := json.Unmarshal(req.Payload, &body); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	if err := b.validateUsername(body.Username); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := b.run(ctx, "/usr/bin/passwd", "-u", body.Username)
	if err != nil {
		return b.error(req, ipc.ErrExecutionFailed, string(out))
	}
	return b.ok(req, map[string]string{"username": body.Username, "status": "unlocked"})
}

func (b *broker) handleSshConfigRead(req ipc.Envelope) ipc.Envelope {
	data, err := os.ReadFile("/etc/ssh/sshd_config")
	if err != nil {
		return b.error(req, ipc.ErrExecutionFailed, err.Error())
	}
	return b.ok(req, map[string]string{"content": string(data)})
}

func (b *broker) handleSshConfigWrite(req ipc.Envelope) ipc.Envelope {
	var body ipc.SshConfigWriteReq
	if err := json.Unmarshal(req.Payload, &body); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	if err := os.WriteFile("/etc/ssh/sshd_config", []byte(body.Content), 0644); err != nil {
		return b.error(req, ipc.ErrExecutionFailed, err.Error())
	}
	return b.ok(req, map[string]string{"status": "written"})
}

func (b *broker) handleCronRead(req ipc.Envelope) ipc.Envelope {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := b.run(ctx, "/usr/bin/crontab", "-l")
	if err != nil && len(out) == 0 {
		return b.error(req, ipc.ErrExecutionFailed, string(out))
	}
	return b.ok(req, map[string]string{"content": string(out)})
}

func (b *broker) handleCronWrite(req ipc.Envelope) ipc.Envelope {
	var body ipc.CronWriteReq
	if err := json.Unmarshal(req.Payload, &body); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/crontab", "-")
	cmd.Stdin = strings.NewReader(body.Content)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return b.error(req, ipc.ErrExecutionFailed, string(out))
	}
	return b.ok(req, map[string]string{"status": "written"})
}

func (b *broker) handleProcessKill(req ipc.Envelope) ipc.Envelope {
	var body ipc.ProcessKillReq
	if err := json.Unmarshal(req.Payload, &body); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	if body.PID <= 0 {
		return b.error(req, ipc.ErrInvalidRequest, "invalid pid")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := b.run(ctx, "/bin/kill", fmt.Sprintf("%d", body.PID))
	if err != nil {
		return b.error(req, ipc.ErrExecutionFailed, string(out))
	}
	return b.ok(req, map[string]string{"pid": fmt.Sprintf("%d", body.PID), "status": "killed"})
}

func (b *broker) handleLogRead(req ipc.Envelope) ipc.Envelope {
	var body ipc.LogReadReq
	if err := json.Unmarshal(req.Payload, &body); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	if body.Limit <= 0 || body.Limit > 10000 {
		body.Limit = 500
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
		if body.Filter != "" && !strings.Contains(line, body.Filter) {
			continue
		}
		lines = append(lines, line)
	}
	start := 0
	if body.Offset > 0 && body.Offset < len(lines) {
		start = body.Offset
	}
	end := start + body.Limit
	if end > len(lines) {
		end = len(lines)
	}
	result := lines[start:end]
	return b.ok(req, map[string]interface{}{
		"lines":  result,
		"total":  len(lines),
		"offset": start,
		"limit":  body.Limit,
	})
}

// ── Mount / Unmount ──────────────────────────────

func (b *broker) handleMount(req ipc.Envelope) ipc.Envelope {
	if !b.checkAllow("/bin/mount") {
		return b.error(req, ipc.ErrCapabilityDenied, "/bin/mount not allowed")
	}
	var body ipc.MountReq
	if err := json.Unmarshal(req.Payload, &body); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	if err := b.validateDevice(body.Device); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	if err := b.validateMountpoint(body.Mountpoint); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	args := []string{body.Device, body.Mountpoint}
	if body.FSType != "" {
		args = append(args, "-t", body.FSType)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := b.run(ctx, "/bin/mount", args...)
	if err != nil {
		return b.error(req, ipc.ErrExecutionFailed, string(out))
	}
	return b.ok(req, map[string]string{"device": body.Device, "mountpoint": body.Mountpoint, "status": "mounted"})
}

func (b *broker) handleUnmount(req ipc.Envelope) ipc.Envelope {
	if !b.checkAllow("/bin/umount") {
		return b.error(req, ipc.ErrCapabilityDenied, "/bin/umount not allowed")
	}
	var body ipc.UnmountReq
	if err := json.Unmarshal(req.Payload, &body); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	if err := b.validateMountpoint(body.Mountpoint); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := b.run(ctx, "/bin/umount", body.Mountpoint)
	if err != nil {
		return b.error(req, ipc.ErrExecutionFailed, string(out))
	}
	return b.ok(req, map[string]string{"mountpoint": body.Mountpoint, "status": "unmounted"})
}

// ── Kernel Modules ─────────────────────────────────

func (b *broker) handleKModLoad(req ipc.Envelope) ipc.Envelope {
	if !b.checkAllow("/sbin/modprobe") {
		return b.error(req, ipc.ErrCapabilityDenied, "/sbin/modprobe not allowed")
	}
	var body ipc.KModLoadReq
	if err := json.Unmarshal(req.Payload, &body); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	if err := b.validateModule(body.Module); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := b.run(ctx, "/sbin/modprobe", body.Module)
	if err != nil {
		return b.error(req, ipc.ErrExecutionFailed, string(out))
	}
	return b.ok(req, map[string]string{"module": body.Module, "status": "loaded"})
}

func (b *broker) handleKModUnload(req ipc.Envelope) ipc.Envelope {
	if !b.checkAllow("/sbin/modprobe") {
		return b.error(req, ipc.ErrCapabilityDenied, "/sbin/modprobe not allowed")
	}
	var body ipc.KModUnloadReq
	if err := json.Unmarshal(req.Payload, &body); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	if err := b.validateModule(body.Module); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := b.run(ctx, "/sbin/modprobe", "-r", body.Module)
	if err != nil {
		return b.error(req, ipc.ErrExecutionFailed, string(out))
	}
	return b.ok(req, map[string]string{"module": body.Module, "status": "unloaded"})
}

// ── Network Interface Up/Down ───────────────────────

func (b *broker) handleNetIfUp(req ipc.Envelope) ipc.Envelope {
	if !b.checkAllow("/sbin/ip") {
		return b.error(req, ipc.ErrCapabilityDenied, "/sbin/ip not allowed")
	}
	var body ipc.NetIfReq
	if err := json.Unmarshal(req.Payload, &body); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	if err := b.validateInterface(body.Interface); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := b.run(ctx, "/sbin/ip", "link", "set", body.Interface, "up")
	if err != nil {
		return b.error(req, ipc.ErrExecutionFailed, string(out))
	}
	return b.ok(req, map[string]string{"interface": body.Interface, "status": "up"})
}

func (b *broker) handleNetIfDown(req ipc.Envelope) ipc.Envelope {
	if !b.checkAllow("/sbin/ip") {
		return b.error(req, ipc.ErrCapabilityDenied, "/sbin/ip not allowed")
	}
	var body ipc.NetIfReq
	if err := json.Unmarshal(req.Payload, &body); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	if err := b.validateInterface(body.Interface); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := b.run(ctx, "/sbin/ip", "link", "set", body.Interface, "down")
	if err != nil {
		return b.error(req, ipc.ErrExecutionFailed, string(out))
	}
	return b.ok(req, map[string]string{"interface": body.Interface, "status": "down"})
}

// ── fstab ──────────────────────────────────────────

func (b *broker) handleFstabRead(req ipc.Envelope) ipc.Envelope {
	data, err := os.ReadFile("/etc/fstab")
	if err != nil {
		return b.error(req, ipc.ErrExecutionFailed, err.Error())
	}
	return b.ok(req, map[string]string{"content": string(data)})
}

func (b *broker) handleFstabWrite(req ipc.Envelope) ipc.Envelope {
	var body ipc.FstabWriteReq
	if err := json.Unmarshal(req.Payload, &body); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	if err := os.WriteFile("/etc/fstab", []byte(body.Content), 0644); err != nil {
		return b.error(req, ipc.ErrExecutionFailed, err.Error())
	}
	return b.ok(req, map[string]string{"status": "written"})
}

// ── Timezone / NTP ─────────────────────────────────

func (b *broker) handleTimezoneSet(req ipc.Envelope) ipc.Envelope {
	var body ipc.TimezoneSetReq
	if err := json.Unmarshal(req.Payload, &body); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	if body.Timezone == "" || strings.Contains(body.Timezone, "..") {
		return b.error(req, ipc.ErrInvalidRequest, "invalid timezone")
	}
	if strings.HasPrefix(body.Timezone, "/") && !strings.HasPrefix(body.Timezone, "/usr/share/zoneinfo/") {
		return b.error(req, ipc.ErrInvalidRequest, "invalid timezone path")
	}
	tzPath := body.Timezone
	if !strings.HasPrefix(tzPath, "/") {
		tzPath = "/usr/share/zoneinfo/" + body.Timezone
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Copy timezone file to /etc/localtime
	out, err := b.run(ctx, "/bin/cp", tzPath, "/etc/localtime")
	if err != nil {
		return b.error(req, ipc.ErrExecutionFailed, string(out))
	}
	if err := os.WriteFile("/etc/timezone", []byte(body.Timezone+"\n"), 0644); err != nil {
		return b.error(req, ipc.ErrExecutionFailed, err.Error())
	}
	return b.ok(req, map[string]string{"timezone": body.Timezone, "status": "set"})
}

func (b *broker) handleNtpSet(req ipc.Envelope) ipc.Envelope {
	var body ipc.NtpSetReq
	if err := json.Unmarshal(req.Payload, &body); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	// Try chronyd first, then ntpd
	var svc string
	if _, err := os.Stat("/etc/init.d/chronyd"); err == nil {
		svc = "chronyd"
	} else if _, err := os.Stat("/etc/init.d/ntpd"); err == nil {
		svc = "ntpd"
	} else {
		return b.error(req, ipc.ErrExecutionFailed, "no NTP service found")
	}
	action := "stop"
	if body.Enabled {
		action = "start"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := b.run(ctx, "/sbin/rc-service", svc, action)
	if err != nil {
		return b.error(req, ipc.ErrExecutionFailed, string(out))
	}
	return b.ok(req, map[string]string{"service": svc, "action": action, "status": "ok"})
}

// ── Service Log ────────────────────────────────────

func (b *broker) handleServiceLogRead(req ipc.Envelope) ipc.Envelope {
	var body ipc.ServiceLogReadReq
	if err := json.Unmarshal(req.Payload, &body); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	if err := b.validateService(body.Name); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	// Try common log paths
	paths := []string{
		fmt.Sprintf("/var/log/%s.log", body.Name),
		fmt.Sprintf("/var/log/%s", body.Name),
		"/var/log/messages",
	}
	var data []byte
	for _, p := range paths {
		if d, err := os.ReadFile(p); err == nil {
			data = d
			break
		}
	}
	// Also get service status
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	statusOut, _ := b.run(ctx, "/sbin/rc-service", body.Name, "status")
	return b.ok(req, map[string]interface{}{
		"name":   body.Name,
		"log":    string(data),
		"status": string(statusOut),
	})
}

// ── User Groups ────────────────────────────────────

func (b *broker) handleUserGroupAdd(req ipc.Envelope) ipc.Envelope {
	var body ipc.UserGroupReq
	if err := json.Unmarshal(req.Payload, &body); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	if err := b.validateUsername(body.Username); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	if err := b.validateGroup(body.Group); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := b.run(ctx, "/usr/sbin/adduser", body.Username, body.Group)
	if err != nil {
		return b.error(req, ipc.ErrExecutionFailed, string(out))
	}
	return b.ok(req, map[string]string{"username": body.Username, "group": body.Group, "status": "added"})
}

func (b *broker) handleUserGroupRemove(req ipc.Envelope) ipc.Envelope {
	var body ipc.UserGroupReq
	if err := json.Unmarshal(req.Payload, &body); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	if err := b.validateUsername(body.Username); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	if err := b.validateGroup(body.Group); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := b.run(ctx, "/usr/sbin/delgroup", body.Username, body.Group)
	if err != nil {
		return b.error(req, ipc.ErrExecutionFailed, string(out))
	}
	return b.ok(req, map[string]string{"username": body.Username, "group": body.Group, "status": "removed"})
}

func (b *broker) validateInterface(name string) error {
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "..") {
		return fmt.Errorf("invalid interface name")
	}
	return nil
}

func (b *broker) validateGroup(name string) error {
	if name == "" || !reUsername.MatchString(name) {
		return fmt.Errorf("invalid group name")
	}
	return nil
}

// ── User Shell Change ────────────────────────────

func (b *broker) handleShellChange(req ipc.Envelope) ipc.Envelope {
	if !b.checkAllow("/usr/bin/chsh") && !b.checkAllow("/bin/chsh") {
		return b.error(req, ipc.ErrCapabilityDenied, "shell change not allowed")
	}
	var body ipc.ShellChangeReq
	if err := json.Unmarshal(req.Payload, &body); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	if err := b.validateUsername(body.Username); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	if body.Shell == "" || !strings.HasPrefix(body.Shell, "/") || strings.Contains(body.Shell, "..") {
		return b.error(req, ipc.ErrInvalidRequest, "invalid shell path")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := b.run(ctx, "/usr/sbin/usermod", "-s", body.Shell, body.Username)
	if err != nil {
		// fallback to chsh if usermod fails
		out, err = b.run(ctx, "/usr/bin/chsh", "-s", body.Shell, body.Username)
		if err != nil {
			return b.error(req, ipc.ErrExecutionFailed, string(out))
		}
	}
	return b.ok(req, map[string]string{"username": body.Username, "shell": body.Shell, "status": "changed"})
}

// ── LBU Backup / Restore ───────────────────────────

func (b *broker) handleLbuCommit(req ipc.Envelope) ipc.Envelope {
	if !b.checkAllow("/sbin/lbu") {
		return b.error(req, ipc.ErrCapabilityDenied, "/sbin/lbu not allowed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := b.run(ctx, "/sbin/lbu", "commit")
	if err != nil {
		return b.error(req, ipc.ErrExecutionFailed, string(out))
	}
	return b.ok(req, map[string]string{"status": "committed", "output": string(out)})
}

func (b *broker) handleLbuStatus(req ipc.Envelope) ipc.Envelope {
	if !b.checkAllow("/sbin/lbu") {
		return b.error(req, ipc.ErrCapabilityDenied, "/sbin/lbu not allowed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := b.run(ctx, "/sbin/lbu", "status")
	if err != nil {
		return b.error(req, ipc.ErrExecutionFailed, string(out))
	}
	return b.ok(req, map[string]string{"status": "ok", "output": string(out)})
}

func (b *broker) handleLbuList(req ipc.Envelope) ipc.Envelope {
	if !b.checkAllow("/sbin/lbu") {
		return b.error(req, ipc.ErrCapabilityDenied, "/sbin/lbu not allowed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := b.run(ctx, "/sbin/lbu", "list-backup")
	if err != nil {
		return b.error(req, ipc.ErrExecutionFailed, string(out))
	}
	backups := []string{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			backups = append(backups, line)
		}
	}
	return b.ok(req, map[string]interface{}{"backups": backups})
}

func (b *broker) handleLbuRestore(req ipc.Envelope) ipc.Envelope {
	if !b.checkAllow("/sbin/lbu") {
		return b.error(req, ipc.ErrCapabilityDenied, "/sbin/lbu not allowed")
	}
	var body ipc.LbuRestoreReq
	if err := json.Unmarshal(req.Payload, &body); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	if body.Backup == "" || strings.Contains(body.Backup, "..") || strings.Contains(body.Backup, "/") {
		return b.error(req, ipc.ErrInvalidRequest, "invalid backup name")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := b.run(ctx, "/sbin/lbu", "restore", body.Backup)
	if err != nil {
		return b.error(req, ipc.ErrExecutionFailed, string(out))
	}
	return b.ok(req, map[string]string{"status": "restored", "backup": body.Backup})
}

func (b *broker) handleSshKeysRead(req ipc.Envelope) ipc.Envelope {
	var body ipc.SshKeysReadReq
	if err := json.Unmarshal(req.Payload, &body); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	if err := b.validateUsername(body.Username); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	usr, err := user.Lookup(body.Username)
	if err != nil {
		return b.error(req, ipc.ErrExecutionFailed, "user lookup failed: "+err.Error())
	}
	path := filepath.Join(usr.HomeDir, ".ssh", "authorized_keys")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return b.ok(req, map[string]string{"content": ""})
		}
		return b.error(req, ipc.ErrExecutionFailed, err.Error())
	}
	return b.ok(req, map[string]string{"content": string(data)})
}

func (b *broker) handleSshKeysWrite(req ipc.Envelope) ipc.Envelope {
	var body ipc.SshKeysWriteReq
	if err := json.Unmarshal(req.Payload, &body); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	if err := b.validateUsername(body.Username); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	usr, err := user.Lookup(body.Username)
	if err != nil {
		return b.error(req, ipc.ErrExecutionFailed, "user lookup failed: "+err.Error())
	}
	sshDir := filepath.Join(usr.HomeDir, ".ssh")
	path := filepath.Join(sshDir, "authorized_keys")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return b.error(req, ipc.ErrExecutionFailed, err.Error())
	}
	if err := os.WriteFile(path, []byte(body.Content), 0600); err != nil {
		return b.error(req, ipc.ErrExecutionFailed, err.Error())
	}
	uid, _ := strconv.Atoi(usr.Uid)
	gid, _ := strconv.Atoi(usr.Gid)
	if err := os.Chown(sshDir, uid, gid); err != nil {
		return b.error(req, ipc.ErrExecutionFailed, err.Error())
	}
	if err := os.Chown(path, uid, gid); err != nil {
		return b.error(req, ipc.ErrExecutionFailed, err.Error())
	}
	return b.ok(req, map[string]string{"status": "saved", "username": body.Username})
}

func (b *broker) handleClockSet(req ipc.Envelope) ipc.Envelope {
	if !b.checkAllow("/bin/date") {
		return b.error(req, ipc.ErrCapabilityDenied, "/bin/date not allowed")
	}
	var body ipc.ClockSetReq
	if err := json.Unmarshal(req.Payload, &body); err != nil {
		return b.error(req, ipc.ErrInvalidRequest, err.Error())
	}
	if body.DateTime == "" {
		return b.error(req, ipc.ErrInvalidRequest, "datetime required")
	}
	// Validate format: YYYY-MM-DD HH:MM:SS
	matched, _ := regexp.MatchString(`^\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}:\d{2}$`, body.DateTime)
	if !matched {
		return b.error(req, ipc.ErrInvalidRequest, "invalid datetime format, expected YYYY-MM-DD HH:MM:SS")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := b.run(ctx, "/bin/date", "-s", body.DateTime)
	if err != nil {
		return b.error(req, ipc.ErrExecutionFailed, string(out))
	}
	return b.ok(req, map[string]string{"status": "clock set", "datetime": body.DateTime})
}

// generateRandomPassword creates a 16-char alphanumeric temporary password.
func generateRandomPassword(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	fd, err := os.Open("/dev/urandom")
	if err != nil {
		// fallback: not cryptographically secure, but we need something
		for i := range b {
			b[i] = chars[i%len(chars)]
		}
		return string(b)
	}
	defer fd.Close()
	buf := make([]byte, n)
	fd.Read(buf)
	for i := range b {
		b[i] = chars[buf[i]%byte(len(chars))]
	}
	return string(b)
}

