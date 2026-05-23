package cli

import (
	"github.com/spf13/cobra"

	"xsh/internal/log"
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
			log.Info("k8s join: not yet implemented (PR1 skeleton)")
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
