package apk

import (
	"strings"
)

// parseList parses `apk list --installed` output.
// Each line: "package-name-1.2.3-r0 installed ..."
func (m *Manager) parseList(output string) []PackageInfo {
	var pkgs []PackageInfo
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// The first field is the full package name including version
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		fullName := fields[0]
		// Strip the version suffix: "nginx-1.24.0-r6" → "nginx"
		name := stripVersion(fullName)
		if name == "" {
			continue
		}
		pkgs = append(pkgs, PackageInfo{
			Name:      name,
			Version:   extractVersion(fullName),
			Installed: true,
		})
	}
	return pkgs
}

// parseSearch parses `apk search <query>` output.
// Each line: "package-name-1.2.3-r0 - description"
func (m *Manager) parseSearch(output string) []PackageInfo {
	var pkgs []PackageInfo
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, desc := "", ""
		if idx := strings.Index(line, " - "); idx > 0 {
			name = stripVersion(line[:idx])
			desc = strings.TrimSpace(line[idx+3:])
		} else {
			name = stripVersion(line)
		}
		if name == "" {
			continue
		}
		pkgs = append(pkgs, PackageInfo{
			Name:        name,
			Description: desc,
		})
	}
	return pkgs
}

// parseInfo parses `apk info <package>` output.
func (m *Manager) parseInfo(name, output string) *PackageInfo {
	info := &PackageInfo{Name: name}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "description:") {
			info.Description = strings.TrimSpace(line[len("description:"):])
		} else if strings.HasPrefix(line, "installed size:") {
			info.Size = strings.TrimSpace(line[len("installed size:"):])
		} else if strings.HasPrefix(line, name+"-") {
			// Version line: "nginx-1.24.0-r0 description:"
			if idx := strings.Index(line, " "); idx > 0 {
				info.Version = extractVersion(line[:idx])
			}
		}
	}
	return info
}

// stripVersion removes the trailing version from a package name like "nginx-1.24.0-r6".
func stripVersion(s string) string {
	// Walk backwards from the end, looking for the first dash that starts the version
	// Format: name-version-release, where version may contain dots and dashes
	// Heuristic: last segment after a dash that looks like a release (contains 'r' + digits)
	lastDash := strings.LastIndex(s, "-")
	if lastDash <= 0 {
		return s
	}
	// Check if the part after the last dash is a release like "r6" or "r0"
	after := s[lastDash+1:]
	if len(after) > 1 && after[0] == 'r' {
		allDigits := true
		for _, c := range after[1:] {
			if c < '0' || c > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			// This is the release segment; strip it and the version before it
			rest := s[:lastDash]
			versionDash := strings.LastIndex(rest, "-")
			if versionDash > 0 {
				return rest[:versionDash]
			}
			return rest
		}
	}
	return s
}

// extractVersion extracts "1.24.0-r6" from "nginx-1.24.0-r6".
func extractVersion(s string) string {
	name := stripVersion(s)
	if name == "" || name == s {
		return ""
	}
	return s[len(name)+1:]
}
