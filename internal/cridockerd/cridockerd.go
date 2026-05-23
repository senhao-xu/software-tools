// Package cridockerd downloads and installs the cri-dockerd .deb published by
// Mirantis on GitHub releases. It exists to centralise the (distro, codename)
// → release-artifact mapping that runtime/docker currently hardcodes as
// "0.3.21.3-0.debian-bookworm".
//
// Mirantis does not ship every codename: as of v0.4.3 the matrix is
// {debian-bullseye, debian-bookworm, ubuntu-bionic, ubuntu-focal,
// ubuntu-jammy}. trixie (Debian 13) and noble (Ubuntu 24.04) require
// fallbacks; see artifactCodename.
//
// PR1 only ships the package; runtime/docker keeps its hardcoded constant
// until PR2.
package cridockerd

import (
	"context"
	"fmt"
	"strings"

	xexec "xsh/internal/exec"
	"xsh/internal/log"
	"xsh/internal/osinfo"
)

// DefaultVersion is the cri-dockerd version this binary targets. It matches
// the existing runtime/docker constant so PR2 is a pure call-site swap.
const DefaultVersion = "0.3.21"

// tmpDeb is the on-disk cache path for the downloaded .deb. Re-running an
// install benefits from the cache (xexec.Download is idempotent on non-zero
// size).
const tmpDeb = "/tmp/cri-dockerd.deb"

// artifactCodename maps (distro ID, host codename) → the codename embedded in
// the cri-dockerd release artifact name. Some host codenames have no upstream
// artifact and fall back to the nearest older one:
//
//   - debian trixie (13) → debian-bookworm: Mirantis has not shipped a trixie
//     artifact through v0.4.3; bookworm builds install cleanly on trixie.
//     This matches the existing runtime/docker behaviour from before this PR
//     (the constant was "0.3.21.3-0.debian-bookworm" regardless of host).
//   - ubuntu noble (24.04) → ubuntu-jammy: Mirantis has not shipped a noble
//     artifact through v0.4.3. The jammy .deb installs on noble in practice
//     but carries an ABI-mismatch risk; TODO bump when upstream ships a
//     native artifact (Mirantis/cri-dockerd#... — no concrete issue yet).
var artifactCodename = map[string]map[string]string{
	"debian": {
		"bookworm": "debian-bookworm",
		"trixie":   "debian-bookworm",
	},
	"ubuntu": {
		"jammy": "ubuntu-jammy",
		"noble": "ubuntu-jammy",
	},
}

// BuildURL returns the GitHub releases URL for the cri-dockerd .deb that
// matches (version, distro, codename, arch). It is a pure function so callers
// can unit-test it without touching the network. version is "0.3.21"-style
// (no leading v in the artifact name component, but the tag DOES have v).
// arch is dpkg's --print-architecture value, e.g. "amd64".
//
// Returns an error for unsupported (distro, codename) combinations.
func BuildURL(version, distro, codename, arch string) (string, error) {
	if version == "" {
		return "", fmt.Errorf("cri-dockerd version is empty")
	}
	codenames, ok := artifactCodename[distro]
	if !ok {
		return "", fmt.Errorf("unsupported distro %q for cri-dockerd (supported: debian, ubuntu)", distro)
	}
	artifact, ok := codenames[codename]
	if !ok {
		return "", fmt.Errorf("unsupported %s codename %q for cri-dockerd", distro, codename)
	}
	// Release name carries a "3-0." build-revision suffix and is appended to
	// the version: cri-dockerd_<version>.3-0.<artifact>_<arch>.deb. The .3-0
	// component is upstream-stable; if Mirantis ever bumps it, callers will
	// need a new BuildURL signature, not a knob here.
	return fmt.Sprintf(
		"https://github.com/Mirantis/cri-dockerd/releases/download/v%s/cri-dockerd_%s.3-0.%s_%s.deb",
		version, version, artifact, arch,
	), nil
}

// Install detects the running OS, builds the cri-dockerd release URL,
// downloads the .deb (cached at /tmp/cri-dockerd.deb), and installs via
// `dpkg -i` with an `apt-get install -f` fallback for any unresolved deps.
// Behaviour mirrors the existing runtime/docker.installCRIDockerd helper.
func Install(_ context.Context, version string) error {
	log.Info("cridockerd: install start (version=%s)", version)

	info, err := osinfo.Detect()
	if err != nil {
		return fmt.Errorf("detect os: %w", err)
	}
	if info.Codename == "" {
		return fmt.Errorf("VERSION_CODENAME missing in /etc/os-release")
	}

	arch, err := xexec.RunOutput("dpkg", "--print-architecture")
	if err != nil {
		return fmt.Errorf("dpkg --print-architecture: %w", err)
	}
	arch = strings.TrimSpace(arch)

	url, err := BuildURL(version, info.ID, info.Codename, arch)
	if err != nil {
		return err
	}

	if err := xexec.Download(url, tmpDeb); err != nil {
		return fmt.Errorf("download cri-dockerd (need offline .deb or network): %w", err)
	}

	if err := xexec.Run("dpkg", "-i", tmpDeb); err != nil {
		// Same dep-fixup dance as the existing runtime/docker.installCRIDockerd:
		// let apt resolve missing deps then retry dpkg once.
		log.Warn("dpkg -i cri-dockerd reported errors, attempting apt-get install -f: %v", err)
		if fixErr := xexec.Run("apt-get", "install", "-f", "-y"); fixErr != nil {
			return fmt.Errorf("apt-get install -f cri-dockerd: %w (after dpkg: %v)", fixErr, err)
		}
		if err2 := xexec.Run("dpkg", "-i", tmpDeb); err2 != nil {
			return fmt.Errorf("dpkg -i cri-dockerd (retry): %w", err2)
		}
	}
	log.Info("cridockerd: install done")
	return nil
}
