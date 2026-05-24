package configtx

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/alpine-webadmin/alpine-webadmin/pkg/log"
)

// Status represents the lifecycle stage of a transaction.
type Status string

const (
	StatusPending    Status = "pending"
	StatusStaged     Status = "staged"
	StatusValidated  Status = "validated"
	StatusCommitted  Status = "committed"
	StatusConfirmed  Status = "confirmed"
	StatusRolledBack Status = "rolled_back"
	StatusFailed     Status = "failed"
)

// TargetConfig describes a managed configuration file.
type TargetConfig struct {
	Name       string
	Path       string
	Validate   func(path string) error
	IsNetwork  bool
	NeedReload bool
	Service    string // OpenRC service name for reload
}

// Transaction represents an atomic config change with rollback support.
type Transaction struct {
	ID         string
	Target     *TargetConfig
	BackupDir  string
	BackupPath string // timestamped backup of original
	StagedPath string // proposed new config written here first
	Status     Status
	CreatedAt  time.Time
	Error      string
	mu         sync.Mutex
}

// Engine manages config transactions, backups, validation, and rollback.
type Engine struct {
	backupDir    string
	transactions map[string]*Transaction
	mu           sync.RWMutex
	netSafety    *NetworkSafety
	persist      *PersistenceManager
	logger       *log.Logger
}

// NewEngine creates a transaction engine.
// backupDir should be on persistent storage for diskless; falls back to /tmp.
func NewEngine(backupDir string, logger *log.Logger) *Engine {
	if backupDir == "" {
		backupDir = "/var/backups/webadmin"
	}
	_ = os.MkdirAll(backupDir, 0750)
	return &Engine{
		backupDir:    backupDir,
		transactions: make(map[string]*Transaction),
		netSafety:    NewNetworkSafety(logger),
		persist:      NewPersistenceManager(logger),
		logger:       logger,
	}
}

// Begin starts a new transaction. Returns the transaction ID.
func (e *Engine) Begin(target *TargetConfig) (*Transaction, error) {
	txid, err := randomID()
	if err != nil {
		return nil, fmt.Errorf("configtx: generate txid: %w", err)
	}

	backupDir := filepath.Join(e.backupDir, target.Name)
	if err := os.MkdirAll(backupDir, 0750); err != nil {
		return nil, fmt.Errorf("configtx: mkdir backup: %w", err)
	}

	ts := time.Now().UTC().Format("20060102_150405")
	backupPath := filepath.Join(backupDir, fmt.Sprintf("%s.bak", ts))
	stagedPath := filepath.Join(backupDir, fmt.Sprintf("%s.staged", txid))

	// Backup the current config if it exists
	if _, err := os.Stat(target.Path); err == nil {
		if err := copyFile(target.Path, backupPath); err != nil {
			return nil, fmt.Errorf("configtx: backup failed: %w", err)
		}
	}

	tx := &Transaction{
		ID:         txid,
		Target:     target,
		BackupDir:  backupDir,
		BackupPath: backupPath,
		StagedPath: stagedPath,
		Status:     StatusPending,
		CreatedAt:  time.Now(),
	}

	e.mu.Lock()
	e.transactions[txid] = tx
	e.mu.Unlock()

	e.logger.Info("tx begin", map[string]interface{}{
		"txid": txid, "target": target.Name, "path": target.Path,
	})
	return tx, nil
}

// Stage writes proposed config content to the staging file.
func (tx *Transaction) Stage(content []byte) error {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	if tx.Status != StatusPending {
		return fmt.Errorf("configtx: cannot stage from status %s", tx.Status)
	}

	if err := os.WriteFile(tx.StagedPath, content, 0640); err != nil {
		tx.Status = StatusFailed
		return fmt.Errorf("configtx: stage write failed: %w", err)
	}

	tx.Status = StatusStaged
	return nil
}

// Validate runs the target's validation function against the staged config.
func (tx *Transaction) Validate() error {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	if tx.Status != StatusStaged {
		return fmt.Errorf("configtx: cannot validate from status %s", tx.Status)
	}

	if tx.Target.Validate != nil {
		if err := tx.Target.Validate(tx.StagedPath); err != nil {
			tx.Status = StatusFailed
			tx.Error = err.Error()
			return fmt.Errorf("configtx: validation failed: %w", err)
		}
	}

	tx.Status = StatusValidated
	return nil
}

// Commit moves the staged config to the live path.
func (e *Engine) Commit(txid string) error {
	tx, ok := e.Get(txid)
	if !ok {
		return fmt.Errorf("configtx: transaction %s not found", txid)
	}

	tx.mu.Lock()
	defer tx.mu.Unlock()

	if tx.Status != StatusValidated {
		return fmt.Errorf("configtx: cannot commit from status %s", tx.Status)
	}

	// For network configs, use the network safety path
	if tx.Target.IsNetwork {
		tx.Status = StatusCommitted
		return e.netSafety.StageCommit(tx.Target, tx.StagedPath, tx.BackupPath, func() {
			tx.mu.Lock()
			tx.Status = StatusConfirmed
			tx.mu.Unlock()
			e.logger.Info("tx confirmed", map[string]interface{}{"txid": txid})
			_ = e.Reload(tx.Target)
			_ = e.persist.Persist(tx.Target)
		})
	}

	// Atomic rename: staged → live
	if err := os.Rename(tx.StagedPath, tx.Target.Path); err != nil {
		tx.Status = StatusFailed
		return fmt.Errorf("configtx: commit rename failed: %w", err)
	}

	tx.Status = StatusCommitted
	e.logger.Info("tx committed", map[string]interface{}{"txid": txid, "target": tx.Target.Name})

	// Reload service if needed
	if tx.Target.NeedReload && tx.Target.Service != "" {
		if err := e.Reload(tx.Target); err != nil {
			e.logger.Warn("tx reload failed", map[string]interface{}{"txid": txid, "error": err.Error()})
		}
	}

	// Persist (lbu commit for diskless)
	if err := e.persist.Persist(tx.Target); err != nil {
		e.logger.Warn("tx persist failed", map[string]interface{}{"txid": txid, "error": err.Error()})
	}

	tx.Status = StatusConfirmed
	return nil
}

// Rollback restores the original config from backup.
func (e *Engine) Rollback(txid string) error {
	tx, ok := e.Get(txid)
	if !ok {
		return fmt.Errorf("configtx: transaction %s not found", txid)
	}

	tx.mu.Lock()
	defer tx.mu.Unlock()

	e.logger.Info("tx rollback", map[string]interface{}{"txid": txid, "target": tx.Target.Name})

	// If we have a backup, restore it
	if _, err := os.Stat(tx.BackupPath); err == nil {
		if err := copyFile(tx.BackupPath, tx.Target.Path); err != nil {
			return fmt.Errorf("configtx: rollback copy failed: %w", err)
		}
	}

	// Clean up staged file if it still exists
	_ = os.Remove(tx.StagedPath)

	tx.Status = StatusRolledBack
	return nil
}

// Confirm explicitly confirms a network change, stopping the auto-rollback timer.
func (e *Engine) Confirm(txid string) error {
	return e.netSafety.Confirm(txid)
}

// Get returns a transaction by ID.
func (e *Engine) Get(txid string) (*Transaction, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	tx, ok := e.transactions[txid]
	return tx, ok
}

// List returns all known transactions.
func (e *Engine) List() []*Transaction {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]*Transaction, 0, len(e.transactions))
	for _, tx := range e.transactions {
		out = append(out, tx)
	}
	return out
}

// Cleanup removes transactions older than ttl and stale backups beyond retention.
func (e *Engine) Cleanup(ttl time.Duration, retainBackups int) {
	e.mu.Lock()
	defer e.mu.Unlock()

	cutoff := time.Now().Add(-ttl)
	for id, tx := range e.transactions {
		if tx.CreatedAt.Before(cutoff) {
			_ = os.Remove(tx.StagedPath)
			delete(e.transactions, id)
		}
	}

	// Prune old backups per target, keeping the N most recent
	targets, _ := os.ReadDir(e.backupDir)
	for _, t := range targets {
		if !t.IsDir() {
			continue
		}
		backups, _ := os.ReadDir(filepath.Join(e.backupDir, t.Name()))
		if len(backups) > retainBackups {
			// Sort by name (timestamp format sorts lexicographically)
			for i := 0; i < len(backups)-retainBackups; i++ {
				_ = os.Remove(filepath.Join(e.backupDir, t.Name(), backups[i].Name()))
			}
		}
	}
}

// Reload triggers an OpenRC service reload without restart.
func (e *Engine) Reload(target *TargetConfig) error {
	if target.Service == "" {
		return nil
	}
	// Use rc-service to reload the service
	return runCmd("/sbin/rc-service", target.Service, "reload")
}

// ── Helpers ────────────────────────────────────────

func randomID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
