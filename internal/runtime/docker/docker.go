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
// The apt repo setup is delegated to internal/aptrepo; cri-dockerd fetch +
// install is delegated to internal/cridockerd. Either way
// /etc/docker/daemon.json is rendered (systemd cgroup driver, json-file
// 100m x 3) and both docker and cri-docker services are enabled.
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

	"xsh/internal/aptrepo"
	"xsh/internal/cridockerd"
	xexec "xsh/internal/exec"
	"xsh/internal/log"
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
	daemonJSONPath = "/etc/docker/daemon.json"
	daemonDir      = "/etc/docker"
)

// Install installs and configures docker + cri-dockerd. Offline path is tried
// first when AssetsDir is given; on missing/empty deb dir we fall back online.
func Install(ctx context.Context, opts Options) error {
	log.Info("runtime/docker: install start")

	installed, err := tryOfflineInstall(opts)
	if err != nil {
		return err
	}
	if !installed {
		if err := onlineInstall(ctx); err != nil {
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

func onlineInstall(ctx context.Context) error {
	if err := aptrepo.EnsureDockerRepo(ctx); err != nil {
		return err
	}
	pkgs := []string{
		"docker-ce", "docker-ce-cli", "containerd.io",
		"docker-buildx-plugin", "docker-compose-plugin", "docker-ce-rootless-extras",
	}
	if err := xexec.Run("apt-get", append([]string{"install", "-y"}, pkgs...)...); err != nil {
		return fmt.Errorf("apt-get install docker: %w", err)
	}

	if err := cridockerd.Install(ctx, cridockerd.DefaultVersion); err != nil {
		return fmt.Errorf("install cri-dockerd: %w", err)
	}
	log.Info("runtime/docker: online install done")
	return nil
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

	body, err := renderDaemonJSON()
	if err != nil {
		return fmt.Errorf("marshal daemon.json: %w", err)
	}

	return writeFileIfChanged(daemonJSONPath, body, 0o644)
}

// renderDaemonJSON marshals the runtime/docker daemon config. Extracted as a
// pure function so unit tests can assert on the bytes without touching disk.
func renderDaemonJSON() ([]byte, error) {
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
		return nil, err
	}
	return append(body, '\n'), nil
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
