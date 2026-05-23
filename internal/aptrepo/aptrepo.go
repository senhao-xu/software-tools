// Package aptrepo installs apt keyrings and sources.list entries for the
// docker.com and pkgs.k8s.io repositories. It centralises the per-distro
// codename and URL-prefix knowledge that 4 install packages
// (dockerinstall, runtime/docker, runtime/containerd, kube) used to copy
// from each other.
//
// PR1 only ships the package; caller migration is PR2. The public API is
// stable from PR1 so PR2 is a pure call-site swap.
package aptrepo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	xexec "xsh/internal/exec"
	"xsh/internal/log"
	"xsh/internal/osinfo"
)

const (
	keyringDir            = "/etc/apt/keyrings"
	dockerKeyringPath     = "/etc/apt/keyrings/docker.gpg"
	dockerSourcesPath     = "/etc/apt/sources.list.d/docker.list"
	k8sKeyringPath        = "/etc/apt/keyrings/kubernetes.gpg"
	k8sSourcesPath        = "/etc/apt/sources.list.d/kubernetes.list"
	k8sOfficialRepoFmt    = "https://pkgs.k8s.io/core:/stable:/%s/deb/"
	k8sMirrorCNRepoFmt    = "https://mirrors.aliyun.com/kubernetes-new/core:/stable:/%s/deb/"
	dockerLinuxURLPattern = "https://download.docker.com/linux/%s"
)

// distroEntry captures the per-(ID,VERSION_ID) data needed to talk to apt
// repos: the upstream codename to put on the deb source line, and the
// docker.com URL slug ("debian" or "ubuntu").
type distroEntry struct {
	Codename   string
	DockerSlug string
}

// supported is the (distro,version) → codename + docker.com slug map. It is
// the single source of truth for which OS combos this binary can install on,
// mirrored by osinfo.RequireSupported. Add entries here AND there together.
var supported = map[string]map[string]distroEntry{
	"debian": {
		"12": {Codename: "bookworm", DockerSlug: "debian"},
		"13": {Codename: "trixie", DockerSlug: "debian"},
	},
	"ubuntu": {
		"22.04": {Codename: "jammy", DockerSlug: "ubuntu"},
		"24.04": {Codename: "noble", DockerSlug: "ubuntu"},
	},
}

// DistroCodename returns the apt codename for the given OSInfo, falling back
// to info.Codename when /etc/os-release happens to carry a codename we don't
// have in our map (we still prefer the map to guard against typos).
// Returns an error for unsupported (distro,version) combinations.
func DistroCodename(info *osinfo.OSInfo) (string, error) {
	if info == nil {
		return "", fmt.Errorf("os info is nil")
	}
	versions, ok := supported[info.ID]
	if !ok {
		return "", fmt.Errorf("unsupported distro %q (supported: debian, ubuntu)", info.ID)
	}
	entry, ok := versions[info.VersionID]
	if !ok {
		return "", fmt.Errorf("unsupported %s version %q", info.ID, info.VersionID)
	}
	return entry.Codename, nil
}

// dockerURLPrefix returns the docker.com path segment ("debian" or "ubuntu")
// for the given OSInfo.
func dockerURLPrefix(info *osinfo.OSInfo) (string, error) {
	if info == nil {
		return "", fmt.Errorf("os info is nil")
	}
	versions, ok := supported[info.ID]
	if !ok {
		return "", fmt.Errorf("unsupported distro %q (supported: debian, ubuntu)", info.ID)
	}
	entry, ok := versions[info.VersionID]
	if !ok {
		return "", fmt.Errorf("unsupported %s version %q", info.ID, info.VersionID)
	}
	return entry.DockerSlug, nil
}

// DockerSourceLine returns the exact content of /etc/apt/sources.list.d/docker.list
// for the given distro slug ("debian"|"ubuntu"), apt codename, dpkg architecture
// (e.g. "amd64"), and keyring path. Trailing newline included so byte-equality
// idempotence works against on-disk files.
func DockerSourceLine(distroSlug, codename, arch, keyringPath string) string {
	return fmt.Sprintf(
		"deb [arch=%s signed-by=%s] https://download.docker.com/linux/%s %s stable\n",
		arch, keyringPath, distroSlug, codename,
	)
}

// K8sSourceLine returns the content of /etc/apt/sources.list.d/kubernetes.list
// for the pkgs.k8s.io (or Aliyun mirror) flat repo at the given minor (e.g.
// "v1.35"). pkgs.k8s.io is a flat repo so the trailing " /" after the URL is
// required apt syntax.
func K8sSourceLine(mirror, minor, keyringPath string) string {
	return fmt.Sprintf("deb [signed-by=%s] %s /\n", keyringPath, K8sRepoBaseURL(mirror, minor))
}

// K8sRepoBaseURL returns the apt repo base URL (with trailing slash) for the
// requested mirror selection and minor (e.g. "v1.35"). Exposed for PR2 callers
// that need the URL itself (e.g. to fetch Release.key).
func K8sRepoBaseURL(mirror, minor string) string {
	if mirror == "cn" {
		return fmt.Sprintf(k8sMirrorCNRepoFmt, minor)
	}
	return fmt.Sprintf(k8sOfficialRepoFmt, minor)
}

// EnsureDockerRepo installs the apt keyring and sources entry for
// download.docker.com on the host. It detects the running OS, installs
// ca-certificates/curl/gnupg/lsb-release if needed, downloads and dearmors
// the docker.com GPG key (only when the keyring file doesn't already exist),
// writes /etc/apt/sources.list.d/docker.list, and runs `apt-get update`.
// Idempotent: re-running on a fully configured host writes nothing new.
func EnsureDockerRepo(_ context.Context) error {
	log.Info("aptrepo: ensure docker apt repo")

	info, err := osinfo.Detect()
	if err != nil {
		return fmt.Errorf("detect os: %w", err)
	}
	codename, err := DistroCodename(info)
	if err != nil {
		return err
	}
	slug, err := dockerURLPrefix(info)
	if err != nil {
		return err
	}

	if err := installRepoDeps(); err != nil {
		return err
	}

	if err := os.MkdirAll(keyringDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", keyringDir, err)
	}

	if _, err := os.Stat(dockerKeyringPath); errors.Is(err, fs.ErrNotExist) {
		// curl | gpg --dearmor — no clean way to pipe through xexec.Run, so
		// shell out via bash -c (matches the existing helpers we are about
		// to replace in PR2).
		curl := fmt.Sprintf(
			"curl -fsSL "+dockerLinuxURLPattern+"/gpg | gpg --dearmor -o %s",
			slug, dockerKeyringPath,
		)
		if err := xexec.Run("bash", "-c", curl); err != nil {
			return fmt.Errorf("install docker gpg key: %w", err)
		}
		if err := os.Chmod(dockerKeyringPath, 0o644); err != nil {
			return fmt.Errorf("chmod %s: %w", dockerKeyringPath, err)
		}
	} else if err != nil {
		return fmt.Errorf("stat %s: %w", dockerKeyringPath, err)
	}

	arch, err := xexec.RunOutput("dpkg", "--print-architecture")
	if err != nil {
		return fmt.Errorf("dpkg --print-architecture: %w", err)
	}
	arch = strings.TrimSpace(arch)

	srcLine := DockerSourceLine(slug, codename, arch, dockerKeyringPath)
	if err := writeFileIfChanged(dockerSourcesPath, []byte(srcLine), 0o644); err != nil {
		return err
	}

	if err := xexec.Run("apt-get", "update"); err != nil {
		return fmt.Errorf("apt-get update (post-repo): %w", err)
	}
	return nil
}

// EnsureK8sRepo installs the apt keyring and sources entry for pkgs.k8s.io
// (or the Aliyun mirror when mirror=="cn") at the given minor (e.g. "v1.35").
// k8sMinor is the URL-shaped minor — pass "v1.35", not "1.35" or "v1.35.0".
// Idempotent: existing-and-correct keyring/sources files are not rewritten.
func EnsureK8sRepo(_ context.Context, mirror, k8sMinor string) error {
	log.Info("aptrepo: ensure kubernetes apt repo (minor=%s, mirror=%q)", k8sMinor, mirror)

	if err := installRepoDeps(); err != nil {
		return err
	}

	if err := os.MkdirAll(keyringDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", keyringDir, err)
	}

	baseURL := K8sRepoBaseURL(mirror, k8sMinor)

	if _, err := os.Stat(k8sKeyringPath); errors.Is(err, fs.ErrNotExist) {
		curl := "curl -fsSL " + baseURL + "Release.key | " +
			"gpg --dearmor -o " + k8sKeyringPath
		if err := xexec.Run("bash", "-c", curl); err != nil {
			return fmt.Errorf("install kubernetes gpg key: %w", err)
		}
		if err := os.Chmod(k8sKeyringPath, 0o644); err != nil {
			return fmt.Errorf("chmod %s: %w", k8sKeyringPath, err)
		}
	} else if err != nil {
		return fmt.Errorf("stat %s: %w", k8sKeyringPath, err)
	}

	srcLine := K8sSourceLine(mirror, k8sMinor, k8sKeyringPath)
	if err := writeFileIfChanged(k8sSourcesPath, []byte(srcLine), 0o644); err != nil {
		return err
	}

	if err := xexec.Run("apt-get", "update"); err != nil {
		log.Warn("apt-get update (post-repo): %v", err)
	}
	return nil
}

// installRepoDeps installs the apt utilities required to fetch and verify
// remote apt sources (curl + gpg + lsb-release + ca-certificates). Runs
// `apt-get update` first so the initial install can resolve from a stale
// cache; that update may legitimately fail on hosts whose existing sources
// are broken (e.g. EOL distro) and is downgraded to a WARN.
func installRepoDeps() error {
	if err := xexec.Run("apt-get", "update"); err != nil {
		log.Warn("apt-get update (pre-repo): %v", err)
	}
	if err := xexec.Run("apt-get", "install", "-y",
		"ca-certificates", "curl", "gnupg", "lsb-release"); err != nil {
		return fmt.Errorf("apt-get install deps: %w", err)
	}
	return nil
}

func writeFileIfChanged(path string, content []byte, perm os.FileMode) error {
	existing, err := os.ReadFile(path)
	if err == nil && bytes.Equal(existing, content) {
		log.Info("aptrepo: %s already up to date", path)
		return nil
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := os.WriteFile(path, content, perm); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	log.Info("aptrepo: wrote %s", path)
	return nil
}
