package configtx

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/alpine-webadmin/alpine-webadmin/pkg/log"
)

// PersistenceManager handles Alpine diskless (lbu) persistence.
type PersistenceManager struct {
	logger    *log.Logger
	diskless  *bool
	lbuFound  *bool
}

// NewPersistenceManager creates a persistence manager.
func NewPersistenceManager(logger *log.Logger) *PersistenceManager {
	return &PersistenceManager{logger: logger}
}

// IsDiskless detects whether the system is running in Alpine diskless mode.
// It checks for lbu presence and tmpfs mounts on critical paths.
func (pm *PersistenceManager) IsDiskless() bool {
	if pm.diskless != nil {
		return *pm.diskless
	}
	// Check if / is mounted as tmpfs or squashfs (diskless indicators)
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		v := false
		pm.diskless = &v
		return false
	}
	root := false
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[1] == "/" {
			fsType := fields[2]
			if fsType == "tmpfs" || fsType == "squashfs" || fsType == "overlay" {
				root = true
			}
			break
		}
	}
	pm.diskless = &root
	return root
}

// HasLBU returns true if the `lbu` command is available.
func (pm *PersistenceManager) HasLBU() bool {
	if pm.lbuFound != nil {
		return *pm.lbuFound
	}
	_, err := exec.LookPath("lbu")
	v := err == nil
	pm.lbuFound = &v
	return v
}

// Persist attempts to make a config change durable.
// For diskless systems with lbu, it calls `lbu commit`.
// For disk-based systems, the change is already on disk.
func (pm *PersistenceManager) Persist(target *TargetConfig) error {
	if !pm.IsDiskless() {
		pm.logger.Debug("persist: disk-based, no lbu needed", map[string]interface{}{
			"target": target.Name,
		})
		return nil
	}
	if !pm.HasLBU() {
		return fmt.Errorf("configtx: diskless system without lbu")
	}

	pm.logger.Info("lbu commit", map[string]interface{}{"target": target.Name})
	out, err := exec.Command("lbu", "commit", "-d").CombinedOutput()
	if err != nil {
		return fmt.Errorf("configtx: lbu commit failed: %w: %s", err, string(out))
	}
	return nil
}

// PendingChanges lists files that have been modified since the last lbu commit.
// Requires lbu status support (some Alpine versions).
func (pm *PersistenceManager) PendingChanges() ([]string, error) {
	if !pm.HasLBU() {
		return nil, fmt.Errorf("configtx: lbu not available")
	}
	out, err := exec.Command("lbu", "status", "-av").CombinedOutput()
	if err != nil {
		// lbu status may fail on some versions; return empty
		return nil, nil
	}
	var files []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && strings.HasPrefix(line, "/") {
			files = append(files, line)
		}
	}
	return files, nil
}
