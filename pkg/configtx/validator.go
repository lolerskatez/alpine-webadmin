package configtx

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// Validators is a registry of built-in config validation functions.
var Validators = map[string]func(path string) error{
	"sshd":    ValidateSSHD,
	"samba":   ValidateSamba,
	"network": ValidateNetwork,
	"fstab":   ValidateFSTAB,
	"exports": ValidateExports,
	"openrc":  ValidateOpenRC,
}

// ValidateSSHD runs sshd -t against the given config file.
func ValidateSSHD(path string) error {
	cmd := exec.Command("/usr/sbin/sshd", "-t", "-f", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("sshd validation failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// ValidateSamba runs testparm against the given config file.
func ValidateSamba(path string) error {
	cmd := exec.Command("/usr/bin/testparm", "-s", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("samba validation failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// ValidateNetwork performs basic syntax checks on network interface configs.
func ValidateNetwork(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("cannot read network config: %w", err)
	}
	lines := strings.Split(string(data), "\n")
	ifaceRe := regexp.MustCompile(`^iface\s+([a-zA-Z0-9_-]+)`)
	addrRe := regexp.MustCompile(`^\s*address\s+(\S+)`)

	for n, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if m := ifaceRe.FindStringSubmatch(line); m != nil {
			if m[1] == "" {
				return fmt.Errorf("line %d: empty interface name", n+1)
			}
		}
		if m := addrRe.FindStringSubmatch(line); m != nil {
			ip := m[1]
			if net.ParseIP(ip) == nil && !strings.Contains(ip, "/") {
				// Could be a CIDR; try parsing
				_, _, err := net.ParseCIDR(ip)
				if err != nil {
					return fmt.Errorf("line %d: invalid IP address %q", n+1, ip)
				}
			}
		}
	}
	return nil
}

// ValidateFSTAB runs findmnt --verify or basic syntax checks.
func ValidateFSTAB(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("cannot read fstab: %w", err)
	}
	lines := strings.Split(string(data), "\n")
	for n, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			return fmt.Errorf("line %d: fstab needs at least 4 fields", n+1)
		}
		device := fields[0]
		mountpoint := fields[1]
		if !strings.HasPrefix(mountpoint, "/") {
			return fmt.Errorf("line %d: mountpoint must be absolute", n+1)
		}
		if strings.Contains(device, "..") || strings.Contains(mountpoint, "..") {
			return fmt.Errorf("line %d: path traversal detected", n+1)
		}
	}
	// If findmnt exists, use it for deeper validation
	if _, err := exec.LookPath("findmnt"); err == nil {
		cmd := exec.Command("findmnt", "--verify", "--tab-file", path)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("findmnt validation failed: %s", strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// ValidateExports checks exports syntax.
func ValidateExports(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("cannot read exports: %w", err)
	}
	lines := strings.Split(string(data), "\n")
	for n, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Basic: must have at least a path and one client
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return fmt.Errorf("line %d: exports needs path and client", n+1)
		}
		if !strings.HasPrefix(fields[0], "/") {
			return fmt.Errorf("line %d: export path must be absolute", n+1)
		}
		if strings.Contains(fields[0], "..") {
			return fmt.Errorf("line %d: path traversal detected", n+1)
		}
	}
	return nil
}

// ValidateOpenRC checks OpenRC service config syntax.
func ValidateOpenRC(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("cannot read openrc config: %w", err)
	}
	lines := strings.Split(string(data), "\n")
	for n, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Basic: reject shell command injection patterns
		if strings.Contains(line, "`") || strings.Contains(line, "$") {
			return fmt.Errorf("line %d: suspicious shell syntax", n+1)
		}
		// Must be a valid variable assignment or function
		if !strings.Contains(line, "=") && !strings.Contains(line, "() {") {
			return fmt.Errorf("line %d: invalid openrc syntax", n+1)
		}
	}
	return nil
}
