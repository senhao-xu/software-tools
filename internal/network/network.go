// Package network implements Step 5: CNI (flannel) + metrics-server install.
//
// Both components are applied via `kubectl apply -f <source>` against the
// freshly-initialized cluster (using /etc/kubernetes/admin.conf for auth). The
// source is chosen per-component:
//
//   - offline: a local YAML under <AssetsDir> when present
//   - online:  the upstream manifest URL (flannel pinned to a known-stable tag;
//     metrics-server uses the GitHub "latest" redirect)
//
// Failure semantics differ:
//
//   - flannel failure -> return error (the cluster is unusable without a CNI)
//   - metrics-server failure -> log.Warn and continue (the cluster runs fine
//     without it; HPA / `kubectl top` won't, but that's a non-blocking nicety)
//
// Rollback uses `kubectl delete -f <same-source> --ignore-not-found`; failures
// are degraded to WARN. If admin.conf is gone entirely (extreme rollback path),
// we skip with a WARN rather than erroring out.
//
// Mirror handling: there is no Chinese CDN mirror for the flannel or
// metrics-server YAMLs (both are GitHub-hosted). When --mirror=cn is set and
// we're on the online path we emit a WARN suggesting --assets-dir for users
// behind slow GitHub links; we do not rewrite the URL.
package network

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	xexec "xsh/internal/exec"
	"xsh/internal/log"
)

// Options controls Install/Rollback behaviour.
type Options struct {
	// AssetsDir, when non-empty, is searched for kube-flannel.yml and
	// components.yaml. Each file is independent: a found flannel YAML
	// triggers offline-flannel; a missing one falls back to online for
	// that component only.
	AssetsDir string

	// Mirror is "" for the upstream URLs, "cn" to emit an advisory WARN
	// (no actual URL rewrite — there is no CN mirror for these YAMLs).
	Mirror string
}

const (
	adminConfPath = "/etc/kubernetes/admin.conf"

	flannelAssetFile       = "kube-flannel.yml"
	metricsServerAssetFile = "components.yaml"

	// flannelVersion is the upstream tag we pin for the online manifest URL.
	// Pinning avoids surprise breakage from manifest schema drift; bump this
	// constant when validating against a newer flannel release.
	flannelVersion = "v0.27.4"

	flannelOnlineURL = "https://raw.githubusercontent.com/flannel-io/flannel/" +
		flannelVersion + "/Documentation/kube-flannel.yml"

	// metrics-server uses GitHub's "latest" redirect; this is the upstream
	// recommended URL and avoids hard-coding patch versions for a component
	// that ships frequent compatible releases.
	metricsServerOnlineURL = "https://github.com/kubernetes-sigs/metrics-server/" +
		"releases/latest/download/components.yaml"
)

// Install applies flannel and metrics-server to the cluster. flannel is
// required (failure -> error); metrics-server failure is logged and ignored.
// The returned error, when non-nil, comes from flannel only.
func Install(_ context.Context, opts Options) error {
	log.Info("network: install CNI + metrics-server ...")

	if opts.Mirror == "cn" {
		warnIfOnline(opts, flannelAssetFile, "flannel")
		warnIfOnline(opts, metricsServerAssetFile, "metrics-server")
	}

	flannelSrc := chooseSource(opts.AssetsDir, flannelAssetFile, flannelOnlineURL)
	log.Info("network: applying flannel from %s", flannelSrc)
	if err := kubectlApply(flannelSrc); err != nil {
		return fmt.Errorf("kubectl apply flannel (%s): %w", flannelSrc, err)
	}
	log.Info("network: flannel applied")

	metricsSrc := chooseSource(opts.AssetsDir, metricsServerAssetFile, metricsServerOnlineURL)
	log.Info("network: applying metrics-server from %s", metricsSrc)
	if err := kubectlApply(metricsSrc); err != nil {
		// metrics-server is not load-bearing for cluster availability; degrade
		// to WARN so a transient GitHub hiccup doesn't trash the whole install.
		log.Warn("metrics-server apply failed (cluster will run without it): %v", err)
	} else {
		log.Info("network: metrics-server applied")
	}

	log.Info("network: install done -- run 'kubectl get pods -A' to confirm flannel/metrics-server are Running")
	return nil
}

// Rollback `kubectl delete`s both manifests with --ignore-not-found. All
// failures are degraded to WARN; the caller's chain (`_ = network.Rollback`)
// already treats this best-effort. If admin.conf is missing we skip both
// deletes — there's no cluster left to talk to.
func Rollback(_ context.Context, opts Options) error {
	log.Info("network: rollback")

	if _, err := os.Stat(adminConfPath); errors.Is(err, fs.ErrNotExist) {
		log.Warn("network rollback: %s missing, skipping kubectl delete", adminConfPath)
		log.Info("network rollback done")
		return nil
	} else if err != nil {
		log.Warn("network rollback: stat %s: %v (proceeding)", adminConfPath, err)
	}

	metricsSrc := chooseSource(opts.AssetsDir, metricsServerAssetFile, metricsServerOnlineURL)
	if err := kubectlDelete(metricsSrc); err != nil {
		log.Warn("kubectl delete metrics-server (%s): %v", metricsSrc, err)
	}

	flannelSrc := chooseSource(opts.AssetsDir, flannelAssetFile, flannelOnlineURL)
	if err := kubectlDelete(flannelSrc); err != nil {
		log.Warn("kubectl delete flannel (%s): %v", flannelSrc, err)
	}

	log.Info("network rollback done")
	return nil
}

// chooseSource returns the local YAML path when <AssetsDir>/<assetFile> exists
// and is a regular file; otherwise returns onlineURL. Empty AssetsDir or any
// stat error (other than "file not found") falls through to online — the same
// "missing means online" convention used by sysprep / kube / runtime offline
// paths.
func chooseSource(assetsDir, assetFile, onlineURL string) string {
	if assetsDir == "" {
		return onlineURL
	}
	path := filepath.Join(assetsDir, assetFile)
	info, err := os.Stat(path)
	if err != nil {
		return onlineURL
	}
	if info.IsDir() {
		return onlineURL
	}
	return path
}

// warnIfOnline emits the mirror=cn advisory only when the given component
// would resolve to the online URL (i.e. no matching asset file). Avoids a
// misleading warning when the user already provided a local YAML.
func warnIfOnline(opts Options, assetFile, component string) {
	if opts.AssetsDir != "" {
		if _, err := os.Stat(filepath.Join(opts.AssetsDir, assetFile)); err == nil {
			return
		}
	}
	log.Warn("%s YAML is fetched from GitHub; no CN mirror is available. "+
		"If download is slow, retry with --assets-dir pointing at a directory "+
		"containing %s.", component, assetFile)
}

// kubectlApply / kubectlDelete are thin wrappers that always pin --kubeconfig
// to admin.conf (we cannot rely on ~/.kube/config existing on the rollback
// path — kubeconfig copy may have failed earlier in the chain).
func kubectlApply(source string) error {
	return xexec.Run("kubectl", "--kubeconfig="+adminConfPath, "apply", "-f", source)
}

func kubectlDelete(source string) error {
	return xexec.Run("kubectl", "--kubeconfig="+adminConfPath,
		"delete", "-f", source, "--ignore-not-found")
}
