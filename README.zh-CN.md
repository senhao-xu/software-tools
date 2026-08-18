# xsh：Debian / Ubuntu 上的 Kubernetes 与 Docker 安装工具

[English](README.md)

`xsh` 是一个 Go 编写的单文件命令行工具，用来把一台干净的 Debian / Ubuntu 主机初始化为 Kubernetes 节点，或安装为独立 Docker 主机。除系统自带的 `apt-get`、`dpkg`、`systemctl` 等工具外无额外运行时依赖。

## 核心功能

- 一键初始化 Kubernetes 控制平面：`xsh k8s`；Worker 加入：`xsh k8s join`
- 支持 `containerd`（默认）和 `docker + cri-dockerd` 两种运行时
- 在线安装，或通过 `--assets-dir` 使用校验过的离线资源包
- `--mirror=cn` 将 Kubernetes apt 源与镜像仓库切换到国内镜像
- 独立 Docker 安装，支持交互选择版本：`xsh docker`
- 通过别名安装常用 apt 软件包：`xsh install`（`python`、`nodejs`、`java`、`maven` 等，未知名称原样透传）
- 卸载 Kubernetes 并可选卸载运行时：`xsh k8s uninstall`
- 各步骤尽量幂等；失败时按步骤 best-effort 回滚

## 系统要求

- Debian 12/13 或 Ubuntu 22.04/24.04，root 权限
- 至少 2 GB 内存 / 2 CPU / 20 GB 磁盘（kubeadm 基线）

## 安装

```bash
curl -L -o xsh.tar.gz "https://github.com/senhao-xu/software-tools/releases/latest/download/xsh_linux_$(dpkg --print-architecture).tar.gz" \
  && tar -xzf xsh.tar.gz && sudo install -m 0755 xsh /usr/local/bin/xsh && xsh version
```

Release 同时发布 `checksums.txt`（sha256），生产环境建议先校验。
源码构建：`make build`（需要 Go 1.25）。

## 快速上手

```bash
sudo xsh k8s                    # 一键初始化控制平面（containerd，v1.35.0）
sudo xsh k8s --mirror=cn        # 国内网络
sudo xsh k8s join --master=192.168.1.10:6443 --token=<token> \
  --discovery-token-ca-cert-hash=sha256:<hash>    # Worker 加入
sudo xsh docker                 # 独立 Docker（交互选版本）
sudo xsh docker -y --major=27   # 非交互，固定 27.x
sudo xsh install python nodejs java maven htop    # apt 别名 + 透传
sudo xsh k8s uninstall -y --remove-runtime=none   # 卸载 Kubernetes
```

控制平面装完后，join 命令会保存到 `/var/cache/xsh/join-command.sh`。
`docker` 和 `k8s` 在 `xsh install` 中是保留名称（会提示改用专用子命令）。

## 命令参考

### `xsh k8s` / `xsh k8s join`

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `--runtime` | `containerd` | `containerd` 或 `docker`（须与控制平面一致） |
| `--version` | `v1.35.0` | Kubernetes 版本（选择对应 minor apt 源） |
| `--mirror` | 空 | `cn` 使用国内 Kubernetes 源和镜像 |
| `--assets-dir` | 空 | 离线资源目录 |
| `-y`, `--yes` | `false` | 跳过覆盖确认 |
| `--hostname` / `--advertise` | `master` / 自动探测 | 节点主机名 / apiserver 地址 |
| `--pod-cidr` / `--service-cidr` | flannel 默认值 | Pod / Service 网段 |
| `--master` / `--token` / `--discovery-token-ca-cert-hash` | 必填（仅 join） | 加入集群所需参数 |

版本说明：`--version` 只选择 minor apt 源（如 `v1.35.0` -> `v1.35`），不固定
patch，apt 会安装该 minor 源中当前最新的 patch 包。

### `xsh k8s bundle` - 离线资源包

在有网主机上（需本机 Docker 可用）下载 `.deb`、CNI/metrics-server YAML
和镜像归档，生成 `<output-dir>/` 目录和 `.tar.gz` 压缩包。拷贝到离线主机
解压后用 `sudo xsh k8s --assets-dir ./xsh-k8s-offline` 安装。安装前会先校验
离线包，缺失必需资源会直接失败，不会静默回退到在线下载。

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `--runtime` | `containerd` | 为 `containerd` 或 `docker` 准备资源 |
| `--version` | `v1.35.0` | Kubernetes 版本 |
| `--mirror` | 空 | `cn` 使用国内镜像 |
| `--output-dir` | `xsh-k8s-offline` | 输出目录 |
| `--archive` | `<output-dir>.tar.gz` | 输出压缩包路径 |

### `xsh k8s uninstall`

执行 `kubeadm reset`、purge Kubernetes 包，并删除 Kubernetes / CNI / etcd /
kubelet 目录及 Kubernetes apt 源和 keyring。

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `--remove-runtime` | `ask` | 运行时卸载：`ask`、`none`、`docker`、`containerd`、`all`、`auto` |
| `--cri-runtime` | `auto` | `kubeadm reset` 的 CRI socket：`auto`、`containerd`、`docker` |
| `-y`, `--yes` | `false` | 跳过卸载确认 |

运行时卸载是显式选择项：只有明确指定时才会删除 Docker/containerd 的包和数据。

### `xsh docker`

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `--major` | `0` | 固定 Docker CE 主版本；`0` 表示交互选择 |
| `-y`, `--yes` | `false` | 跳过提示，安装最新版 |

### `xsh install`

别名展开为对应的 Debian 包组（`python` -> `python3 python3-pip
python-is-python3`；`nodejs` -> 先配 NodeSource 22.x 源；`java` -> 先配
Adoptium 源并安装 Temurin 21 JDK；`maven` -> maven），合并为一次
`apt-get install -y`，需要时先执行配源钩子；执行过钩子后会再刷新一次
apt 索引使新源生效。

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `--no-update` | `false` | 跳过开头的 `apt-get update` |
| `-y`, `--yes` | `false` | 跳过安装确认提示 |

## 常见问题

- `apt-get install kubeadm` 404 / 慢，或 `kubeadm init` 拉镜像卡住：
  `pkgs.k8s.io` 和 Google 镜像仓库可能被墙 - 使用 `--mirror=cn` 或离线包。
- 指定 `v1.35.0` 但离线包里是 `1.35.5`：正常，patch 不固定（见版本说明）。
- Worker join 报运行时错误：`--runtime` 必须与控制平面一致。
- `kubelet` 在 `kubeadm init` 前不断重启：正常现象，配置由 init 写入。

需要 `apt-get` / `dpkg` / `kubeadm` 的原始输出时加 `-v` / `--verbose`。

## 开发与测试

```bash
make build      # bin/xsh
make fmt vet    # gofmt + go vet
go test ./...   # 单元测试（纯函数，无需 root）
```

Release 由 `.github/workflows/release.yml` 构建（linux/amd64 + arm64）。

## 许可证

Apache License 2.0，详见 `LICENSE`。
