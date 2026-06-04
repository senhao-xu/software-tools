// Package assets validates and resolves offline resource locations.
package assets

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// K8sBundleOptions describes which Kubernetes install path will consume the
// offline bundle.
type K8sBundleOptions struct {
	// Runtime is "containerd" or "docker".
	Runtime string

	// IncludeControlPlane requires master-only assets such as image archives
	// and CNI manifests. Worker join does not need those files.
	IncludeControlPlane bool
}

// MissingAssetsError reports every required bundle asset that was absent.
type MissingAssetsError struct {
	Missing []string
}

func (e MissingAssetsError) Error() string {
	return "offline assets bundle is incomplete; missing: " + strings.Join(e.Missing, ", ")
}

// ValidateK8sBundle checks that dir contains every asset needed by the chosen
// Kubernetes install path. It does not validate package versions; dpkg/kubeadm
// still own compatibility checks during install.
func ValidateK8sBundle(dir string, opts K8sBundleOptions) error {
	if dir == "" {
		return nil
	}

	info, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("offline assets dir %q does not exist", dir)
		}
		return fmt.Errorf("stat offline assets dir %q: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("offline assets path %q is not a directory", dir)
	}

	var missing []string
	requireGlob(&missing, dir, "deb/ipvs/*.deb")
	requireGlob(&missing, dir, "deb/kubernetes/*.deb")

	switch opts.Runtime {
	case "", "containerd":
		requireGlob(&missing, dir, "deb/docker/containerd.io_*.deb")
	case "docker":
		requireGlob(&missing, dir, "deb/docker/docker-ce_*.deb")
		requireGlob(&missing, dir, "deb/docker/docker-ce-cli_*.deb")
		requireGlob(&missing, dir, "deb/docker/containerd.io_*.deb")
		requireGlob(&missing, dir, "deb/docker/cri-dockerd_*.deb")
	default:
		return fmt.Errorf("unknown runtime %q", opts.Runtime)
	}

	if opts.IncludeControlPlane {
		requireGlob(&missing, dir, "images/*.tar")
		requireFile(&missing, dir, "kube-flannel.yml")
		requireFile(&missing, dir, "components.yaml")
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		return MissingAssetsError{Missing: missing}
	}
	return nil
}

func requireGlob(missing *[]string, root, pattern string) {
	matches, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(pattern)))
	if err != nil || len(matches) == 0 {
		*missing = append(*missing, pattern)
	}
}

func requireFile(missing *[]string, root, rel string) {
	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil || info.IsDir() {
		*missing = append(*missing, rel)
	}
}
