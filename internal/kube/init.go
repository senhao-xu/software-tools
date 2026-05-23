// Package kube — Step 4: kubeadm init for the master node.
//
// Init runs the full master-side init pipeline:
//
//  1. set hostname (hostnamectl) if it differs from opts.Hostname
//  2. probe the outbound IP (UDP dial trick, no packet actually sent) and
//     ensure /etc/hosts maps <hostname> -> that IP
//  3. preload images — offline: ctr/docker import every tar under
//     <AssetsDir>/images; online: `kubeadm config images pull`
//  4. `kubeadm init` with cri-socket / cidrs / advertise / mirror options
//  5. copy /etc/kubernetes/admin.conf -> $HOME/.kube/config (+ SUDO_USER copy)
//  6. remove control-plane taint for single-node usability
//  7. generate the join command via `kubeadm token create --print-join-command`
//     and persist it to /var/cache/xsh/join-command.sh
//
// ResetInit is the inverse for the rollback chain: best-effort `kubeadm reset`
// + remove the kubeconfig copies + remove the join-command file. It deliberately
// does *not* touch /etc/kubernetes (detect.Cleanup owns that) or /etc/hosts
// (which the user may legitimately want to keep).

package kube

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	xexec "xsh/internal/exec"
	"xsh/internal/log"
)

// InitOptions controls kubeadm init behaviour.
type InitOptions struct {
	// Runtime is "containerd" or "docker"; selects the cri-socket.
	Runtime string

	// Version is the kubernetes version passed to kubeadm
	// (e.g. "v1.35.0").
	Version string

	// ServiceCIDR is the --service-cidr value (default "10.96.0.0/12").
	ServiceCIDR string

	// PodCIDR is the --pod-network-cidr value (default "10.244.0.0/16",
	// flannel-locked).
	PodCIDR string

	// Hostname is the node hostname (default "master").
	Hostname string

	// Advertise is the --apiserver-advertise-address. When empty, Init
	// probes the outbound IP via a UDP dial trick.
	Advertise string

	// Mirror is "" for official registries, "cn" to route to
	// registry.aliyuncs.com/google_containers via --image-repository.
	Mirror string

	// AssetsDir, when non-empty and contains <AssetsDir>/images with at
	// least one .tar, switches image preload to offline (ctr/docker import).
	AssetsDir string
}

const (
	containerdSocket = "unix:///var/run/containerd/containerd.sock"
	criDockerdSocket = "unix:///var/run/cri-dockerd.sock"

	cnImageRepository = "registry.aliyuncs.com/google_containers"

	adminConfPath     = "/etc/kubernetes/admin.conf"
	xshCacheDir       = "/var/cache/xsh"
	joinCmdPath       = "/var/cache/xsh/join-command.sh"
	etcHostsPath      = "/etc/hosts"
	controlPlaneTaint = "node-role.kubernetes.io/control-plane-"
)

// Init runs the kubeadm-init pipeline described in the package comment.
// Each step's failure is fatal and bubbles up so the CLI can chain ResetInit
// followed by the rest of the rollback chain.
func Init(ctx context.Context, opts InitOptions) error {
	log.Info("kubeinit: starting kubeadm init")

	if err := ensureHostname(opts.Hostname); err != nil {
		return err
	}

	ip, err := resolveAdvertiseIP(opts.Advertise)
	if err != nil {
		return err
	}
	log.Info("kubeinit: advertise IP = %s", ip)

	if err := ensureEtcHosts(opts.Hostname, ip); err != nil {
		return err
	}

	if err := preloadImages(opts); err != nil {
		return err
	}

	if err := runKubeadmInit(opts, ip); err != nil {
		return err
	}

	if err := copyKubeconfig(); err != nil {
		return err
	}

	removeControlPlaneTaint()

	if err := generateJoinCommand(opts); err != nil {
		return err
	}

	log.Info("kubeinit: kubeadm init done")
	return nil
}

// ResetInit best-effort reverses Init. All steps are tolerant of "missing"
// states because rollback is invoked after partial-success situations too.
func ResetInit(_ context.Context, opts InitOptions) error {
	log.Info("kubeinit: rollback")

	sock := criSocket(opts.Runtime)
	if err := xexec.Run("kubeadm", "reset", "-f", "--cri-socket="+sock); err != nil {
		log.Warn("kubeadm reset: %v", err)
	}

	for _, p := range kubeconfigCopyPaths() {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			log.Warn("remove %s: %v", p, err)
		}
	}

	if err := os.Remove(joinCmdPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Warn("remove %s: %v", joinCmdPath, err)
	}

	log.Info("kubeinit: rollback done")
	return nil
}

// --- step 1: hostname ------------------------------------------------------

// ensureHostname runs hostnamectl set-hostname when the current hostname does
// not match the desired one. os.Hostname() can return an FQDN ("master.lan");
// we compare against just the leading label so a mere domain-suffix mismatch
// doesn't trigger a needless rename.
func ensureHostname(want string) error {
	if want == "" {
		return nil
	}
	got, err := os.Hostname()
	if err != nil {
		log.Warn("os.Hostname(): %v (will set anyway)", err)
	} else {
		short := strings.SplitN(got, ".", 2)[0]
		if short == want {
			log.Info("kubeinit: hostname already %s", want)
			return nil
		}
	}
	log.Info("kubeinit: set hostname %s", want)
	if err := xexec.Run("hostnamectl", "set-hostname", want); err != nil {
		return fmt.Errorf("hostnamectl set-hostname %s: %w", want, err)
	}
	return nil
}

// --- step 2: advertise IP + /etc/hosts ------------------------------------

// resolveAdvertiseIP returns the explicit advertise value when set, otherwise
// probes the outbound IPv4 by opening a UDP "connection" to 8.8.8.8:80. UDP
// dial does not send packets, so this works offline as long as the routing
// table has a default route; if it doesn't, the user must pass --advertise.
func resolveAdvertiseIP(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "", fmt.Errorf("probe outbound IP: %w (pass --advertise=<IP> to override)", err)
	}
	defer conn.Close()
	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || addr == nil || addr.IP == nil {
		return "", fmt.Errorf("probe outbound IP: unexpected LocalAddr %v (pass --advertise=<IP> to override)", conn.LocalAddr())
	}
	return addr.IP.String(), nil
}

// ensureEtcHosts upserts a "<ip>\t<hostname>" record in /etc/hosts. We rewrite
// at most one matching line (first match) and leave everything else — comments,
// IPv6, other aliases — intact. The check is on whitespace-separated tokens
// rather than substring so "master2" doesn't match a "master" rule.
func ensureEtcHosts(hostname, ip string) error {
	if hostname == "" || ip == "" {
		return nil
	}
	existing, err := os.ReadFile(etcHostsPath)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("read %s: %w", etcHostsPath, err)
		}
		existing = nil
	}
	lines := strings.Split(string(existing), "\n")
	newLine := ip + "\t" + hostname
	replaced := false
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// fields[0] is the IP, the rest are aliases.
		for _, alias := range fields[1:] {
			if alias == hostname {
				if line == newLine {
					log.Info("kubeinit: /etc/hosts already maps %s -> %s", hostname, ip)
					return nil
				}
				lines[i] = newLine
				replaced = true
				break
			}
		}
		if replaced {
			break
		}
	}
	if !replaced {
		// Preserve trailing newline behaviour: drop a single empty trailing
		// element before appending so we don't end up with a blank line.
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		lines = append(lines, newLine)
	}
	out := strings.Join(lines, "\n") + "\n"
	if bytes.Equal([]byte(out), existing) {
		return nil
	}
	if err := os.WriteFile(etcHostsPath, []byte(out), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", etcHostsPath, err)
	}
	log.Info("kubeinit: updated /etc/hosts (%s -> %s)", hostname, ip)
	return nil
}

// --- step 3: image preload -------------------------------------------------

// preloadImages routes to the offline tar-import or online pull path. Offline
// is selected when AssetsDir is non-empty AND <AssetsDir>/images contains at
// least one .tar; otherwise we fall back to `kubeadm config images pull`.
func preloadImages(opts InitOptions) error {
	if opts.AssetsDir != "" {
		imagesDir := filepath.Join(opts.AssetsDir, "images")
		tars, err := listTars(imagesDir)
		if err != nil {
			return err
		}
		if len(tars) > 0 {
			return importOfflineImages(opts.Runtime, tars)
		}
		log.Warn("kubeinit: no .tar under %s, falling back to online pull", imagesDir)
	}
	return pullOnlineImages(opts)
}

func listTars(dir string) ([]string, error) {
	info, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat %s: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, nil
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*.tar"))
	if err != nil {
		return nil, fmt.Errorf("glob %s: %w", dir, err)
	}
	sort.Strings(matches)
	return matches, nil
}

func importOfflineImages(runtime string, tars []string) error {
	log.Info("kubeinit: importing %d image archive(s) offline", len(tars))
	switch runtime {
	case "docker":
		for _, tar := range tars {
			if err := xexec.Run("docker", "load", "-i", tar); err != nil {
				return fmt.Errorf("docker load %s: %w", tar, err)
			}
		}
	default:
		// containerd path (also covers empty/default)
		for _, tar := range tars {
			if err := xexec.Run("ctr", "-n", "k8s.io", "images", "import", tar); err != nil {
				return fmt.Errorf("ctr images import %s: %w", tar, err)
			}
		}
	}
	return nil
}

func pullOnlineImages(opts InitOptions) error {
	log.Info("kubeinit: pulling images online")
	args := []string{"config", "images", "pull",
		"--kubernetes-version=" + opts.Version,
	}
	if opts.Mirror == "cn" {
		args = append(args, "--image-repository="+cnImageRepository)
	}
	if err := xexec.Run("kubeadm", args...); err != nil {
		return fmt.Errorf("kubeadm config images pull: %w", err)
	}
	return nil
}

// --- step 4: kubeadm init --------------------------------------------------

// runKubeadmInit assembles the kubeadm init invocation. Flag order matches the
// prd to make eyeball comparison against logs easy.
func runKubeadmInit(opts InitOptions, advertise string) error {
	sock := criSocket(opts.Runtime)
	args := []string{
		"init",
		"--kubernetes-version=" + opts.Version,
		"--service-cidr=" + opts.ServiceCIDR,
		"--pod-network-cidr=" + opts.PodCIDR,
		"--cri-socket=" + sock,
	}
	if opts.Mirror == "cn" {
		args = append(args, "--image-repository="+cnImageRepository)
	}
	if advertise != "" {
		args = append(args, "--apiserver-advertise-address="+advertise)
	}
	if err := xexec.Run("kubeadm", args...); err != nil {
		return fmt.Errorf("kubeadm init: %w", err)
	}
	return nil
}

// criSocket maps the runtime kind to its socket path; unknown values fall back
// to containerd's socket so a stray flag value doesn't break the install.
func criSocket(runtime string) string {
	switch runtime {
	case "docker":
		return criDockerdSocket
	default:
		return containerdSocket
	}
}

// --- step 5: kubeconfig copy ----------------------------------------------

// copyKubeconfig installs /etc/kubernetes/admin.conf into the invoking user's
// ~/.kube/config. When running under sudo it ALSO mirrors the file into the
// original (non-root) user's home and chowns it back, so they can run kubectl
// without su -. The sudo-user mirror is best-effort (WARN on failure) — it's
// a UX nicety, not a correctness requirement.
func copyKubeconfig() error {
	src, err := os.ReadFile(adminConfPath)
	if err != nil {
		return fmt.Errorf("read %s (kubeadm init likely failed earlier): %w", adminConfPath, err)
	}

	// Primary copy: current process (root) home dir.
	home, err := homeDir()
	if err != nil {
		return err
	}
	if err := installKubeconfig(home, src, os.Getuid(), os.Getgid()); err != nil {
		return err
	}

	// Best-effort: mirror to SUDO_USER's home so the human user can use kubectl.
	if u := os.Getenv("SUDO_USER"); u != "" && u != "root" {
		uid, gid, sudoHome, ok := sudoUserInfo()
		if ok && sudoHome != "" && sudoHome != home {
			if err := installKubeconfig(sudoHome, src, uid, gid); err != nil {
				log.Warn("install kubeconfig for sudo user %s: %v", u, err)
			}
		}
	}
	return nil
}

// installKubeconfig writes src to <home>/.kube/config (creating .kube as
// needed) and chowns both to (uid,gid).
func installKubeconfig(home string, src []byte, uid, gid int) error {
	dir := filepath.Join(home, ".kube")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, src, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Chown(path, uid, gid); err != nil {
		log.Warn("chown %s to %d:%d: %v", path, uid, gid, err)
	}
	if err := os.Chown(dir, uid, gid); err != nil {
		log.Warn("chown %s to %d:%d: %v", dir, uid, gid, err)
	}
	log.Info("kubeinit: wrote %s", path)
	return nil
}

// homeDir returns the current user's home dir, defaulting to /root if
// $HOME is unset (typical under early systemd contexts).
func homeDir() (string, error) {
	if h := os.Getenv("HOME"); h != "" {
		return h, nil
	}
	if os.Getuid() == 0 {
		return "/root", nil
	}
	return "", fmt.Errorf("HOME is unset and not running as root")
}

// sudoUserInfo extracts (uid, gid, home, ok) from SUDO_UID / SUDO_GID env
// vars plus the assumed /home/<SUDO_USER> directory. Returns ok=false when
// uid/gid can't be parsed.
func sudoUserInfo() (int, int, string, bool) {
	uidStr := os.Getenv("SUDO_UID")
	gidStr := os.Getenv("SUDO_GID")
	user := os.Getenv("SUDO_USER")
	if uidStr == "" || gidStr == "" || user == "" {
		return 0, 0, "", false
	}
	uid, err := strconv.Atoi(uidStr)
	if err != nil {
		return 0, 0, "", false
	}
	gid, err := strconv.Atoi(gidStr)
	if err != nil {
		return 0, 0, "", false
	}
	home := "/home/" + user
	if user == "root" {
		home = "/root"
	}
	return uid, gid, home, true
}

// kubeconfigCopyPaths returns the kubeconfig copies installed by Init, used by
// ResetInit to clean them up. Missing files are fine (best-effort).
func kubeconfigCopyPaths() []string {
	var out []string
	if home, err := homeDir(); err == nil {
		out = append(out, filepath.Join(home, ".kube", "config"))
	}
	if _, _, sudoHome, ok := sudoUserInfo(); ok && sudoHome != "" {
		out = append(out, filepath.Join(sudoHome, ".kube", "config"))
	}
	return out
}

// --- step 6: untaint -------------------------------------------------------

// removeControlPlaneTaint removes the control-plane NoSchedule taint so the
// node will accept pods in single-node clusters. Failure is non-fatal: in k8s
// 1.35+ the taint key could be renamed, and a multi-node deployment doesn't
// want this anyway.
func removeControlPlaneTaint() {
	log.Info("kubeinit: removing control-plane taint (single-node usability)")
	if err := xexec.Run("kubectl", "--kubeconfig="+adminConfPath,
		"taint", "nodes", "--all", controlPlaneTaint); err != nil {
		log.Warn("kubectl taint (safe to ignore on multi-node or renamed-taint k8s): %v", err)
	}
}

// --- step 7: join command --------------------------------------------------

// generateJoinCommand asks kubeadm for the worker join invocation and persists
// it under /var/cache/xsh. The upstream `--print-join-command` output omits
// --cri-socket, so we append it ourselves to keep workers on the same runtime
// as the master (otherwise kubeadm picks containerd by default, breaking
// docker-based clusters).
func generateJoinCommand(opts InitOptions) error {
	out, err := xexec.RunOutput("kubeadm", "token", "create", "--print-join-command")
	if err != nil {
		return fmt.Errorf("kubeadm token create: %w", err)
	}
	cmd := strings.TrimSpace(out)
	if cmd == "" {
		return fmt.Errorf("kubeadm token create returned empty output")
	}
	sock := criSocket(opts.Runtime)
	if !strings.Contains(cmd, "--cri-socket") {
		cmd = cmd + " --cri-socket=" + sock
	}

	if err := os.MkdirAll(xshCacheDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", xshCacheDir, err)
	}
	body := "#!/bin/bash\n" + cmd + "\n"
	if err := os.WriteFile(joinCmdPath, []byte(body), 0o755); err != nil {
		return fmt.Errorf("write %s: %w", joinCmdPath, err)
	}

	log.Info("kubeinit: kubeadm init done; join command (also saved to %s):", joinCmdPath)
	log.Info("  %s", cmd)
	return nil
}
