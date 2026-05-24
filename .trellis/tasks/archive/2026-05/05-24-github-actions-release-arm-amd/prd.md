# GitHub Actions Release (amd64 + arm64)

## Goal

新增 GitHub Actions workflow，自动交叉编译 `xsh` 为 Linux amd64 与 arm64 二进制，并发布到 GitHub Releases。降低发版手工劳动，保证产物可追溯（版本号 + 校验和）。

## What I already know

- 单 binary 入口：`cmd/xsh/main.go`，纯 Go（无 CGO 依赖）
- Go module：`xsh`，`go 1.25.0`
- 目标 OS：Debian 12/13、Ubuntu 22.04/24.04（仅 Linux）
- Makefile 已具备 `build/fmt/vet`，本机交叉编译 `GOOS=linux GOARCH=amd64` 验证通过
- 远程：`https://github.com/senhao-xu/software-tools.git`
- 当前 `.github/` 目录不存在；仓库尚无 tag
- `main.go` 目前没有 `version` 变量；版本信息暂未注入

## Assumptions (temporary)

- 发版触发方式：tag push（`v*`）—— 显式版本控制是 Go 工具链事实标准
- 架构：`linux/amd64` + `linux/arm64`（k8s/docker 宿主机常见组合；armv7 不在目标内）
- 产物形态：`tar.gz` 打包二进制 + LICENSE/README，并生成 `sha256sum.txt`
- CGO 关闭、静态链接

## Requirements

- `cmd/xsh/main.go` 新增 `version`、`commit`、`date` 包级变量，默认值 `dev`/`none`/`unknown`
- 新增 `xsh version` 子命令，按 `xsh version v0.1.0 commit=<sha> built=<rfc3339>` 格式输出
- 根目录新增 `.goreleaser.yaml`：
  - `builds`：单 binary `xsh`，`goos=[linux]`、`goarch=[amd64, arm64]`、`env=[CGO_ENABLED=0]`
  - `ldflags`：注入 `main.version` / `main.commit` / `main.date`
  - `archives`：tar.gz，命名 `xsh_{{.Version}}_{{.Os}}_{{.Arch}}`，附 LICENSE + README.md
  - `checksum`：`checksums.txt`（sha256）
  - `changelog`：commit-based，过滤 `^chore:` `^docs:` `^test:`
- 新增 `.github/workflows/release.yml`：
  - 触发器：`push` tag `v*` + `workflow_dispatch`
  - 权限：`contents: write`
  - 步骤：checkout(`fetch-depth: 0`) → setup-go(`go-version-file: go.mod`) → goreleaser-action(`args: release --clean`)
- `README.md` 加 "Releases" 章节，指向 `https://github.com/senhao-xu/software-tools/releases/latest`
- 单 PR 交付（用户指定不拆分）

## Acceptance Criteria

- [ ] 本地 `go run ./cmd/xsh version` 输出 `xsh version dev commit=none built=unknown`
- [ ] 本地 `go build` + `go vet` + `go fmt` 通过，现有测试不受影响
- [ ] 推送 `v0.0.1` tag 到 origin 后 workflow 自动跑通（首次 dry run）
- [ ] Release 页面包含 `xsh_0.0.1_linux_amd64.tar.gz` 与 `xsh_0.0.1_linux_arm64.tar.gz` 及 `checksums.txt`
- [ ] 解压后 `./xsh version` 显示 `v0.0.1`，commit/built 非默认值

## Definition of Done

- workflow 文件 lint 通过（actionlint 或目测）
- README 增加 "Download from Releases" 章节（如范围内）
- 首次 dry-run（push 临时 tag）成功产出 Release
- Rollback 路径：删除 tag + release，无副作用

## Technical Approach

- 工具选型：**GoReleaser**（声明式、Go 生态事实标准）+ `goreleaser/goreleaser-action@v6`
- 触发：tag push `v*` + 手动 `workflow_dispatch`（重跑兜底）
- 版本注入：`-X main.version={{.Version}} -X main.commit={{.Commit}} -X main.date={{.Date}}`
- 单 PR：Go 改动 + 配置文件 + workflow + README 同时合入（用户指定）

## Decision (ADR-lite)

**Context**：xsh 是单 binary Go 工具，需要为 Debian/Ubuntu (amd64+arm64) 提供可下载的发版二进制。
**Decision**：采用 GoReleaser + GitHub Actions 自动化发版，tag push 触发，附带 sha256 checksums 与 commit-based changelog。
**Consequences**：
- 新增对 `.goreleaser.yaml` 配置文件的维护负担（小）
- 引入 GoReleaser 作为 CI 工具链依赖（pinned by goreleaser-action 版本）
- 后续如需 Docker image / deb / rpm，可在同一 `.goreleaser.yaml` 内扩展，无需重写

## Out of Scope (explicit)

- Windows / macOS 二进制（xsh 只跑 Linux）
- ARMv7 / 386 / mips 等小众架构
- 自动写 changelog 之外的版本管理（无 semantic-release）
- Docker image / apt repo 发布
- 代码签名 / cosign

## Technical Notes

- 仓库无 CGO，跨平台构建只需 `GOOS/GOARCH` 即可
- GitHub Actions 需要 `permissions: contents: write` 来创建 Release
- GoReleaser 0.x 配置文件 `.goreleaser.yaml`；GitHub Actions 官方 action：`goreleaser/goreleaser-action`
