package cli

import (
	"github.com/spf13/cobra"

	"xsh/internal/detect"
	"xsh/internal/dockerinstall"
	"xsh/internal/log"
)

// DockerOptions holds flags for `xsh docker`.
type DockerOptions struct {
	Major int
	Yes   bool
}

// NewDockerCmd builds the `xsh docker` command (online docker install).
func NewDockerCmd() *cobra.Command {
	opts := &DockerOptions{}

	cmd := &cobra.Command{
		Use:   "docker",
		Short: "Install Docker on Debian 12/13",
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
			installOpts := dockerinstall.Options{
				Major:  opts.Major,
				Mirror: "",
			}
			if err := dockerinstall.Install(ctx, installOpts); err != nil {
				log.Error("docker install failed, rolling back: %v", err)
				_ = dockerinstall.Rollback(ctx, installOpts)
				return err
			}
			log.Info("docker install: done -- run 'docker run hello-world' to verify")
			return nil
		},
	}

	f := cmd.Flags()
	f.IntVar(&opts.Major, "major", 0, "pin docker major version (0 = latest)")
	f.BoolVarP(&opts.Yes, "yes", "y", false, "skip overwrite confirmation")

	return cmd
}
