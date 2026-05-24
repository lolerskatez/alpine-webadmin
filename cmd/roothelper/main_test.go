package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alpine-webadmin/alpine-webadmin/pkg/config"
	"github.com/alpine-webadmin/alpine-webadmin/pkg/ipc"
	"github.com/alpine-webadmin/alpine-webadmin/pkg/log"
)

func newTestBroker() *broker {
	cfg := &config.Config{
		AllowedHelpers: []string{
			"/sbin/rc-status",
			"/sbin/rc-service",
			"/sbin/rc-update",
			"/sbin/apk",
			"/sbin/reboot",
			"/sbin/poweroff",
			"/sbin/adduser",
			"/usr/bin/passwd",
			"/bin/mount",
			"/bin/umount",
			"/sbin/modprobe",
		},
	}
	return newBroker(cfg, log.New(log.Error))
}

// ── Validation ─────────────────────────────────────

func TestValidateService(t *testing.T) {
	b := newTestBroker()
	if err := b.validateService("nginx"); err != nil {
		t.Errorf("valid service rejected: %v", err)
	}
	if err := b.validateService("../etc/passwd"); err == nil {
		t.Error("traversal service should be rejected")
	}
	if err := b.validateService("service; rm -rf /"); err == nil {
		t.Error("injected service should be rejected")
	}
	if err := b.validateService(""); err == nil {
		t.Error("empty service should be rejected")
	}
}

func TestValidateUsername(t *testing.T) {
	b := newTestBroker()
	good := []string{"root", "admin", "webadmin", "user_123"}
	for _, u := range good {
		if err := b.validateUsername(u); err != nil {
			t.Errorf("valid username %q rejected: %v", u, err)
		}
	}
	bad := []string{"", "123root", "root; rm -rf /", "root/../etc"}
	for _, u := range bad {
		if err := b.validateUsername(u); err == nil {
			t.Errorf("invalid username %q should be rejected", u)
		}
	}
}

func TestValidateModule(t *testing.T) {
	b := newTestBroker()
	if err := b.validateModule("ext4"); err != nil {
		t.Errorf("valid module rejected: %v", err)
	}
	if err := b.validateModule("ext4; /bin/sh"); err == nil {
		t.Error("injected module should be rejected")
	}
}

func TestValidateMountpoint(t *testing.T) {
	b := newTestBroker()
	if err := b.validateMountpoint("/mnt/data"); err != nil {
		t.Errorf("valid mountpoint rejected: %v", err)
	}
	if err := b.validateMountpoint("../etc"); err == nil {
		t.Error("relative mountpoint should be rejected")
	}
	if err := b.validateMountpoint("/mnt/../etc"); err == nil {
		t.Error("traversal mountpoint should be rejected")
	}
}

func TestValidateDevice(t *testing.T) {
	b := newTestBroker()
	if err := b.validateDevice("/dev/sda1"); err != nil {
		t.Errorf("valid device rejected: %v", err)
	}
	if err := b.validateDevice("../dev/sda1"); err == nil {
		t.Error("relative device should be rejected")
	}
	if err := b.validateDevice("/tmp/evil"); err == nil {
		t.Error("non-/dev device should be rejected")
	}
}

func TestValidateAction(t *testing.T) {
	b := newTestBroker()
	if err := b.validateAction("start", "start", "stop"); err != nil {
		t.Errorf("valid action rejected: %v", err)
	}
	if err := b.validateAction("rm -rf /", "start", "stop"); err == nil {
		t.Error("injected action should be rejected")
	}
}

// ── Handler Dispatch ───────────────────────────────

func TestDispatchUnknownType(t *testing.T) {
	b := newTestBroker()
	req := ipc.Envelope{Version: ipc.Version, RequestID: "1", MessageType: "cap.unknown", Payload: []byte("{}")}
	resp := b.handle(req)
	if resp.MessageType != ipc.TypeResponseError {
		t.Fatalf("expected error response, got %q", resp.MessageType)
	}
}

func TestDispatchVersionMismatch(t *testing.T) {
	b := newTestBroker()
	req := ipc.Envelope{Version: 999, RequestID: "1", MessageType: ipc.TypeSystemInfo}
	resp := b.handle(req)
	if resp.MessageType != ipc.TypeResponseError {
		t.Fatalf("expected error response, got %q", resp.MessageType)
	}
}

// ── Capability Denied ──────────────────────────────

func TestCapabilityDenied(t *testing.T) {
	cfg := &config.Config{AllowedHelpers: []string{"/sbin/rc-status"}}
	b := newBroker(cfg, log.New(log.Error))

	// apk is NOT in the allowlist
	req := ipc.Envelope{
		Version:     ipc.Version,
		RequestID:   "1",
		MessageType: ipc.TypePackageList,
		Payload:     []byte("{}"),
	}
	resp := b.handle(req)
	if resp.MessageType != ipc.TypeResponseError {
		t.Fatalf("expected error, got %q", resp.MessageType)
	}
	var err ipc.ResponseError
	json.Unmarshal(resp.Payload, &err)
	if err.Code != ipc.ErrCapabilityDenied {
		t.Errorf("error code = %q, want CapabilityDenied", err.Code)
	}
}

// ── Audit Logging ──────────────────────────────────

func TestAudit(t *testing.T) {
	b := newTestBroker()
	// audit just logs; ensure no panic
	b.audit("test", ipc.Envelope{RequestID: "1", MessageType: ipc.TypeSystemInfo})
	b.audit("test", ipc.Envelope{RequestID: "2", MessageType: ipc.TypeSystemInfo}, "key", "value")
}

// ── Timeout ────────────────────────────────────────

func TestRunTimeout(t *testing.T) {
	b := newTestBroker()
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	// sleep should be killed by timeout
	_, err := b.run(ctx, "/bin/sleep", "10")
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

// ── Password Generation ────────────────────────────

func TestGenerateRandomPassword(t *testing.T) {
	p1 := generateRandomPassword(16)
	if len(p1) != 16 {
		t.Fatalf("password length = %d, want 16", len(p1))
	}
	p2 := generateRandomPassword(16)
	if p1 == p2 {
		t.Fatal("two random passwords should differ")
	}
}

// ── Benchmarks ─────────────────────────────────────

func BenchmarkValidateService(b *testing.B) {
	broker := newTestBroker()
	for i := 0; i < b.N; i++ {
		broker.validateService("nginx")
	}
}

func BenchmarkValidateUsername(b *testing.B) {
	broker := newTestBroker()
	for i := 0; i < b.N; i++ {
		broker.validateUsername("admin")
	}
}
