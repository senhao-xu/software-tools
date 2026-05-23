package cli

import (
	"github.com/spf13/cobra"

	"xsh/internal/detect"
	"xsh/internal/log"
	cruntime "xsh/internal/runtime"
	"xsh/internal/sysprep"
)

// K8sJoinOptions holds flags for `xsh k8s join`.
type K8sJoinOptions struct {
	Master                   string
	Token                    string
	DiscoveryTokenCACertHash string
	Runtime                  string
	Mirror                   string
	AssetsDir                string
	Yes                      bool
}

// NewK8sJoinCmd builds the `xsh k8s join` subcommand.
func NewK8sJoinCmd() *cobra.Command {
	opts := &K8sJoinOptions{}

	cmd := &cobra.Command{
		Use:   "join",
		Short: "Join this node to an existing cluster as a worker",
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

			log.Info("k8s join: continuing (Step 3-4 placeholder, PR5+ will implement)")
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&opts.Master, "master", "", "master endpoint (host:port) [required]")
	f.StringVar(&opts.Token, "token", "", "bootstrap token [required]")
	f.StringVar(&opts.DiscoveryTokenCACertHash, "discovery-token-ca-cert-hash", "", "CA cert hash from master [required]")
	f.StringVar(&opts.Runtime, "runtime", "containerd", "container runtime: containerd|docker")
	f.StringVar(&opts.Mirror, "mirror", "", "package/image mirror (empty = official, supported: cn)")
	f.StringVar(&opts.AssetsDir, "assets-dir", "", "offline assets directory (overrides auto-detect)")
	f.BoolVarP(&opts.Yes, "yes", "y", false, "skip overwrite confirmation")

	_ = cmd.MarkFlagRequired("master")
	_ = cmd.MarkFlagRequired("token")
	_ = cmd.MarkFlagRequired("discovery-token-ca-cert-hash")

	return cmd
}
