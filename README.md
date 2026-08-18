# xsh - Debian / Ubuntu Kubernetes & Docker Installer

[中文](README.zh-CN.md)

A single Go binary that turns a clean Debian 12/13 or Ubuntu 22.04/24.04 host
into a Kubernetes node (master one-shot, or worker join) or a standalone
Docker host. No runtime dependencies beyond the standard distribution tools
(`apt-get`, `dpkg`, `systemctl`, ...).

## Features

- One-shot Kubernetes master install (`xsh k8s`) and worker join (`xsh k8s join`)
- `containerd` (default) or `docker + cri-dockerd` runtimes
- Offline install via validated bundles (`--assets-dir`)
- `--mirror=cn` switches Kubernetes apt repo and image registry to Aliyun
- Standalone Docker installer with interactive version choice (`xsh docker`)
- `xsh install` for apt packages with friendly aliases (`python`, `nodejs`,
  `java`, `maven`, ...; unknown names pass through verbatim)
- Kubernetes uninstall (`xsh k8s uninstall`) with opt-in runtime removal
- Step-by-step rollback on failure; idempotent re-runs

## Requirements

- Debian 12/13 or Ubuntu 22.04/24.04, root privilege
- 2 GB RAM / 2 CPU / 20 GB disk (kubeadm baseline)

## Install

```bash
curl -L -o xsh.tar.gz "https://github.com/senhao-xu/software-tools/releases/latest/download/xsh_linux_$(dpkg --print-architecture).tar.gz" \
  && tar -xzf xsh.tar.gz && sudo install -m 0755 xsh /usr/local/bin/xsh && xsh version
```

Releases publish `checksums.txt` (sha256); verify before production installs.
Build from source: `make build` (needs Go 1.25).

## Quick Start

```bash
sudo xsh k8s                    # master one-shot (containerd, v1.35.0)
sudo xsh k8s --mirror=cn        # behind the Great Firewall
sudo xsh k8s join --master=192.168.1.10:6443 --token=<token> \
  --discovery-token-ca-cert-hash=sha256:<hash>    # worker join
sudo xsh docker                 # standalone Docker (interactive version choice)
sudo xsh docker -y --major=27   # non-interactive, pinned to 27.x
sudo xsh install python nodejs java maven htop    # apt aliases + passthrough
sudo xsh k8s uninstall -y --remove-runtime=none   # uninstall Kubernetes
```

The join command is also saved to `/var/cache/xsh/join-command.sh` after a
master install. `docker` and `k8s` are reserved in `xsh install` (hinted to
the dedicated subcommands).

## Command Reference

### `xsh k8s` / `xsh k8s join`

| Flag                              | Default     | Description                                        |
|-----------------------------------|-------------|----------------------------------------------------|
| `--runtime`                       | `containerd`| `containerd` or `docker` (must match the master)   |
| `--version`                       | `v1.35.0`   | Kubernetes version (selects the minor apt repo)    |
| `--mirror`                        | _empty_     | `cn` uses Aliyun for Kubernetes repo/images        |
| `--assets-dir`                    | _empty_     | Offline assets directory                           |
| `-y`, `--yes`                     | `false`     | Skip the overwrite prompt                          |
| `--hostname` / `--advertise`      | `master` / auto | Node hostname / apiserver advertise address    |
| `--pod-cidr` / `--service-cidr`   | flannel defaults | Pod / Service CIDR                           |
| `--master` / `--token` / `--discovery-token-ca-cert-hash` | required (join only) | Join inputs |

Version note: `--version` selects the minor apt repo (e.g. `v1.35.0` ->
`v1.35`); patch is not pinned, apt installs the newest patch in that repo.

### `xsh k8s bundle` - offline bundle

Prepares `.deb` files, CNI/metrics-server YAML, and image archives on a
networked host (Docker required), producing `<output-dir>/` + `.tar.gz`.
Move the archive to the offline host, extract, then install with
`sudo xsh k8s --assets-dir ./xsh-k8s-offline`. Bundles are validated before
any system change; missing assets fail fast instead of falling back online.

| Flag           | Default             | Description                          |
|----------------|---------------------|--------------------------------------|
| `--runtime`    | `containerd`        | Prepare for `containerd` or `docker` |
| `--version`    | `v1.35.0`           | Kubernetes version                   |
| `--mirror`     | _empty_             | `cn` uses Aliyun                     |
| `--output-dir` | `xsh-k8s-offline`   | Output directory                     |
| `--archive`    | `<output-dir>.tar.gz` | Output archive path                |

### `xsh k8s uninstall`

Runs `kubeadm reset`, purges Kubernetes packages, and removes Kubernetes/CNI/
etcd/kubelet directories plus the Kubernetes apt repo/keyring.

| Flag               | Default | Description                                                           |
|--------------------|---------|-----------------------------------------------------------------------|
| `--remove-runtime` | `ask`   | Runtime removal: `ask`, `none`, `docker`, `containerd`, `all`, `auto` |
| `--cri-runtime`    | `auto`  | CRI socket for `kubeadm reset`: `auto`, `containerd`, `docker`        |
| `-y`, `--yes`      | `false` | Skip the uninstall confirmation                                      |

Runtime removal is opt-in: Docker/containerd packages and data are only
removed when explicitly selected.

### `xsh docker`

| Flag         | Default | Description                                  |
|--------------|---------|----------------------------------------------|
| `--major`    | `0`     | Pin docker-ce major (0 = interactive choice) |
| `-y`, `--yes`| `false` | Skip prompts, install latest                 |

### `xsh install`

Expands aliases to their Debian package set (`python` -> `python3
python3-pip python-is-python3`; `nodejs` -> NodeSource 22.x repo + nodejs;
`java` -> Adoptium repo + Temurin 21 JDK; `maven` -> maven), merges them into
one `apt-get install -y`, and runs repo-setup hooks first when needed. After
any hook ran, the apt index is refreshed once so new sources are visible.

| Flag           | Default | Description                            |
|----------------|---------|----------------------------------------|
| `--no-update`  | `false` | Skip the initial `apt-get update`      |
| `-y`, `--yes`  | `false` | Skip the install confirmation prompt   |

## Troubleshooting

- `apt-get install kubeadm` 404 / slow, or `kubeadm init` hangs pulling
  images: `pkgs.k8s.io` and Google registries may be blocked - use
  `--mirror=cn` or an offline bundle.
- Bundle contains `1.35.5` when `v1.35.0` was requested: expected; patch is
  not pinned (see version note above).
- Worker join fails with a runtime error: `--runtime` must match the master's.
- `kubelet` crash-loops before `kubeadm init`: normal; config is only written
  by init.

Use `-v` / `--verbose` for raw `apt-get` / `dpkg` / `kubeadm` output.

## Build

```bash
make build      # bin/xsh
make fmt vet    # gofmt + go vet
go test ./...   # unit tests (pure functions, no root needed)
```

Releases are built by `.github/workflows/release.yml` (linux/amd64 + arm64).

## License

Apache License 2.0 - see `LICENSE`.
