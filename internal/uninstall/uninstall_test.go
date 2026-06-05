package uninstall

import (
	"context"
	"errors"
	"strings"
	"testing"

	"xsh/internal/detect"
)

type recordedCommand struct {
	Name string
	Args []string
}

type fakeRunner struct {
	commands []recordedCommand
	fail     map[string]error
}

func (r *fakeRunner) Run(name string, args ...string) error {
	copied := append([]string(nil), args...)
	r.commands = append(r.commands, recordedCommand{Name: name, Args: copied})
	if err, ok := r.fail[commandKey(name, args...)]; ok {
		return err
	}
	return nil
}

type fakeRemove struct {
	paths []string
}

func (r *fakeRemove) RemoveAll(path string) error {
	r.paths = append(r.paths, path)
	return nil
}

func TestRunYesWithDefaultRuntimeKeepsDetectedRuntimes(t *testing.T) {
	runner, remover := &fakeRunner{}, &fakeRemove{}
	err := run(context.Background(), Options{Yes: true}, dependencies{
		Runner:    runner,
		RemoveAll: remover.RemoveAll,
		Detect: func(context.Context) detect.State {
			return detect.State{DockerActive: true, ContainerdActive: true, HasDockerCmd: true}
		},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	for _, pkg := range []string{"kubeadm", "kubelet", "kubectl", "kubernetes-cni", "cri-tools"} {
		if !hasPackage(runner.commands, pkg) {
			t.Fatalf("kubernetes-only uninstall missing package %s", pkg)
		}
	}
	for _, pkg := range []string{"docker-ce", "containerd.io", "cri-dockerd"} {
		if hasPackage(runner.commands, pkg) {
			t.Fatalf("-y default unexpectedly purges runtime package %s", pkg)
		}
	}
	for _, path := range []string{"/var/lib/docker", "/var/lib/containerd", "/home/test/.kube"} {
		if hasPath(remover.paths, path) {
			t.Fatalf("-y default unexpectedly removes %s", path)
		}
	}
	if hasCommand(runner.commands, "apt-get", "autoremove", "-y") {
		t.Fatalf("-y default should not autoremove while detected runtimes are kept")
	}
}

func TestRunInteractiveAskDefaultsToKeepRuntime(t *testing.T) {
	runner, remover := &fakeRunner{}, &fakeRemove{}
	err := run(context.Background(), Options{
		In:  strings.NewReader("y\n\n"),
		Out: &strings.Builder{},
	}, dependencies{
		Runner:    runner,
		RemoveAll: remover.RemoveAll,
		Detect: func(context.Context) detect.State {
			return detect.State{DockerActive: true, HasDockerCmd: true}
		},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if hasPackage(runner.commands, "docker-ce") {
		t.Fatalf("empty runtime answer should keep Docker")
	}
	if hasPath(remover.paths, "/var/lib/docker") {
		t.Fatalf("empty runtime answer should keep Docker data")
	}
}

func TestRunRemoveDockerDoesNotRemoveContainerdWhenItIsKept(t *testing.T) {
	runner, remover := &fakeRunner{}, &fakeRemove{}
	err := run(context.Background(), Options{
		Yes:           true,
		RemoveRuntime: RemoveRuntimeDocker,
	}, dependencies{
		Runner:    runner,
		RemoveAll: remover.RemoveAll,
		Detect: func(context.Context) detect.State {
			return detect.State{DockerActive: true, ContainerdActive: true, HasDockerCmd: true}
		},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	for _, pkg := range []string{"docker-ce", "docker-ce-cli", "cri-dockerd"} {
		if !hasPackage(runner.commands, pkg) {
			t.Fatalf("docker runtime uninstall missing package %s", pkg)
		}
	}
	if hasPackage(runner.commands, "containerd.io") {
		t.Fatalf("docker runtime uninstall should keep containerd.io when containerd is detected")
	}
	if !hasPath(remover.paths, "/var/lib/docker") {
		t.Fatalf("docker runtime uninstall missing /var/lib/docker removal")
	}
	for _, path := range []string{"/var/lib/containerd", "/etc/apt/sources.list.d/docker.list", "/etc/apt/keyrings/docker.gpg"} {
		if hasPath(remover.paths, path) {
			t.Fatalf("docker runtime uninstall should keep %s while containerd remains", path)
		}
	}
	if hasCommand(runner.commands, "apt-get", "autoremove", "-y") {
		t.Fatalf("docker runtime uninstall should not autoremove while containerd is kept")
	}
}

func TestRunRemoveAllRemovesRuntimeDataAndDockerRepo(t *testing.T) {
	runner, remover := &fakeRunner{}, &fakeRemove{}
	err := run(context.Background(), Options{
		Yes:           true,
		RemoveRuntime: RemoveRuntimeAll,
	}, dependencies{
		Runner:    runner,
		RemoveAll: remover.RemoveAll,
		Detect: func(context.Context) detect.State {
			return detect.State{DockerActive: true, ContainerdActive: true, HasDockerCmd: true}
		},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	for _, pkg := range []string{"docker-ce", "cri-dockerd", "containerd.io"} {
		if !hasPackage(runner.commands, pkg) {
			t.Fatalf("remove all missing package %s", pkg)
		}
	}
	for _, path := range []string{
		"/var/lib/docker",
		"/var/lib/containerd",
		"/etc/apt/sources.list.d/docker.list",
		"/etc/apt/keyrings/docker.gpg",
	} {
		if !hasPath(remover.paths, path) {
			t.Fatalf("remove all missing path %s", path)
		}
	}
	if !hasCommand(runner.commands, "apt-get", "autoremove", "-y") {
		t.Fatalf("remove all should run apt-get autoremove")
	}
}

func TestRunRemoveRuntimeAutoRemovesDetectedRuntimes(t *testing.T) {
	runner, remover := &fakeRunner{}, &fakeRemove{}
	err := run(context.Background(), Options{
		Yes:           true,
		RemoveRuntime: RemoveRuntimeAuto,
	}, dependencies{
		Runner:    runner,
		RemoveAll: remover.RemoveAll,
		Detect: func(context.Context) detect.State {
			return detect.State{DockerActive: true, ContainerdActive: true, HasDockerCmd: true}
		},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	for _, pkg := range []string{"docker-ce", "cri-dockerd", "containerd.io"} {
		if !hasPackage(runner.commands, pkg) {
			t.Fatalf("auto runtime uninstall missing detected runtime package %s", pkg)
		}
	}
	for _, path := range []string{"/var/lib/docker", "/var/lib/containerd"} {
		if !hasPath(remover.paths, path) {
			t.Fatalf("auto runtime uninstall missing detected runtime path %s", path)
		}
	}
}

func TestCRIRuntimeSelectsResetSocket(t *testing.T) {
	runner, remover := &fakeRunner{}, &fakeRemove{}
	err := run(context.Background(), Options{
		Yes:           true,
		RemoveRuntime: RemoveRuntimeNone,
		CRIRuntime:    "docker",
	}, dependencies{
		Runner:    runner,
		RemoveAll: remover.RemoveAll,
		Detect:    func(context.Context) detect.State { return detect.State{} },
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if !hasCommand(runner.commands, "kubeadm", "reset", "-f", "--cri-socket="+criDockerdSocket) {
		t.Fatalf("docker cri runtime should reset with cri-dockerd socket")
	}
	if hasCommand(runner.commands, "kubeadm", "reset", "-f", "--cri-socket="+containerdSocket) {
		t.Fatalf("docker cri runtime should not reset with containerd socket")
	}
}

func TestResetFailureWarnsAndPurgeFailureStops(t *testing.T) {
	purgeErr := errors.New("purge failed")
	runner, remover := &fakeRunner{
		fail: map[string]error{
			commandKey("kubeadm", "reset", "-f", "--cri-socket="+containerdSocket):                                    errors.New("reset failed"),
			commandKey("apt-get", append([]string{"purge", "-y", "--allow-change-held-packages"}, k8sPackages...)...): purgeErr,
		},
	}, &fakeRemove{}

	err := run(context.Background(), Options{
		Yes:           true,
		RemoveRuntime: RemoveRuntimeNone,
		CRIRuntime:    "containerd",
	}, dependencies{
		Runner:    runner,
		RemoveAll: remover.RemoveAll,
		Detect:    func(context.Context) detect.State { return detect.State{} },
	})
	if !errors.Is(err, purgeErr) {
		t.Fatalf("run error = %v, want purgeErr", err)
	}
	if !hasCommand(runner.commands, "systemctl", "disable", "--now", "kubelet") {
		t.Fatalf("reset failure should not stop the uninstall flow before kubelet stop")
	}
	if len(remover.paths) != 0 {
		t.Fatalf("fatal purge failure should stop before path removal, got %v", remover.paths)
	}
}

func TestRunNeverRemovesHomeKubeOrHostState(t *testing.T) {
	runner, remover := &fakeRunner{}, &fakeRemove{}
	err := run(context.Background(), Options{
		Yes:           true,
		RemoveRuntime: RemoveRuntimeAll,
	}, dependencies{
		Runner:    runner,
		RemoveAll: remover.RemoveAll,
		Detect: func(context.Context) detect.State {
			return detect.State{DockerActive: true, ContainerdActive: true, HasDockerCmd: true}
		},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	for _, forbidden := range []string{".kube", "/etc/hosts", "/etc/fstab", "firewalld", "selinux"} {
		for _, path := range remover.paths {
			if strings.Contains(strings.ToLower(path), forbidden) {
				t.Fatalf("uninstall should not remove host-managed path %q", path)
			}
		}
	}
}

func hasPackage(commands []recordedCommand, pkg string) bool {
	for _, cmd := range commands {
		if cmd.Name != "apt-get" {
			continue
		}
		for _, arg := range cmd.Args {
			if arg == pkg {
				return true
			}
		}
	}
	return false
}

func hasCommand(commands []recordedCommand, name string, args ...string) bool {
	for _, cmd := range commands {
		if cmd.Name != name || len(cmd.Args) != len(args) {
			continue
		}
		matches := true
		for i := range args {
			if cmd.Args[i] != args[i] {
				matches = false
				break
			}
		}
		if matches {
			return true
		}
	}
	return false
}

func hasPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}

func commandKey(name string, args ...string) string {
	return name + " " + strings.Join(args, " ")
}
