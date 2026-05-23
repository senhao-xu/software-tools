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

// supportedVersions enumerates the (ID, VERSION_ID) pairs xsh supports. The
// shape mirrors internal/aptrepo's distro map; the two MUST stay in sync —
// every entry here needs a codename + docker.com slug there. ID matching is
// exact: derived distros (linuxmint, raspbian, ...) are rejected even when
// their /etc/os-release sets ID_LIKE=debian|ubuntu (deliberate per PRD).
var supportedVersions = map[string][]string{
	"debian": {"12", "13"},
	"ubuntu": {"22.04", "24.04"},
}

// RequireSupported returns nil iff os is one of the supported (distro,
// version) pairs: Debian 12/13 or Ubuntu 22.04/24.04. Errors describe which
// field is wrong, what the current value is, and the supported set.
func RequireSupported(info *OSInfo) error {
	if info == nil {
		return fmt.Errorf("os info is nil")
	}
	versions, ok := supportedVersions[info.ID]
	if !ok {
		return fmt.Errorf("unsupported distro ID=%q: only %s are supported",
			info.ID, supportedList())
	}
	for _, v := range versions {
		if info.VersionID == v {
			return nil
		}
	}
	return fmt.Errorf("unsupported %s VERSION_ID=%q: supported %s versions are %s",
		info.ID, info.VersionID, info.ID, strings.Join(versions, ", "))
}

// supportedList renders the supported (distro, versions) set for error
// messages, e.g. "debian 12/13, ubuntu 22.04/24.04".
func supportedList() string {
	// Stable order so test assertions on the error substring don't flake.
	parts := []string{
		"debian " + strings.Join(supportedVersions["debian"], "/"),
		"ubuntu " + strings.Join(supportedVersions["ubuntu"], "/"),
	}
	return strings.Join(parts, ", ")
}
