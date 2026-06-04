package assets

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateK8sBundleContainerdMaster(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "deb/ipvs/ipvsadm_1.0.deb")
	writeTestFile(t, dir, "deb/kubernetes/kubeadm_1.35.deb")
	writeTestFile(t, dir, "deb/docker/containerd.io_1.7.deb")
	writeTestFile(t, dir, "images/kube-apiserver.tar")
	writeTestFile(t, dir, "kube-flannel.yml")
	writeTestFile(t, dir, "components.yaml")

	err := ValidateK8sBundle(dir, K8sBundleOptions{
		Runtime:             "containerd",
		IncludeControlPlane: true,
	})
	if err != nil {
		t.Fatalf("ValidateK8sBundle() error = %v", err)
	}
}

func TestValidateK8sBundleJoinDoesNotRequireControlPlaneAssets(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "deb/ipvs/ipvsadm_1.0.deb")
	writeTestFile(t, dir, "deb/kubernetes/kubeadm_1.35.deb")
	writeTestFile(t, dir, "deb/docker/containerd.io_1.7.deb")

	err := ValidateK8sBundle(dir, K8sBundleOptions{
		Runtime:             "containerd",
		IncludeControlPlane: false,
	})
	if err != nil {
		t.Fatalf("ValidateK8sBundle() error = %v", err)
	}
}

func TestValidateK8sBundleDockerRuntimeRequiresDockerDebs(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "deb/ipvs/ipvsadm_1.0.deb")
	writeTestFile(t, dir, "deb/kubernetes/kubeadm_1.35.deb")
	writeTestFile(t, dir, "deb/docker/docker-ce_27.deb")
	writeTestFile(t, dir, "deb/docker/docker-ce-cli_27.deb")
	writeTestFile(t, dir, "deb/docker/containerd.io_1.7.deb")
	writeTestFile(t, dir, "deb/docker/cri-dockerd_0.3.deb")

	err := ValidateK8sBundle(dir, K8sBundleOptions{
		Runtime: "docker",
	})
	if err != nil {
		t.Fatalf("ValidateK8sBundle() error = %v", err)
	}
}

func TestValidateK8sBundleReportsMissingAssets(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "deb/ipvs/ipvsadm_1.0.deb")

	err := ValidateK8sBundle(dir, K8sBundleOptions{
		Runtime:             "containerd",
		IncludeControlPlane: true,
	})
	if err == nil {
		t.Fatal("ValidateK8sBundle() error = nil, want missing assets error")
	}
	var missing MissingAssetsError
	if !errors.As(err, &missing) {
		t.Fatalf("ValidateK8sBundle() error type = %T, want MissingAssetsError", err)
	}
	want := map[string]bool{
		"components.yaml":                true,
		"deb/docker/containerd.io_*.deb": true,
		"deb/kubernetes/*.deb":           true,
		"images/*.tar":                   true,
		"kube-flannel.yml":               true,
	}
	if len(missing.Missing) != len(want) {
		t.Fatalf("missing assets = %v, want %d entries", missing.Missing, len(want))
	}
	for _, got := range missing.Missing {
		if !want[got] {
			t.Fatalf("unexpected missing asset %q in %v", got, missing.Missing)
		}
	}
}

func TestValidateK8sBundleRejectsNonDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bundle.tar.gz")
	if err := os.WriteFile(path, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	err := ValidateK8sBundle(path, K8sBundleOptions{})
	if err == nil {
		t.Fatal("ValidateK8sBundle() error = nil, want non-directory error")
	}
}

func writeTestFile(t *testing.T, root, rel string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("test"), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}
