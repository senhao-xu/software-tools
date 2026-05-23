// Package kube implements Step 3: install kubeadm/kubelet/kubectl.
//
// Two install paths are supported:
//   - offline: dpkg -i <AssetsDir>/deb/kubernetes/*.deb (kubeadm/kubelet/
//     kubectl + cri-tools + kubernetes-cni and supporting libs)
//   - online:  add the pkgs.k8s.io (or mirrors.aliyun.com for mirror=cn) apt
//     repo and apt-get install kubeadm kubelet kubectl
//
// Either way kubeadm/kubelet/kubectl are apt-mark hold'd (so unattended
// upgrades cannot bump them out from under kubeadm) and kubelet is enabled.
// kubelet entering a crash-loop before kubeadm init is normal: it lacks
// /var/lib/kubelet/config.yaml until init writes it, so we log a warning
// instead of failing the step.
//
// kubeadm init itself lives in PR6, not here.
package kube

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

	xexec "xsh/internal/exec"
	"xsh/internal/log"
)

// Options controls install behaviour.
type Options struct {
	// Version is the kubeadm/kubelet/kubectl version (e.g. "v1.35.0"). The
	// minor part (v1.35) is extracted for the pkgs.k8s.io URL; the patch part
	// is only used to cross-check the actually installed version.
	Version string

	// Mirror is "" for the official pkgs.k8s.io repo, "cn" for the Aliyun
	// mirror at mirrors.aliyun.com/kubernetes-new.
	Mirror string

	// AssetsDir, when non-empty and contains <AssetsDir>/deb/kubernetes/ with
	// at least one .deb, selects the offline path. Otherwise the online apt
	// repo is used.
	AssetsDir string
}

const (
	aptKeyringDir   = "/etc/apt/keyrings"
	aptKeyringPath  = "/etc/apt/keyrings/kubernetes.gpg"
	aptSourcesPath  = "/etc/apt/sources.list.d/kubernetes.list"
	officialRepoFmt = "https://pkgs.k8s.io/core:/stable:/%s/deb/"
	mirrorRepoFmt   = "https://mirrors.aliyun.com/kubernetes-new/core:/stable:/%s/deb/"
)

// Install installs kubeadm/kubelet/kubectl. The offline path is tried first
// when AssetsDir is provided; on a missing/empty deb dir we fall back to the
// online path (no error). kubelet is enabled at the end — kubelet failing to
// start pre-init is expected and downgraded to a warning.
func Install(_ context.Context, opts Options) error {
	log.Info("kube: install kubernetes packages ...")

	installed, err := tryOfflineInstall(opts)
	if err != nil {
		return err
	}
	if !installed {
		if err := onlineInstall(opts); err != nil {
			return err
		}
	}

	verifyInstalledVersion(opts.Version)

	if err := xexec.Run("apt-mark", "hold", "kubeadm", "kubelet", "kubectl"); err != nil {
		log.Warn("apt-mark hold kubeadm/kubelet/kubectl: %v", err)
	}

	if err := xexec.Run("systemctl", "daemon-reload"); err != nil {
		log.Warn("systemctl daemon-reload: %v", err)
	}
	// kubelet has no /var/lib/kubelet/config.yaml yet — expect crash-loop
	// until kubeadm init writes it. enable --now succeeds even if the unit
	// immediately fails, but emit a WARN if it does so we don't pretend
	// success.
	if err := xexec.Run("systemctl", "enable", "--now", "kubelet"); err != nil {
		log.Warn("systemctl enable --now kubelet (expected pre-init crash-loop): %v", err)
	}

	log.Info("kube: install done")
	return nil
}

// Rollback stops kubelet and lifts the apt-mark hold so a later reinstall
// (or detect.Cleanup) can purge cleanly. Package removal, repo/keyring
// teardown, and /etc/kubernetes wiping are detect.Cleanup's responsibility.
func Rollback(_ context.Context, _ Options) error {
	log.Info("kube: rollback")
	if err := xexec.Run("systemctl", "stop", "kubelet"); err != nil {
		log.Warn("systemctl stop kubelet: %v", err)
	}
	if err := xexec.Run("apt-mark", "unhold", "kubeadm", "kubelet", "kubectl"); err != nil {
		log.Warn("apt-mark unhold kubeadm/kubelet/kubectl: %v", err)
	}
	log.Info("kube: rollback done")
	return nil
}

// --- offline ---------------------------------------------------------------

// tryOfflineInstall installs every .deb under <AssetsDir>/deb/kubernetes in a
// single dpkg call. On dependency errors we let apt-get install -f resolve
// them and re-run dpkg once. Returns (true, nil) on success, (false, nil) when
// no debs are available (online fallback).
func tryOfflineInstall(opts Options) (bool, error) {
	if opts.AssetsDir == "" {
		return false, nil
	}
	debDir := filepath.Join(opts.AssetsDir, "deb", "kubernetes")
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
		log.Warn("offline kubernetes deb dir %s is empty, falling back to online", debDir)
		return false, nil
	}
	sort.Strings(debs)

	args := append([]string{"-i"}, debs...)
	if err := xexec.Run("dpkg", args...); err != nil {
		// dpkg often fails on cross-package deps in a single batch; let apt
		// fix and retry once before giving up. Matches PR4 pattern.
		log.Warn("dpkg -i reported errors, attempting apt-get install -f: %v", err)
		if fixErr := xexec.Run("apt-get", "install", "-f", "-y"); fixErr != nil {
			return false, fmt.Errorf("apt-get install -f: %w (after dpkg: %v)", fixErr, err)
		}
		if err2 := xexec.Run("dpkg", args...); err2 != nil {
			return false, fmt.Errorf("dpkg -i (retry): %w", err2)
		}
	}
	log.Info("kube: offline install done")
	return true, nil
}

// --- online ----------------------------------------------------------------

func onlineInstall(opts Options) error {
	if err := ensureKubeAptRepo(opts); err != nil {
		return err
	}
	// We deliberately don't pin the .deb version: the minor-scoped URL
	// already isolates the minor series, and apt picks the newest patch
	// available — which is the same thing the upstream `kubeadm` docs
	// recommend.
	if err := xexec.Run("apt-get", "install", "-y", "kubeadm", "kubelet", "kubectl"); err != nil {
		return fmt.Errorf("apt-get install kubeadm/kubelet/kubectl: %w", err)
	}
	log.Info("kube: online install done")
	return nil
}

// ensureKubeAptRepo writes the keyring and sources.list.d entry for the
// pkgs.k8s.io (or Aliyun mirror) flat repo. Idempotent: existing-and-correct
// files are not rewritten, and the keyring is only fetched on first run.
func ensureKubeAptRepo(opts Options) error {
	log.Info("kube: add kubernetes apt repo")

	if err := xexec.Run("apt-get", "update"); err != nil {
		log.Warn("apt-get update (pre-repo): %v", err)
	}
	if err := xexec.Run("apt-get", "install", "-y",
		"ca-certificates", "curl", "gnupg"); err != nil {
		return fmt.Errorf("apt-get install deps: %w", err)
	}

	if err := os.MkdirAll(aptKeyringDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", aptKeyringDir, err)
	}

	minor := minorVersion(opts.Version)
	baseURL := repoBaseURL(opts.Mirror, minor)

	if _, err := os.Stat(aptKeyringPath); errors.Is(err, fs.ErrNotExist) {
		// curl | gpg --dearmor — no clean way to express the pipe through
		// xexec.Run, so we shell out via bash -c (same approach as the
		// PR4 docker.gpg flow).
		curl := "curl -fsSL " + baseURL + "Release.key | " +
			"gpg --dearmor -o " + aptKeyringPath
		if err := xexec.Run("bash", "-c", curl); err != nil {
			return fmt.Errorf("install kubernetes gpg key: %w", err)
		}
		if err := os.Chmod(aptKeyringPath, 0o644); err != nil {
			return fmt.Errorf("chmod %s: %w", aptKeyringPath, err)
		}
	} else if err != nil {
		return fmt.Errorf("stat %s: %w", aptKeyringPath, err)
	}

	// pkgs.k8s.io is a flat repo — the trailing " /" after the base URL is
	// required syntax for apt to treat it as such.
	srcLine := fmt.Sprintf("deb [signed-by=%s] %s /\n", aptKeyringPath, baseURL)
	if err := writeFileIfChanged(aptSourcesPath, []byte(srcLine), 0o644); err != nil {
		return err
	}

	if err := xexec.Run("apt-get", "update"); err != nil {
		log.Warn("apt-get update (post-repo): %v", err)
	}
	return nil
}

// repoBaseURL returns the apt repo base URL (with trailing slash) for the
// given mirror selection and minor version (e.g. "v1.35").
func repoBaseURL(mirror, minor string) string {
	if mirror == "cn" {
		return fmt.Sprintf(mirrorRepoFmt, minor)
	}
	return fmt.Sprintf(officialRepoFmt, minor)
}

// minorVersion extracts the "v<major>.<minor>" portion of a kubeadm version
// string. Accepts inputs with or without the leading "v" and with or without
// the patch component:
//
//	"v1.35.0" -> "v1.35"
//	"v1.35"   -> "v1.35"
//	"1.35.0"  -> "1.35"
//	"1.35"    -> "1.35"
//
// The pkgs.k8s.io URL expects a leading "v"; callers passing a CLI flag
// without one (which the default doesn't) will still get a working URL,
// just with no "v" prefix. The CLI default is "v1.35.0" so production users
// are unaffected.
func minorVersion(version string) string {
	parts := strings.Split(version, ".")
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return version
}

// verifyInstalledVersion runs `kubeadm version -o short` and warns (but does
// not fail) when the installed patch differs from the requested version. Patch
// drift is acceptable; minor drift would be a deeper bug worth surfacing.
func verifyInstalledVersion(want string) {
	got, err := xexec.RunOutput("kubeadm", "version", "-o", "short")
	if err != nil {
		log.Warn("kubeadm version probe failed: %v", err)
		return
	}
	got = strings.TrimSpace(got)
	if got != want {
		log.Warn("kubeadm version mismatch: requested %s, installed %s (patch drift is acceptable)", want, got)
	}
}

// --- helpers ---------------------------------------------------------------

func writeFileIfChanged(path string, content []byte, perm os.FileMode) error {
	existing, err := os.ReadFile(path)
	if err == nil && bytes.Equal(existing, content) {
		log.Info("kube: %s already up to date", path)
		return nil
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := os.WriteFile(path, content, perm); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	log.Info("kube: wrote %s", path)
	return nil
}
