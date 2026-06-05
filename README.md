# xsh — Debian / Ubuntu Kubernetes & Docker Installer

[中文](README.zh-CN.md)

A single Go binary that turns a clean Debian 12/13 or Ubuntu 22.04/24.04 host
into either a working Kubernetes node (master one-shot, or worker join) or a
standalone Docker host.

The two install paths consolidate two pre-existing shell scripts (an offline
Kubernetes installer and the online Docker recipe at `docker.senhao.eu.cc`)
into one ~10 MB binary that runs without external runtime dependencies beyond
the standard distribution tools (`apt-get`, `dpkg`, `systemctl`, ...).

## Features

- One-shot Kubernetes master install (containerd by default, optional
  docker + cri-dockerd)
- Worker `join` subcommand sharing the same prep / runtime / kube install
  chain
- Kubernetes uninstall subcommand with explicit runtime removal choices
- Standalone Docker installer (mirrors the docker.senhao.eu.cc recipe)
- Dual install mode: online installs, or validated Kubernetes offline bundles
  via `--assets-dir`
- `--mirror=cn` switches the Kubernetes apt repo and image repository to
  Aliyun; Docker packages still use Docker's official apt repo
- Detailed install logs show the selected online/offline path, package lists,
  config files, and service starts
- Step-by-step rollback on failure; idempotent re-runs

## System Requirements

- One of the following Debian-family hosts:
  - Debian 12 (bookworm)
  - Debian 13 (trixie)
  - Ubuntu 22.04 LTS (jammy)
  - Ubuntu 24.04 LTS (noble)
- root privilege (the binary checks `euid` at startup)
- At least 2 GB RAM / 2 CPU / 20 GB disk (the kubeadm baseline)

## Install

Pre-built Linux binaries (`amd64` + `arm64`) are published on the
[Releases page](https://github.com/senhao-xu/software-tools/releases). Download
the latest release directly by picking the archive matching your host
architecture:

```bash
curl -L -o xsh.tar.gz "https://github.com/senhao-xu/software-tools/releases/latest/download/xsh_linux_$(dpkg --print-architecture).tar.gz" && tar -xzf xsh.tar.gz && sudo install -m 0755 xsh /usr/local/bin/xsh && xsh version
```

To pin an exact release instead of following `latest`, use tag `v202606051643`.

```bash
TAG=v202606051643
VERSION=202606051643
curl -L -o xsh.tar.gz "https://github.com/senhao-xu/software-tools/releases/download/${TAG}/xsh_${VERSION}_linux_$(dpkg --print-architecture).tar.gz" && tar -xzf xsh.tar.gz && sudo install -m 0755 xsh /usr/local/bin/xsh && xsh version
```

Each release also publishes `checksums.txt` (sha256) alongside the archives;
verify it before installing on production hosts.

## Quick Start

### Master one-shot

```bash
sudo ./xsh k8s
```

Behind the Great Firewall:

```bash
sudo ./xsh k8s --mirror=cn
```

### Worker join

After the master finishes, the join command is saved to
`/var/cache/xsh/join-command.sh`. You can `scp` that file to the worker
and run it directly, or use the `xsh k8s join` wrapper:

```bash
sudo ./xsh k8s join \
  --master=192.168.1.10:6443 \
  --token=<token> \
  --discovery-token-ca-cert-hash=sha256:<hash>
```

`xsh k8s join` runs the same Step 0–3 pipeline (detect, sysprep, runtime,
kube install) as the master, then issues `kubeadm join`. Step 5 (CNI) is
skipped — the master already deployed flannel cluster-wide.

### Standalone Docker

```bash
sudo ./xsh docker              # latest docker-ce
sudo ./xsh docker --major=27   # pin to 27.x
```

### Uninstall Kubernetes

```bash
sudo ./xsh k8s uninstall
```

The default `--remove-runtime=ask` removes Kubernetes state first and asks
whether to also remove the container runtime. `-y` skips the Kubernetes
confirmation but does not remove Docker/containerd unless you say so
explicitly:

```bash
sudo ./xsh k8s uninstall -y --remove-runtime=none
sudo ./xsh k8s uninstall -y --remove-runtime=containerd
sudo ./xsh k8s uninstall -y --remove-runtime=docker
sudo ./xsh k8s uninstall -y --remove-runtime=all
sudo ./xsh k8s uninstall -y --remove-runtime=auto
```

## CLI Reference

### `xsh k8s` — master one-shot

| Flag             | Default              | Description                                                  |
|------------------|----------------------|--------------------------------------------------------------|
| `--runtime`      | `containerd`         | Container runtime: `containerd` or `docker`                  |
| `--version`      | `v1.35.0`            | Kubernetes version passed to kubeadm and image selection     |
| `--pod-cidr`     | `10.244.0.0/16`      | Pod network CIDR (flannel-locked; do not change)             |
| `--service-cidr` | `10.96.0.0/12`       | Service CIDR                                                 |
| `--hostname`     | `master`             | Node hostname (set via `hostnamectl`)                        |
| `--advertise`    | auto-detect          | `--apiserver-advertise-address`; auto = outbound UDP probe   |
| `--mirror`       | _empty_              | `cn` switches Kubernetes apt repo + image registry to Aliyun |
| `--assets-dir`   | _empty_              | Offline assets directory (see below)                         |
| `-y`, `--yes`    | `false`              | Skip the Step 0 overwrite prompt (defaults to Overwrite)     |
| `-v`, `--verbose`| `false`              | Pass-through verbose output from apt/dpkg/kubeadm            |

Version note: online Kubernetes package installation and `xsh k8s bundle`
select the Kubernetes minor apt repo from `--version` (`v1.35.0` -> `v1.35`),
but the `.deb` packages are not patch-pinned. Apt installs or downloads the
newest patch currently available in that minor repo.

### `xsh k8s join` — worker join

All `xsh k8s` flags (except the kubeadm-init-specific ones: `--pod-cidr`,
`--service-cidr`, `--hostname`, `--advertise`) plus the three required join
inputs:

| Flag                              | Required | Description                                           |
|-----------------------------------|----------|-------------------------------------------------------|
| `--master`                        | yes      | Control-plane endpoint, e.g. `10.0.0.10:6443`         |
| `--token`                         | yes      | Bootstrap token from master                           |
| `--discovery-token-ca-cert-hash`  | yes      | `sha256:...` CA hash from master                      |

### `xsh k8s bundle` — offline bundle

| Flag           | Default             | Description                                      |
|----------------|---------------------|--------------------------------------------------|
| `--runtime`    | `containerd`        | Prepare assets for `containerd` or `docker`      |
| `--version`    | `v1.35.0`           | Kubernetes version used for repo/image selection |
| `--mirror`     | _empty_             | `cn` uses Aliyun for Kubernetes repo/images      |
| `--output-dir` | `xsh-k8s-offline`   | Output directory for offline assets              |
| `--archive`    | `<output-dir>.tar.gz` | Output archive path                            |

### `xsh k8s uninstall` — remove Kubernetes

Uninstalls Kubernetes control-plane or worker state with best-effort cleanup:
`kubeadm reset`, stop kubelet, unhold/purge Kubernetes packages
(`kubeadm`, `kubelet`, `kubectl`, `kubernetes-cni`, `cri-tools`), and remove
Kubernetes/CNI/etcd/kubelet directories plus the Kubernetes apt repo/keyring.

| Flag               | Default | Description                                                            |
|--------------------|---------|------------------------------------------------------------------------|
| `--remove-runtime` | `ask`   | Runtime removal: `ask`, `none`, `docker`, `containerd`, `all`, `auto`  |
| `--cri-runtime`    | `auto`  | CRI socket for `kubeadm reset`: `auto`, `containerd`, or `docker`       |
| `-y`,`--yes`       | `false` | Skip the Kubernetes uninstall confirmation                             |

Runtime cleanup is opt-in. `--remove-runtime=docker` stops Docker and
cri-dockerd, purges Docker packages plus `cri-dockerd`, and removes
`/etc/docker` and `/var/lib/docker`. `--remove-runtime=containerd` stops
containerd, purges `containerd.io`, and removes `/etc/containerd` and
`/var/lib/containerd`. `--remove-runtime=all` removes both runtime data sets.
`--remove-runtime=auto` removes every detected Docker/containerd runtime.
`--cri-runtime` only selects the reset socket; it does not opt in to runtime
package or data removal.

### `xsh docker` — standalone Docker

| Flag         | Default | Description                                  |
|--------------|---------|----------------------------------------------|
| `--major`    | `0`     | Pin docker-ce major (0 = latest)             |
| `-y`,`--yes` | `false` | Skip the Step 0 overwrite prompt             |

## Offline Mode

Prepare a Kubernetes offline bundle on a networked host that matches the
target distro family / architecture. The bundle command uses Docker to pull
and export Kubernetes image archives, so Docker must be available on the
networked preparation host:

```bash
sudo ./xsh k8s bundle --runtime=containerd --version v1.35.0 --output-dir ./xsh-k8s-offline
```

Important version behavior: `--version v1.35.0` selects the `v1.35` Kubernetes
apt repo and the kubeadm image list for `v1.35.0`. The apt download step is not
patch-pinned, so it may place newer same-minor packages in the bundle, such as
`kubeadm_1.35.5-1.1_amd64.deb`. During offline installation, `dpkg` installs the
exact `.deb` files in the bundle; `xsh` only warns if the installed kubeadm
patch differs from `--version`.

The command leaves both `./xsh-k8s-offline/` and
`./xsh-k8s-offline.tar.gz`. Move the archive to the offline host, extract it,
then pass the extracted directory to the installer:

```bash
sudo ./xsh k8s --assets-dir ./xsh-k8s-offline
sudo ./xsh k8s join --assets-dir ./xsh-k8s-offline \
  --master=192.168.1.10:6443 \
  --token=<token> \
  --discovery-token-ca-cert-hash=sha256:<hash>
```

When `--assets-dir=<path>` is set, `xsh` validates the bundle before making
system changes. Missing required assets fail fast with a clear error instead
of falling back to online downloads. Once validation passes, the install uses
the bundle's local `.deb`, YAML, and image archive files for the required
offline assets.

Expected layout under `--assets-dir`:

```
<assets-dir>/
├── deb/
│   ├── docker/             # containerd.io_*.deb (+ docker-ce, cri-dockerd, ... for docker runtime)
│   ├── ipvs/               # ipset_*.deb, ipvsadm_*.deb
│   └── kubernetes/         # kubeadm/kubelet/kubectl/cri-tools/kubernetes-cni .deb
├── images/                 # *.tar (ctr/docker import; only used during kubeadm init)
├── kube-flannel.yml        # CNI manifest
└── components.yaml         # metrics-server manifest
```

The bundle command currently targets Kubernetes install flows (`xsh k8s` and
`xsh k8s join`). Standalone `xsh docker` offline bundle preparation is not part
of this MVP.

## Reading install logs

Default `[INFO]` logs now summarize decisions that usually matter during
troubleshooting:

- whether each step chose offline assets or online repositories
- which `.deb` files are downloaded into a bundle or installed from a bundle
- which Kubernetes minor repo is selected from `--version`
- which Docker/containerd packages and config files are used
- when system services are enabled and started

Use `-v` / `--verbose` when you also need raw `apt-get`, `dpkg`, `kubeadm`,
`kubectl`, `docker`, or `ctr` output.

## How rollback works

- Each Step records its config writes and reverts them on failure. Steps
  unwind in reverse order: kubeadm init → kube install → runtime → sysprep.
- The Step 0 cleanup (`detect.Cleanup`) is a broader sweep that runs *before*
  install begins (when the user picks `Overwrite`). It removes packages,
  apt repos, keyrings, and the `/etc/{docker,containerd,kubernetes,cni}`
  trees — bigger scope than a single Step's Rollback.
- `xsh k8s uninstall` is the explicit post-install cleanup command. It always
  removes Kubernetes state and only removes Docker/containerd when
  `--remove-runtime` or the interactive prompt selects that runtime.
- A Step's Rollback only undoes what *that step* wrote (e.g. the containerd
  config file, the kubelet apt-mark hold). Apt packages and keyrings stay
  put — they are detect.Cleanup's responsibility, so a subsequent
  `Overwrite` reinstall is cheap.
- `kubeadm reset` failures during rollback are logged as WARN and do not
  block subsequent step rollbacks.

## Troubleshooting

### `apt-get install kubeadm` fails with 404
`pkgs.k8s.io` is sometimes blocked from China. Try `--mirror=cn`.

### I requested `v1.35.0`, but the bundle contains `1.35.5` packages
This is expected with the current package strategy. `--version v1.35.0`
selects the `v1.35` apt repo; apt then downloads the newest patch available in
that minor, for example `1.35.5-1.1`. The install log prints both the requested
version and the exact `.deb` files used.

### `kubeadm init` hangs at "pulling images"
Same cause. Use `--mirror=cn` (routes images through
`registry.aliyuncs.com/google_containers`) or supply pre-pulled tars under
`<assets-dir>/images/`.

### `kubelet` keeps restarting before `kubeadm init` runs
This is normal — `/var/lib/kubelet/config.yaml` is only written by
`kubeadm init`. `xsh` downgrades the pre-init crash-loop to a WARN and
continues. After `kubeadm init` the unit should stabilise.

If it keeps restarting *after* init, check `swapon --show`. `xsh` calls
`swapoff -a` and comments `/etc/fstab` swap lines, but a swap file added by
a separate systemd unit (`swap.img`) may still be active.

### Worker join fails with "unknown runtime"
The worker's runtime must match the master's. `xsh k8s join --runtime=docker`
on a master that ran with `--runtime=containerd` (or vice versa) will fail
at `kubeadm join`. Pick the same `--runtime` on both sides.

### `docker run hello-world` hangs on standalone install
The default `daemon.json` shipped by `xsh docker` does not configure any
registry mirror. Edit `/etc/docker/daemon.json`, add a
`registry-mirrors: [...]` block, then `systemctl restart docker`.

## Build

```bash
make build      # produces bin/xsh
make fmt vet    # gofmt + go vet
go test ./...   # unit tests (pure functions only — no Linux/root needed)
```

Releases such as `v202606051643` are published by
`.github/workflows/release.yml`, which cross-compiles `linux/amd64` +
`linux/arm64`, attaches `checksums.txt`, and publishes to GitHub Releases. The
binary stamps `main.version` / `main.commit` / `main.date` via `-ldflags`
so `xsh version` reports the exact build.

## Project Status

- PR1–PR13 complete: CLI skeleton, detect/cleanup, sysprep, runtime
  (containerd + docker), kube install, kubeadm init, network, worker
  join, standalone docker, rollback hardening + unit tests + this README,
  plus multi-OS support (Debian 12/13, Ubuntu 22.04/24.04).
- Integration tests run manually on clean Debian 12 and Debian 13 VMs
  across four paths: offline/containerd, offline/docker, online/containerd,
  online/docker with `--mirror=cn`. Ubuntu 22.04 / 24.04 are code-level
  supported (shared apt-repo + cri-dockerd artifact mapping, unit-tested)
  but the end-to-end install matrix has not yet been run on Ubuntu in CI
  or by hand — treat Ubuntu support as beta until that pass lands.
- Not supported (intentionally out of scope): multi-master HA control plane,
  Kubernetes version upgrade,
  CentOS / Rocky / AlmaLinux / RHEL / SUSE / Arch / other non-Debian-family
  hosts, CNIs other than flannel.

## License

See `LICENSE`.
