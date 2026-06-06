// Package dockerinstall installs Docker CE as a standalone, day-to-day
// container engine on Debian/Ubuntu — distinct from internal/runtime/docker,
// which installs docker + cri-dockerd as the k8s runtime.
//
// The flow mirrors docker.senhao.eu.cc:
//   - add the download.docker.com apt repo + gpg keyring (idempotent,
//     delegated to internal/aptrepo)
//   - list apt-cache madison docker-ce versions for interactive selection
//   - render /etc/docker/daemon.json (json-file 100m x 5, systemd cgroup)
//   - apt-get install docker-ce + plugins
//   - systemctl enable --now docker
package dockerinstall

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"strconv"
	"strings"

	"xsh/internal/aptrepo"
	xexec "xsh/internal/exec"
	"xsh/internal/log"
)

// Options controls install behaviour.
type Options struct {
	// Major pins the docker-ce major version (e.g. 27 -> install 27.x.x).
	// 0 means interactive version selection unless Yes is set.
	Major int

	// Yes skips interactive version selection and lets apt install the latest
	// available docker-ce package unless Major is set.
	Yes bool

	// Mirror is reserved for future use. download.docker.com is reachable from
	// CN today, so PR9 ignores this field; it stays on the API so callers can
	// be wired without churn.
	Mirror string

	// In/Out are used for interactive version selection. Nil means stdin/stderr.
	In  io.Reader
	Out io.Writer
}

const (
	daemonJSONPath = "/etc/docker/daemon.json"
	daemonDir      = "/etc/docker"
)

// dockerPkgs are the packages installed by `xsh docker`. docker-model-plugin
// is new in late 2024 releases; on older repos it is missing and we tolerate
// its absence with a WARN rather than failing the whole install.
var dockerPkgs = []string{
	"docker-ce",
	"docker-ce-cli",
	"containerd.io",
	"docker-buildx-plugin",
	"docker-compose-plugin",
	"docker-model-plugin",
}

// Install runs the full standalone docker install. Steps 1-6 must all succeed;
// step 7 (docker --version) is best-effort because the service is already
// enabled by the time we get there.
func Install(ctx context.Context, opts Options) error {
	opts = normalizeOptions(opts)

	log.Info("dockerinstall: install start (major=%d)", opts.Major)
	if opts.Major == 0 {
		if opts.Yes {
			log.Info("dockerinstall: no Docker major pin requested; apt will install the newest docker-ce available")
		} else {
			log.Info("dockerinstall: no Docker major pin requested; available versions will be listed for selection")
		}
	} else {
		log.Info("dockerinstall: Docker major pin requested: %d.x", opts.Major)
	}

	if err := installAptDeps(); err != nil {
		return err
	}
	if err := aptrepo.EnsureDockerRepo(ctx); err != nil {
		return err
	}

	version, err := resolveVersion(opts)
	if err != nil {
		return err
	}

	if err := writeDaemonJSON(); err != nil {
		return fmt.Errorf("write daemon.json: %w", err)
	}

	if err := installPackages(version); err != nil {
		return err
	}

	log.Info("dockerinstall: enabling and starting docker service")
	if err := xexec.Run("systemctl", "enable", "--now", "docker"); err != nil {
		return fmt.Errorf("enable docker: %w", err)
	}

	if out, verr := xexec.RunOutput("docker", "--version"); verr != nil {
		log.Warn("docker --version failed (service is up, treating as non-fatal): %v", verr)
	} else {
		log.Info("dockerinstall: %s", out)
	}

	log.Info("dockerinstall: install done")
	return nil
}

// Rollback is intentionally narrow: stop the service and remove daemon.json.
// Packages, apt repo, gpg keyring are owned by detect.Cleanup and stay put.
func Rollback(_ context.Context, _ Options) error {
	log.Info("dockerinstall: rollback")
	if err := xexec.Run("systemctl", "stop", "docker"); err != nil {
		log.Warn("systemctl stop docker: %v", err)
	}
	if err := os.Remove(daemonJSONPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Warn("remove %s: %v", daemonJSONPath, err)
	}
	log.Info("dockerinstall: rollback done")
	return nil
}

// --- step 1: apt deps ------------------------------------------------------

func installAptDeps() error {
	log.Info("dockerinstall: installing apt helper packages: ca-certificates, curl, gnupg, lsb-release")
	if err := xexec.Run("apt-get", "update"); err != nil {
		log.Warn("apt-get update (pre-deps): %v", err)
	}
	if err := xexec.Run("apt-get", "install", "-y",
		"ca-certificates", "curl", "gnupg", "lsb-release"); err != nil {
		return fmt.Errorf("apt-get install deps: %w", err)
	}
	return nil
}

// --- step 2: docker apt repo ----------------------------------------------
//
// The download.docker.com keyring and sources.list.d entry are installed by
// aptrepo.EnsureDockerRepo (called from Install). This package no longer
// carries its own copy; aptrepo handles Debian and Ubuntu uniformly.

// --- step 3: version selection --------------------------------------------

// resolveVersion returns the apt version string (epoch-prefixed, e.g.
// "5:27.5.1-1~debian.12~bookworm") to pass to apt-get install, or "" when
// apt should resolve the latest package. The major prefix match runs against
// the version component only; the epoch (`<n>:`) is preserved so apt accepts
// the string.
func resolveVersion(opts Options) (string, error) {
	if opts.Major == 0 && opts.Yes {
		log.Info("dockerinstall: skipping apt-cache madison because no major pin was requested")
		return "", nil
	}

	out, err := xexec.RunOutput("apt-cache", "madison", "docker-ce")
	if err != nil {
		if opts.Major > 0 {
			return "", fmt.Errorf("apt-cache madison docker-ce: %w", err)
		}
		log.Warn("apt-cache madison docker-ce failed; falling back to apt latest: %v", err)
		return "", nil
	}

	versions := parseMadisonVersions(out, opts.Major)
	if len(versions) == 0 {
		if opts.Major > 0 {
			return "", fmt.Errorf("no docker-ce version matching major=%d in apt cache", opts.Major)
		}
		log.Warn("apt-cache madison docker-ce returned no versions; falling back to apt latest")
		return "", nil
	}

	if opts.Major > 0 {
		log.Info("dockerinstall: selected docker-ce version %s for major=%d", versions[0].Full, opts.Major)
		return versions[0].Full, nil
	}

	version := askVersion(opts.In, opts.Out, versions)
	log.Info("dockerinstall: selected docker-ce version %s", version)
	return version, nil
}

// parseMadisonVersion scans the output of `apt-cache madison docker-ce` for
// the first version whose numeric prefix (after any `<epoch>:` strip) matches
// the requested major. Each madison row is `docker-ce | <version> | <repo>`.
// Returns "" when no row matches. Extracted as a pure function so unit tests
// can exercise the parser without invoking apt-cache.
func parseMadisonVersion(output string, major int) string {
	versions := parseMadisonVersions(output, major)
	if len(versions) == 0 {
		return ""
	}
	return versions[0].Full
}

type dockerVersion struct {
	Full    string
	Display string
}

func parseMadisonVersions(output string, major int) []dockerVersion {
	var prefix *regexp.Regexp
	if major > 0 {
		prefix = regexp.MustCompile(fmt.Sprintf(`^%d\.`, major))
	}

	seen := map[string]bool{}
	var versions []dockerVersion
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), "|")
		if len(parts) < 2 {
			continue
		}
		full := strings.TrimSpace(parts[1])
		if full == "" {
			continue
		}
		cmp := full
		if idx := strings.Index(cmp, ":"); idx >= 0 {
			cmp = cmp[idx+1:]
		}
		if prefix != nil && !prefix.MatchString(cmp) {
			continue
		}
		if seen[full] {
			continue
		}
		seen[full] = true
		versions = append(versions, dockerVersion{
			Full:    full,
			Display: cmp,
		})
	}
	return versions
}

func askVersion(in io.Reader, out io.Writer, versions []dockerVersion) string {
	fmt.Fprintln(out, "Available Docker CE versions:")
	for i, version := range versions {
		fmt.Fprintf(out, "  %d) %s\n", i+1, version.Display)
	}

	reader := bufio.NewReader(in)
	for attempt := 0; attempt < 3; attempt++ {
		fmt.Fprintf(out, "Select Docker version [1-%d, Enter=1/latest]: ", len(versions))
		line, err := reader.ReadString('\n')
		if err != nil && len(line) == 0 {
			log.Warn("no Docker version answer; defaulting to latest available: %s", versions[0].Display)
			return versions[0].Full
		}

		answer := strings.TrimSpace(line)
		if answer == "" {
			return versions[0].Full
		}

		n, err := strconv.Atoi(answer)
		if err == nil && n >= 1 && n <= len(versions) {
			return versions[n-1].Full
		}
		fmt.Fprintf(out, "  please enter a number between 1 and %d\n", len(versions))
	}
	log.Warn("no valid Docker version answer after 3 attempts, defaulting to latest available: %s", versions[0].Display)
	return versions[0].Full
}

// --- step 4: daemon.json ---------------------------------------------------

// daemonConfig is the on-disk shape of /etc/docker/daemon.json. max-file is 5
// here (vs 3 in internal/runtime/docker) to match the standalone-docker recipe
// at docker.senhao.eu.cc, which keeps a deeper retention window for daily use.
type daemonConfig struct {
	RegistryMirrors []string          `json:"registry-mirrors"`
	LogDriver       string            `json:"log-driver"`
	LogOpts         map[string]string `json:"log-opts"`
	ExecOpts        []string          `json:"exec-opts"`
}

func writeDaemonJSON() error {
	if err := os.MkdirAll(daemonDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", daemonDir, err)
	}

	cfg := daemonConfig{
		RegistryMirrors: []string{},
		LogDriver:       "json-file",
		LogOpts: map[string]string{
			"max-size": "100m",
			"max-file": "5",
		},
		ExecOpts: []string{"native.cgroupdriver=systemd"},
	}
	log.Info("dockerinstall: rendering %s (log-driver=%s, max-size=%s, max-file=%s, cgroup-driver=systemd)",
		daemonJSONPath, cfg.LogDriver, cfg.LogOpts["max-size"], cfg.LogOpts["max-file"])
	body, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal daemon.json: %w", err)
	}
	body = append(body, '\n')

	return writeFileIfChanged(daemonJSONPath, body, 0o644)
}

// --- step 5: install docker packages --------------------------------------

// installPackages runs apt-get install. When version != "", only docker-ce
// and docker-ce-cli are pinned (the rest let apt resolve dependencies).
// docker-model-plugin is attempted last on its own so an old repo missing
// the package downgrades to a WARN rather than a hard failure.
func installPackages(version string) error {
	primary := make([]string, 0, len(dockerPkgs)-1)
	for _, p := range dockerPkgs {
		if p == "docker-model-plugin" {
			continue
		}
		if version != "" && (p == "docker-ce" || p == "docker-ce-cli") {
			primary = append(primary, p+"="+version)
			continue
		}
		primary = append(primary, p)
	}

	if version != "" {
		log.Info("dockerinstall: pinning docker-ce and docker-ce-cli to apt version %s", version)
	}
	log.Info("dockerinstall: installing Docker packages: %s", strings.Join(primary, ", "))
	args := append([]string{"install", "-y"}, primary...)
	if err := xexec.Run("apt-get", args...); err != nil {
		return fmt.Errorf("apt-get install docker: %w", err)
	}

	log.Info("dockerinstall: installing optional docker-model-plugin when available")
	if err := xexec.Run("apt-get", "install", "-y", "docker-model-plugin"); err != nil {
		log.Warn("apt-get install docker-model-plugin (only on newer repos): %v", err)
	}
	return nil
}

// --- helpers ---------------------------------------------------------------

func writeFileIfChanged(path string, content []byte, perm os.FileMode) error {
	existing, err := os.ReadFile(path)
	if err == nil && bytes.Equal(existing, content) {
		log.Info("dockerinstall: %s already up to date", path)
		return nil
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := os.WriteFile(path, content, perm); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	log.Info("dockerinstall: wrote %s", path)
	return nil
}

func normalizeOptions(opts Options) Options {
	if opts.In == nil {
		opts.In = os.Stdin
	}
	if opts.Out == nil {
		opts.Out = os.Stderr
	}
	return opts
}
