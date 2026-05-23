// Package exec wraps os/exec with project-wide logging conventions.
package exec

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"xsh/internal/log"
)

// Run executes name with args synchronously. In verbose mode stdout/stderr are
// streamed to the current process; otherwise stdout is discarded and stderr is
// captured (and included in the returned error on failure).
func Run(name string, args ...string) error {
	log.Info("[CMD] %s", formatCmd(name, args))

	cmd := exec.Command(name, args...)
	var stderr bytes.Buffer

	if log.Verbose() {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	} else {
		cmd.Stdout = nil
		cmd.Stderr = &stderr
	}

	if err := cmd.Run(); err != nil {
		errOut := strings.TrimSpace(stderr.String())
		if !log.Verbose() && errOut != "" {
			log.Error("%s stderr: %s", name, errOut)
		}
		return fmt.Errorf("exec %s: %w (stderr: %s)", name, err, errOut)
	}
	return nil
}

// RunOutput executes name with args and returns trimmed stdout. On failure it
// returns an error that includes captured stderr.
func RunOutput(name string, args ...string) (string, error) {
	log.Info("[CMD] %s", formatCmd(name, args))

	cmd := exec.Command(name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errOut := strings.TrimSpace(stderr.String())
		return "", fmt.Errorf("exec %s: %w (stderr: %s)", name, err, errOut)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func formatCmd(name string, args []string) string {
	if len(args) == 0 {
		return name
	}
	return name + " " + strings.Join(args, " ")
}
