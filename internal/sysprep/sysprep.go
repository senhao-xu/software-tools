// Package sysprep implements Step 1: firewall/swap/SELinux/sysctl/ipvs
// preparation for Kubernetes hosts.
//
// All substeps are idempotent: existing-and-correct state is detected and
// short-circuited so re-running Run() on an already-prepared host is a no-op.
// Rollback removes only the files this package wrote (sysctl + modules-load
// drop-ins); it deliberately does NOT undo swapoff, restore fstab edits, or
// re-enable firewalls — those are destructive system-wide changes the user
// likely wants preserved for any subsequent reinstall.
package sysprep

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	xexec "xsh/internal/exec"
	"xsh/internal/log"
)

// Options controls sysprep behaviour.
type Options struct {
	// AssetsDir, when non-empty and contains <AssetsDir>/deb/ipvs/, switches
	// the ipvs tool install to offline dpkg. Empty or missing falls back to
	// online apt-get install.
	AssetsDir string
}

const (
	sysctlPath      = "/etc/sysctl.d/k8s.conf"
	modulesK8sPath  = "/etc/modules-load.d/k8s.conf"
	modulesIPVSPath = "/etc/modules-load.d/ipvs.conf"
	fstabPath       = "/etc/fstab"
	selinuxConfig   = "/etc/selinux/config"
	procSwaps       = "/proc/swaps"
)

// sysctlContent is the kernel-tuning drop-in installed under /etc/sysctl.d.
// Keeping it as a constant lets us compare-before-write for idempotency.
const sysctlContent = `net.bridge.bridge-nf-call-ip6tables = 1
net.bridge.bridge-nf-call-iptables = 1
net.ipv4.ip_forward = 1
`

// ipvsModules lists the modules to load for kube-proxy ipvs mode. ip_conntrack
// is best-effort because newer kernels rename it to nf_conntrack.
var ipvsModules = []string{
	"ip_vs",
	"ip_vs_rr",
	"ip_vs_wrr",
	"ip_vs_sh",
	"ip_conntrack",
}

// Run executes all sysprep substeps in order. Any failure returns immediately;
// the caller is responsible for invoking Rollback.
func Run(_ context.Context, opts Options) error {
	if err := disableFirewall(); err != nil {
		return fmt.Errorf("disable firewall: %w", err)
	}
	if err := disableSELinux(); err != nil {
		return fmt.Errorf("disable selinux: %w", err)
	}
	if err := disableSwap(); err != nil {
		return fmt.Errorf("disable swap: %w", err)
	}
	if err := writeSysctl(); err != nil {
		return fmt.Errorf("write sysctl: %w", err)
	}
	if err := loadKernelModules(); err != nil {
		return fmt.Errorf("load kernel modules: %w", err)
	}
	if err := installIPVSTools(opts); err != nil {
		return fmt.Errorf("install ipvs tools: %w", err)
	}
	log.Info("sysprep: all substeps done")
	return nil
}

// Rollback removes persistent config files written by Run. It is best-effort:
// missing files are ignored. It deliberately keeps swap off, leaves fstab
// commented, does not re-enable firewalls, and does not uninstall ipvs tools
// (those are general-purpose utilities outside the docker/k8s purge scope).
func Rollback(_ context.Context) error {
	log.Info("sysprep: rollback")
	for _, p := range []string{sysctlPath, modulesK8sPath, modulesIPVSPath} {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			log.Warn("sysprep rollback: remove %s: %v", p, err)
		}
	}
	log.Info("sysprep rollback done")
	return nil
}

// --- substep 1: firewall ---------------------------------------------------

func disableFirewall() error {
	log.Info("sysprep: disable firewall ...")
	handled := false

	if _, err := exec.LookPath("ufw"); err == nil {
		handled = true
		if err := xexec.Run("systemctl", "disable", "--now", "ufw"); err != nil {
			log.Warn("ufw systemctl disable: %v", err)
		}
		if err := xexec.Run("ufw", "disable"); err != nil {
			log.Warn("ufw disable: %v", err)
		}
	}

	if _, err := exec.LookPath("firewall-cmd"); err == nil {
		handled = true
		if err := xexec.Run("systemctl", "disable", "--now", "firewalld"); err != nil {
			log.Warn("firewalld systemctl disable: %v", err)
		}
	}

	if !handled {
		log.Info("no firewall service detected, skipping")
	}
	log.Info("sysprep: disable firewall done")
	return nil
}

// --- substep 2: SELinux ----------------------------------------------------

func disableSELinux() error {
	log.Info("sysprep: disable selinux ...")

	if _, err := os.Stat(selinuxConfig); errors.Is(err, fs.ErrNotExist) {
		log.Warn("SELinux config not found (typical on Debian), skipping")
		return nil
	} else if err != nil {
		return fmt.Errorf("stat %s: %w", selinuxConfig, err)
	}

	// setenforce returns non-zero when SELinux is already disabled; downgrade
	// to WARN so we don't abort.
	if err := xexec.Run("setenforce", "0"); err != nil {
		log.Warn("setenforce 0: %v (likely already disabled)", err)
	}

	raw, err := os.ReadFile(selinuxConfig)
	if err != nil {
		return fmt.Errorf("read %s: %w", selinuxConfig, err)
	}
	updated := strings.ReplaceAll(string(raw), "SELINUX=enforcing", "SELINUX=disabled")
	if updated == string(raw) {
		log.Info("sysprep: selinux config already disabled")
	} else if err := os.WriteFile(selinuxConfig, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", selinuxConfig, err)
	}

	log.Info("sysprep: disable selinux done")
	return nil
}

// --- substep 3: swap -------------------------------------------------------

func disableSwap() error {
	log.Info("sysprep: disable swap ...")

	if hasActiveSwap() {
		if err := xexec.Run("swapoff", "-a"); err != nil {
			return fmt.Errorf("swapoff -a: %w", err)
		}
	} else {
		log.Info("no active swap")
	}

	if err := commentFstabSwap(); err != nil {
		return err
	}

	log.Info("sysprep: disable swap done")
	return nil
}

// hasActiveSwap reads /proc/swaps; the file has a header line followed by one
// row per active swap area. No rows (or missing file) means no active swap.
func hasActiveSwap() bool {
	raw, err := os.ReadFile(procSwaps)
	if err != nil {
		return false
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	return len(lines) > 1 && strings.TrimSpace(lines[1]) != ""
}

// commentFstabSwap idempotently prefixes any uncommented swap entry with '#'.
// A fstab entry has whitespace-separated fields where field 3 (1-indexed) is
// the filesystem type. We only touch lines whose 3rd field equals "swap" and
// that aren't already comments.
func commentFstabSwap() error {
	raw, err := os.ReadFile(fstabPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			log.Warn("%s not found, skipping swap comment", fstabPath)
			return nil
		}
		return fmt.Errorf("read %s: %w", fstabPath, err)
	}

	// Preserve trailing newline state of original file.
	hadTrailingNL := strings.HasSuffix(string(raw), "\n")
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")

	changed := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) >= 3 && fields[2] == "swap" {
			lines[i] = "#" + line
			changed = true
		}
	}

	if !changed {
		return nil
	}

	out := strings.Join(lines, "\n")
	if hadTrailingNL {
		out += "\n"
	}
	if err := os.WriteFile(fstabPath, []byte(out), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", fstabPath, err)
	}
	log.Info("sysprep: commented swap entries in %s", fstabPath)
	return nil
}

// --- substep 4: sysctl -----------------------------------------------------

func writeSysctl() error {
	log.Info("sysprep: write sysctl ...")
	if err := writeFileIfChanged(sysctlPath, []byte(sysctlContent), 0o644); err != nil {
		return err
	}
	if err := xexec.Run("sysctl", "--system"); err != nil {
		log.Warn("sysctl --system: %v", err)
	}
	log.Info("sysprep: write sysctl done")
	return nil
}

// --- substep 5: kernel modules ---------------------------------------------

func loadKernelModules() error {
	log.Info("sysprep: load kernel modules ...")

	// br_netfilter is required for k8s; failure is fatal.
	if err := xexec.Run("modprobe", "br_netfilter"); err != nil {
		return fmt.Errorf("modprobe br_netfilter: %w", err)
	}
	log.Info("sysprep: br_netfilter loaded")

	if err := writeFileIfChanged(modulesK8sPath, []byte("br_netfilter\n"), 0o644); err != nil {
		return err
	}

	// IPVS modules: best-effort. ip_conntrack may be renamed to nf_conntrack on
	// newer kernels; either way we don't abort the install.
	for _, m := range ipvsModules {
		if err := xexec.Run("modprobe", m); err != nil {
			log.Warn("modprobe %s: %v", m, err)
		}
	}

	ipvsBody := strings.Join(ipvsModules, "\n") + "\n"
	if err := writeFileIfChanged(modulesIPVSPath, []byte(ipvsBody), 0o644); err != nil {
		return err
	}

	log.Info("sysprep: load kernel modules done")
	return nil
}

// --- substep 6: ipvs tools -------------------------------------------------

func installIPVSTools(opts Options) error {
	log.Info("sysprep: install ipvs tools ...")

	if opts.AssetsDir != "" {
		ipvsDir := filepath.Join(opts.AssetsDir, "deb", "ipvs")
		if info, err := os.Stat(ipvsDir); err == nil && info.IsDir() {
			debs, err := findDebs(ipvsDir)
			if err != nil {
				return fmt.Errorf("scan %s: %w", ipvsDir, err)
			}
			if len(debs) == 0 {
				log.Warn("offline ipvs dir %s has no .deb files, falling back to online apt", ipvsDir)
			} else {
				args := append([]string{"-i"}, debs...)
				if err := xexec.Run("dpkg", args...); err != nil {
					return fmt.Errorf("dpkg -i ipvs debs: %w", err)
				}
				log.Info("sysprep: install ipvs tools done (offline)")
				return nil
			}
		}
	}

	// Online path.
	if err := xexec.Run("apt-get", "update"); err != nil {
		log.Warn("apt-get update: %v", err)
	}
	if err := xexec.Run("apt-get", "install", "-y", "ipset", "ipvsadm"); err != nil {
		return fmt.Errorf("apt-get install ipset ipvsadm: %w", err)
	}
	log.Info("sysprep: install ipvs tools done (online)")
	return nil
}

// findDebs returns a sorted list of .deb paths directly inside dir.
func findDebs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".deb") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out, nil
}

// --- helpers ---------------------------------------------------------------

// writeFileIfChanged writes content to path with perm, but only if the file
// is absent or its contents differ. This keeps Run idempotent and avoids
// surprising mtime churn on reruns.
func writeFileIfChanged(path string, content []byte, perm os.FileMode) error {
	existing, err := os.ReadFile(path)
	if err == nil && string(existing) == string(content) {
		log.Info("sysprep: %s already up to date", path)
		return nil
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := os.WriteFile(path, content, perm); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	log.Info("sysprep: wrote %s", path)
	return nil
}
