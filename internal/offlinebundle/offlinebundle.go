// Package offlinebundle prepares a Kubernetes offline assets directory.
package offlinebundle

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"xsh/internal/aptrepo"
	"xsh/internal/cridockerd"
	xexec "xsh/internal/exec"
	"xsh/internal/log"
	"xsh/internal/osinfo"
)

const (
	defaultOutputDir = "xsh-k8s-offline"
	defaultRuntime   = "containerd"
	defaultVersion   = "v1.35.0"
	defaultImageTool = "docker"

	flannelAssetFile       = "kube-flannel.yml"
	metricsServerAssetFile = "components.yaml"

	flannelOnlineURL = "https://raw.githubusercontent.com/flannel-io/flannel/" +
		"v0.27.4/Documentation/kube-flannel.yml"
	metricsServerOnlineURL = "https://github.com/kubernetes-sigs/metrics-server/" +
		"releases/latest/download/components.yaml"

	officialImageRepository = "registry.k8s.io"
	cnImageRepository       = "registry.aliyuncs.com/google_containers"
)

var (
	ipvsPackages = []string{"ipset", "ipvsadm"}
	k8sPackages  = []string{"kubeadm", "kubelet", "kubectl", "cri-tools", "kubernetes-cni"}

	containerdPackages    = []string{"containerd.io"}
	dockerRuntimePackages = []string{
		"docker-ce",
		"docker-ce-cli",
		"containerd.io",
		"docker-buildx-plugin",
		"docker-compose-plugin",
		"docker-ce-rootless-extras",
	}
)

// Options controls offline bundle preparation.
type Options struct {
	// OutputDir receives the unpacked bundle tree.
	OutputDir string

	// ArchivePath receives the .tar.gz archive. Empty means OutputDir+".tar.gz".
	ArchivePath string

	// Runtime is "containerd" or "docker" and controls deb/docker contents.
	Runtime string

	// Version is the Kubernetes version, e.g. "v1.35.0".
	Version string

	// Mirror is "" for upstream registries and "cn" for Aliyun Kubernetes
	// package/image mirrors.
	Mirror string

	// ImageTool is the local OCI tool used to pull and export images.
	// MVP supports docker.
	ImageTool string
}

// Result describes the files produced by Prepare.
type Result struct {
	AssetsDir   string
	ArchivePath string
}

type commandRunner interface {
	Run(name string, args ...string) error
	RunOutput(name string, args ...string) (string, error)
}

type hostInfo struct {
	Distro   string
	Codename string
	Arch     string
}

type dependencies struct {
	Runner        commandRunner
	Download      func(url, dst string) error
	DetectHost    func() (hostInfo, error)
	EnsureDocker  func(context.Context) error
	EnsureK8s     func(context.Context, string, string) error
	ListImages    func(Options, commandRunner) ([]string, error)
	CreateArchive func(sourceDir, archivePath string) error
}

type execRunner struct{}

func (execRunner) Run(name string, args ...string) error {
	return xexec.Run(name, args...)
}

func (execRunner) RunOutput(name string, args ...string) (string, error) {
	return xexec.RunOutput(name, args...)
}

// Prepare downloads packages, manifests, and images into the standard assets
// tree, then writes a transport-friendly .tar.gz archive next to it.
func Prepare(ctx context.Context, opts Options) (Result, error) {
	return prepare(ctx, opts, defaultDependencies())
}

func defaultDependencies() dependencies {
	runner := execRunner{}
	return dependencies{
		Runner:        runner,
		Download:      xexec.Download,
		DetectHost:    detectHost,
		EnsureDocker:  aptrepo.EnsureDockerRepo,
		EnsureK8s:     aptrepo.EnsureK8sRepo,
		ListImages:    defaultImageList,
		CreateArchive: createTarGz,
	}
}

func prepare(ctx context.Context, opts Options, deps dependencies) (Result, error) {
	opts = normalizeOptions(opts)
	if err := validateOptions(opts); err != nil {
		return Result{}, err
	}

	assetsDir, err := filepath.Abs(opts.OutputDir)
	if err != nil {
		return Result{}, fmt.Errorf("resolve output dir: %w", err)
	}
	archivePath := opts.ArchivePath
	if archivePath == "" {
		archivePath = assetsDir + ".tar.gz"
	}
	archivePath, err = filepath.Abs(archivePath)
	if err != nil {
		return Result{}, fmt.Errorf("resolve archive path: %w", err)
	}

	host, err := deps.DetectHost()
	if err != nil {
		return Result{}, err
	}

	if err := ensureLayout(assetsDir); err != nil {
		return Result{}, err
	}

	minor := minorVersion(opts.Version)
	if err := deps.EnsureDocker(ctx); err != nil {
		return Result{}, fmt.Errorf("ensure docker apt repo: %w", err)
	}
	if err := deps.EnsureK8s(ctx, opts.Mirror, minor); err != nil {
		return Result{}, fmt.Errorf("ensure kubernetes apt repo: %w", err)
	}

	if err := downloadDebs(deps.Runner, filepath.Join(assetsDir, "deb", "ipvs"), ipvsPackages); err != nil {
		return Result{}, fmt.Errorf("download ipvs debs: %w", err)
	}
	if err := downloadDebs(deps.Runner, filepath.Join(assetsDir, "deb", "kubernetes"), k8sPackages); err != nil {
		return Result{}, fmt.Errorf("download kubernetes debs: %w", err)
	}
	if err := downloadRuntimeDebs(ctx, opts, deps, host, filepath.Join(assetsDir, "deb", "docker")); err != nil {
		return Result{}, err
	}

	if err := deps.Download(flannelOnlineURL, filepath.Join(assetsDir, flannelAssetFile)); err != nil {
		return Result{}, fmt.Errorf("download %s: %w", flannelAssetFile, err)
	}
	if err := deps.Download(metricsServerOnlineURL, filepath.Join(assetsDir, metricsServerAssetFile)); err != nil {
		return Result{}, fmt.Errorf("download %s: %w", metricsServerAssetFile, err)
	}

	if err := exportImages(opts, deps, filepath.Join(assetsDir, "images", "kubernetes-images.tar")); err != nil {
		return Result{}, err
	}

	if err := deps.CreateArchive(assetsDir, archivePath); err != nil {
		return Result{}, fmt.Errorf("create archive: %w", err)
	}

	return Result{AssetsDir: assetsDir, ArchivePath: archivePath}, nil
}

func normalizeOptions(opts Options) Options {
	if opts.OutputDir == "" {
		opts.OutputDir = defaultOutputDir
	}
	if opts.Runtime == "" {
		opts.Runtime = defaultRuntime
	}
	if opts.Version == "" {
		opts.Version = defaultVersion
	}
	if opts.ImageTool == "" {
		opts.ImageTool = defaultImageTool
	}
	return opts
}

func validateOptions(opts Options) error {
	switch opts.Runtime {
	case "containerd", "docker":
	default:
		return fmt.Errorf("invalid runtime %q (must be containerd or docker)", opts.Runtime)
	}
	switch opts.Mirror {
	case "", "cn":
	default:
		return fmt.Errorf("invalid mirror %q (must be empty or cn)", opts.Mirror)
	}
	if opts.ImageTool != "docker" {
		return fmt.Errorf("invalid image tool %q (only docker is supported)", opts.ImageTool)
	}
	return nil
}

func detectHost() (hostInfo, error) {
	info, err := osinfo.Detect()
	if err != nil {
		return hostInfo{}, fmt.Errorf("detect os: %w", err)
	}
	if err := osinfo.RequireSupported(info); err != nil {
		return hostInfo{}, err
	}
	arch, err := xexec.RunOutput("dpkg", "--print-architecture")
	if err != nil {
		return hostInfo{}, fmt.Errorf("dpkg --print-architecture: %w", err)
	}
	return hostInfo{
		Distro:   info.ID,
		Codename: info.Codename,
		Arch:     strings.TrimSpace(arch),
	}, nil
}

func ensureLayout(root string) error {
	for _, rel := range []string{
		"deb/docker",
		"deb/ipvs",
		"deb/kubernetes",
		"images",
	} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(rel)), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", rel, err)
		}
	}
	return nil
}

func downloadRuntimeDebs(ctx context.Context, opts Options, deps dependencies, host hostInfo, debDir string) error {
	switch opts.Runtime {
	case "containerd":
		if err := downloadDebs(deps.Runner, debDir, containerdPackages); err != nil {
			return fmt.Errorf("download containerd debs: %w", err)
		}
	case "docker":
		if err := downloadDebs(deps.Runner, debDir, dockerRuntimePackages); err != nil {
			return fmt.Errorf("download docker runtime debs: %w", err)
		}
		if err := downloadCRIDockerd(ctx, deps, host, debDir); err != nil {
			return err
		}
	}
	return nil
}

func downloadDebs(r commandRunner, dir string, packages []string) error {
	if err := os.MkdirAll(filepath.Join(dir, "partial"), 0o755); err != nil {
		return fmt.Errorf("mkdir apt archive dir: %w", err)
	}
	args := []string{
		"-o", "Dir::Cache::archives=" + dir,
		"install",
		"--download-only",
		"--reinstall",
		"-y",
	}
	args = append(args, packages...)
	if err := r.Run("apt-get", args...); err != nil {
		return err
	}
	return nil
}

func downloadCRIDockerd(_ context.Context, deps dependencies, host hostInfo, debDir string) error {
	url, err := cridockerd.BuildURL(cridockerd.DefaultVersion, host.Distro, host.Codename, host.Arch)
	if err != nil {
		return fmt.Errorf("build cri-dockerd url: %w", err)
	}
	dst := filepath.Join(debDir, filepath.Base(url))
	if err := deps.Download(url, dst); err != nil {
		return fmt.Errorf("download cri-dockerd: %w", err)
	}
	return nil
}

func exportImages(opts Options, deps dependencies, tarPath string) error {
	images, err := deps.ListImages(opts, deps.Runner)
	if err != nil {
		return fmt.Errorf("list kubernetes images: %w", err)
	}
	if len(images) == 0 {
		return fmt.Errorf("no kubernetes images selected")
	}
	if err := os.MkdirAll(filepath.Dir(tarPath), 0o755); err != nil {
		return fmt.Errorf("mkdir images dir: %w", err)
	}
	for _, image := range images {
		if err := deps.Runner.Run(opts.ImageTool, "pull", image); err != nil {
			return fmt.Errorf("%s pull %s: %w", opts.ImageTool, image, err)
		}
	}
	args := append([]string{"save", "-o", tarPath}, images...)
	if err := deps.Runner.Run(opts.ImageTool, args...); err != nil {
		return fmt.Errorf("%s save: %w", opts.ImageTool, err)
	}
	return nil
}

func defaultImageList(opts Options, r commandRunner) ([]string, error) {
	args := []string{"config", "images", "list", "--kubernetes-version=" + opts.Version}
	if opts.Mirror == "cn" {
		args = append(args, "--image-repository="+cnImageRepository)
	}
	out, err := r.RunOutput("kubeadm", args...)
	if err == nil {
		images := parseImageList(out)
		if len(images) > 0 {
			return addRuntimeImages(images, opts.Mirror), nil
		}
	}

	images, fallbackErr := fallbackImageList(opts)
	if fallbackErr != nil {
		if err != nil {
			return nil, fmt.Errorf("%w (kubeadm probe also failed: %v)", fallbackErr, err)
		}
		return nil, fallbackErr
	}
	if err != nil {
		log.Warn("kubeadm image list failed, using built-in image list for %s: %v", opts.Version, err)
	}
	return images, nil
}

func parseImageList(out string) []string {
	seen := map[string]bool{}
	var images []string
	for _, line := range strings.Split(out, "\n") {
		image := strings.TrimSpace(line)
		if image == "" || seen[image] {
			continue
		}
		seen[image] = true
		images = append(images, image)
	}
	return images
}

func fallbackImageList(opts Options) ([]string, error) {
	if opts.Version != defaultVersion {
		return nil, fmt.Errorf("kubeadm is required to derive images for Kubernetes %s", opts.Version)
	}
	repo := officialImageRepository
	if opts.Mirror == "cn" {
		repo = cnImageRepository
		return []string{
			repo + "/kube-apiserver:" + opts.Version,
			repo + "/kube-controller-manager:" + opts.Version,
			repo + "/kube-scheduler:" + opts.Version,
			repo + "/kube-proxy:" + opts.Version,
			repo + "/etcd:3.6.8-0",
			repo + "/coredns:v1.14.2",
			repo + "/pause:3.10.2",
			repo + "/pause:3.10",
		}, nil
	}
	return []string{
		repo + "/kube-apiserver:" + opts.Version,
		repo + "/kube-controller-manager:" + opts.Version,
		repo + "/kube-scheduler:" + opts.Version,
		repo + "/kube-proxy:" + opts.Version,
		repo + "/etcd:3.6.8-0",
		repo + "/coredns/coredns:v1.14.2",
		repo + "/pause:3.10.2",
		repo + "/pause:3.10",
	}, nil
}

func addRuntimeImages(images []string, mirror string) []string {
	repo := officialImageRepository
	if mirror == "cn" {
		repo = cnImageRepository
	}
	pause := repo + "/pause:3.10"
	for _, image := range images {
		if image == pause {
			return images
		}
	}
	return append(images, pause)
}

func minorVersion(version string) string {
	parts := strings.Split(version, ".")
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return version
}

func createTarGz(sourceDir, archivePath string) error {
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
		return fmt.Errorf("mkdir archive dir: %w", err)
	}
	file, err := os.Create(archivePath)
	if err != nil {
		return fmt.Errorf("create %s: %w", archivePath, err)
	}
	defer file.Close()

	gz := gzip.NewWriter(file)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	sourceDir, err = filepath.Abs(sourceDir)
	if err != nil {
		return err
	}
	archivePath, err = filepath.Abs(archivePath)
	if err != nil {
		return err
	}
	rootName := filepath.Base(sourceDir)

	return filepath.WalkDir(sourceDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if samePath(path, archivePath) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			header.Name = rootName
		} else {
			header.Name = filepath.ToSlash(filepath.Join(rootName, rel))
		}
		if d.IsDir() {
			header.Name += "/"
		}
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		if _, err := io.Copy(tw, in); err != nil {
			_ = in.Close()
			return err
		}
		return in.Close()
	})
}

func samePath(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return false
	}
	return aa == bb
}

func listArchiveEntries(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var entries []string
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		entries = append(entries, header.Name)
	}
	sort.Strings(entries)
	return entries, nil
}
