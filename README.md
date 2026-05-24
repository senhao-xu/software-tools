# xsh — Debian / Ubuntu Kubernetes & Docker Installer

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
- Standalone Docker installer (mirrors the docker.senhao.eu.cc recipe)
- Dual install mode: offline (`--assets-dir`) or online (auto-fallback to
  official apt repos + GitHub releases)
- `--mirror=cn` switches packages to `mirrors.aliyun.com` and Kubernetes
  images to `registry.aliyuncs.com/google_containers`
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
[Releases page](https://github.com/senhao-xu/software-tools/releases). Pick
the archive matching your host architecture:

```bash
# Replace <VERSION> with the tag you want (e.g. 0.1.0) and <ARCH> with
# amd64 (x86_64) or arm64 (aarch64).
VERSION=0.0.1
ARCH=amd64

curl -L -o xsh.tar.gz \
  "https://github.com/senhao-xu/software-tools/releases/download/v${VERSION}/xsh_${VERSION}_linux_${ARCH}.tar.gz"
tar -xzf xsh.tar.gz
sudo install -m 0755 xsh /usr/local/bin/xsh
xsh version
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

## CLI Reference

### `xsh k8s` — master one-shot

| Flag             | Default              | Description                                                  |
|------------------|----------------------|--------------------------------------------------------------|
| `--runtime`      | `containerd`         | Container runtime: `containerd` or `docker`                  |
| `--version`      | `v1.35.0`            | Kubernetes version                                           |
| `--pod-cidr`     | `10.244.0.0/16`      | Pod network CIDR (flannel-locked; do not change)             |
| `--service-cidr` | `10.96.0.0/12`       | Service CIDR                                                 |
| `--hostname`     | `master`             | Node hostname (set via `hostnamectl`)                        |
| `--advertise`    | auto-detect          | `--apiserver-advertise-address`; auto = outbound UDP probe   |
| `--mirror`       | _empty_              | `cn` switches apt repo + image registry to Aliyun            |
| `--assets-dir`   | _empty_              | Offline assets directory (see below)                         |
| `-y`, `--yes`    | `false`              | Skip the Step 0 overwrite prompt (defaults to Overwrite)     |
| `-v`, `--verbose`| `false`              | Pass-through verbose output from apt/dpkg/kubeadm            |

### `xsh k8s join` — worker join

All `xsh k8s` flags (except the kubeadm-init-specific ones: `--pod-cidr`,
`--service-cidr`, `--hostname`, `--advertise`) plus the three required join
inputs:

| Flag                              | Required | Description                                           |
|-----------------------------------|----------|-------------------------------------------------------|
| `--master`                        | yes      | Control-plane endpoint, e.g. `10.0.0.10:6443`         |
| `--token`                         | yes      | Bootstrap token from master                           |
| `--discovery-token-ca-cert-hash`  | yes      | `sha256:...` CA hash from master                      |

### `xsh docker` — standalone Docker

| Flag         | Default | Description                                  |
|--------------|---------|----------------------------------------------|
| `--major`    | `0`     | Pin docker-ce major (0 = latest)             |
| `-y`,`--yes` | `false` | Skip the Step 0 overwrite prompt             |

## Offline Mode

When `--assets-dir=<path>` is set and the expected subdirectories exist,
`xsh` switches each step to `dpkg -i` / local `kubectl apply -f`. Anything
missing falls back to the online path for that component only — partial
offline mode is supported.

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

The resource lookup order matches the `--assets-dir` flag, then the binary's
own directory (`./assets/`), then `/var/cache/xsh/v<ver>/`.

## How rollback works

- Each Step records its config writes and reverts them on failure. Steps
  unwind in reverse order: kubeadm init → kube install → runtime → sysprep.
- The Step 0 cleanup (`detect.Cleanup`) is a broader sweep that runs *before*
  install begins (when the user picks `Overwrite`). It removes packages,
  apt repos, keyrings, and the `/etc/{docker,containerd,kubernetes,cni}`
  trees — bigger scope than a single Step's Rollback.
- A Step's Rollback only undoes what *that step* wrote (e.g. the containerd
  config file, the kubelet apt-mark hold). Apt packages and keyrings stay
  put — they are detect.Cleanup's responsibility, so a subsequent
  `Overwrite` reinstall is cheap.
- `kubeadm reset` failures during rollback are logged as WARN and do not
  block subsequent step rollbacks.

## Troubleshooting

### `apt-get install kubeadm` fails with 404
`pkgs.k8s.io` is sometimes blocked from China. Try `--mirror=cn`.

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

Tagged releases (`vX.Y.Z`) trigger `.github/workflows/release.yml`, which
runs [GoReleaser](https://goreleaser.com/) to cross-compile `linux/amd64` +
`linux/arm64`, attach `checksums.txt`, and publish to GitHub Releases. The
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
  Kubernetes version upgrade, uninstall subcommand,
  CentOS / Rocky / AlmaLinux / RHEL / SUSE / Arch / other non-Debian-family
  hosts, CNIs other than flannel.

## License

See `LICENSE`.
