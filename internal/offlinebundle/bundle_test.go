package offlinebundle

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeOptions(t *testing.T) {
	got := normalizeOptions(Options{})
	if got.Runtime != "containerd" {
		t.Fatalf("Runtime = %q, want containerd", got.Runtime)
	}
	if got.Version != "v1.35.0" {
		t.Fatalf("Version = %q, want v1.35.0", got.Version)
	}
	if got.OutputDir != defaultOutputDir {
		t.Fatalf("OutputDir = %q, want %q", got.OutputDir, defaultOutputDir)
	}
	if got.ImageTool != defaultImageTool {
		t.Fatalf("ImageTool = %q, want %q", got.ImageTool, defaultImageTool)
	}
}

func TestMinorVersion(t *testing.T) {
	tests := map[string]string{
		"v1.35.0": "v1.35",
		"v1.35":   "v1.35",
		"1.35.1":  "1.35",
		"":        "",
	}
	for in, want := range tests {
		if got := minorVersion(in); got != want {
			t.Fatalf("minorVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestK8sPackagesIncludeOfflineDependencies(t *testing.T) {
	want := map[string]bool{
		"kubeadm":        true,
		"kubelet":        true,
		"kubectl":        true,
		"cri-tools":      true,
		"kubernetes-cni": true,
	}
	for _, pkg := range k8sPackages {
		delete(want, pkg)
	}
	for pkg := range want {
		t.Fatalf("k8sPackages missing %q", pkg)
	}
}

func TestPrepareFailsBeforeDownloadsWhenDockerMissing(t *testing.T) {
	var calls []string
	runner := &recordingRunner{}
	deps := dependencies{
		Runner: runner,
		LookPath: func(name string) (string, error) {
			calls = append(calls, "look:"+name)
			if name == "docker" {
				return "", errors.New("not found")
			}
			return "/usr/bin/" + name, nil
		},
		DetectHost: func() (hostInfo, error) {
			calls = append(calls, "detect")
			return hostInfo{}, nil
		},
		EnsureDocker: func(context.Context) error {
			calls = append(calls, "ensureDocker")
			return nil
		},
		EnsureK8s: func(context.Context, string, string) error {
			calls = append(calls, "ensureK8s")
			return nil
		},
		Download: func(string, string) error {
			calls = append(calls, "download")
			return nil
		},
		ListImages: func(Options, commandRunner) ([]string, error) {
			calls = append(calls, "listImages")
			return []string{"registry.k8s.io/pause:3.10"}, nil
		},
		CreateArchive: func(string, string) error {
			calls = append(calls, "createArchive")
			return nil
		},
	}
	outputDir := filepath.Join(t.TempDir(), "bundle")

	_, err := prepare(context.Background(), Options{OutputDir: outputDir}, deps)
	if err == nil {
		t.Fatalf("prepare() error = nil, want missing docker error")
	}
	wantErr := `missing required command "docker" for k8s bundle image export; install Docker first or run xsh docker -y`
	if !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("prepare() error = %q, want substring %q", err.Error(), wantErr)
	}
	if len(runner.runs) > 0 || len(runner.outputs) > 0 {
		t.Fatalf("runner calls = runs %v outputs %v, want none", runner.runs, runner.outputs)
	}
	wantCalls := []string{"look:apt-get", "look:dpkg", "look:bash", "look:curl", "look:gpg", "look:docker"}
	if strings.Join(calls, ",") != strings.Join(wantCalls, ",") {
		t.Fatalf("calls = %v, want only preflight calls %v", calls, wantCalls)
	}
	if _, statErr := os.Stat(outputDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("output dir stat error = %v, want not exist", statErr)
	}
}

func TestPrepareRunsOriginalPathWhenDependenciesPresent(t *testing.T) {
	var calls []string
	runner := &recordingRunner{}
	outputDir := filepath.Join(t.TempDir(), "bundle")
	deps := dependencies{
		Runner: runner,
		LookPath: func(name string) (string, error) {
			calls = append(calls, "look:"+name)
			return "/usr/bin/" + name, nil
		},
		DetectHost: func() (hostInfo, error) {
			calls = append(calls, "detect")
			return hostInfo{Distro: "debian", Codename: "bookworm", Arch: "amd64"}, nil
		},
		EnsureDocker: func(context.Context) error {
			calls = append(calls, "ensureDocker")
			return nil
		},
		EnsureK8s: func(_ context.Context, mirror, minor string) error {
			calls = append(calls, "ensureK8s:"+mirror+":"+minor)
			return nil
		},
		Download: func(url, dst string) error {
			calls = append(calls, "download:"+filepath.Base(dst))
			return nil
		},
		ListImages: func(Options, commandRunner) ([]string, error) {
			calls = append(calls, "listImages")
			return []string{"registry.k8s.io/pause:3.10"}, nil
		},
		CreateArchive: func(sourceDir, archivePath string) error {
			calls = append(calls, "createArchive")
			return nil
		},
	}

	result, err := prepare(context.Background(), Options{OutputDir: outputDir}, deps)
	if err != nil {
		t.Fatalf("prepare() unexpected error: %v", err)
	}
	wantAssetsDir, err := filepath.Abs(outputDir)
	if err != nil {
		t.Fatalf("abs output dir: %v", err)
	}
	if result.AssetsDir != wantAssetsDir {
		t.Fatalf("AssetsDir = %q, want %q", result.AssetsDir, wantAssetsDir)
	}
	if result.ArchivePath != wantAssetsDir+".tar.gz" {
		t.Fatalf("ArchivePath = %q, want %q", result.ArchivePath, wantAssetsDir+".tar.gz")
	}

	wantCalls := []string{
		"look:apt-get",
		"look:dpkg",
		"look:bash",
		"look:curl",
		"look:gpg",
		"look:docker",
		"detect",
		"ensureDocker",
		"ensureK8s::v1.35",
		"download:kube-flannel.yml",
		"download:components.yaml",
		"listImages",
		"createArchive",
	}
	for _, want := range wantCalls {
		if !containsString(calls, want) {
			t.Fatalf("calls = %v, missing %q", calls, want)
		}
	}
	for _, want := range []string{
		"apt-get -o Dir::Cache::archives=",
		"docker pull registry.k8s.io/pause:3.10",
		"docker save -o ",
	} {
		if !containsRunPrefix(runner.runs, want) {
			t.Fatalf("runner runs = %v, missing prefix %q", runner.runs, want)
		}
	}
}

func TestCreateTarGz(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bundle")
	writeFile(t, dir, "deb/kubernetes/kubeadm_1.35.deb")
	writeFile(t, dir, "kube-flannel.yml")
	archive := filepath.Join(t.TempDir(), "bundle.tar.gz")

	if err := createTarGz(dir, archive); err != nil {
		t.Fatalf("createTarGz() error = %v", err)
	}

	names := readTarNames(t, archive)
	want := map[string]bool{
		"bundle/":                                true,
		"bundle/deb/":                            true,
		"bundle/deb/kubernetes/":                 true,
		"bundle/deb/kubernetes/kubeadm_1.35.deb": true,
		"bundle/kube-flannel.yml":                true,
	}
	if len(names) != len(want) {
		t.Fatalf("archive names = %v", names)
	}
	for _, name := range names {
		if !want[name] {
			t.Fatalf("unexpected archive entry %q in %v", name, names)
		}
	}
}

type recordingRunner struct {
	runs    []string
	outputs []string
}

func (r *recordingRunner) Run(name string, args ...string) error {
	r.runs = append(r.runs, formatTestCmd(name, args))
	return nil
}

func (r *recordingRunner) RunOutput(name string, args ...string) (string, error) {
	r.outputs = append(r.outputs, formatTestCmd(name, args))
	return "", nil
}

func formatTestCmd(name string, args []string) string {
	if len(args) == 0 {
		return name
	}
	return name + " " + strings.Join(args, " ")
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsRunPrefix(values []string, want string) bool {
	for _, value := range values {
		if strings.HasPrefix(value, want) {
			return true
		}
	}
	return false
}

func writeFile(t *testing.T, root, rel string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("content"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func readTarNames(t *testing.T, archive string) []string {
	t.Helper()
	f, err := os.Open(archive)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var names []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read tar: %v", err)
		}
		names = append(names, h.Name)
	}
	return names
}
