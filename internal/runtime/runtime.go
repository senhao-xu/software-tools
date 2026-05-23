// Package cruntime dispatches container-runtime install/rollback to the
// appropriate subpackage (containerd or docker+cri-dockerd).
//
// The directory is internal/runtime but the package name is "cruntime" to
// avoid colliding with the standard library "runtime" package; callers should
// import this with an alias such as:
//
//	import cruntime "xsh/internal/runtime"
package cruntime

import (
	"context"
	"fmt"

	"xsh/internal/runtime/containerd"
	dockerrt "xsh/internal/runtime/docker"
)

// Kind selects which container runtime to install.
type Kind string

const (
	Containerd Kind = "containerd"
	Docker     Kind = "docker"
)

// Options is the common configuration accepted by Install/Rollback. The Kind
// field decides which subpackage is dispatched to; Mirror and AssetsDir are
// passed through verbatim.
type Options struct {
	// Kind is "containerd" (default) or "docker".
	Kind Kind
	// Mirror is "" for official, "cn" for the Aliyun mirror.
	Mirror string
	// AssetsDir, when non-empty and contains the expected subtree, switches
	// the chosen subpackage to its offline path.
	AssetsDir string
}

// Install dispatches to the appropriate runtime install function. An empty
// Kind defaults to containerd to match the CLI default.
func Install(ctx context.Context, opts Options) error {
	switch normalizeKind(opts.Kind) {
	case Containerd:
		return containerd.Install(ctx, containerd.Options{
			Mirror:    opts.Mirror,
			AssetsDir: opts.AssetsDir,
		})
	case Docker:
		return dockerrt.Install(ctx, dockerrt.Options{
			Mirror:    opts.Mirror,
			AssetsDir: opts.AssetsDir,
		})
	default:
		return fmt.Errorf("unknown runtime kind %q", opts.Kind)
	}
}

// Rollback dispatches to the appropriate runtime rollback function.
func Rollback(ctx context.Context, opts Options) error {
	switch normalizeKind(opts.Kind) {
	case Containerd:
		return containerd.Rollback(ctx, containerd.Options{
			Mirror:    opts.Mirror,
			AssetsDir: opts.AssetsDir,
		})
	case Docker:
		return dockerrt.Rollback(ctx, dockerrt.Options{
			Mirror:    opts.Mirror,
			AssetsDir: opts.AssetsDir,
		})
	default:
		return fmt.Errorf("unknown runtime kind %q", opts.Kind)
	}
}

func normalizeKind(k Kind) Kind {
	if k == "" {
		return Containerd
	}
	return k
}
