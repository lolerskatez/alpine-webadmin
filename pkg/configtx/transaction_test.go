package configtx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alpine-webadmin/alpine-webadmin/pkg/log"
)

func newTestEngine() *Engine {
	dir, _ := os.MkdirTemp("", "configtx-test")
	return NewEngine(dir, log.New(log.Error))
}

func TestBeginCreatesBackup(t *testing.T) {
	e := newTestEngine()
	defer os.RemoveAll(e.backupDir)

	// Create a dummy config file
	tmpDir, _ := os.MkdirTemp("", "config")
	configPath := filepath.Join(tmpDir, "test.conf")
	os.WriteFile(configPath, []byte("original content"), 0644)

	target := &TargetConfig{
		Name: "test",
		Path: configPath,
		Validate: func(path string) error {
			return nil
		},
	}

	tx, err := e.Begin(target)
	if err != nil {
		t.Fatalf("Begin failed: %v", err)
	}
	if tx.ID == "" {
		t.Fatal("transaction ID is empty")
	}
	if tx.Status != StatusPending {
		t.Fatalf("initial status = %q, want pending", tx.Status)
	}

	// Backup should exist
	if _, err := os.Stat(tx.BackupPath); err != nil {
		t.Fatalf("backup not created: %v", err)
	}
	data, _ := os.ReadFile(tx.BackupPath)
	if string(data) != "original content" {
		t.Fatalf("backup content = %q, want original content", string(data))
	}
}

func TestStageAndValidate(t *testing.T) {
	e := newTestEngine()
	defer os.RemoveAll(e.backupDir)

	tmpDir, _ := os.MkdirTemp("", "config")
	configPath := filepath.Join(tmpDir, "test.conf")
	os.WriteFile(configPath, []byte("old"), 0644)

	validated := false
	target := &TargetConfig{
		Name: "test",
		Path: configPath,
		Validate: func(path string) error {
			validated = true
			data, _ := os.ReadFile(path)
			if string(data) != "new content" {
				return os.ErrInvalid
			}
			return nil
		},
	}

	tx, _ := e.Begin(target)
	if err := tx.Stage([]byte("new content")); err != nil {
		t.Fatalf("Stage failed: %v", err)
	}
	if tx.Status != StatusStaged {
		t.Fatalf("status after stage = %q, want staged", tx.Status)
	}

	if err := tx.Validate(); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
	if !validated {
		t.Fatal("validator was not called")
	}
	if tx.Status != StatusValidated {
		t.Fatalf("status after validate = %q, want validated", tx.Status)
	}
}

func TestValidateFailure(t *testing.T) {
	e := newTestEngine()
	defer os.RemoveAll(e.backupDir)

	tmpDir, _ := os.MkdirTemp("", "config")
	configPath := filepath.Join(tmpDir, "test.conf")
	os.WriteFile(configPath, []byte("old"), 0644)

	target := &TargetConfig{
		Name: "test",
		Path: configPath,
		Validate: func(path string) error {
			return os.ErrInvalid
		},
	}

	tx, _ := e.Begin(target)
	tx.Stage([]byte("bad"))
	if err := tx.Validate(); err == nil {
		t.Fatal("expected validation to fail")
	}
	if tx.Status != StatusFailed {
		t.Fatalf("status after failed validate = %q, want failed", tx.Status)
	}
}

func TestCommitAndRollback(t *testing.T) {
	e := newTestEngine()
	defer os.RemoveAll(e.backupDir)

	tmpDir, _ := os.MkdirTemp("", "config")
	configPath := filepath.Join(tmpDir, "test.conf")
	os.WriteFile(configPath, []byte("original"), 0644)

	target := &TargetConfig{
		Name:       "test",
		Path:       configPath,
		NeedReload: false,
	}

	tx, _ := e.Begin(target)
	tx.Stage([]byte("modified"))
	tx.Validate()

	if err := e.Commit(tx.ID); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	data, _ := os.ReadFile(configPath)
	if string(data) != "modified" {
		t.Fatalf("live config = %q, want modified", string(data))
	}

	// Rollback
	if err := e.Rollback(tx.ID); err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	data, _ = os.ReadFile(configPath)
	if string(data) != "original" {
		t.Fatalf("live config after rollback = %q, want original", string(data))
	}
}

func TestNetworkCommitConfirm(t *testing.T) {
	e := newTestEngine()
	defer os.RemoveAll(e.backupDir)

	// Shorten confirm window for testing
	e.netSafety.confirmTime = 100 * time.Millisecond

	tmpDir, _ := os.MkdirTemp("", "config")
	configPath := filepath.Join(tmpDir, "network.conf")
	os.WriteFile(configPath, []byte("original network"), 0644)

	target := &TargetConfig{
		Name:      "network",
		Path:      configPath,
		IsNetwork: true,
		Service:   "networking",
	}

	tx, _ := e.Begin(target)
	tx.Stage([]byte("new network"))
	tx.Validate()

	// Commit triggers StageCommit which applies and starts timer
	if err := e.Commit(tx.ID); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// Wait for auto-rollback (no confirm)
	time.Sleep(200 * time.Millisecond)

	// Config should have been rolled back
	data, _ := os.ReadFile(configPath)
	if string(data) != "original network" {
		t.Fatalf("config after auto-rollback = %q, want original", string(data))
	}
}

func TestNetworkCommitWithConfirm(t *testing.T) {
	e := newTestEngine()
	defer os.RemoveAll(e.backupDir)

	// Shorten confirm window for testing
	e.netSafety.confirmTime = 500 * time.Millisecond

	tmpDir, _ := os.MkdirTemp("", "config")
	configPath := filepath.Join(tmpDir, "network.conf")
	os.WriteFile(configPath, []byte("original network"), 0644)

	target := &TargetConfig{
		Name:      "network",
		Path:      configPath,
		IsNetwork: true,
		Service:   "networking",
	}

	tx, _ := e.Begin(target)
	tx.Stage([]byte("new network"))
	tx.Validate()

	if err := e.Commit(tx.ID); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// Confirm before timer expires
	time.Sleep(50 * time.Millisecond)
	if err := e.Confirm(target.Name); err != nil {
		t.Fatalf("Confirm failed: %v", err)
	}

	// Wait past the original timer
	time.Sleep(600 * time.Millisecond)

	// Config should remain new (not rolled back)
	data, _ := os.ReadFile(configPath)
	if string(data) != "new network" {
		t.Fatalf("config after confirm = %q, want new network", string(data))
	}
}

func TestCleanup(t *testing.T) {
	e := newTestEngine()
	defer os.RemoveAll(e.backupDir)

	tmpDir, _ := os.MkdirTemp("", "config")
	configPath := filepath.Join(tmpDir, "test.conf")
	os.WriteFile(configPath, []byte("x"), 0644)

	// Create several transactions
	for i := 0; i < 3; i++ {
		tx, _ := e.Begin(&TargetConfig{Name: "test", Path: configPath})
		tx.Stage([]byte("y"))
	}

	if len(e.List()) != 3 {
		t.Fatalf("expected 3 transactions, got %d", len(e.List()))
	}

	// Cleanup with zero TTL removes all
	e.Cleanup(0, 10)
	if len(e.List()) != 0 {
		t.Fatalf("expected 0 transactions after cleanup, got %d", len(e.List()))
	}
}

func TestValidators(t *testing.T) {
	tests := []struct {
		name    string
		content string
		fn      func(string) error
		wantErr bool
	}{
		{
			name:    "network valid",
			content: "iface eth0 inet dhcp\n",
			fn:      ValidateNetwork,
			wantErr: false,
		},
		{
			name:    "network bad ip",
			content: "iface eth0 inet static\n  address notanip\n",
			fn:      ValidateNetwork,
			wantErr: true,
		},
		{
			name:    "fstab valid",
			content: "/dev/sda1 / ext4 defaults 0 1\n",
			fn:      ValidateFSTAB,
			wantErr: false,
		},
		{
			name:    "fstab traversal",
			content: "/dev/sda1 /etc/../tmp ext4 defaults 0 1\n",
			fn:      ValidateFSTAB,
			wantErr: true,
		},
		{
			name:    "exports valid",
			content: "/home 192.168.1.0/24(rw)\n",
			fn:      ValidateExports,
			wantErr: false,
		},
		{
			name:    "exports missing client",
			content: "/home\n",
			fn:      ValidateExports,
			wantErr: true,
		},
		{
			name:    "openrc valid",
			content: "command=\"/usr/sbin/nginx\"\n",
			fn:      ValidateOpenRC,
			wantErr: false,
		},
		{
			name:    "openrc injection",
			content: "command=\`/bin/rm -rf /\`\n",
			fn:      ValidateOpenRC,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, _ := os.CreateTemp("", "configtx")
			f.WriteString(tt.content)
			f.Close()
			defer os.Remove(f.Name())

			err := tt.fn(f.Name())
			if tt.wantErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestCopyFile(t *testing.T) {
	src, _ := os.CreateTemp("", "src")
	src.WriteString("hello")
	src.Close()
	defer os.Remove(src.Name())

	dst := src.Name() + ".copy"
	defer os.Remove(dst)

	if err := copyFile(src.Name(), dst); err != nil {
		t.Fatalf("copyFile failed: %v", err)
	}
	data, _ := os.ReadFile(dst)
	if string(data) != "hello" {
		t.Fatalf("copied content = %q, want hello", string(data))
	}
}

func BenchmarkTransactionLifecycle(b *testing.B) {
	e := newTestEngine()
	defer os.RemoveAll(e.backupDir)

	tmpDir, _ := os.MkdirTemp("", "config")
	configPath := filepath.Join(tmpDir, "test.conf")
	os.WriteFile(configPath, []byte("original"), 0644)

	target := &TargetConfig{Name: "bench", Path: configPath}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tx, _ := e.Begin(target)
		tx.Stage([]byte("modified"))
		tx.Validate()
		e.Commit(tx.ID)
	}
}
