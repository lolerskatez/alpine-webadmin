package apk

import (
	"testing"
)

func TestValidatePackageName(t *testing.T) {
	good := []string{"nginx", "openssh-server", "alpine-baselayout", "lua5.4", "pkg+name"}
	for _, n := range good {
		if err := validatePackageName(n); err != nil {
			t.Errorf("valid name %q rejected: %v", n, err)
		}
	}
	bad := []string{"", "nginx; rm -rf /", "nginx../../etc", "pkg name"}
	for _, n := range bad {
		if err := validatePackageName(n); err == nil {
			t.Errorf("invalid name %q should be rejected", n)
		}
	}
}

func TestStripVersion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"nginx-1.24.0-r6", "nginx"},
		{"openssh-server-9.3_p2-r0", "openssh-server"},
		{"a-b-c-1.0-r1", "a-b-c"},
		{"alpine-baselayout", "alpine-baselayout"},
		{"simple", "simple"},
	}
	for _, tt := range tests {
		if got := stripVersion(tt.input); got != tt.want {
			t.Errorf("stripVersion(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestExtractVersion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"nginx-1.24.0-r6", "1.24.0-r6"},
		{"openssh-server-9.3_p2-r0", "9.3_p2-r0"},
		{"simple", ""},
	}
	for _, tt := range tests {
		if got := extractVersion(tt.input); got != tt.want {
			t.Errorf("extractVersion(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseList(t *testing.T) {
	mgr := NewManager(nil)
	fixture := `
		nginx-1.24.0-r6 installed size:1234KiB
		openssh-9.3_p2-r0 installed size:5678KiB
		alpine-baselayout-3.4.3-r1 installed size:89KiB
	`
	pkgs := mgr.parseList(fixture)
	if len(pkgs) != 3 {
		t.Fatalf("expected 3 packages, got %d", len(pkgs))
	}
	if pkgs[0].Name != "nginx" || pkgs[0].Version != "1.24.0-r6" {
		t.Errorf("pkg[0] = %+v", pkgs[0])
	}
	if pkgs[1].Name != "openssh" || pkgs[1].Version != "9.3_p2-r0" {
		t.Errorf("pkg[1] = %+v", pkgs[1])
	}
	if !pkgs[0].Installed {
		t.Error("expected installed=true")
	}
}

func TestParseSearch(t *testing.T) {
	mgr := NewManager(nil)
	fixture := `
		nginx-1.24.0-r6 - HTTP and reverse proxy server
		nginx-doc-1.24.0-r6 - Nginx documentation
		lua-nginx - Lua module for nginx
	`
	pkgs := mgr.parseSearch(fixture)
	if len(pkgs) != 3 {
		t.Fatalf("expected 3 packages, got %d", len(pkgs))
	}
	if pkgs[0].Name != "nginx" || pkgs[0].Description != "HTTP and reverse proxy server" {
		t.Errorf("pkg[0] = %+v", pkgs[0])
	}
	if pkgs[1].Name != "nginx-doc" || pkgs[1].Description != "Nginx documentation" {
		t.Errorf("pkg[1] = %+v", pkgs[1])
	}
}

func TestParseInfo(t *testing.T) {
	mgr := NewManager(nil)
	fixture := `nginx-1.24.0-r6 description:
nginx-1.24.0-r6 webpage:
nginx-1.24.0-r6 installed size:
 1234 KiB
description:
 HTTP and reverse proxy server
installed size:
 1234 KiB
`
	info := mgr.parseInfo("nginx", fixture)
	if info.Name != "nginx" {
		t.Errorf("name = %q, want nginx", info.Name)
	}
	if info.Size != "1234 KiB" {
		t.Errorf("size = %q, want 1234 KiB", info.Size)
	}
	if info.Description != "HTTP and reverse proxy server" {
		t.Errorf("description = %q", info.Description)
	}
}

func TestManagerQueue(t *testing.T) {
	mgr := NewManager(nil)
	mgr.SetMaxQueue(2)

	if mgr.QueueLen() != 0 {
		t.Fatalf("expected empty queue")
	}

	// Simulate locked state
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	// Queue should accept when locked
	mgr.queueMu.Lock()
	mgr.running = &Operation{ID: "test"}
	mgr.queueMu.Unlock()

	// Try to queue an operation (it should succeed as pending)
	op1, err := mgr.runMutating(nil, OpUpdate, []string{"update"})
	if err != nil {
		t.Fatalf("queue failed: %v", err)
	}
	if op1.State != StatePending {
		t.Fatalf("expected pending, got %s", op1.State)
	}

	// Queue another
	op2, err := mgr.runMutating(nil, OpUpgrade, []string{"upgrade"})
	if err != nil {
		t.Fatalf("queue failed: %v", err)
	}
	if op2.State != StatePending {
		t.Fatalf("expected pending, got %s", op2.State)
	}

	// Third should fail (queue full)
	_, err = mgr.runMutating(nil, OpInstall, []string{"install", "nginx"})
	if err == nil {
		t.Fatal("expected queue full error")
	}

	mgr.queueMu.Lock()
	mgr.running = nil
	mgr.queue = nil
	mgr.queueMu.Unlock()
}

func TestOperationProgress(t *testing.T) {
	op := &Operation{ID: "test"}
	op.mu.Lock()
	op.Progress = []ProgressLine{
		{Line: "(1/3) Installing..."},
		{Line: "(2/3) Installing..."},
		{Line: "(3/3) Installing..."},
	}
	op.mu.Unlock()

	if op.LastProgress() != "(3/3) Installing..." {
		t.Errorf("last progress = %q", op.LastProgress())
	}

	snap := op.ProgressSnapshot()
	if len(snap) != 3 {
		t.Fatalf("snapshot len = %d", len(snap))
	}
}

func BenchmarkParseList(b *testing.B) {
	mgr := NewManager(nil)
	fixture := `
		nginx-1.24.0-r6 installed size:1234KiB
		openssh-9.3_p2-r0 installed size:5678KiB
		alpine-baselayout-3.4.3-r1 installed size:89KiB
		busybox-1.36.1-r0 installed size:456KiB
		musl-1.2.4-r0 installed size:345KiB
	`
	for i := 0; i < b.N; i++ {
		mgr.parseList(fixture)
	}
}
