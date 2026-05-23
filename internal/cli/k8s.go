package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"xsh/internal/detect"
	"xsh/internal/kube"
	"xsh/internal/log"
	"xsh/internal/network"
	"xsh/internal/osinfo"
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
			info, err := osinfo.Detect()
			if err != nil {
				return fmt.Errorf("detect os: %w", err)
			}
			log.Info("k8s: detected OS: %s %s (%s)", info.ID, info.VersionID, info.Codename)
			if err := osinfo.RequireSupported(info); err != nil {
				return err
			}

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

			initOpts := kube.InitOptions{
				Runtime:     opts.Runtime,
				Version:     opts.Version,
				ServiceCIDR: opts.ServiceCIDR,
				PodCIDR:     opts.PodCIDR,
				Hostname:    opts.Hostname,
				Advertise:   opts.Advertise,
				Mirror:      opts.Mirror,
				AssetsDir:   opts.AssetsDir,
			}
			if err := kube.Init(ctx, initOpts); err != nil {
				log.Error("kubeadm init failed, rolling back: %v", err)
				_ = kube.ResetInit(ctx, initOpts)
				_ = kube.Rollback(ctx, kubeOpts)
				_ = cruntime.Rollback(ctx, rtOpts)
				_ = sysprep.Rollback(ctx)
				return err
			}

			netOpts := network.Options{
				AssetsDir: opts.AssetsDir,
				Mirror:    opts.Mirror,
			}
			if err := network.Install(ctx, netOpts); err != nil {
				log.Error("network install failed, rolling back: %v", err)
				_ = network.Rollback(ctx, netOpts)
				_ = kube.ResetInit(ctx, initOpts)
				_ = kube.Rollback(ctx, kubeOpts)
				_ = cruntime.Rollback(ctx, rtOpts)
				_ = sysprep.Rollback(ctx)
				return err
			}

			log.Info("k8s install: cluster ready -- run 'kubectl get nodes' to verify")
			log.Info("k8s install: worker join command in /var/cache/xsh/join-command.sh")
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
