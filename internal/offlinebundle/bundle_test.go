package offlinebundle

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
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
