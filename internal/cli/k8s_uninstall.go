package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"xsh/internal/log"
	"xsh/internal/osinfo"
	"xsh/internal/uninstall"
)

// K8sUninstallOptions holds flags for `xsh k8s uninstall`.
type K8sUninstallOptions struct {
	RemoveRuntime string
	CRIRuntime    string
	Yes           bool
}

// NewK8sUninstallCmd builds the Kubernetes uninstall subcommand.
func NewK8sUninstallCmd() *cobra.Command {
	opts := &K8sUninstallOptions{}

	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall Kubernetes components from this node",
		RunE: func(cmd *cobra.Command, args []string) error {
			info, err := osinfo.Detect()
			if err != nil {
				return fmt.Errorf("detect os: %w", err)
			}
			log.Info("k8s uninstall: detected OS: %s %s (%s)", info.ID, info.VersionID, info.Codename)
			if err := osinfo.RequireSupported(info); err != nil {
				return err
			}

			if err := uninstall.Run(cmd.Context(), uninstall.Options{
				Yes:           opts.Yes,
				RemoveRuntime: uninstall.RuntimeRemoval(opts.RemoveRuntime),
				CRIRuntime:    opts.CRIRuntime,
				In:            cmd.InOrStdin(),
				Out:           cmd.ErrOrStderr(),
			}); err != nil {
				return fmt.Errorf("k8s uninstall: %w", err)
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&opts.RemoveRuntime, "remove-runtime", string(uninstall.RemoveRuntimeAsk),
		"container runtime removal: ask|none|docker|containerd|all|auto")
	f.StringVar(&opts.RemoveRuntime, "runtime", string(uninstall.RemoveRuntimeAsk),
		"deprecated alias for --remove-runtime")
	f.StringVar(&opts.CRIRuntime, "cri-runtime", "auto", "CRI socket for kubeadm reset: auto|containerd|docker")
	f.BoolVarP(&opts.Yes, "yes", "y", false, "skip Kubernetes uninstall confirmation")
	_ = f.MarkHidden("runtime")

	return cmd
}
