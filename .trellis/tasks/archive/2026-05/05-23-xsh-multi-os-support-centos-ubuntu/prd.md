# xsh multi-OS support (Ubuntu)

## Goal

把 xsh 从 Debian 12/13 单一 distro 扩展到同时支持 Ubuntu 22.04/24.04（同 apt/dpkg 家族），覆盖 `xsh docker` / `xsh k8s` / `xsh k8s join` 三条安装命令。借此机会把现存于 4 个包里复制粘贴的 apt repo 设置抽成共享 helper（`internal/aptrepo`），把 cri-dockerd 的 release artifact 名称从硬编码 `debian-bookworm` 改成按 distro+codename 查表（`internal/cridockerd`）。安装启动时在日志打印探测到的 OS（distro + 版本 + codename），便于诊断。

## Requirements

1. **支持矩阵**：`{debian:12, debian:13, ubuntu:22.04, ubuntu:24.04}` 共 4 个 OS+版本组合，三条命令全覆盖
2. **入口 OS 日志**：每条命令（`xsh docker` / `xsh k8s` / `xsh k8s join`）在 detect step 之前先 `log.Info("detected OS: <ID> <VERSION_ID> (<CODENAME>)")`
3. **拒绝不支持 OS 时打印明确错误信息**（替换当前 `osinfo.RequireDebian` 为 `RequireSupported`）
4. **`internal/aptrepo` 新包**：apt keyring + sources.list 设置 + codename/arch 探测，统一对接 4 个 caller。内部映射表：(distro,version) → codename + docker.com URL 前缀
5. **`internal/cridockerd` 新包**：cri-dockerd GitHub release URL 拼接 + 下载 + dpkg install，映射 (distro,codename) → artifact 名（如 `ubuntu-jammy`、`debian-bookworm`）。**仅 `runtime/docker` 调用**，`dockerinstall` 不动它（standalone docker 不需要 cri-dockerd）
6. **`--mirror=cn` 在 Ubuntu 上仍工作**：保持当前作用范围（仅 k8s 链），把 aliyun ubuntu 路径加进映射
7. **保持单一二进制 + 无外部 runtime 依赖**（仅 apt/dpkg/systemctl 这些自带工具）
8. **保持现有幂等 + 步骤化回滚**

## Acceptance Criteria

- [ ] `osinfo.RequireSupported` 返回 nil 当且仅当 OS 在 4 个支持组合内；其它都返回明确错误
- [ ] `xsh docker` 在 ubuntu:22.04、ubuntu:24.04 跑完后 `docker run hello-world` 成功
- [ ] `xsh k8s --runtime=containerd` 在 ubuntu:22.04 跑完后 `kubectl get nodes` Ready
- [ ] `xsh k8s --runtime=docker` 在 ubuntu:22.04 跑完后 `kubectl get nodes` Ready，且 cri-dockerd .deb 走的是 `ubuntu-jammy` artifact
- [ ] `xsh k8s join` 在 ubuntu worker 接 ubuntu master 成功
- [ ] 入口日志显示正确探测到的 OS（4 个组合都验证一次）
- [ ] Debian 12/13 上行为完全不变（回归测试）
- [ ] `--mirror=cn` 在 ubuntu:22.04 上仍能切到 aliyun 镜像
- [ ] `internal/aptrepo` 和 `internal/cridockerd` 的纯函数（codename 映射 / URL 拼接 / madison parsing 等）有单元测试
- [ ] README 支持矩阵更新

## Definition of Done

- 单元测试新增（aptrepo / cridockerd 的映射表 + URL 拼接 + parser）
- `go vet` / `gofmt` / `go build` 全过；不在 Windows 上执行二进制
- README 的 "System Requirements" 段从 "Debian 12/13" 改成 "Debian 12/13 or Ubuntu 22.04/24.04"
- README 的 "Not supported" 段去掉 Ubuntu，保留 CentOS/RHEL
- 集成测试矩阵：4 OS × 3 命令至少手工跑一次（Debian 12/13 + Ubuntu 22.04/24.04 × docker/k8s/join）
- Rollback / 幂等性在 Ubuntu 上也验证一次

## Technical Approach

**新包**：

```
internal/aptrepo/
    aptrepo.go        // EnsureDockerRepo / EnsureK8sRepo + 探测函数
    aptrepo_test.go   // codename map / URL build 纯函数测试
internal/cridockerd/
    cridockerd.go     // BuildURL + Install (download + dpkg)
    cridockerd_test.go
```

**改动 4 个 caller**：
- `internal/dockerinstall/dockerinstall.go`：`ensureDockerAptRepo` + `debianCodename` 删掉，改调 `aptrepo.EnsureDockerRepo`
- `internal/runtime/docker/docker.go`：同上 + `installCRIDockerd` 改调 `cridockerd.Install`
- `internal/runtime/containerd/containerd.go`：同 dockerinstall
- `internal/kube/kube.go`：apt key/source 部分改调 `aptrepo.EnsureK8sRepo`（k8s 源 URL 不含 codename，但 mirror=cn 路径要按 distro 分支）

**入口 OS 日志**：在 `internal/cli/docker.go`、`internal/cli/k8s.go`、`internal/cli/k8s_join.go` 三处的 `RunE` 顶部加 `osinfo.Detect() → log.Info → RequireSupported`。

**osinfo 改动**：保留 `Detect` 不变；`RequireDebian` → `RequireSupported`（接受 debian 12/13 + ubuntu 22.04/24.04）；保留旧符号一段时间或者直接改名（小工程，4 个 caller 都顺手改）→ 直接改名，无外部 API。

## Decision (ADR-lite)

**Context**：xsh 当前 Debian-only，4 个安装包重复 apt repo 设置代码，cri-dockerd release artifact 名硬编码 `debian-bookworm`。用户要求扩展到 Ubuntu，CentOS 已 EOL 不在本任务。

**Decision**：
- Scope = Debian 12/13 + Ubuntu 22.04/24.04，三命令全覆盖
- 抽 `internal/aptrepo` 共享包（4 caller 都改），抽 `internal/cridockerd` 独立小包（仅 runtime/docker 调用）
- 不抽更通用的 `internal/pkgmgr` 接口（YAGNI，本任务无 yum 需求）
- `--mirror=cn` 作用范围不扩大，仅把 ubuntu 路径加进现有 k8s mirror 映射

**Consequences**：
- 一次性多动 4 个包，PR2 PR 较大但结构变化是局部的
- 未来如果接 RHEL-family，是引入 `internal/pkgmgr` 接口的合适时机；那时 `aptrepo` 变成其一个实现
- cri-dockerd 仍是手工拉 GitHub release 的方式，未变；只是 artifact 名映射化

## Out of Scope（explicit）

- CentOS 7 / 8、Rocky / AlmaLinux、CentOS Stream、其它 RHEL-family
- SUSE / Arch / 其它非主流 distro
- Ubuntu 20.04（已 EOL）
- 衍生发行版（Linux Mint / Pop!_OS / Kali / Raspbian）—— 它们的 `ID` 字段不是 `ubuntu` 也不是 `debian`，本任务**精确匹配 ID**，不 fallback 到 `ID_LIKE`
- `--codename-override` 用户覆盖 flag（用户可以自己魔改 /etc/os-release 走 escape hatch）
- 把 `--mirror=cn` 扩展到 docker.com apt 源（独立任务）
- 默认 `daemon.json` 填 `registry-mirrors` 字段（产品决策，与本任务正交）
- 跨 distro 的 `--assets-dir` 离线 .deb 兼容性验证（用户责任）

## Future Work（surfaced from expansion sweep）

- **RHEL-family 支持**：合适时机引入 `internal/pkgmgr` 接口，`aptrepo` 成为 apt 实现，新增 yum/dnf 实现
- **Mirror 扩大覆盖**：`--mirror=cn` 是否覆盖 docker.com apt 源；是否默认 `daemon.json` 注入 registry-mirrors
- **测试自动化**：跨 distro 集成测试目前只能手工，将来用 multipass / vagrant 自动化

## Technical Notes

- 入口在 `internal/cli/docker.go:18`、`internal/cli/k8s.go:30`、`internal/cli/k8s_join.go`
- `osinfo.Detect()` 已经解析 `ID/VERSION_ID/VERSION_CODENAME`，无需重写；测试中已经覆盖 ubuntu/jammy 解析
- `sysprep` 包基本无需改动：firewall 已经处理 ufw + firewalld 两路；SELinux 段已在 Debian 上跳过且行为正确（Ubuntu 上同样无 SELinux）
- `detect.Cleanup` 基本无需改动：`apt-get purge` 在 Ubuntu 上同样工作；apt keyring/sources 路径一致
- cri-dockerd GitHub release artifact 命名规律：`cri-dockerd_<version>-0.<distro>-<codename>_<arch>.deb`，可用的 (distro, codename)：`debian-bookworm`、`debian-bullseye`、`ubuntu-focal`、`ubuntu-jammy`、`ubuntu-noble`（24.04 是新的，需要在调研阶段确认 release artifact 是否齐全）
- docker.com apt repo URL 模板：`https://download.docker.com/linux/<distro>/...`（distro ∈ {debian, ubuntu}）
- aliyun docker-ce 镜像 URL 模板（仅供参考，本任务不动）：`https://mirrors.aliyun.com/docker-ce/linux/<distro>`
- aliyun k8s 镜像 URL 模板（本任务用到）：`https://mirrors.aliyun.com/kubernetes-new/core/stable/v1.X/deb/`（distro 无关，但需确认）

## Implementation Plan（small PRs）

**PR1（脚手架）**：~300 lines
- 新建 `internal/aptrepo` + `internal/cridockerd` 两个包（含纯函数 + 单测）
- `osinfo.RequireDebian` → `RequireSupported`，更新现有 caller（仅函数名替换 + 单测扩充）
- 三个 CLI 入口加 `log.Info("detected OS: ...")`
- 旧包**暂不**接 aptrepo（保留各自的 `ensureDockerAptRepo` 等），保证 PR1 独立可合并、行为不变

**PR2（迁移）**：~200 lines
- 4 个 caller（`dockerinstall` / `runtime/docker` / `runtime/containerd` / `kube`）切换到 `aptrepo`，删除各自的 `ensureDockerAptRepo` / `debianCodename`
- `runtime/docker` 的 `installCRIDockerd` 改调 `cridockerd.Install`，删除硬编码 `criDockerdRelease`
- `kube` 包改调 `aptrepo.EnsureK8sRepo`（**PR12 实际发现**：aliyun k8s mirror URL `mirrors.aliyun.com/kubernetes-new/core:/stable:/<minor>/deb/` 是 distro-agnostic，无 distro slot，因此 kube 内部**不需要**ubuntu 分支，aptrepo 一处处理 mirror 即可）

**PR3（收尾）**：~100 lines
- 更新 README 支持矩阵
- 跑完 Debian 12/13 + Ubuntu 22.04/24.04 × {docker, k8s, join} 集成测试，记录任何 surface 出来的小问题
- 修补集成测试发现的边角

## Research References

调研放进实施阶段（每个新包的 PR 自己拉数据），不在本 PRD 之前做：
- cri-dockerd 0.3.21+ 的 release artifact 列表（确认 ubuntu-noble 是否有官方 .deb）
- aliyun k8s 镜像在 ubuntu 上的 source.list 写法
