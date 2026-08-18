# Add java and maven install support

## Goal

Extend `xsh install` so `xsh install java maven` installs a controllable,
cross-distro JDK (Eclipse Temurin via the Adoptium apt repo) and Apache Maven
(distro apt), following the existing alias + pre-hook architecture.

## Background

- `xsh install` (internal/cli/install.go) resolves friendly names via
  `installAliases` (name -> apt package set) and runs `installPreHooks`
  (third-party repo setup) before a single `apt-get install -y`.
- Current flow is: `apt-get update` -> pre-hooks -> `apt-get install`. The
  Adoptium hook adds an apt repo, so an additional `apt-get update` is needed
  after hooks that modify sources.
- Distro apt OpenJDK versions vary by release (Debian 12 -> 17, Ubuntu 24.04
  -> 21); Adoptium gives a stable source for LTS 8/11/17/21.

## Requirements

- `xsh install java` installs Temurin 21 JDK (current LTS) from Adoptium:
  pre-hook sets up keyring (`/etc/apt/keyrings/adoptium.gpg`) and sources
  entry (`/etc/apt/sources.list.d/adoptium.list`) using the distro codename,
  then refreshes the apt index.
- Alias `maven` maps to apt package `maven`; no third-party repo needed.
- Ordering: pre-hooks that add apt sources must be followed by an apt index
  refresh before `apt-get install`. Fix applies generically (run one
  `apt-get update` after hooks when any hook ran), not java-specific hardcode.
- `--no-update` keeps its current meaning (skip the initial update); the
  post-hook refresh still runs when a hook modified sources.
- Reserved-name behavior and confirmation flow unchanged.
- Unit tests for: new aliases resolution, hook collection/dedup including the
  new java hook, and the update-after-hook behavior decision logic.
- README.md / README.zh-CN.md updated with `xsh install java maven` examples.

## Non-Goals

- Interactive Java major-version selection (`--major` for java) — default 21
  only; version pinning can be a follow-up.
- Non-Debian/Ubuntu support.

## Acceptance Criteria

- [ ] `resolveInstallPackages(["java"])` yields `temurin-21-jdk`; `["maven"]`
      yields `maven`; `["java","maven"]` yields both deduplicated.
- [ ] `collectInstallPreHooks(["java"])` returns the Adoptium setup snippet.
- [ ] Install flow runs `apt-get update` again after any pre-hook that adds
      sources (unit-tested decision logic).
- [ ] `go test ./...` and `go build ./...` pass.
- [ ] READMEs document the new aliases.

## Notes

- Adoptium repo setup snippet follows the same keyring + sources.list.d
  pattern as aptrepo / NodeSource hooks.
