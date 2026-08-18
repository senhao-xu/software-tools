package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"xsh/internal/exec"
	"xsh/internal/log"
)

// installAliases maps friendly names to the Debian-family package set they
// expand to. New aliases are one-line additions here.
var installAliases = map[string][]string{
	"python": {"python3", "python3-pip", "python-is-python3"},
	"nodejs": {"nodejs"}, // NodeSource repo, not distro apt; see installPreHooks.
}

// installPreHooks maps user-facing names to a shell snippet that must run
// before `apt-get install` (e.g. third-party repo setup). Names without an
// entry install straight from distro apt; new hooks are one-line additions
// here. Snippets run via `bash -c`, following the aptrepo curl|gpg pattern.
var installPreHooks = map[string]string{
	"nodejs": "curl -fsSL https://deb.nodesource.com/setup_22.x | bash -",
}

// installReserved lists names owned by dedicated subcommands; `xsh install`
// refuses them so it never shadows `xsh docker` / `xsh k8s`.
var installReserved = map[string]string{
	"docker": "xsh docker",
	"k8s":    "xsh k8s",
}

// InstallOptions holds flags for `xsh install`.
type InstallOptions struct {
	NoUpdate bool
	Yes      bool
}

// NewInstallCmd builds the `xsh install` command (apt packages via aliases).
func NewInstallCmd() *cobra.Command {
	opts := &InstallOptions{}

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install apt packages, expanding aliases like 'python' to their Debian package set",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pkgs, err := resolveInstallPackages(args)
			if err != nil {
				return err
			}
			log.Info("install: resolved packages: %s", strings.Join(pkgs, " "))

			if !opts.Yes {
				cont, err := confirmInstall(pkgs)
				if err != nil {
					return err
				}
				if !cont {
					log.Info("cancelled by user")
					return nil
				}
			}

			if !opts.NoUpdate {
				if err := exec.Run("apt-get", "update"); err != nil {
					return fmt.Errorf("apt-get update: %w", err)
				}
			}

			for _, hook := range collectInstallPreHooks(args) {
				if err := exec.Run("bash", "-c", hook); err != nil {
					return fmt.Errorf("pre-install hook failed: %w", err)
				}
			}

			installArgs := append([]string{"install", "-y"}, pkgs...)
			if err := exec.Run("apt-get", installArgs...); err != nil {
				return fmt.Errorf("apt-get install: %w", err)
			}
			log.Info("install: done -- %s", strings.Join(pkgs, " "))
			return nil
		},
	}

	f := cmd.Flags()
	f.BoolVar(&opts.NoUpdate, "no-update", false, "skip the apt-get update step")
	f.BoolVarP(&opts.Yes, "yes", "y", false, "skip the install confirmation prompt")

	return cmd
}

// resolveInstallPackages maps each user-supplied name through installAliases
// (unknown names pass through verbatim) and merges the result into a single
// deduplicated list that preserves first-seen order.
func resolveInstallPackages(args []string) ([]string, error) {
	seen := make(map[string]bool)
	var pkgs []string
	for _, arg := range args {
		if hint, reserved := installReserved[arg]; reserved {
			return nil, fmt.Errorf("%q is handled by %q; run that command instead", arg, hint)
		}
		names, ok := installAliases[arg]
		if !ok {
			names = []string{arg}
		}
		for _, name := range names {
			if !seen[name] {
				seen[name] = true
				pkgs = append(pkgs, name)
			}
		}
	}
	return pkgs, nil
}

// collectInstallPreHooks returns the pre-install snippets (in first-seen
// order, deduplicated) for those user-supplied names that have one. Reserved
// and unknown names have no hooks; reserved names are rejected earlier by
// resolveInstallPackages.
func collectInstallPreHooks(args []string) []string {
	seen := make(map[string]bool)
	var hooks []string
	for _, arg := range args {
		hook, ok := installPreHooks[arg]
		if !ok || seen[arg] {
			continue
		}
		seen[arg] = true
		hooks = append(hooks, hook)
	}
	return hooks
}

// confirmInstall lists the resolved packages once and prompts [Y/n]. EOF or
// invalid input defaults to cancel, mirroring detect.Confirm conventions.
func confirmInstall(pkgs []string) (bool, error) {
	fmt.Fprintf(os.Stderr, "The following packages will be installed:\n  %s\n", strings.Join(pkgs, " "))

	reader := bufio.NewReader(os.Stdin)
	for attempt := 0; attempt < 3; attempt++ {
		fmt.Fprint(os.Stderr, "Install? [Y/n]: ")
		line, err := reader.ReadString('\n')
		if err != nil {
			// EOF / closed stdin: default to cancel.
			log.Warn("no input available (non-interactive?); use -y to skip the prompt")
			return false, nil
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "", "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
		fmt.Fprintln(os.Stderr, "  please answer Y (yes) or n (no)")
	}
	log.Warn("no valid answer after 3 attempts, defaulting to cancel")
	return false, nil
}
