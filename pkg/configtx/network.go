package configtx

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/alpine-webadmin/alpine-webadmin/pkg/log"
)

// NetworkSafety implements the commit-confirm pattern for network configs.
// After applying a network change, an auto-rollback timer starts. If the
// admin does not explicitly confirm within the window (e.g., 120s), the
// original config is restored. This prevents remote lockout.
type NetworkSafety struct {
	confirmCh   map[string]chan struct{}
	mu          sync.Mutex
	logger      *log.Logger
	confirmTime time.Duration
}

// NewNetworkSafety creates a network safety manager.
func NewNetworkSafety(logger *log.Logger) *NetworkSafety {
	return &NetworkSafety{
		confirmCh:   make(map[string]chan struct{}),
		logger:      logger,
		confirmTime: 120 * time.Second,
	}
}

// StageCommit applies a staged network config and starts the confirmation timer.
// If not confirmed before the timer expires, it restores the backup.
func (ns *NetworkSafety) StageCommit(target *TargetConfig, stagedPath, backupPath string, onConfirm func()) error {
	ns.mu.Lock()
	if _, exists := ns.confirmCh[target.Name]; exists {
		ns.mu.Unlock()
		return fmt.Errorf("configtx: network change already pending for %s", target.Name)
	}
	ch := make(chan struct{})
	ns.confirmCh[target.Name] = ch
	ns.mu.Unlock()

	ns.logger.Info("network commit-confirm", map[string]interface{}{
		"target": target.Name,
		"window": ns.confirmTime.Seconds(),
	})

	// Apply the staged config to live path
	if err := os.Rename(stagedPath, target.Path); err != nil {
		ns.cleanup(target.Name)
		return fmt.Errorf("configtx: network apply failed: %w", err)
	}

	// Reload the network service
	if target.Service != "" {
		if err := runCmd("/sbin/rc-service", target.Service, "restart"); err != nil {
			ns.logger.Warn("network service restart failed", map[string]interface{}{
				"target": target.Name, "error": err.Error(),
			})
		}
	}

	// Start auto-rollback goroutine
	go ns.awaitConfirm(target.Name, backupPath, target.Path, target.Service, onConfirm)
	return nil
}

// Confirm stops the auto-rollback timer for the given target.
func (ns *NetworkSafety) Confirm(txid string) error {
	// For now, confirmation is by target name. Could be extended to txid.
	ns.mu.Lock()
	ch, ok := ns.confirmCh[txid]
	ns.mu.Unlock()

	if !ok {
		return fmt.Errorf("configtx: no pending network change for %s", txid)
	}
	close(ch)
	return nil
}

func (ns *NetworkSafety) awaitConfirm(name, backupPath, livePath, serviceName string, onConfirm func()) {
	ns.mu.Lock()
	ch := ns.confirmCh[name]
	ns.mu.Unlock()

	if ch == nil {
		return
	}

	select {
	case <-ch:
		ns.logger.Info("network confirmed", map[string]interface{}{"target": name})
		onConfirm()
	case <-time.After(ns.confirmTime):
		ns.logger.Warn("network auto-rollback", map[string]interface{}{"target": name})
		_ = copyFile(backupPath, livePath)
		// Restart the correct service with restored config
		if serviceName != "" {
			_ = runCmd("/sbin/rc-service", serviceName, "restart")
		}
	}

	ns.cleanup(name)
}

func (ns *NetworkSafety) cleanup(name string) {
	ns.mu.Lock()
	delete(ns.confirmCh, name)
	ns.mu.Unlock()
}

// IsPending returns true if a network change is awaiting confirmation.
func (ns *NetworkSafety) IsPending(name string) bool {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	_, ok := ns.confirmCh[name]
	return ok
}
