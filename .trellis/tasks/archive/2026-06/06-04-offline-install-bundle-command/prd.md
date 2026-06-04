# brainstorm: offline install bundle command

## Goal

Add a user-facing workflow for preparing an offline installation bundle ahead of time, then let install commands consume that bundle explicitly and predictably instead of relying on partial per-step fallback behavior.

## What I already know

* `xsh k8s` and `xsh k8s join` already accept `--assets-dir`.
* The current install path already supports partial offline behavior in several places:
* `internal/sysprep` reads `deb/ipvs/`.
* `internal/runtime/containerd` and `internal/runtime/docker` read `deb/docker/`.
* `internal/kube` reads `deb/kubernetes/`.
* `internal/kube/init` reads `images/*.tar`.
* `internal/network` reads `kube-flannel.yml` and `components.yaml`.
* Current behavior is permissive: if an expected offline asset is missing, most steps warn and fall back to online installation or download.
* There is no dedicated CLI command yet for collecting or packaging offline assets.
* `internal/assets/assets.go` exists only as a placeholder and is not yet the central resolver for offline resources.

## Assumptions (temporary)

* The new command will live under the existing `xsh` CLI rather than as a separate tool.
* The bundle should be suitable for both Kubernetes install flows and standalone Docker install, but scope may be reduced for MVP.
* "指定离线包路径" means explicit bundle path input from the install command, not just auto-discovery near the binary.

## Open Questions

* Should the first offline packaging command target `k8s` only, or both `k8s` and standalone `docker`?
* When `--assets-dir` is provided, should installation fail fast if required assets are missing, or continue with the current partial-online fallback?
* Should the packaging command output a directory tree, a single compressed archive, or both?

## Requirements (evolving)

* Provide one command that downloads required offline assets and packages them for later transport/use.
* Install commands must detect and use a user-specified offline bundle path.
* The offline workflow should be documented clearly enough that a user can prepare assets on a networked machine and install on an air-gapped machine.
* MVP scope is `xsh k8s` and `xsh k8s join` only; standalone `xsh docker` is out of scope for this task.
* When a user explicitly provides an offline bundle path, installation must use strict offline validation instead of silently falling back to online downloads.
* The prepare command should produce both an unpacked standard asset directory and a compressed `.tar.gz` archive.

## Acceptance Criteria (evolving)

* [ ] A user can run one command to prepare an offline install bundle.
* [ ] A user can point install commands at that bundle path.
* [ ] The program behavior is clear when the bundle path is invalid or incomplete.
* [ ] When an explicit offline bundle path is provided and required assets are missing, the install fails fast with a clear missing-assets error.
* [ ] The prepare command leaves a human-inspectable directory and a transport-friendly `.tar.gz`.
* [ ] Tests cover the new bundle detection and validation behavior.
* [ ] Docs explain the prepare-then-install offline flow.

## Definition of Done (team quality bar)

* Tests added/updated (unit/integration where appropriate)
* Lint / typecheck / CI green
* Docs/notes updated if behavior changes
* Rollout/rollback considered if risky

## Out of Scope (explicit)

* Multi-master HA or general package mirroring beyond what this tool needs
* Non-Debian-family operating systems
* Reworking every install step unless needed for the agreed offline UX
* Standalone `xsh docker` offline bundle preparation in this MVP

## Technical Notes

* Relevant code paths discovered from repo inspection:
* `internal/cli/k8s.go`
* `internal/cli/k8s_join.go`
* `internal/runtime/containerd/containerd.go`
* `internal/runtime/docker/docker.go`
* `internal/kube/kube.go`
* `internal/kube/init.go`
* `internal/network/network.go`
* `internal/sysprep/sysprep.go`
* `internal/assets/assets.go`
* Current README already defines an expected offline layout under `--assets-dir`, so the new command should likely produce that layout or an intentional evolution of it.

## Decision (ADR-lite)

**Context**: The new offline bundle flow could target Kubernetes only, Kubernetes plus standalone Docker, or a generic asset-packaging skeleton.

**Decision**: MVP targets Kubernetes flows only: `xsh k8s` and `xsh k8s join`.

**Consequences**: We can align the bundle layout tightly to existing `--assets-dir` consumers and keep the first implementation smaller. Standalone `xsh docker` can be added in a follow-up task.

**Context**: Current offline consumers usually fall back to online resources when bundle contents are missing. That is convenient on connected hosts but ambiguous for an explicitly requested offline install.

**Decision**: When the user explicitly passes an offline bundle path, treat that as strict offline mode and fail fast on missing required assets.

**Consequences**: Air-gapped installs become predictable, error reporting becomes more important, and install steps need a shared validation contract instead of independent silent fallback behavior.

**Context**: The downloaded assets need to be easy to inspect during debugging and easy to move to an offline host.

**Decision**: The prepare command produces both a directory tree and a `.tar.gz` archive.

**Consequences**: Install commands can continue to consume a directory path, while users get a single archive for transfer. The archive can be extracted on the target host before passing its directory to `xsh k8s --assets-dir`.
