package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"xsh/internal/detect"
	"xsh/internal/kube"
	"xsh/internal/log"
	cruntime "xsh/internal/runtime"
	"xsh/internal/sysprep"
)

// K8sOptions holds flags shared by `xsh k8s` (master install).
type K8sOptions struct {
	Runtime     string
	Mirror      string
	AssetsDir   string
	Version     string
	PodCIDR     string
	ServiceCIDR string
	Hostname    string
	Advertise   string
	Yes         bool
}

// NewK8sCmd builds the `xsh k8s` command (master one-shot install).
func NewK8sCmd() *cobra.Command {
	opts := &K8sOptions{}

	cmd := &cobra.Command{
		Use:   "k8s",
		Short: "Install Kubernetes cluster (master one-shot)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateRuntime(&opts.Runtime); err != nil {
				return err
			}

			ctx := cmd.Context()
			state := detect.Detect(ctx)
			cont, err := detect.Confirm(state, opts.Yes)
			if err != nil {
				return err
			}
			if !cont {
				log.Info("cancelled by user")
				return nil
			}
			if state.HasAny() {
				if err := detect.Cleanup(ctx); err != nil {
					return err
				}
			}
			if err := sysprep.Run(ctx, sysprep.Options{AssetsDir: opts.AssetsDir}); err != nil {
				log.Error("sysprep failed, rolling back: %v", err)
				_ = sysprep.Rollback(ctx)
				return err
			}

			rtOpts := cruntime.Options{
				Kind:      cruntime.Kind(opts.Runtime),
				Mirror:    opts.Mirror,
				AssetsDir: opts.AssetsDir,
			}
			if err := cruntime.Install(ctx, rtOpts); err != nil {
				log.Error("runtime install failed, rolling back: %v", err)
				_ = cruntime.Rollback(ctx, rtOpts)
				_ = sysprep.Rollback(ctx)
				return err
			}

			kubeOpts := kube.Options{
				Version:   opts.Version,
				Mirror:    opts.Mirror,
				AssetsDir: opts.AssetsDir,
			}
			if err := kube.Install(ctx, kubeOpts); err != nil {
				log.Error("kube install failed, rolling back: %v", err)
				_ = kube.Rollback(ctx, kubeOpts)
				_ = cruntime.Rollback(ctx, rtOpts)
				_ = sysprep.Rollback(ctx)
				return err
			}

			log.Info("k8s install: continuing (Step 4-5 placeholder, PR6+ will implement)")
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&opts.Runtime, "runtime", "containerd", "container runtime: containerd|docker")
	f.StringVar(&opts.Mirror, "mirror", "", "package/image mirror (empty = official, supported: cn)")
	f.StringVar(&opts.AssetsDir, "assets-dir", "", "offline assets directory (overrides auto-detect)")
	f.StringVar(&opts.Version, "version", "v1.35.0", "Kubernetes version")
	f.StringVar(&opts.PodCIDR, "pod-cidr", "10.244.0.0/16", "pod network CIDR (flannel-locked default)")
	f.StringVar(&opts.ServiceCIDR, "service-cidr", "10.96.0.0/12", "service CIDR")
	f.StringVar(&opts.Hostname, "hostname", "master", "node hostname")
	f.StringVar(&opts.Advertise, "advertise", "", "advertise address (default: auto-detect outbound IP)")
	f.BoolVarP(&opts.Yes, "yes", "y", false, "skip overwrite confirmation")

	cmd.AddCommand(NewK8sJoinCmd())
	return cmd
}

// validateRuntime normalizes empty -> "containerd" and rejects unknown values.
// Centralised here so `k8s` and `k8s join` share the same accepted set.
func validateRuntime(rt *string) error {
	switch *rt {
	case "containerd", "docker":
		return nil
	case "":
		*rt = "containerd"
		return nil
	default:
		return fmt.Errorf("invalid --runtime=%s (must be containerd or docker)", *rt)
	}
}
