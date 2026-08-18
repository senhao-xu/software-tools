# Add xsh install subcommand for apt packages

## Goal

Add an `xsh install <name>...` subcommand that installs common software via
apt, mapping friendly aliases (e.g. `python`) to their Debian-family package
sets (`python3 python3-pip python-is-python3`) while passing any other package
name straight through to `apt-get install -y`.

## Requirements

- New command `xsh install <name> [<name>...]` registered on the root command
  (`cmd/xsh/main.go`), implemented in `internal/cli/install.go` following the
  existing cobra command style (e.g. `docker.go`).
- Alias mapping table (initial set, extensible):
  - `python` -> `python3 python3-pip python-is-python3`
  - `nodejs` -> `nodejs` (from NodeSource, NOT distro apt), with a pre-install
    repo setup step: `curl -fsSL https://deb.nodesource.com/setup_22.x | bash -`
    run before `apt-get install` (equivalent of `sudo -E bash -`; xsh already
    requires root). Implemented as a generic per-name pre-install hook table
    so future repo-backed packages (e.g. other Node majors) are small additions.
  - `docker` / `k8s` are reserved words handled by existing subcommands and
    must NOT be shadowed; installing them via `xsh install` should error with
    a hint pointing at `xsh docker` / `xsh k8s`.
- Unknown names pass through verbatim to `apt-get install -y` (single apt
  invocation for the merged package list).
- Before install: run `apt-get update` (skip via `--no-update` flag).
- Reuse existing helpers: `xsh/internal/exec.Run` for command execution,
  existing root/OS checks from `PersistentPreRunE` (no duplication).
- `--yes/-y` flag mirrors existing convention: skip confirmation prompt when
  installing (default prompt lists the resolved package list once).

## Acceptance Criteria

- [ ] `xsh install python` executes
      `apt-get install -y python3 python3-pip python-is-python3` (after
      `apt-get update`), logging the command via `exec.Run` conventions.
- [ ] `xsh install nodejs` runs the NodeSource setup script
      (`curl -fsSL https://deb.nodesource.com/setup_22.x | bash -`) and then
      `apt-get install -y nodejs`. `curl | bash` requires streaming the pipe
      (bash reads stdin from curl output) - exec.Run's stdout-discard mode is
      fine, but stdin must be wired.
- [ ] `xsh install htop` installs the passthrough package `htop`.
- [ ] `xsh install python htop` resolves aliases and installs the merged
      package list in one apt invocation.
- [ ] `xsh install nodejs htop` runs the nodejs pre-install hook once, then
      one apt invocation for `nodejs htop`.
- [ ] `xsh install docker` and `xsh install k8s` error with a hint instead of
      installing.
- [ ] `xsh install` with no args prints usage error.
- [ ] `--no-update` skips `apt-get update`.
- [ ] `go build ./...`, `go vet ./...`, and existing tests pass; new command
      has unit tests for alias resolution and reserved-name rejection.
- [ ] README.md / README.zh-CN.md list the new subcommand.

## Notes

- Lightweight task: PRD-only planning.
- Design constraint: keep alias table a plain Go map in one place so future
  aliases are one-line additions.
