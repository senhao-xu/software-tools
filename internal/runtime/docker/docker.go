// Package dockerrt installs and configures docker + cri-dockerd as the
// container runtime for kubelet. (The package directory is "docker" but the
// package name is "dockerrt" to avoid colliding with the docker.com tooling
// and the more general "docker" symbol space.)
//
// Two install paths are supported:
//   - offline: dpkg -i <AssetsDir>/deb/docker/*.deb  (must contain docker-ce,
//     docker-ce-cli, containerd.io, buildx/compose plugins, rootless-extras,
//     cri-dockerd and supporting libs)
//   - online:  add download.docker.com apt repo + apt-get install + fetch
//     cri-dockerd .deb from GitHub releases
//
// Either way /etc/docker/daemon.json is rendered (systemd cgroup driver,
// json-file 100m x 3) and both docker and cri-docker services are enabled.
package dockerrt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	xexec "xsh/internal/exec"
	"xsh/internal/log"
	"xsh/internal/osinfo"
)

// Options controls install behaviour.
type Options struct {
	// Mirror is accepted for API symmetry with the containerd subpackage but
	// is currently unused: per PRD daemon.json stays vendor-neutral and the
	// kubeadm step routes images via --image-repository instead.
	Mirror string

	// AssetsDir, when non-empty and contains <AssetsDir>/deb/docker/*.deb,
	// selects the offline path. Otherwise the online path is used.
	AssetsDir string
}

const (
	daemonJSONPath    = "/etc/docker/daemon.json"
	daemonDir         = "/etc/docker"
	aptKeyringDir     = "/etc/apt/keyrings"
	aptKeyringPath    = "/etc/apt/keyrings/docker.gpg"
	aptSourcesPath    = "/etc/apt/sources.list.d/docker.list"
	criDockerdVersion = "0.3.21"
	criDockerdRelease = "0.3.21.3-0.debian-bookworm"
	criDockerdTmpDeb  = "/tmp/cri-dockerd.deb"
)

// Install installs and configures docker + cri-dockerd. Offline path is tried
// first when AssetsDir is given; on missing/empty deb dir we fall back online.
func Install(_ context.Context, opts Options) error {
	log.Info("runtime/docker: install start")

	installed, err := tryOfflineInstall(opts)
	if err != nil {
		return err
	}
	if !installed {
		if err := onlineInstall(); err != nil {
			return err
		}
	}

	if err := writeDaemonJSON(); err != nil {
		return fmt.Errorf("write daemon.json: %w", err)
	}

	if err := xexec.Run("systemctl", "daemon-reload"); err != nil {
		log.Warn("systemctl daemon-reload: %v", err)
	}
	if err := xexec.Run("systemctl", "enable", "--now", "docker", "cri-docker"); err != nil {
		return fmt.Errorf("enable docker/cri-docker: %w", err)
	}

	log.Info("runtime/docker: install done")
	return nil
}

// Rollback stops services and removes daemon.json. Packages and the apt repo
// stay (handled by detect.Cleanup); cri-dockerd .deb under /tmp is left in
// place — re-running Install benefits from the cached file.
func Rollback(_ context.Context, _ Options) error {
	log.Info("runtime/docker: rollback")
	if err := xexec.Run("systemctl", "stop", "docker", "cri-docker"); err != nil {
		log.Warn("systemctl stop docker/cri-docker: %v", err)
	}
	if err := os.Remove(daemonJSONPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Warn("remove %s: %v", daemonJSONPath, err)
	}
	log.Info("runtime/docker: rollback done")
	return nil
}

// --- offline ---------------------------------------------------------------

// tryOfflineInstall installs every .deb under <AssetsDir>/deb/docker in one
// dpkg call. On dependency errors we let apt-get install -f resolve them and
// re-run dpkg once. Returns (true, nil) on success, (false, nil) when no debs
// are available (online fallback).
func tryOfflineInstall(opts Options) (bool, error) {
	if opts.AssetsDir == "" {
		return false, nil
	}
	debDir := filepath.Join(opts.AssetsDir, "deb", "docker")
	info, err := os.Stat(debDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("stat %s: %w", debDir, err)
	}
	if !info.IsDir() {
		return false, nil
	}

	entries, err := os.ReadDir(debDir)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", debDir, err)
	}
	var debs []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".deb") {
			debs = append(debs, filepath.Join(debDir, e.Name()))
		}
	}
	if len(debs) == 0 {
		log.Warn("offline docker deb dir %s is empty, falling back to online", debDir)
		return false, nil
	}
	sort.Strings(debs)

	args := append([]string{"-i"}, debs...)
	if err := xexec.Run("dpkg", args...); err != nil {
		// dpkg often fails on cross-package deps in a single batch; let apt
		// fix and retry once before giving up.
		log.Warn("dpkg -i reported errors, attempting apt-get install -f: %v", err)
		if fixErr := xexec.Run("apt-get", "install", "-f", "-y"); fixErr != nil {
			return false, fmt.Errorf("apt-get install -f: %w (after dpkg: %v)", fixErr, err)
		}
		if err2 := xexec.Run("dpkg", args...); err2 != nil {
			return false, fmt.Errorf("dpkg -i (retry): %w", err2)
		}
	}
	log.Info("runtime/docker: offline install done")
	return true, nil
}

// --- online ----------------------------------------------------------------

func onlineInstall() error {
	if err := ensureDockerAptRepo(); err != nil {
		return err
	}
	pkgs := []string{
		"docker-ce", "docker-ce-cli", "containerd.io",
		"docker-buildx-plugin", "docker-compose-plugin", "docker-ce-rootless-extras",
	}
	if err := xexec.Run("apt-get", append([]string{"install", "-y"}, pkgs...)...); err != nil {
		return fmt.Errorf("apt-get install docker: %w", err)
	}

	if err := installCRIDockerd(); err != nil {
		return fmt.Errorf("install cri-dockerd: %w", err)
	}
	log.Info("runtime/docker: online install done")
	return nil
}

// installCRIDockerd fetches the upstream .deb (Debian 13 has no dedicated
// release; bookworm builds work on trixie too) and dpkg-installs it. We use a
// pure-Go HTTP client so the only external runtime dependency is dpkg.
func installCRIDockerd() error {
	arch, err := xexec.RunOutput("dpkg", "--print-architecture")
	if err != nil {
		return fmt.Errorf("dpkg --print-architecture: %w", err)
	}
	arch = strings.TrimSpace(arch)

	url := fmt.Sprintf(
		"https://github.com/Mirantis/cri-dockerd/releases/download/v%s/cri-dockerd_%s_%s.deb",
		criDockerdVersion, criDockerdRelease, arch,
	)
	if err := xexec.Download(url, criDockerdTmpDeb); err != nil {
		return fmt.Errorf("download cri-dockerd (need offline .deb or network): %w", err)
	}
	if err := xexec.Run("dpkg", "-i", criDockerdTmpDeb); err != nil {
		// Same dep-fixup dance as the offline batch install.
		if fixErr := xexec.Run("apt-get", "install", "-f", "-y"); fixErr != nil {
			return fmt.Errorf("apt-get install -f cri-dockerd: %w (after dpkg: %v)", fixErr, err)
		}
		if err2 := xexec.Run("dpkg", "-i", criDockerdTmpDeb); err2 != nil {
			return fmt.Errorf("dpkg -i cri-dockerd (retry): %w", err2)
		}
	}
	return nil
}

// ensureDockerAptRepo mirrors the helper in the containerd subpackage. It is
// duplicated rather than imported across runtimes to keep the two install
// paths independent (a future refactor can lift this into runtime/internal/
// once a third caller appears).
func ensureDockerAptRepo() error {
	log.Info("runtime/docker: add docker apt repo")

	if err := xexec.Run("apt-get", "update"); err != nil {
		log.Warn("apt-get update (pre-repo): %v", err)
	}
	if err := xexec.Run("apt-get", "install", "-y",
		"ca-certificates", "curl", "gnupg", "lsb-release"); err != nil {
		return fmt.Errorf("apt-get install deps: %w", err)
	}

	if err := os.MkdirAll(aptKeyringDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", aptKeyringDir, err)
	}

	if _, err := os.Stat(aptKeyringPath); errors.Is(err, fs.ErrNotExist) {
		curl := "curl -fsSL https://download.docker.com/linux/debian/gpg | " +
			"gpg --dearmor -o " + aptKeyringPath
		if err := xexec.Run("bash", "-c", curl); err != nil {
			return fmt.Errorf("install docker gpg key: %w", err)
		}
		if err := os.Chmod(aptKeyringPath, 0o644); err != nil {
			return fmt.Errorf("chmod %s: %w", aptKeyringPath, err)
		}
	} else if err != nil {
		return fmt.Errorf("stat %s: %w", aptKeyringPath, err)
	}

	codename, err := debianCodename()
	if err != nil {
		return err
	}
	arch, err := xexec.RunOutput("dpkg", "--print-architecture")
	if err != nil {
		return fmt.Errorf("dpkg --print-architecture: %w", err)
	}
	arch = strings.TrimSpace(arch)

	srcLine := fmt.Sprintf(
		"deb [arch=%s signed-by=%s] https://download.docker.com/linux/debian %s stable\n",
		arch, aptKeyringPath, codename,
	)
	if err := writeFileIfChanged(aptSourcesPath, []byte(srcLine), 0o644); err != nil {
		return err
	}

	if err := xexec.Run("apt-get", "update"); err != nil {
		return fmt.Errorf("apt-get update (post-repo): %w", err)
	}
	return nil
}

func debianCodename() (string, error) {
	info, err := osinfo.Detect()
	if err != nil {
		return "", fmt.Errorf("detect os: %w", err)
	}
	if info.Codename == "" {
		return "", fmt.Errorf("VERSION_CODENAME missing in /etc/os-release")
	}
	return info.Codename, nil
}

// --- daemon.json -----------------------------------------------------------

// daemonConfig is the in-memory shape of /etc/docker/daemon.json. encoding/json
// guarantees the output stays valid JSON across edits.
type daemonConfig struct {
	RegistryMirrors []string          `json:"registry-mirrors"`
	LogDriver       string            `json:"log-driver"`
	LogOpts         map[string]string `json:"log-opts"`
	ExecOpts        []string          `json:"exec-opts"`
}

func writeDaemonJSON() error {
	if err := os.MkdirAll(daemonDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", daemonDir, err)
	}

	cfg := daemonConfig{
		RegistryMirrors: []string{},
		LogDriver:       "json-file",
		LogOpts: map[string]string{
			"max-size": "100m",
			"max-file": "3",
		},
		ExecOpts: []string{"native.cgroupdriver=systemd"},
	}
	body, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal daemon.json: %w", err)
	}
	body = append(body, '\n')

	return writeFileIfChanged(daemonJSONPath, body, 0o644)
}

// --- helpers ---------------------------------------------------------------

func writeFileIfChanged(path string, content []byte, perm os.FileMode) error {
	existing, err := os.ReadFile(path)
	if err == nil && bytes.Equal(existing, content) {
		log.Info("runtime/docker: %s already up to date", path)
		return nil
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := os.WriteFile(path, content, perm); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	log.Info("runtime/docker: wrote %s", path)
	return nil
}
