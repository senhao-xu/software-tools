# go-xsh

## Goal

用 Go 实现一个单二进制 CLI 部署工具，整合两个 shell 脚本的能力，在 Debian 12/13 上一键部署 Kubernetes 集群：

- **功能 1（`k8s` 命令）**：以 `F:\Script\kubernetes\off-docker-1.35-debian12\install.sh` 为蓝本，**单一命令**一次跑完环境准备 → 装运行时 → 装 k8s → kubeadm init → 装网络。worker 节点用 `k8s join` 加入。
- **功能 2（`docker` 命令）**：以 `https://docker.senhao.eu.cc` 为蓝本，在线单独安装 Docker。

---

## Decisions (locked)

| # | 主题 | 决定 |
|---|-----|------|
| **D1** | 资源分发 / 安装模式 | **双模式自动切换**：检测到本地资源走离线 dpkg；未检测到则走在线官方源 apt install。二进制本体 ~10MB，不托管 assets.tar.gz。 |
| **D2** | CLI 结构 | 不分步暴露。命令树仅 3 条：`xsh k8s`（master 一键全装）、`xsh k8s join ...`（worker 加入）、`xsh docker`（在线装 docker）。 |
| **D3** | 资源查找顺序 | ① `--assets-dir`；② 二进制同级 `./assets/`；③ `/var/cache/xsh/v<ver>/`；④ 都没有 → 切在线模式。 |
| **D4** | 在线源 | 默认官方（`download.docker.com` / `pkgs.k8s.io` / `registry.k8s.io` / GitHub raw）。`--mirror=cn` 切到 `mirrors.aliyun.com/kubernetes-new` + `registry.aliyuncs.com/google_containers`。 |
| **D5** | 容器运行时 | 默认 **containerd**（`--cri-socket=unix:///var/run/containerd/containerd.sock`）；`--runtime=docker` 切 docker + cri-dockerd（`--cri-socket=unix:///var/run/cri-dockerd.sock`）。 |
| **D6** | CNI / 附加 | 仅 **flannel + metrics-server**。pod-cidr 与 flannel 强绑定 `10.244.0.0/16`。 |
| **D7** | Worker join | 支持。master 完成 init 后输出 join 命令并落盘 `/var/cache/xsh/join-command.sh`；worker 执行 `xsh k8s join --master=... --token=... --discovery-token-ca-cert-hash=...`，内部自动前置环境准备 + runtime + kube 安装。 |
| **D8** | 参数传递 | **flag-only**，不引入 config 文件 / 环境变量。 |
| **D9** | 失败行为 | **预检 + 交互式覆盖 + 失败回滚**。运行前探测已存在组件（docker / containerd / kubelet 服务、`/etc/kubernetes/admin.conf`、`/etc/kubernetes/kubelet.conf`、kubeadm 已 init）；任意一项命中 → 终端列出已检测项 + 询问 `[O]verwrite / [C]ancel`：`Overwrite` 执行清理（kubeadm reset / apt purge / 删配置）再安装；`Cancel` 退出。`-y / --yes` 跳过确认默认 Overwrite。运行中失败 → 倒序回滚已成功步骤。 |
| **D10** | kubeadm 默认值 | 全部贴原脚本（version=v1.35.0、pod-cidr=10.244.0.0/16、service-cidr=10.96.0.0/12、hostname=master、advertise=本机出口 IP），任意项可被 flag 覆盖。 |
| **D11** | 终端输出 | 纯文本带颜色 log（INFO/WARN/ERROR），不引入 spinner / TUI。 |
| **D12** | bug 修复 | 修原脚本的 SELinux 路径错误、补 swap on 检测、firewall 同时处理 ufw 和 firewalld。 |
| **D13** | 命名 | 不存在 `init` 命令；所有 k8s 安装步骤合并为单一 `xsh k8s` 命令一键执行。 |

---

## Requirements

### 顶层
- 单 Go 二进制，编译产物 ~10MB
- 必须 root 运行（启动时校验）
- 仅支持 Debian 12（bookworm）和 Debian 13（trixie），读 `/etc/os-release` 校验
- 命令树：
  - `xsh k8s [flags]` — master 一键全装
  - `xsh k8s join --master=... --token=... --discovery-token-ca-cert-hash=... [flags]` — worker 加入
  - `xsh docker [flags]` — 在线装 docker

### `xsh k8s`（master 一键安装）

**Step 0 — 环境探测 + 交互确认**
- 探测项：docker / containerd / kubelet 服务是否 active；`command -v kubectl/kubeadm`；`/etc/kubernetes/admin.conf`、`/etc/kubernetes/kubelet.conf` 是否存在
- 命中任意 → 打印"已检测到 X / Y / Z" + 提示 `[O]verwrite / [C]ancel`
- `-y` 跳过提示默认 Overwrite
- Overwrite 清理：`kubeadm reset -f` → `apt purge -y docker-ce* containerd.io cri-dockerd kubeadm kubelet kubectl` → 删 `/etc/{docker,containerd,kubernetes,cni}`、`/var/lib/{docker,containerd,kubelet,etcd}`、`/etc/apt/sources.list.d/{docker,kubernetes}.list`

**Step 1 — 环境准备**
- 关防火墙：ufw 和 firewalld 都探测，存在哪个关哪个
- 关 SELinux：仅在 `/etc/selinux/config` 存在时操作（Debian 默认无此文件 → no-op + warn）
- 关 swap：`swapoff -a` + 注释 `/etc/fstab` 里 swap 行
- 写 `/etc/sysctl.d/k8s.conf`（bridge-nf-call、ip_forward）+ `sysctl --system`
- modprobe br_netfilter，写 `/etc/modules-load.d/k8s.conf` 持久化
- 加载 ipvs 模块（ip_vs / ip_vs_rr / ip_vs_wrr / ip_vs_sh / ip_conntrack），写 `/etc/modules-load.d/ipvs.conf` 持久化
- 离线：`dpkg -i assets/deb/ipvs/*.deb`；在线：`apt install -y ipset ipvsadm`

**Step 2 — 安装运行时**
- containerd（默认）：
  - 离线：`dpkg -i assets/deb/docker/containerd.io_*.deb`
  - 在线：加 docker apt repo → `apt install -y containerd.io`
  - 生成 `/etc/containerd/config.toml`：systemd cgroup driver；`--mirror=cn` 时配 registry mirror
  - `systemctl enable --now containerd`
- docker（`--runtime=docker`）：
  - 离线：dpkg 装 `assets/deb/docker/*.deb`（含 cri-dockerd）
  - 在线：加 docker apt repo → `apt install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin`，cri-dockerd 走 GitHub release deb
  - 写 `/etc/docker/daemon.json`（systemd cgroup、json-file 100m × 3）
  - `systemctl enable --now docker cri-docker`

**Step 3 — 安装 kubernetes**
- 离线：`dpkg -i assets/deb/kubernetes/*.deb`
- 在线：加 k8s apt repo（默认 `pkgs.k8s.io`，`--mirror=cn` 切 `mirrors.aliyun.com/kubernetes-new`）→ `apt install -y kubeadm=<ver> kubelet=<ver> kubectl=<ver>`
- `apt-mark hold kubeadm kubelet kubectl`
- `systemctl enable --now kubelet`

**Step 4 — kubeadm init（master）**
- 自动探测出口 IP → 写 `/etc/hosts` 映射 hostname
- `hostnamectl set-hostname <hostname>`（默认 master）
- 镜像准备：
  - 离线：containerd 走 `ctr -n k8s.io images import`，docker 走 `docker load`
  - 在线：`kubeadm config images pull [--image-repository=<mirror>]`
- `kubeadm init --kubernetes-version=<ver> --service-cidr=<sc> --pod-network-cidr=<pc> --cri-socket=<sock> [--image-repository=<mirror>]`
- 拷贝 `/etc/kubernetes/admin.conf` → `$HOME/.kube/config` + chown
- 单节点：`kubectl taint nodes --all node-role.kubernetes.io/control-plane-`
- 生成 join 命令 → `/var/cache/xsh/join-command.sh` + 终端打印

**Step 5 — 安装网络**
- 离线：`kubectl apply -f assets/kube-flannel.yml` + `kubectl apply -f assets/components.yaml`
- 在线：`kubectl apply -f https://raw.githubusercontent.com/flannel-io/flannel/.../kube-flannel.yml` + `kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/download/.../components.yaml`

任一 Step 1-5 失败 → 倒序回滚到 Step 0 状态。

### `xsh k8s join`（worker 加入）
- Step 0：探测 + 覆盖确认（同 master）
- Step 1-3：环境准备 + runtime + kube 三步与 master 共用代码
- Step 4：`kubeadm join <master> --token=<t> --discovery-token-ca-cert-hash=<h> --cri-socket=<sock>`
- 不执行 Step 5

### `xsh docker`
- 同样 Step 0 探测 + 覆盖确认（探测 docker / containerd active）
- 检测 Debian 12/13（`/etc/os-release`）
- 加 docker apt repo + GPG key
- `apt-cache madison docker-ce` 列版本；`--major=N` 锁大版本（默认 latest）
- 写 `/etc/docker/daemon.json`（systemd cgroup、json-file 100m × 5）
- 装 docker-ce / docker-ce-cli / containerd.io / docker-buildx-plugin / docker-compose-plugin / docker-model-plugin
- `systemctl enable --now docker`

### 全局 flag
- `--runtime=containerd|docker`（默认 containerd）
- `--mirror=cn`（默认空 = 官方源）
- `--assets-dir=PATH`（指定离线资源目录）
- `--version=v1.35.0`、`--pod-cidr=10.244.0.0/16`、`--service-cidr=10.96.0.0/12`、`--hostname=master`、`--advertise=<IP>`
- `-y / --yes`（跳过覆盖确认）
- `--verbose`（透传 apt/dpkg/kubeadm 详细输出）
- `docker` 独有：`--major=N`

---

## Acceptance Criteria

- [ ] 干净 Debian 12 上执行 `sudo xsh k8s`，5 分钟内集群 ready，`kubectl get nodes` Ready，`kubectl get pods -A` 全部 Running
- [ ] 同机器再次执行 `sudo xsh k8s`，触发 Step 0 覆盖提示；选 Cancel 退出无变更；选 Overwrite（或 `-y`）能从已安装状态干净重装
- [ ] 离线模式：把 assets/ 拷贝到二进制旁边，无网络也能装完
- [ ] 在线模式：`--mirror=cn` 能在国内网络下完成安装
- [ ] `--runtime=docker` 与默认 containerd 两条路径都能通
- [ ] worker 节点跑 `xsh k8s join ...` 能成功加入 master 集群
- [ ] `xsh docker` 在 Debian 12 和 13 都能完成，`docker run hello-world` 成功
- [ ] 任一步失败回滚后系统状态不留垃圾（apt purge 干净、kubeadm reset 干净）
- [ ] 修复原脚本 3 个 bug：SELinux 路径、firewall 兼容、swap 检测

---

## Definition of Done

- 单元测试覆盖：daemon.json / containerd config.toml 生成、kubeadm 命令拼装、apt repo 配置、IP 探测、状态探测
- 集成测试在干净 Debian 12 VM 上手动跑通 4 条主路径（离线 containerd / 离线 docker / 在线 containerd / 在线 docker + mirror=cn）
- `go vet` / `golangci-lint` 通过
- README：使用方法、系统要求、`--mirror=cn` 说明、worker join 流程、故障排查
- Makefile：`make build` / `make release`（多架构 amd64/arm64）

---

## Out of Scope (MVP)

- 多 master HA（control-plane join）
- 升级（k8s minor version 间）
- 卸载子命令（uninstall）
- 备份 / 恢复（etcd snapshot）
- 非 Debian 系统（Ubuntu/CentOS/RHEL/openEuler）
- Calico / Cilium / 其他 CNI
- Ingress controller / dashboard / cert-manager 等附加组件
- registry-mirrors 自动配置（用户自行通过 daemon.json / containerd config 配）
- kubeconfig scp 提示
- 配置文件 / 环境变量
- TUI / spinner / dry-run
- 分步暴露子命令（已合并为单命令）

---

## Technical Approach

### 项目结构
```
software-tools/                   # 仓库根
  cmd/xsh/main.go                 # cobra root，二进制名为 xsh
  internal/
    cli/
      k8s.go                      # k8s 主命令
      k8s_join.go                 # k8s join 子命令
      docker.go                   # docker 命令
    detect/                       # Step 0 探测 + 覆盖确认 + 清理
    sysprep/                      # 关防火墙/swap/SELinux + sysctl + ipvs
    runtime/{containerd,docker}/  # runtime 安装与配置
    kube/                         # apt repo、kubeadm 包装、image pull/load
    network/                      # flannel + metrics-server apply
    assets/                       # 资源查找（D3）
    exec/                         # 命令执行 + 日志
    osinfo/                       # /etc/os-release 解析
    log/                          # 彩色 log
  go.mod
  Makefile
  README.md
```

### 关键依赖
- `github.com/spf13/cobra` — CLI
- `github.com/fatih/color` — 终端颜色
- 标准库：`os/exec`、`net`、`encoding/json`、`text/template`

### 执行模型
- 每个 Step 封装为 `Step{ Name; PreCheck() State; Do() error; Rollback() error }`
- 主命令串成链：Step1 → Step2 → ... → StepN；任一 `Do()` 失败 → 倒序 `Rollback()`
- Step 0（探测 + 确认 + 清理）独立成函数，不在 Step 链里（清理本身不需要回滚）
- 所有外部命令通过统一 `exec.Run(name, args...)` 包装：日志前缀 `[CMD]`、stderr 实时输出、超时控制

### 镜像处理（在线 mirror=cn）
- containerd：`/etc/containerd/config.toml` 配 `[plugins."io.containerd.grpc.v1.cri".registry.mirrors."registry.k8s.io"]` 指向 `registry.aliyuncs.com/google_containers`
- docker：`daemon.json` 不动；kubeadm 走 `--image-repository=registry.aliyuncs.com/google_containers`

---

## Decision (ADR-lite)

**Context**：用户有两个现有脚本（离线 k8s、在线 docker），希望用 Go 统一并增强；要求工具体积小、兼容国内网络、安装步骤一键化。

**Decision**：单二进制 + 双模式自动切换（D1）+ 镜像源切换（D4）+ 双 runtime（D5）+ 单命令不分步（D2/D13）+ 交互式覆盖确认（D9）。

**Consequences**：
- 优点：体积小（~10MB）、一键化、国内可用、有覆盖确认避免误重装
- 代价：双路径意味着每个 Step 有两套代码分支；测试矩阵 4 条主路径
- 风险：cri-dockerd 在线安装需要从 GitHub release 拉，国内可能慢（无官方国内镜像）→ 建议国内用户走离线 deb 包

---

## Implementation Plan (small PRs)

- **PR1（骨架）**：cobra 命令树（3 条命令）、`Step` 框架、`exec.Run`、os/root 校验、彩色 log。可执行但所有 Step 返回"未实现"。
- **PR2（detect）**：Step 0 探测 + 交互式覆盖确认 + 清理函数。
- **PR3（sysprep）**：Step 1 环境准备 + 修原脚本 bug。
- **PR4（runtime）**：Step 2 含 containerd / docker 两条路径，离线 + 在线。
- **PR5（kube）**：Step 3 + apt repo 配置 + `--mirror=cn` 支持。
- **PR6（kubeadm-init）**：Step 4 kubeadm init + image pull/load + join 命令生成。
- **PR7（network）**：Step 5 flannel + metrics-server。
- **PR8（join）**：`k8s join` 子命令 = 复用 Step 0-3 + `kubeadm join`。
- **PR9（docker）**：`docker` 命令，参考 docker.senhao.eu.cc。
- **PR10（rollback + 测试）**：完善每个 Step 的 Rollback、单元测试、README。

---

## Technical Notes

- 参考脚本：
  - 离线 k8s：`F:\Script\kubernetes\off-docker-1.35-debian12\install.sh`
  - 在线 docker：`https://docker.senhao.eu.cc`
- 离线资源：`deb/{docker,ipvs,kubernetes}/`、`images/{k8s-1.35.0,kube-flannel-img,metrics-server}.tar`、`{kube-flannel,components}.yml`
- cri-dockerd socket：`unix:///var/run/cri-dockerd.sock`
- containerd socket：`unix:///var/run/containerd/containerd.sock`
- Flannel pod CIDR 与 kubeadm `--pod-network-cidr` 强绑定 `10.244.0.0/16`
- 原脚本 3 处 bug：
  1. `sed -i ... /etc/selinuxconfig` 应为 `/etc/selinux/config`，且 Debian 默认无此文件
  2. firewall 只关 ufw，未处理 firewalld
  3. swap 处理无幂等检测
