// Package osinfo parses /etc/os-release and enforces supported distros.
package osinfo

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// OSInfo captures the subset of /etc/os-release we care about.
type OSInfo struct {
	ID        string
	VersionID string
	Codename  string
}

// Detect reads /etc/os-release and returns the parsed OSInfo.
func Detect() (*OSInfo, error) {
	const path = "/etc/os-release"
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return parseOSRelease(string(raw)), nil
}

// parseOSRelease parses the content of /etc/os-release into an OSInfo. It is
// extracted as a pure function so unit tests can exercise the parser without
// touching the filesystem.
func parseOSRelease(content string) *OSInfo {
	info := &OSInfo{}
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		val = strings.Trim(val, `"'`)
		switch key {
		case "ID":
			info.ID = val
		case "VERSION_ID":
			info.VersionID = val
		case "VERSION_CODENAME":
			info.Codename = val
		}
	}
	return info
}

// RequireDebian returns an error unless os is Debian 12 or 13.
func RequireDebian(os *OSInfo) error {
	if os == nil {
		return fmt.Errorf("os info is nil")
	}
	if os.ID != "debian" {
		return fmt.Errorf("unsupported distro %q: only Debian 12/13 is supported", os.ID)
	}
	switch os.VersionID {
	case "12", "13":
		return nil
	default:
		return fmt.Errorf("unsupported Debian version %q: only 12 and 13 are supported", os.VersionID)
	}
}
