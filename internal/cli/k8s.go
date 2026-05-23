package cli

import (
	"github.com/spf13/cobra"

	"xsh/internal/detect"
	"xsh/internal/log"
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
			log.Info("k8s install: continuing (Step 2-5 placeholder, PR4+ will implement)")
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
