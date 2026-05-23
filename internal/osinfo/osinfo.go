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
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	defer f.Close()

	info := &OSInfo{}
	scanner := bufio.NewScanner(f)
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
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}
	return info, nil
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
