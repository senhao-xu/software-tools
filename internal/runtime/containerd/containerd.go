// Package containerd installs and configures the containerd runtime.
//
// Two install paths are supported:
//   - offline: dpkg -i <AssetsDir>/deb/docker/containerd.io_*.deb
//   - online:  add download.docker.com apt repo + apt-get install containerd.io
//
// Either way the package renders /etc/containerd/config.toml with the
// kubelet-friendly defaults (sandbox image + systemd cgroups) and (when
// mirror=cn) a registry mirror to registry.aliyuncs.com.
package containerd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	xexec "xsh/internal/exec"
	"xsh/internal/log"
	"xsh/internal/osinfo"
)

// Options controls install behaviour.
type Options struct {
	// Mirror, when set to "cn", switches the sandbox image and registry mirror
	// to registry.aliyuncs.com/google_containers. Empty = official.
	Mirror string

	// AssetsDir, when non-empty and contains <AssetsDir>/deb/docker/ with at
	// least one containerd.io_*.deb, selects the offline path. Otherwise the
	// online apt repo is used.
	AssetsDir string
}

const (
	configPath        = "/etc/containerd/config.toml"
	configDir         = "/etc/containerd"
	aptKeyringDir     = "/etc/apt/keyrings"
	aptKeyringPath    = "/etc/apt/keyrings/docker.gpg"
	aptSourcesPath    = "/etc/apt/sources.list.d/docker.list"
	defaultSandbox    = "registry.k8s.io/pause:3.10"
	mirrorSandbox     = "registry.aliyuncs.com/google_containers/pause:3.10"
	mirrorRegistryURL = "https://registry.aliyuncs.com/google_containers"
)

// configTemplate is a minimal config.toml that satisfies kubelet's two hard
// requirements: a reachable sandbox image and SystemdCgroup=true on runc.
// Hardcoding (rather than scraping `containerd config default`) lets us run
// before containerd is even installed.
const configTemplate = `version = 2
[plugins."io.containerd.grpc.v1.cri"]
  sandbox_image = "{{.SandboxImage}}"
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runc]
  runtime_type = "io.containerd.runc.v2"
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runc.options]
  SystemdCgroup = true
{{- if eq .Mirror "cn" }}
[plugins."io.containerd.grpc.v1.cri".registry]
[plugins."io.containerd.grpc.v1.cri".registry.mirrors]
[plugins."io.containerd.grpc.v1.cri".registry.mirrors."registry.k8s.io"]
  endpoint = ["{{.MirrorEndpoint}}"]
{{- end }}
`

// Install installs and configures containerd. Offline path is tried first when
// AssetsDir is given; on missing/empty deb dir we fall back to the online path
// (no error). On success containerd is enabled and started.
func Install(_ context.Context, opts Options) error {
	log.Info("runtime/containerd: install start")

	installed, err := tryOfflineInstall(opts)
	if err != nil {
		return err
	}
	if !installed {
		if err := onlineInstall(); err != nil {
			return err
		}
	}

	if err := writeConfig(opts); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	if err := xexec.Run("systemctl", "daemon-reload"); err != nil {
		log.Warn("systemctl daemon-reload: %v", err)
	}
	if err := xexec.Run("systemctl", "enable", "--now", "containerd"); err != nil {
		return fmt.Errorf("enable containerd: %w", err)
	}

	log.Info("runtime/containerd: install done")
	return nil
}

// Rollback stops containerd and removes the config we wrote. It deliberately
// leaves the containerd.io package and the docker apt repo in place — those
// belong to the detect.Cleanup scope, not the install-step rollback.
func Rollback(_ context.Context, _ Options) error {
	log.Info("runtime/containerd: rollback")
	if err := xexec.Run("systemctl", "stop", "containerd"); err != nil {
		log.Warn("systemctl stop containerd: %v", err)
	}
	if err := os.Remove(configPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Warn("remove %s: %v", configPath, err)
	}
	log.Info("runtime/containerd: rollback done")
	return nil
}

// --- offline ---------------------------------------------------------------

// tryOfflineInstall returns (true, nil) when the offline dpkg path completed
// successfully. (false, nil) means "no offline assets available — fall back
// online". Errors are surfaced as-is.
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

	matches, err := filepath.Glob(filepath.Join(debDir, "containerd.io_*.deb"))
	if err != nil {
		return false, fmt.Errorf("glob containerd.io deb: %w", err)
	}
	if len(matches) == 0 {
		log.Warn("offline containerd deb not found in %s, falling back to online", debDir)
		return false, nil
	}
	sort.Strings(matches)

	args := append([]string{"-i"}, matches...)
	if err := xexec.Run("dpkg", args...); err != nil {
		return false, fmt.Errorf("dpkg -i containerd.io: %w", err)
	}
	log.Info("runtime/containerd: offline install done")
	return true, nil
}

// --- online ----------------------------------------------------------------

func onlineInstall() error {
	if err := ensureDockerAptRepo(); err != nil {
		return err
	}
	if err := xexec.Run("apt-get", "install", "-y", "containerd.io"); err != nil {
		return fmt.Errorf("apt-get install containerd.io: %w", err)
	}
	log.Info("runtime/containerd: online install done")
	return nil
}

// ensureDockerAptRepo sets up download.docker.com as an apt source. Idempotent:
// the keyring and sources file are only rewritten when content differs.
//
// Exposed (lowercase, package-internal) so the docker subpackage can re-use it
// in a future refactor; for now it stays local because the two runtimes ship
// independently and the duplication is small.
func ensureDockerAptRepo() error {
	log.Info("runtime/containerd: add docker apt repo")

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
		// curl | gpg --dearmor — there is no clean way to express the
		// pipe through xexec.Run, so we shell out via bash -c. The gpg
		// step is unavoidable because download.docker.com only publishes
		// ASCII-armored keys.
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

// --- config rendering ------------------------------------------------------

func writeConfig(opts Options) error {
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", configDir, err)
	}

	rendered, err := renderConfigTOML(opts)
	if err != nil {
		return err
	}
	return writeFileIfChanged(configPath, []byte(rendered), 0o644)
}

// renderConfigTOML renders configTemplate against opts and returns the result
// as a string. Extracted as a pure function so unit tests can assert on the
// generated config without touching the filesystem.
func renderConfigTOML(opts Options) (string, error) {
	sandbox := defaultSandbox
	if opts.Mirror == "cn" {
		sandbox = mirrorSandbox
	}

	tpl, err := template.New("containerd-config").Parse(configTemplate)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	data := struct {
		SandboxImage   string
		Mirror         string
		MirrorEndpoint string
	}{
		SandboxImage:   sandbox,
		Mirror:         opts.Mirror,
		MirrorEndpoint: mirrorRegistryURL,
	}
	if err := tpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render template: %w", err)
	}
	return buf.String(), nil
}

// --- helpers ---------------------------------------------------------------

func writeFileIfChanged(path string, content []byte, perm os.FileMode) error {
	existing, err := os.ReadFile(path)
	if err == nil && bytes.Equal(existing, content) {
		log.Info("runtime/containerd: %s already up to date", path)
		return nil
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := os.WriteFile(path, content, perm); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	log.Info("runtime/containerd: wrote %s", path)
	return nil
}
