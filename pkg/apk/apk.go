package apk

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alpine-webadmin/alpine-webadmin/pkg/log"
)

// OpType identifies the kind of package operation.
type OpType string

const (
	OpSearch  OpType = "search"
	OpInstall OpType = "install"
	OpRemove  OpType = "remove"
	OpUpdate  OpType = "update"
	OpUpgrade OpType = "upgrade"
	OpInfo    OpType = "info"
	OpList    OpType = "list"
)

// OpState tracks the lifecycle of an operation.
type OpState string

const (
	StatePending   OpState = "pending"
	StateRunning   OpState = "running"
	StateCompleted OpState = "completed"
	StateFailed    OpState = "failed"
	StateCancelled OpState = "cancelled"
)

// Operation represents a single APK command invocation.
type Operation struct {
	ID        string
	Type      OpType
	Args      []string
	State     OpState
	CreatedAt time.Time
	StartedAt *time.Time
	EndedAt   *time.Time
	Error     string
	Progress  []ProgressLine
	mu        sync.RWMutex
}

// ProgressLine is a single line of incremental progress output.
type ProgressLine struct {
	Timestamp time.Time `json:"ts"`
	Line      string    `json:"line"`
}

// PackageInfo holds metadata for a single package.
type PackageInfo struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
	Size        string `json:"size,omitempty"`
	Installed   bool   `json:"installed,omitempty"`
}

// Manager is the safe, mutex-protected APK package manager.
type Manager struct {
	apkBin      string
	apkCacheDir string
	logger      *log.Logger
	mu          sync.Mutex        // global APK mutex: only one operation at a time
	queue       []*Operation      // pending ops
	queueMu     sync.Mutex
	running     *Operation        // currently executing op
	maxQueue    int
	timeout     time.Duration
}

// NewManager creates an APK manager.
func NewManager(logger *log.Logger) *Manager {
	return &Manager{
		apkBin:      "/sbin/apk",
		apkCacheDir: "/var/cache/apk",
		logger:      logger,
		maxQueue:    10,
		timeout:     5 * time.Minute,
	}
}

// SetTimeout adjusts the default operation timeout.
func (m *Manager) SetTimeout(d time.Duration) { m.timeout = d }

// SetMaxQueue adjusts the pending operation queue capacity.
func (m *Manager) SetMaxQueue(n int) { m.maxQueue = n }

// IsLocked returns true if an operation is currently running.
func (m *Manager) IsLocked() bool {
	m.queueMu.Lock()
	defer m.queueMu.Unlock()
	return m.running != nil
}

// RunningID returns the ID of the currently running operation, or empty.
func (m *Manager) RunningID() string {
	m.queueMu.Lock()
	defer m.queueMu.Unlock()
	if m.running != nil {
		return m.running.ID
	}
	return ""
}

// ── Synchronous Queries (read-only, no mutex needed) ───────────

// List returns all installed packages.
func (m *Manager) List(ctx context.Context) ([]PackageInfo, error) {
	out, err := m.exec(ctx, "list", "--installed")
	if err != nil {
		return nil, fmt.Errorf("apk: list failed: %w: %s", err, string(out))
	}
	return m.parseList(string(out)), nil
}

// Search finds packages by name or description.
func (m *Manager) Search(ctx context.Context, query string) ([]PackageInfo, error) {
	if err := validatePackageName(query); err != nil {
		return nil, err
	}
	out, err := m.exec(ctx, "search", query)
	if err != nil {
		return nil, fmt.Errorf("apk: search failed: %w: %s", err, string(out))
	}
	return m.parseSearch(string(out)), nil
}

// Info returns metadata for a specific package.
func (m *Manager) Info(ctx context.Context, name string) (*PackageInfo, error) {
	if err := validatePackageName(name); err != nil {
		return nil, err
	}
	out, err := m.exec(ctx, "info", name)
	if err != nil {
		return nil, fmt.Errorf("apk: info failed: %w: %s", err, string(out))
	}
	return m.parseInfo(name, string(out)), nil
}

// ── Mutations (mutex-protected + progress streaming) ──────────────

// Update refreshes the package index.
func (m *Manager) Update(ctx context.Context) (*Operation, error) {
	return m.runMutating(ctx, OpUpdate, []string{"update"})
}

// Upgrade upgrades all installed packages.
func (m *Manager) Upgrade(ctx context.Context) (*Operation, error) {
	return m.runMutating(ctx, OpUpgrade, []string{"upgrade"})
}

// Install adds one or more packages.
func (m *Manager) Install(ctx context.Context, packages ...string) (*Operation, error) {
	for _, p := range packages {
		if err := validatePackageName(p); err != nil {
			return nil, err
		}
	}
	args := append([]string{"add"}, packages...)
	return m.runMutating(ctx, OpInstall, args)
}

// Remove removes one or more packages.
func (m *Manager) Remove(ctx context.Context, packages ...string) (*Operation, error) {
	for _, p := range packages {
		if err := validatePackageName(p); err != nil {
			return nil, err
		}
	}
	args := append([]string{"del"}, packages...)
	return m.runMutating(ctx, OpRemove, args)
}

// ── Queue Inspection ───────────────────────────────

// QueueLen returns the number of pending operations.
func (m *Manager) QueueLen() int {
	m.queueMu.Lock()
	defer m.queueMu.Unlock()
	return len(m.queue)
}

// GetOperation returns an operation by ID.
func (m *Manager) GetOperation(id string) *Operation {
	m.queueMu.Lock()
	defer m.queueMu.Unlock()
	if m.running != nil && m.running.ID == id {
		return m.running
	}
	for _, op := range m.queue {
		if op.ID == id {
			return op
		}
	}
	return nil
}

// FlushQueue removes all pending (not running) operations.
func (m *Manager) FlushQueue() int {
	m.queueMu.Lock()
	defer m.queueMu.Unlock()
	n := len(m.queue)
	m.queue = nil
	return n
}

// ── Internal ─────────────────────────────────────

// runMutating acquires the global APK mutex, runs the command, and streams progress.
func (m *Manager) runMutating(ctx context.Context, opType OpType, args []string) (*Operation, error) {
	m.queueMu.Lock()
	if m.running != nil {
		// Queue if there's room
		if len(m.queue) >= m.maxQueue {
			m.queueMu.Unlock()
			return nil, fmt.Errorf("apk: queue full (%d ops pending)", m.maxQueue)
		}
		op := &Operation{
			ID:        m.genID(),
			Type:      opType,
			Args:      args,
			State:     StatePending,
			CreatedAt: time.Now(),
		}
		m.queue = append(m.queue, op)
		m.queueMu.Unlock()
		m.logger.Info("apk queued", map[string]interface{}{"id": op.ID, "type": opType})
		return op, nil
	}

	// Can run immediately
	op := &Operation{
		ID:        m.genID(),
		Type:      opType,
		Args:      args,
		State:     StateRunning,
		CreatedAt: time.Now(),
	}
	now := time.Now()
	op.StartedAt = &now
	m.running = op
	m.queueMu.Unlock()

	// Acquire global APK mutex
	m.mu.Lock()
	defer m.mu.Unlock()

	// Execute with progress capture
	err := m.executeWithProgress(ctx, op)
	end := time.Now()
	op.EndedAt = &end

	m.queueMu.Lock()
	m.running = nil
	// Promote next queued op if any
	if len(m.queue) > 0 {
		next := m.queue[0]
		m.queue = m.queue[1:]
		m.running = next
		next.State = StateRunning
		n := time.Now()
		next.StartedAt = &n
		go m.runQueued(next)
	}
	m.queueMu.Unlock()

	if err != nil {
		op.State = StateFailed
		op.Error = err.Error()
		return op, err
	}
	op.State = StateCompleted
	return op, nil
}

// runQueued is called in a goroutine to execute a previously queued operation.
func (m *Manager) runQueued(op *Operation) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), m.timeout)
	defer cancel()

	err := m.executeWithProgress(ctx, op)
	end := time.Now()
	op.EndedAt = &end

	m.queueMu.Lock()
	m.running = nil
	// Chain next queued op
	if len(m.queue) > 0 {
		next := m.queue[0]
		m.queue = m.queue[1:]
		m.running = next
		next.State = StateRunning
		n := time.Now()
		next.StartedAt = &n
		go m.runQueued(next)
	}
	m.queueMu.Unlock()

	if err != nil {
		op.State = StateFailed
		op.Error = err.Error()
	} else {
		op.State = StateCompleted
	}
}

// executeWithProgress runs the APK command and captures incremental output.
func (m *Manager) executeWithProgress(ctx context.Context, op *Operation) error {
	m.logger.Info("apk start", map[string]interface{}{
		"id":   op.ID,
		"type": op.Type,
		"args": op.Args,
	})

	args := append([]string{"--no-progress"}, op.Args...)
	cmd := exec.CommandContext(ctx, m.apkBin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid: uint32(syscall.Getuid()),
			Gid: uint32(syscall.Getgid()),
		},
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("apk: stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("apk: stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("apk: start: %w", err)
	}

	// Merge stdout + stderr and stream into progress
	merged := io.MultiReader(stdout, stderr)
	scanner := bufio.NewScanner(merged)
	for scanner.Scan() {
		line := scanner.Text()
		op.mu.Lock()
		op.Progress = append(op.Progress, ProgressLine{
			Timestamp: time.Now(),
			Line:      line,
		})
		op.mu.Unlock()
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("apk: %w", err)
	}
	return nil
}

// exec runs a simple APK command (no progress capture, for queries).
func (m *Manager) exec(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, m.apkBin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid: uint32(syscall.Getuid()),
			Gid: uint32(syscall.Getgid()),
		},
	}
	return cmd.CombinedOutput()
}

func (m *Manager) genID() string {
	return fmt.Sprintf("apk-%d", time.Now().UnixNano())
}

// ── Accessors ──────────────────────────────────────

// Progress returns a copy of the operation's progress lines.
func (op *Operation) ProgressSnapshot() []ProgressLine {
	op.mu.RLock()
	defer op.mu.RUnlock()
	out := make([]ProgressLine, len(op.Progress))
	copy(out, op.Progress)
	return out
}

// LastProgress returns the most recent progress line, or empty.
func (op *Operation) LastProgress() string {
	op.mu.RLock()
	defer op.mu.RUnlock()
	if len(op.Progress) == 0 {
		return ""
	}
	return op.Progress[len(op.Progress)-1].Line
}

// ── Validation ─────────────────────────────────────

func validatePackageName(name string) error {
	if name == "" {
		return fmt.Errorf("apk: package name is empty")
	}
	for _, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' || c == '+' {
			continue
		}
		return fmt.Errorf("apk: invalid package name: %q", name)
	}
	return nil
}
