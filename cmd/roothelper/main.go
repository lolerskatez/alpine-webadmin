package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"regexp"
	"runtime"
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
	if !b.checkAllow("/sbin/reboot") {
		return b.error(req, ipc.ErrCapabilityDenied, "/sbin/reboot not allowed")
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
		_, _ = b.run(ctx, "/sbin/reboot", "-d", fmt.Sprintf("%d", delay))
	} else {
		_, _ = b.run(ctx, "/sbin/reboot")
	}
	return b.ok(req, map[string]string{"status": "reboot scheduled"})
}

func (b *broker) handleShutdown(req ipc.Envelope) ipc.Envelope {
	if !b.checkAllow("/sbin/poweroff") {
		return b.error(req, ipc.ErrCapabilityDenied, "/sbin/poweroff not allowed")
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
		_, _ = b.run(ctx, "/sbin/poweroff", "-d", fmt.Sprintf("%d", delay))
	} else {
		_, _ = b.run(ctx, "/sbin/poweroff")
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

