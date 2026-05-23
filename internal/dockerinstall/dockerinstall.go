// Package dockerinstall installs Docker CE as a standalone, day-to-day
// container engine on Debian/Ubuntu — distinct from internal/runtime/docker,
// which installs docker + cri-dockerd as the k8s runtime.
//
// The flow mirrors docker.senhao.eu.cc:
//   - add the download.docker.com apt repo + gpg keyring (idempotent,
//     delegated to internal/aptrepo)
//   - optionally pin the docker-ce major version via apt-cache madison
//   - render /etc/docker/daemon.json (json-file 100m x 5, systemd cgroup)
//   - apt-get install docker-ce + plugins
//   - systemctl enable --now docker
package dockerinstall

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"

	"xsh/internal/aptrepo"
	xexec "xsh/internal/exec"
	"xsh/internal/log"
)

// Options controls install behaviour.
type Options struct {
	// Major pins the docker-ce major version (e.g. 27 -> install 27.x.x).
	// 0 means "latest" — no version pin is applied.
	Major int

	// Mirror is reserved for future use. download.docker.com is reachable from
	// CN today, so PR9 ignores this field; it stays on the API so callers can
	// be wired without churn.
	Mirror string
}

const (
	daemonJSONPath = "/etc/docker/daemon.json"
	daemonDir      = "/etc/docker"
)

// dockerPkgs are the packages installed by `xsh docker`. docker-model-plugin
// is new in late 2024 releases; on older repos it is missing and we tolerate
// its absence with a WARN rather than failing the whole install.
var dockerPkgs = []string{
	"docker-ce",
	"docker-ce-cli",
	"containerd.io",
	"docker-buildx-plugin",
	"docker-compose-plugin",
	"docker-model-plugin",
}

// Install runs the full standalone docker install. Steps 1-6 must all succeed;
// step 7 (docker --version) is best-effort because the service is already
// enabled by the time we get there.
func Install(ctx context.Context, opts Options) error {
	log.Info("dockerinstall: install start (major=%d)", opts.Major)

	if err := installAptDeps(); err != nil {
		return err
	}
	if err := aptrepo.EnsureDockerRepo(ctx); err != nil {
		return err
	}

	version, err := resolveVersion(opts.Major)
	if err != nil {
		return err
	}

	if err := writeDaemonJSON(); err != nil {
		return fmt.Errorf("write daemon.json: %w", err)
	}

	if err := installPackages(version); err != nil {
		return err
	}

	if err := xexec.Run("systemctl", "enable", "--now", "docker"); err != nil {
		return fmt.Errorf("enable docker: %w", err)
	}

	if out, verr := xexec.RunOutput("docker", "--version"); verr != nil {
		log.Warn("docker --version failed (service is up, treating as non-fatal): %v", verr)
	} else {
		log.Info("dockerinstall: %s", out)
	}

	log.Info("dockerinstall: install done")
	return nil
}

// Rollback is intentionally narrow: stop the service and remove daemon.json.
// Packages, apt repo, gpg keyring are owned by detect.Cleanup and stay put.
func Rollback(_ context.Context, _ Options) error {
	log.Info("dockerinstall: rollback")
	if err := xexec.Run("systemctl", "stop", "docker"); err != nil {
		log.Warn("systemctl stop docker: %v", err)
	}
	if err := os.Remove(daemonJSONPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Warn("remove %s: %v", daemonJSONPath, err)
	}
	log.Info("dockerinstall: rollback done")
	return nil
}

// --- step 1: apt deps ------------------------------------------------------

func installAptDeps() error {
	if err := xexec.Run("apt-get", "update"); err != nil {
		log.Warn("apt-get update (pre-deps): %v", err)
	}
	if err := xexec.Run("apt-get", "install", "-y",
		"ca-certificates", "curl", "gnupg", "lsb-release"); err != nil {
		return fmt.Errorf("apt-get install deps: %w", err)
	}
	return nil
}

// --- step 2: docker apt repo ----------------------------------------------
//
// The download.docker.com keyring and sources.list.d entry are installed by
// aptrepo.EnsureDockerRepo (called from Install). This package no longer
// carries its own copy; aptrepo handles Debian and Ubuntu uniformly.

// --- step 3: version selection --------------------------------------------

// resolveVersion returns the apt version string (epoch-prefixed, e.g.
// "5:27.5.1-1~debian.12~bookworm") to pass to apt-get install, or "" when
// no pin is requested. The major prefix match runs against the version
// component only; the epoch (`<n>:`) is preserved so apt accepts the string.
func resolveVersion(major int) (string, error) {
	if major == 0 {
		return "", nil
	}

	out, err := xexec.RunOutput("apt-cache", "madison", "docker-ce")
	if err != nil {
		return "", fmt.Errorf("apt-cache madison docker-ce: %w", err)
	}

	version := parseMadisonVersion(out, major)
	if version == "" {
		return "", fmt.Errorf("no docker-ce version matching major=%d in apt cache", major)
	}
	log.Info("dockerinstall: selected docker-ce version %s for major=%d", version, major)
	return version, nil
}

// parseMadisonVersion scans the output of `apt-cache madison docker-ce` for
// the first version whose numeric prefix (after any `<epoch>:` strip) matches
// the requested major. Each madison row is `docker-ce | <version> | <repo>`.
// Returns "" when no row matches. Extracted as a pure function so unit tests
// can exercise the parser without invoking apt-cache.
func parseMadisonVersion(output string, major int) string {
	prefix := regexp.MustCompile(fmt.Sprintf(`^%d\.`, major))

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), "|")
		if len(parts) < 2 {
			continue
		}
		full := strings.TrimSpace(parts[1])
		if full == "" {
			continue
		}
		cmp := full
		if idx := strings.Index(cmp, ":"); idx >= 0 {
			cmp = cmp[idx+1:]
		}
		if prefix.MatchString(cmp) {
			return full
		}
	}
	return ""
}

// --- step 4: daemon.json ---------------------------------------------------

// daemonConfig is the on-disk shape of /etc/docker/daemon.json. max-file is 5
// here (vs 3 in internal/runtime/docker) to match the standalone-docker recipe
// at docker.senhao.eu.cc, which keeps a deeper retention window for daily use.
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
			"max-file": "5",
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

// --- step 5: install docker packages --------------------------------------

// installPackages runs apt-get install. When version != "", only docker-ce
// and docker-ce-cli are pinned (the rest let apt resolve dependencies).
// docker-model-plugin is attempted last on its own so an old repo missing
// the package downgrades to a WARN rather than a hard failure.
func installPackages(version string) error {
	primary := make([]string, 0, len(dockerPkgs)-1)
	for _, p := range dockerPkgs {
		if p == "docker-model-plugin" {
			continue
		}
		if version != "" && (p == "docker-ce" || p == "docker-ce-cli") {
			primary = append(primary, p+"="+version)
			continue
		}
		primary = append(primary, p)
	}

	args := append([]string{"install", "-y"}, primary...)
	if err := xexec.Run("apt-get", args...); err != nil {
		return fmt.Errorf("apt-get install docker: %w", err)
	}

	if err := xexec.Run("apt-get", "install", "-y", "docker-model-plugin"); err != nil {
		log.Warn("apt-get install docker-model-plugin (only on newer repos): %v", err)
	}
	return nil
}

// --- helpers ---------------------------------------------------------------

func writeFileIfChanged(path string, content []byte, perm os.FileMode) error {
	existing, err := os.ReadFile(path)
	if err == nil && bytes.Equal(existing, content) {
		log.Info("dockerinstall: %s already up to date", path)
		return nil
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := os.WriteFile(path, content, perm); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	log.Info("dockerinstall: wrote %s", path)
	return nil
}
