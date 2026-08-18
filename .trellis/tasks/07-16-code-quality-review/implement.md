# Review Execution Plan

## 1. Establish Evidence

- [x] Capture the exact tracked and untracked diff without changing the worktree.
- [x] Map Go packages, tests, command entry points, and documented behavior.
- [x] Identify authoritative Trellis sources versus generated platform copies and backups.

## 2. Review Go Application

- [x] Review CLI validation and option propagation.
- [x] Review command execution, downloads, paths, permissions, and external-input handling.
- [x] Review install, join, bundle, cleanup, uninstall, and rollback flows.
- [x] Cross-check tests and README contracts, including the CRI socket contract.
- [x] Run `go test ./...`, `go vet ./...`, and `go build ./cmd/xsh`. (Blocked: `go` is not installed.)

## 3. Review Trellis And AI Configuration Diff

- [x] Review task/session state and workflow phase handling.
- [x] Review hooks, context injection, safe commit behavior, and CLI adapters.
- [x] Compare platform-specific generated copies for behavioral drift.
- [x] Run discovered non-destructive syntax checks or tests.

## 4. Validate Findings

- [x] Re-read every candidate in its full call path and discard speculative or style-only items.
- [x] Confirm every finding has severity, impact, evidence, `file:line`, and remediation.
- [x] Report test gaps and residual environment-specific risks separately.

## Rollback

No reviewed code is modified. If a verification command creates a build artifact already ignored by Git, remove only that newly created artifact after confirming it was produced by this review.
