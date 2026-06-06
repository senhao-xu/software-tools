// Package uninstall removes Kubernetes components and, only when explicitly
// requested, container runtime packages and data.
package uninstall

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"xsh/internal/cri"
	"xsh/internal/detect"
	xexec "xsh/internal/exec"
	"xsh/internal/log"
)

// RuntimeRemoval controls whether container runtime packages are removed.
type RuntimeRemoval string

const (
	RemoveRuntimeAsk        RuntimeRemoval = "ask"
	RemoveRuntimeNone       RuntimeRemoval = "none"
	RemoveRuntimeDocker     RuntimeRemoval = "docker"
	RemoveRuntimeContainerd RuntimeRemoval = "containerd"
	RemoveRuntimeAll        RuntimeRemoval = "all"
	RemoveRuntimeAuto       RuntimeRemoval = "auto"
)

const (
	containerdSocket = cri.ContainerdSocket
	criDockerdSocket = cri.DockerdSocket
)

var (
	k8sPackages = []string{
		"kubeadm", "kubelet", "kubectl", "kubernetes-cni", "cri-tools",
	}
	k8sPaths = []string{
		"/etc/kubernetes",
		"/etc/cni",
		"/var/lib/kubelet",
		"/var/lib/etcd",
		"/var/cache/xsh/join-command.sh",
		"/etc/apt/sources.list.d/kubernetes.list",
		"/etc/apt/keyrings/kubernetes.gpg",
	}
	dockerPackages = []string{
		"docker-ce",
		"docker-ce-cli",
		"docker-buildx-plugin",
		"docker-compose-plugin",
		"docker-ce-rootless-extras",
		"cri-dockerd",
	}
	dockerOptionalPackages = []string{
		"docker-model-plugin",
	}
	dockerPaths = []string{
		"/etc/docker",
		"/var/lib/docker",
	}
	containerdPackages = []string{
		"containerd.io",
	}
	containerdPaths = []string{
		"/etc/containerd",
		"/var/lib/containerd",
	}
	dockerRepoPaths = []string{
		"/etc/apt/sources.list.d/docker.list",
		"/etc/apt/keyrings/docker.gpg",
	}
)

// Options controls the uninstall flow.
type Options struct {
	Yes           bool
	RemoveRuntime RuntimeRemoval
	CRIRuntime    string
	In            io.Reader
	Out           io.Writer
}

// RuntimeTargets is the resolved runtime removal plan.
type RuntimeTargets struct {
	Docker     bool
	Containerd bool
}

type commandRunner interface {
	Run(name string, args ...string) error
}

type dependencies struct {
	Runner    commandRunner
	RemoveAll func(string) error
	Detect    func(context.Context) detect.State
}

type execRunner struct{}

func (execRunner) Run(name string, args ...string) error {
	return xexec.Run(name, args...)
}

// Run removes Kubernetes and optionally selected container runtimes.
func Run(ctx context.Context, opts Options) error {
	return run(ctx, opts, dependencies{
		Runner:    execRunner{},
		RemoveAll: os.RemoveAll,
		Detect:    detect.Detect,
	})
}

func run(ctx context.Context, opts Options, deps dependencies) error {
	opts = normalizeOptions(opts)
	if err := validateOptions(opts); err != nil {
		return err
	}
	state := deps.Detect(ctx)

	cont, err := confirmKubernetesUninstall(opts)
	if err != nil {
		return err
	}
	if !cont {
		log.Info("k8s uninstall: cancelled by user")
		return nil
	}

	targets, err := resolveRuntimeTargets(opts, state)
	if err != nil {
		return err
	}
	log.Info("k8s uninstall: runtime removal plan: docker=%t containerd=%t",
		targets.Docker, targets.Containerd)

	resetKubeadm(deps.Runner, resetSockets(opts.CRIRuntime, state))
	stopUnits(deps.Runner, "kubelet")
	warnIf(deps.Runner.Run("apt-mark", "unhold", "kubeadm", "kubelet", "kubectl"))

	if err := purgePackages(deps.Runner, k8sPackages); err != nil {
		return fmt.Errorf("purge kubernetes packages: %w", err)
	}
	removePaths(deps.RemoveAll, k8sPaths)

	if targets.Docker {
		if err := uninstallDockerRuntime(deps.Runner, deps.RemoveAll); err != nil {
			return err
		}
	}
	if targets.Containerd {
		if err := uninstallContainerdRuntime(deps.Runner, deps.RemoveAll); err != nil {
			return err
		}
	}

	if shouldRemoveDockerRepo(targets, state) {
		log.Info("k8s uninstall: removing Docker apt repo/keyring because no selected runtime is being kept")
		removePaths(deps.RemoveAll, dockerRepoPaths)
	} else if targets.Docker || targets.Containerd {
		log.Info("k8s uninstall: keeping Docker apt repo/keyring because another Docker-repo runtime appears to remain")
	}

	if shouldRunAutoremove(targets, state) {
		log.Info("k8s uninstall: apt-get autoremove")
		warnIf(deps.Runner.Run("apt-get", "autoremove", "-y"))
	} else {
		log.Info("k8s uninstall: skipping apt-get autoremove because a detected container runtime is being kept")
	}
	log.Info("k8s uninstall: done")
	return nil
}

func normalizeOptions(opts Options) Options {
	if opts.RemoveRuntime == "" {
		opts.RemoveRuntime = RemoveRuntimeAsk
	}
	if opts.CRIRuntime == "" {
		opts.CRIRuntime = "auto"
	}
	if opts.In == nil {
		opts.In = os.Stdin
	}
	if opts.Out == nil {
		opts.Out = os.Stderr
	}
	return opts
}

func validateOptions(opts Options) error {
	switch opts.RemoveRuntime {
	case RemoveRuntimeAsk, RemoveRuntimeNone, RemoveRuntimeDocker,
		RemoveRuntimeContainerd, RemoveRuntimeAll, RemoveRuntimeAuto:
	default:
		return fmt.Errorf("invalid --remove-runtime=%s (must be ask, none, docker, containerd, all, or auto)", opts.RemoveRuntime)
	}
	switch opts.CRIRuntime {
	case "auto", "containerd", "docker":
	default:
		return fmt.Errorf("invalid --cri-runtime=%s (must be auto, containerd, or docker)", opts.CRIRuntime)
	}
	return nil
}

func confirmKubernetesUninstall(opts Options) (bool, error) {
	if opts.Yes {
		log.Info("k8s uninstall: -y given, skipping Kubernetes uninstall confirmation")
		return true, nil
	}
	fmt.Fprintln(opts.Out, "This will uninstall Kubernetes packages and remove /etc/kubernetes, /etc/cni, /var/lib/kubelet, and /var/lib/etcd.")
	return askYesNo(opts.In, opts.Out, "Continue uninstalling Kubernetes? [y/N]: ", false)
}

func resolveRuntimeTargets(opts Options, state detect.State) (RuntimeTargets, error) {
	switch opts.RemoveRuntime {
	case RemoveRuntimeNone:
		return RuntimeTargets{}, nil
	case RemoveRuntimeDocker:
		return RuntimeTargets{Docker: true}, nil
	case RemoveRuntimeContainerd:
		return RuntimeTargets{Containerd: true}, nil
	case RemoveRuntimeAll:
		return RuntimeTargets{Docker: true, Containerd: true}, nil
	case RemoveRuntimeAuto:
		return RuntimeTargets{
			Docker:     state.DockerActive || state.HasDockerCmd,
			Containerd: state.ContainerdActive,
		}, nil
	case RemoveRuntimeAsk:
		if opts.Yes {
			log.Info("k8s uninstall: -y does not remove container runtimes; pass --remove-runtime to opt in")
			return RuntimeTargets{}, nil
		}
		return askRuntimeTargets(opts.In, opts.Out, state)
	default:
		return RuntimeTargets{}, fmt.Errorf("invalid runtime removal mode %q", opts.RemoveRuntime)
	}
}

func askRuntimeTargets(in io.Reader, out io.Writer, state detect.State) (RuntimeTargets, error) {
	if !state.DockerActive && !state.ContainerdActive && !state.HasDockerCmd {
		fmt.Fprintln(out, "No active docker/containerd runtime was detected; keeping container runtime packages.")
		return RuntimeTargets{}, nil
	}
	fmt.Fprintln(out, "Container runtime removal can delete images and container data under /var/lib/docker or /var/lib/containerd.")
	reader := bufio.NewReader(in)
	for attempt := 0; attempt < 3; attempt++ {
		fmt.Fprint(out, "Remove container runtime too? [none/docker/containerd/all]: ")
		line, err := reader.ReadString('\n')
		if err != nil && len(line) == 0 {
			return RuntimeTargets{}, nil
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "", "none", "n", "no":
			return RuntimeTargets{}, nil
		case "docker", "d":
			return RuntimeTargets{Docker: true}, nil
		case "containerd", "c":
			return RuntimeTargets{Containerd: true}, nil
		case "all", "a", "yes", "y":
			return RuntimeTargets{Docker: true, Containerd: true}, nil
		}
		fmt.Fprintln(out, "  please answer none, docker, containerd, or all")
	}
	log.Warn("no valid runtime removal answer after 3 attempts, keeping runtimes")
	return RuntimeTargets{}, nil
}

func askYesNo(in io.Reader, out io.Writer, prompt string, defaultYes bool) (bool, error) {
	reader := bufio.NewReader(in)
	for attempt := 0; attempt < 3; attempt++ {
		fmt.Fprint(out, prompt)
		line, err := reader.ReadString('\n')
		if err != nil && len(line) == 0 {
			return defaultYes, nil
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "":
			return defaultYes, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
		fmt.Fprintln(out, "  please answer y or n")
	}
	log.Warn("no valid answer after 3 attempts, defaulting to no")
	return false, nil
}

func resetSockets(criRuntime string, state detect.State) []string {
	switch criRuntime {
	case "containerd":
		return []string{containerdSocket}
	case "docker":
		return []string{criDockerdSocket}
	}
	var sockets []string
	if state.DockerActive || state.HasDockerCmd {
		sockets = append(sockets, criDockerdSocket)
	}
	if state.ContainerdActive || len(sockets) == 0 {
		sockets = append(sockets, containerdSocket)
	}
	return sockets
}

func resetKubeadm(r commandRunner, sockets []string) {
	for _, sock := range sockets {
		log.Info("k8s uninstall: kubeadm reset with cri-socket=%s", sock)
		warnIf(r.Run("kubeadm", "reset", "-f", "--cri-socket="+sock))
	}
}

func stopUnits(r commandRunner, units ...string) {
	for _, unit := range units {
		log.Info("k8s uninstall: stopping/disabling %s", unit)
		warnIf(r.Run("systemctl", "disable", "--now", unit))
	}
}

func purgePackages(r commandRunner, packages []string) error {
	args := []string{"purge", "-y", "--allow-change-held-packages"}
	args = append(args, packages...)
	log.Info("k8s uninstall: purging packages: %s", strings.Join(packages, ", "))
	return r.Run("apt-get", args...)
}

func removePaths(removeAll func(string) error, paths []string) {
	for _, p := range paths {
		log.Info("k8s uninstall: removing %s", p)
		if err := removeAll(p); err != nil {
			log.Warn("remove %s: %v", p, err)
		}
	}
}

func uninstallDockerRuntime(r commandRunner, removeAll func(string) error) error {
	stopUnits(r, "docker", "cri-docker")
	if err := purgePackages(r, dockerPackages); err != nil {
		return fmt.Errorf("purge docker runtime packages: %w", err)
	}
	log.Info("k8s uninstall: purging optional Docker packages when present: %s", strings.Join(dockerOptionalPackages, ", "))
	warnIf(purgePackages(r, dockerOptionalPackages))
	removePaths(removeAll, dockerPaths)
	return nil
}

func uninstallContainerdRuntime(r commandRunner, removeAll func(string) error) error {
	stopUnits(r, "containerd")
	if err := purgePackages(r, containerdPackages); err != nil {
		return fmt.Errorf("purge containerd packages: %w", err)
	}
	removePaths(removeAll, containerdPaths)
	return nil
}

func shouldRemoveDockerRepo(targets RuntimeTargets, state detect.State) bool {
	if targets.Docker && targets.Containerd {
		return true
	}
	if targets.Docker && !state.ContainerdActive {
		return true
	}
	if targets.Containerd && !state.DockerActive && !state.HasDockerCmd {
		return true
	}
	return false
}

func shouldRunAutoremove(targets RuntimeTargets, state detect.State) bool {
	keepsDocker := !targets.Docker && (state.DockerActive || state.HasDockerCmd)
	keepsContainerd := !targets.Containerd && state.ContainerdActive
	return targets.Containerd && !keepsDocker && !keepsContainerd
}

func warnIf(err error) {
	if err != nil {
		log.Warn("%v", err)
	}
}
