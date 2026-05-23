// Command xsh is a single-binary deployer for Kubernetes and Docker on
// Debian 12/13 or Ubuntu 22.04/24.04.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"xsh/internal/cli"
	"xsh/internal/log"
	"xsh/internal/osinfo"
)

var verbose bool

func main() {
	root := &cobra.Command{
		Use:          "xsh",
		Short:        "Software tools for k8s & docker on Debian 12/13 or Ubuntu 22.04/24.04",
		SilenceUsage: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			log.SetVerbose(verbose)

			// help / version commands don't need privilege or OS checks.
			if isExempt(cmd) {
				return nil
			}

			checkRoot()
			return checkSupportedOS()
		},
	}

	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output (pass-through apt/dpkg/kubeadm)")

	root.AddCommand(cli.NewK8sCmd())
	root.AddCommand(cli.NewDockerCmd())

	root.SetContext(context.Background())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// isExempt reports whether the cobra command should bypass root/OS checks.
// Walks parents so that subcommands of `help` / `completion` (e.g.
// `xsh completion bash`) are exempt as well.
func isExempt(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		name := c.Name()
		if name == "help" || name == "completion" {
			return true
		}
	}
	return false
}

// checkRoot warns (but does not fail) when not running as root. PR1 keeps this
// permissive so the CLI is usable on Windows during development; later PRs will
// promote this to a hard failure on Linux.
func checkRoot() {
	if os.Geteuid() != 0 {
		log.Warn("not running as root (euid=%d); install steps will fail on Linux", os.Geteuid())
	}
}

// checkSupportedOS validates the host is one of the supported (distro,
// version) combos. On non-Linux systems /etc/os-release is absent and we
// downgrade to a warning so developers can exercise --help on Windows.
func checkSupportedOS() error {
	info, err := osinfo.Detect()
	if err != nil {
		log.Warn("os detection skipped: %v", err)
		return nil
	}
	if err := osinfo.RequireSupported(info); err != nil {
		return fmt.Errorf("os check: %w", err)
	}
	return nil
}
