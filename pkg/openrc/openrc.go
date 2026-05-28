package openrc

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/alpine-webadmin/alpine-webadmin/pkg/log"
)

// ServiceState represents the current run state of a service.
type ServiceState string

const (
	StateStarted  ServiceState = "started"
	StateStopped  ServiceState = "stopped"
	StateCrashed  ServiceState = "crashed"
	StateInBoot   ServiceState = "inboot"
	StateStarting ServiceState = "starting"
	StateStopping ServiceState = "stopping"
	StateUnknown  ServiceState = "unknown"
)

// Service holds metadata and runtime state for an OpenRC service.
type Service struct {
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	State       ServiceState `json:"state"`
	Runlevel    string       `json:"runlevel,omitempty"`
	PID         int          `json:"pid,omitempty"`
	Enabled     bool         `json:"enabled"`
	InBoot      bool         `json:"in_boot"`
}

// Dependency holds service dependency information.
type Dependency struct {
	Name        string `json:"name"`
	Type        string `json:"type"` // need, use, after, before, provide
	Description string `json:"description,omitempty"`
}

// Manager provides structured OpenRC service management.
type Manager struct {
	rcService string // absolute path
	rcStatus  string
	rcUpdate  string
	logger    *log.Logger
	timeout   time.Duration
}

// NewManager creates an OpenRC manager with absolute binary paths.
// It tries /sbin first (standard Alpine), then /bin (some distros),
// then falls back to PATH lookup.
func NewManager(logger *log.Logger) *Manager {
	find := func(name string) string {
		candidates := []string{
			"/sbin/" + name,
			"/bin/" + name,
			"/usr/sbin/" + name,
			"/usr/bin/" + name,
		}
		for _, p := range candidates {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
		return "/sbin/" + name // last resort; will fail obviously
	}
	return &Manager{
		rcService: find("rc-service"),
		rcStatus:  find("rc-status"),
		rcUpdate:  find("rc-update"),
		logger:    logger,
		timeout:   30 * time.Second,
	}
}

// SetTimeout adjusts the default execution timeout.
func (m *Manager) SetTimeout(d time.Duration) {
	m.timeout = d
}

// run executes an OpenRC command with a bounded timeout and no shell.
func (m *Manager) run(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), m.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, m.rcService, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid: uint32(syscall.Getuid()),
			Gid: uint32(syscall.Getgid()),
		},
	}
	return cmd.CombinedOutput()
}

// runWithBinary executes a specific binary (for rc-status, rc-update).
func (m *Manager) runWithBinary(bin string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), m.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid: uint32(syscall.Getuid()),
			Gid: uint32(syscall.Getgid()),
		},
	}
	return cmd.CombinedOutput()
}

// List returns all services with their current state and runlevel.
func (m *Manager) List() ([]Service, error) {
	out, err := m.runWithBinary(m.rcStatus, "-s")
	if err != nil {
		return nil, fmt.Errorf("openrc: rc-status failed: %w", err)
	}
	return m.parseServiceList(string(out)), nil
}

// Status returns the detailed status of a single service.
func (m *Manager) Status(name string) (*Service, error) {
	if err := validateServiceName(name); err != nil {
		return nil, err
	}
	out, err := m.run(name, "status")
	// rc-service returns exit 3 for stopped services — that's normal
	status := StateStopped
	pid := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 3 {
			status = StateStopped
		} else {
			return nil, fmt.Errorf("openrc: status query failed: %w: %s", err, string(out))
		}
	} else {
		status = StateStarted
		fmt.Sscanf(string(out), "%*s started; pid=%d", &pid)
	}

	// Determine runlevel
	runlevel := ""
	if rlOut, err := m.runWithBinary(m.rcStatus, "-r", name); err == nil {
		runlevel = strings.TrimSpace(string(rlOut))
	}

	// Check if enabled
	enabled := false
	if enOut, err := m.runWithBinary(m.rcUpdate, "show", name); err == nil {
		enabled = strings.Contains(string(enOut), name)
	}

	return &Service{
		Name:     name,
		State:    status,
		Runlevel: runlevel,
		PID:      pid,
		Enabled:  enabled,
	}, nil
}

// Start starts a service.
func (m *Manager) Start(name string) error {
	if err := validateServiceName(name); err != nil {
		return err
	}
	m.audit("service_start", name)
	out, err := m.run(name, "start")
	if err != nil {
		return fmt.Errorf("openrc: start failed: %w: %s", err, string(out))
	}
	return nil
}

// Stop stops a service.
func (m *Manager) Stop(name string) error {
	if err := validateServiceName(name); err != nil {
		return err
	}
	m.audit("service_stop", name)
	out, err := m.run(name, "stop")
	if err != nil {
		return fmt.Errorf("openrc: stop failed: %w: %s", err, string(out))
	}
	return nil
}

// Restart restarts a service.
func (m *Manager) Restart(name string) error {
	if err := validateServiceName(name); err != nil {
		return err
	}
	m.audit("service_restart", name)
	out, err := m.run(name, "restart")
	if err != nil {
		return fmt.Errorf("openrc: restart failed: %w: %s", err, string(out))
	}
	return nil
}

// Enable adds a service to the default runlevel.
func (m *Manager) Enable(name string) error {
	if err := validateServiceName(name); err != nil {
		return err
	}
	m.audit("service_enable", name)
	out, err := m.runWithBinary(m.rcUpdate, "add", name, "default")
	if err != nil {
		return fmt.Errorf("openrc: enable failed: %w: %s", err, string(out))
	}
	return nil
}

// Disable removes a service from all runlevels.
func (m *Manager) Disable(name string) error {
	if err := validateServiceName(name); err != nil {
		return err
	}
	m.audit("service_disable", name)
	out, err := m.runWithBinary(m.rcUpdate, "del", name)
	if err != nil {
		return fmt.Errorf("openrc: disable failed: %w: %s", err, string(out))
	}
	return nil
}

// Dependencies returns the dependency tree for a service by parsing
// its init script and rc-update output.
func (m *Manager) Dependencies(name string) ([]Dependency, error) {
	if err := validateServiceName(name); err != nil {
		return nil, err
	}

	// Parse the init script for depend() function
	scriptPath := "/etc/init.d/" + name
	out, err := m.runWithBinary("/bin/cat", scriptPath)
	if err != nil {
		return nil, fmt.Errorf("openrc: cannot read init script: %w", err)
	}
	return m.parseDependencies(string(out)), nil
}

// DetectFailures returns services that are in a failed/crashed state.
func (m *Manager) DetectFailures() ([]Service, error) {
	all, err := m.List()
	if err != nil {
		return nil, err
	}
	var failed []Service
	for _, svc := range all {
		if svc.State == StateCrashed || svc.State == StateUnknown {
			failed = append(failed, svc)
			continue
		}
		// Double-check with rc-service status for edge cases
		if svc.State == StateStarted {
			if st, _ := m.Status(svc.Name); st != nil && st.State != StateStarted {
				failed = append(failed, *st)
			}
		}
	}
	return failed, nil
}

// ── Parsers ────────────────────────────────────────

func (m *Manager) parseServiceList(output string) []Service {
	var services []Service
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := fields[0]
		stateStr := fields[1]
		runlevel := ""
		if len(fields) > 2 {
			runlevel = fields[2]
		}
		state := parseState(stateStr)
		services = append(services, Service{
			Name:     name,
			State:    state,
			Runlevel: runlevel,
		})
	}
	return services
}

func parseState(s string) ServiceState {
	switch strings.ToLower(s) {
	case "started":
		return StateStarted
	case "stopped":
		return StateStopped
	case "crashed":
		return StateCrashed
	case "starting":
		return StateStarting
	case "stopping":
		return StateStopping
	case "inboot":
		return StateInBoot
	default:
		return StateUnknown
	}
}

func (m *Manager) parseDependencies(script string) []Dependency {
	var deps []Dependency
	inDepend := false
	for _, line := range strings.Split(script, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "depend() {") {
			inDepend = true
			continue
		}
		if inDepend && line == "}" {
			break
		}
		if !inDepend {
			continue
		}
		// Match: need foo, use bar, after baz, before qux, provide svc
		for _, kw := range []string{"need", "use", "after", "before", "provide"} {
			if strings.HasPrefix(line, kw+" ") || strings.HasPrefix(line, kw+"\t") {
				rest := strings.TrimSpace(line[len(kw):])
				for _, d := range strings.Fields(rest) {
					deps = append(deps, Dependency{
						Name: d,
						Type: kw,
					})
				}
				break
			}
		}
	}
	return deps
}

// ── Helpers ────────────────────────────────────────

func (m *Manager) audit(action, name string) {
	if m.logger != nil {
		m.logger.Info("openrc audit", map[string]interface{}{
			"action":  action,
			"service": name,
		})
	}
}

func validateServiceName(name string) error {
	if name == "" {
		return fmt.Errorf("openrc: service name is empty")
	}
	if strings.ContainsAny(name, "/;|&`$\n\r") {
		return fmt.Errorf("openrc: invalid service name")
	}
	return nil
}
