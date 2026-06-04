package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"xsh/internal/log"
	"xsh/internal/offlinebundle"
)

// K8sBundleOptions holds flags for `xsh k8s bundle`.
type K8sBundleOptions struct {
	Runtime     string
	Mirror      string
	Version     string
	OutputDir   string
	ArchivePath string
}

// NewK8sBundleCmd builds the offline bundle preparation command.
func NewK8sBundleCmd() *cobra.Command {
	opts := &K8sBundleOptions{}

	cmd := &cobra.Command{
		Use:   "bundle",
		Short: "Download Kubernetes offline assets and package them",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateRuntime(&opts.Runtime); err != nil {
				return err
			}
			result, err := offlinebundle.Prepare(cmd.Context(), offlinebundle.Options{
				Runtime:     opts.Runtime,
				Version:     opts.Version,
				Mirror:      opts.Mirror,
				OutputDir:   opts.OutputDir,
				ArchivePath: opts.ArchivePath,
			})
			if err != nil {
				return fmt.Errorf("prepare k8s offline bundle: %w", err)
			}
			log.Info("k8s bundle: use --assets-dir=%s after extracting/copying the directory", result.AssetsDir)
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&opts.Runtime, "runtime", "containerd", "container runtime: containerd|docker")
	f.StringVar(&opts.Mirror, "mirror", "", "package/image mirror (empty = official, supported: cn)")
	f.StringVar(&opts.Version, "version", "v1.35.0", "Kubernetes version")
	f.StringVar(&opts.OutputDir, "output-dir", "xsh-k8s-offline", "directory to write offline assets into")
	f.StringVar(&opts.ArchivePath, "archive", "", "archive path (default: <output-dir>.tar.gz)")
	return cmd
}
