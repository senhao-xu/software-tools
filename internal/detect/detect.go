// Package detect implements Step 0: probing existing components, interactive
// overwrite confirmation and cleanup.
package detect

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	xexec "xsh/internal/exec"
	"xsh/internal/log"
)

// State describes which k8s / container components are already present on the
// host. Each field is independently probed; any probe failure is treated as
// "not present" so the type never carries an error.
type State struct {
	DockerActive     bool
	ContainerdActive bool
	KubeletActive    bool
	HasDockerCmd     bool
	HasKubectl       bool
	HasKubeadm       bool
	HasKubelet       bool
	AdminConf        bool
	KubeletConf      bool
}

// Detect probes the current host state. Any probe failure is treated as
// "not installed". No error is returned.
func Detect(_ context.Context) State {
	return State{
		DockerActive:     isActive("docker"),
		ContainerdActive: isActive("containerd"),
		KubeletActive:    isActive("kubelet"),
		HasDockerCmd:     hasCmd("docker"),
		HasKubectl:       hasCmd("kubectl"),
		HasKubeadm:       hasCmd("kubeadm"),
		HasKubelet:       hasCmd("kubelet"),
		AdminConf:        fileExists("/etc/kubernetes/admin.conf"),
		KubeletConf:      fileExists("/etc/kubernetes/kubelet.conf"),
	}
}

// HasAny reports whether any component was detected. When false the caller
// should skip the overwrite confirmation.
func (s State) HasAny() bool {
	return s.DockerActive || s.ContainerdActive || s.KubeletActive ||
		s.HasDockerCmd || s.HasKubectl || s.HasKubeadm || s.HasKubelet ||
		s.AdminConf || s.KubeletConf
}

// Report returns a multi-line description of every detected component. Empty
// string when nothing was detected.
func (s State) Report() string {
	var lines []string
	if s.DockerActive {
		lines = append(lines, "  - docker (active)")
	}
	if s.ContainerdActive {
		lines = append(lines, "  - containerd (active)")
	}
	if s.KubeletActive {
		lines = append(lines, "  - kubelet (active)")
	}
	if s.HasDockerCmd && !s.DockerActive {
		lines = append(lines, "  - docker (binary)")
	}
	if s.HasKubectl {
		lines = append(lines, "  - kubectl (binary)")
	}
	if s.HasKubeadm {
		lines = append(lines, "  - kubeadm (binary)")
	}
	if s.HasKubelet && !s.KubeletActive {
		lines = append(lines, "  - kubelet (binary)")
	}
	if s.AdminConf {
		lines = append(lines, "  - /etc/kubernetes/admin.conf (kubeadm initialized)")
	}
	if s.KubeletConf {
		lines = append(lines, "  - /etc/kubernetes/kubelet.conf")
	}
	return strings.Join(lines, "\n")
}

// Confirm decides whether to proceed with installation.
//   - state.HasAny() == false: returns (true, nil) immediately.
//   - yes == true: prints the report and an "-y given, overwriting" note,
//     returns (true, nil).
//   - otherwise: prints the report and prompts interactively. Up to 3 invalid
//     inputs default to cancel.
func Confirm(state State, yes bool) (bool, error) {
	if !state.HasAny() {
		return true, nil
	}

	fmt.Fprintln(os.Stderr, "Detected existing components:")
	fmt.Fprintln(os.Stderr, state.Report())

	if yes {
		log.Info("-y given, overwriting")
		return true, nil
	}

	reader := bufio.NewReader(os.Stdin)
	for attempt := 0; attempt < 3; attempt++ {
		fmt.Fprint(os.Stderr, "Overwrite existing components? [O/c]: ")
		line, err := reader.ReadString('\n')
		if err != nil {
			// EOF / closed stdin: default to cancel.
			return false, nil
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "", "o":
			return true, nil
		case "c":
			return false, nil
		}
		fmt.Fprintln(os.Stderr, "  please answer O (overwrite) or c (cancel)")
	}
	log.Warn("no valid answer after 3 attempts, defaulting to cancel")
	return false, nil
}

// Cleanup removes existing kubernetes / container components so a fresh
// install can proceed. Individual command failures are logged as WARN but do
// not abort the sequence; only catastrophic errors propagate.
func Cleanup(_ context.Context) error {
	log.Info("cleanup: resetting kubeadm state")
	warnIf(xexec.Run("kubeadm", "reset", "-f",
		"--cri-socket=unix:///var/run/containerd/containerd.sock"))

	log.Info("cleanup: stopping services")
	for _, unit := range []string{"docker", "cri-docker", "containerd", "kubelet"} {
		warnIf(xexec.Run("systemctl", "stop", unit))
	}

	log.Info("cleanup: purging packages")
	purgeArgs := []string{
		"purge", "-y", "--allow-change-held-packages",
		"docker-ce", "docker-ce-cli",
		"docker-buildx-plugin", "docker-compose-plugin",
		"containerd.io", "cri-dockerd",
		"kubeadm", "kubelet", "kubectl",
		"kubernetes-cni", "cri-tools",
	}
	warnIf(xexec.Run("apt-get", purgeArgs...))

	log.Info("cleanup: removing directories")
	paths := []string{
		"/etc/docker", "/etc/containerd", "/etc/kubernetes", "/etc/cni",
		"/var/lib/docker", "/var/lib/containerd", "/var/lib/kubelet", "/var/lib/etcd",
		"/etc/apt/sources.list.d/docker.list",
		"/etc/apt/sources.list.d/kubernetes.list",
		"/etc/apt/keyrings/docker.gpg",
		"/etc/apt/keyrings/kubernetes.gpg",
	}
	for _, p := range paths {
		if err := os.RemoveAll(p); err != nil {
			log.Warn("remove %s: %v", p, err)
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		kubeDir := filepath.Join(home, ".kube")
		if err := os.RemoveAll(kubeDir); err != nil {
			log.Warn("remove %s: %v", kubeDir, err)
		}
	}

	log.Info("cleanup: apt-get autoremove")
	warnIf(xexec.Run("apt-get", "autoremove", "-y"))

	log.Info("cleanup done")
	return nil
}

func isActive(unit string) bool {
	return xexec.Probe("systemctl", "is-active", "--quiet", unit)
}

func hasCmd(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func warnIf(err error) {
	if err != nil {
		log.Warn("%v", err)
	}
}
