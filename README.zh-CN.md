# xsh：Debian / Ubuntu 上的 Kubernetes 与 Docker 安装工具

[English](README.md)

`xsh` 是一个 Go 编写的单文件命令行工具，用来把一台干净的 Debian / Ubuntu 主机初始化为 Kubernetes 节点，或安装为独立 Docker 主机。它把 Kubernetes 初始化、Worker 加入、离线资源打包、Docker 在线安装等流程收敛到一个二进制文件里，运行时不依赖 Python、Node.js 等额外运行环境，只调用系统自带的 `apt-get`、`dpkg`、`systemctl`、`kubeadm`、`kubectl` 等工具。

项目当前命令名以代码为准：二进制为 `xsh`，主入口在 `cmd/xsh/main.go`。

## 核心功能

- 一键初始化 Kubernetes 控制平面：`xsh k8s`
- 将当前节点加入已有集群：`xsh k8s join`
- 为 Kubernetes 安装流程准备离线资源包：`xsh k8s bundle`
- 安装独立 Docker CE 环境：`xsh docker`
- 支持 `containerd` 和 `docker + cri-dockerd` 两种 Kubernetes 容器运行时
- 支持在线安装，也支持通过 `--assets-dir` 使用 Kubernetes 离线资源
- 支持 `--mirror=cn`，将 Kubernetes apt 源与镜像仓库切换到国内镜像
- 安装前检测已有 Docker / containerd / kubeadm / kubelet 等组件，并可按提示清理后重装
- 各安装步骤尽量保持可重复执行；失败时按步骤做 best-effort 回滚

## 适用场景

`xsh` 适合以下场景：

- 在 Debian 12 / 13 或 Ubuntu 22.04 / 24.04 上快速搭建单控制平面 Kubernetes 集群
- 在网络受限环境中，先在有网主机准备 Kubernetes 离线包，再拷贝到目标主机安装
- 批量初始化 Worker 节点，并确保运行时、kubeadm 参数和主节点一致
- 只需要 Docker CE、Buildx、Compose 等日常容器工具的服务器

不适合或暂未覆盖：

- 多控制平面高可用集群
- Kubernetes 版本升级流程
- 专门的卸载子命令
- Debian / Ubuntu 之外的发行版，例如 CentOS、Rocky、AlmaLinux、RHEL、SUSE、Arch
- flannel 之外的 CNI 自动部署

## 系统要求

支持的操作系统：

| 发行版 | 版本 |
| --- | --- |
| Debian | 12 bookworm、13 trixie |
| Ubuntu | 22.04 jammy、24.04 noble |

建议的 Kubernetes 节点资源：

- 至少 2 CPU
- 至少 2 GB 内存
- 至少 20 GB 磁盘
- 有可用的 `apt` 软件源，或提前准备完整的 Kubernetes 离线资源包

安装命令会修改系统服务、apt 源、内核参数、swap、Docker / containerd / Kubernetes 配置等内容。请在目标主机上用 `sudo` 或 root 身份运行，并优先在测试机验证流程。

## 安装 xsh

### 从 Release 安装

项目发布 Linux `amd64` 和 `arm64` 压缩包。将 `<VERSION>` 替换为目标版本号，将 `<ARCH>` 替换为 `amd64` 或 `arm64`：

```bash
VERSION=0.0.1
ARCH=amd64

curl -L -o xsh.tar.gz \
  "https://github.com/senhao-xu/software-tools/releases/download/v${VERSION}/xsh_${VERSION}_linux_${ARCH}.tar.gz"
tar -xzf xsh.tar.gz
sudo install -m 0755 xsh /usr/local/bin/xsh
xsh version
```

Release 中同时发布 `checksums.txt`，生产环境安装前建议校验 sha256。

### 从源码构建

本项目使用 Go module，模块名为 `xsh`，`go.mod` 当前声明 Go `1.25.0`。

```bash
make build

# 产物位置
./bin/xsh version
```

等价的直接构建命令：

```bash
go build -o bin/xsh ./cmd/xsh
```

## 基础用法

### 初始化 Kubernetes 控制平面

默认运行时是 `containerd`，默认 Kubernetes 版本是 `v1.35.0`：

```bash
sudo xsh k8s
```

国内网络环境可使用：

```bash
sudo xsh k8s --mirror=cn
```

使用 Docker 作为 Kubernetes 运行时：

```bash
sudo xsh k8s --runtime=docker
```

指定控制平面地址、主机名和版本：

```bash
sudo xsh k8s \
  --version=v1.35.0 \
  --hostname=master \
  --advertise=192.168.1.10
```

安装完成后，工具会将 Worker 加入命令保存到：

```text
/var/cache/xsh/join-command.sh
```

可以在控制平面节点上验证：

```bash
kubectl get nodes
kubectl get pods -A
```

### 加入 Worker 节点

在 Worker 上运行：

```bash
sudo xsh k8s join \
  --master=192.168.1.10:6443 \
  --token=<token> \
  --discovery-token-ca-cert-hash=sha256:<hash>
```

`xsh k8s join` 会执行与控制平面安装相同的准备链路：系统准备、运行时安装、kubeadm / kubelet / kubectl 安装，然后执行 `kubeadm join`。Worker 的 `--runtime` 应与控制平面保持一致：

```bash
sudo xsh k8s join --runtime=docker \
  --master=192.168.1.10:6443 \
  --token=<token> \
  --discovery-token-ca-cert-hash=sha256:<hash>
```

### 安装独立 Docker

只安装日常 Docker 环境，不初始化 Kubernetes：

```bash
sudo xsh docker
```

固定 Docker CE 主版本：

```bash
sudo xsh docker --major=27
```

独立 Docker 安装会配置 Docker apt 源，安装 `docker-ce`、`docker-ce-cli`、`containerd.io`、Buildx、Compose 等包，并写入 `/etc/docker/daemon.json`。当前独立 Docker 命令不提供离线 bundle 子命令。

## Kubernetes 离线模式

在一台有网络、系统发行版和 CPU 架构尽量与目标主机一致的准备机上生成离线资源：

```bash
sudo xsh k8s bundle \
  --runtime=containerd \
  --version=v1.35.0 \
  --output-dir ./xsh-k8s-offline
```

命令会生成目录和压缩包：

```text
./xsh-k8s-offline/
./xsh-k8s-offline.tar.gz
```

将压缩包拷贝到离线目标主机，解压后安装：

```bash
tar -xzf xsh-k8s-offline.tar.gz
sudo xsh k8s --assets-dir ./xsh-k8s-offline
```

Worker 节点也可以使用同一套资源：

```bash
sudo xsh k8s join --assets-dir ./xsh-k8s-offline \
  --master=192.168.1.10:6443 \
  --token=<token> \
  --discovery-token-ca-cert-hash=sha256:<hash>
```

`--assets-dir` 会在安装开始前校验 Kubernetes 离线包。控制平面安装要求包含镜像归档和 CNI / metrics-server YAML，Worker 加入不要求控制平面专用资源。

期望目录结构：

```text
<assets-dir>/
├── deb/
│   ├── docker/
│   │   ├── containerd.io_*.deb
│   │   └── docker-ce_*.deb / cri-dockerd_*.deb 等，仅 docker 运行时需要
│   ├── ipvs/
│   │   └── ipset_*.deb、ipvsadm_*.deb
│   └── kubernetes/
│       └── kubeadm / kubelet / kubectl / cri-tools / kubernetes-cni 等 .deb
├── images/
│   └── *.tar
├── kube-flannel.yml
└── components.yaml
```

离线包准备命令需要本机 Docker 可用，因为它会拉取并导出 Kubernetes 镜像。`--mirror=cn` 可用于离线包准备阶段，将 Kubernetes 包源和镜像仓库切换到国内镜像。

## 命令参考

### `xsh k8s`

初始化 Kubernetes 控制平面。

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `--runtime` | `containerd` | 容器运行时，可选 `containerd` 或 `docker` |
| `--version` | `v1.35.0` | Kubernetes 版本 |
| `--pod-cidr` | `10.244.0.0/16` | Pod 网段，默认匹配 flannel |
| `--service-cidr` | `10.96.0.0/12` | Service 网段 |
| `--hostname` | `master` | 设置节点主机名 |
| `--advertise` | 自动探测 | kube-apiserver advertise address；探测失败时请显式传入 |
| `--mirror` | 空 | 传 `cn` 使用国内 Kubernetes apt 源和镜像仓库 |
| `--assets-dir` | 空 | Kubernetes 离线资源目录 |
| `-y`, `--yes` | `false` | 跳过覆盖确认 |

全局参数：

| 参数 | 说明 |
| --- | --- |
| `-v`, `--verbose` | 输出 apt、dpkg、kubeadm 等命令的详细日志 |

### `xsh k8s join`

将当前节点加入已有集群。

| 参数 | 默认值 / 必填 | 说明 |
| --- | --- | --- |
| `--master` | 必填 | 控制平面地址，例如 `10.0.0.10:6443` |
| `--token` | 必填 | kubeadm bootstrap token |
| `--discovery-token-ca-cert-hash` | 必填 | CA hash，形如 `sha256:...` |
| `--runtime` | `containerd` | 应与控制平面运行时一致 |
| `--version` | `v1.35.0` | Kubernetes 版本 |
| `--mirror` | 空 | 传 `cn` 使用国内 Kubernetes apt 源和镜像仓库 |
| `--assets-dir` | 空 | Kubernetes 离线资源目录 |
| `-y`, `--yes` | `false` | 跳过覆盖确认 |

### `xsh k8s bundle`

下载 Kubernetes 离线安装所需资源，并打包为 `.tar.gz`。

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `--runtime` | `containerd` | 为 `containerd` 或 `docker` 运行时准备资源 |
| `--version` | `v1.35.0` | Kubernetes 版本 |
| `--mirror` | 空 | 传 `cn` 使用国内 Kubernetes 包源和镜像仓库 |
| `--output-dir` | `xsh-k8s-offline` | 输出的离线资源目录 |
| `--archive` | `<output-dir>.tar.gz` | 输出压缩包路径 |

### `xsh docker`

安装独立 Docker CE。

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `--major` | `0` | 固定 Docker CE 主版本；`0` 表示安装当前源里的最新版本 |
| `-y`, `--yes` | `false` | 跳过覆盖确认 |

### `xsh version`

打印版本、commit 和构建时间。Release 构建由 GoReleaser 通过 `-ldflags` 注入这些信息，本地构建会显示 `dev`、`none`、`unknown`。

## 安装流程与权限说明

Kubernetes 控制平面大致流程：

1. 检测已有 Docker / containerd / kubelet / kubeadm / kubectl 和 Kubernetes 配置
2. 如存在旧组件，提示是否覆盖；传 `-y` 时直接覆盖
3. 关闭常见防火墙、关闭 swap、写入 Kubernetes 所需 sysctl 和内核模块配置
4. 安装并配置容器运行时
5. 安装 kubeadm、kubelet、kubectl，并对相关包执行 `apt-mark hold`
6. 执行 `kubeadm init`
7. 写入 kubeconfig，移除控制平面 NoSchedule taint，保存 Worker join 命令
8. 部署 flannel 和 metrics-server

覆盖清理会删除 Docker / containerd / Kubernetes 相关包、apt 源、keyring 和若干配置目录，包括：

```text
/etc/docker
/etc/containerd
/etc/kubernetes
/etc/cni
/var/lib/docker
/var/lib/containerd
/var/lib/kubelet
/var/lib/etcd
```

请不要在承载重要工作负载或已有集群状态的机器上直接覆盖执行。

失败回滚是分步骤的 best-effort 机制：某一步失败时会尽量回滚该步骤写入的配置，并按反向顺序停止相关服务或执行 `kubeadm reset`。它不是完整的系统快照恢复；安装前的覆盖清理由 `detect.Cleanup` 负责，范围比单步回滚更大。

## 主要模块

| 路径 | 作用 |
| --- | --- |
| `cmd/xsh/main.go` | Cobra 根命令、版本命令、全局 verbose、OS / 权限检查 |
| `internal/cli/` | `k8s`、`k8s join`、`k8s bundle`、`docker` 命令组装 |
| `internal/detect/` | 安装前组件探测、覆盖确认、旧组件清理 |
| `internal/sysprep/` | firewall / SELinux / swap / sysctl / 内核模块 / IPVS 工具准备 |
| `internal/runtime/` | Kubernetes 容器运行时分发 |
| `internal/runtime/containerd/` | 安装 containerd 并渲染 `/etc/containerd/config.toml` |
| `internal/runtime/docker/` | 安装 Docker + cri-dockerd 作为 Kubernetes CRI |
| `internal/kube/` | kubeadm / kubelet / kubectl 安装、`kubeadm init`、Worker join |
| `internal/network/` | 部署 flannel 和 metrics-server |
| `internal/offlinebundle/` | 下载 .deb、YAML、镜像并生成 Kubernetes 离线包 |
| `internal/assets/` | 校验 `--assets-dir` 的资源完整性 |
| `internal/dockerinstall/` | 独立 Docker CE 安装流程 |
| `internal/aptrepo/` | Docker 和 Kubernetes apt 源、keyring、发行版映射 |
| `internal/osinfo/` | `/etc/os-release` 解析和支持系统校验 |
| `internal/exec/` | 命令执行、输出捕获、下载工具 |
| `internal/log/` | 安装日志输出 |

## 开发与测试

常用命令：

```bash
make fmt
make vet
go test ./...
make build
```

Makefile 中的目标：

```text
make build  # go build -o bin/xsh ./cmd/xsh
make clean  # 删除 bin/
make fmt    # go fmt ./...
make vet    # go vet ./...
```

单元测试主要覆盖纯函数和可替换依赖逻辑，不需要在测试过程中真正改写 Linux 系统。端到端安装仍建议在干净的 Debian / Ubuntu 虚拟机中验证，尤其是以下路径：

- 在线安装 Kubernetes，`containerd` 运行时
- 在线安装 Kubernetes，`docker` 运行时
- 使用 `--mirror=cn`
- 使用 `--assets-dir` 离线安装
- Worker join
- 独立 Docker 安装

Release 由 `.github/workflows/release.yml` 触发，tag 形如 `vX.Y.Z` 时通过 GoReleaser 构建 Linux `amd64` / `arm64` 产物，并生成 `checksums.txt`。

## 常见问题

### `apt-get install kubeadm` 失败或访问 `pkgs.k8s.io` 很慢

国内网络可以尝试：

```bash
sudo xsh k8s --mirror=cn
```

如果 GitHub 上的 flannel / metrics-server YAML 仍然访问慢，建议提前用 `xsh k8s bundle` 准备离线资源，并在安装时传 `--assets-dir`。

### `kubeadm init` 拉镜像卡住

使用 `--mirror=cn`，或提前准备离线包，让安装从 `<assets-dir>/images/*.tar` 导入镜像。

### Worker join 失败并提示运行时相关错误

确认 Worker 使用的 `--runtime` 与控制平面一致。控制平面如果用 `--runtime=docker` 初始化，Worker 也应传 `--runtime=docker`。

### `kubelet` 在 `kubeadm init` 前不断重启

这是常见现象。`kubelet` 在 `kubeadm init` 写入配置前可能处于 crash-loop，安装逻辑会把这个阶段的启动失败降级为警告。`kubeadm init` 完成后再检查：

```bash
systemctl status kubelet
```

### 独立 Docker 安装后拉镜像慢

`xsh docker` 写入的 `/etc/docker/daemon.json` 默认不配置 registry mirror。可以按自己的网络环境添加 `registry-mirrors` 后重启 Docker：

```bash
sudo systemctl restart docker
```

## 许可证

本项目使用 Apache License 2.0，详见 `LICENSE`。
