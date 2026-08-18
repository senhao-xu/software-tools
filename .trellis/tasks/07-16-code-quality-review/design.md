# Code Quality Review Design

## Review Boundaries

The review has two evidence streams that converge into one severity-ordered report:

1. Go application code under `cmd/` and `internal/`, including unit tests and user-visible contracts documented in the README.
2. The current uncommitted diff for Trellis and AI-platform configuration under `.trellis/`, `.agents/`, `.claude/`, `.codex/`, and `.opencode/`.

Existing unrelated working-tree changes are read-only. Generated copies and backups are sampled for synchronization defects rather than reviewed repeatedly as independent implementations.

## Review Method

### Go Application

- Trace CLI input through command construction, privileged system changes, rollback, and user-visible output.
- Prioritize correctness, destructive-operation safeguards, command injection or argument errors, filesystem safety, runtime selection, online/offline behavior, and idempotency.
- Compare implementations with tests, README contracts, and the centralized CRI socket contract.
- Use `go test ./...`, `go vet ./...`, and `go build ./cmd/xsh` as non-destructive verification.

### Trellis And AI Configuration

- Review `git diff` and untracked source files, focusing on behavior changes rather than formatting churn.
- Trace task/session identity, workflow-state injection, context loading, safe commit behavior, and cross-platform generated-file consistency.
- Distinguish authoritative source defects from stale generated copies and backup-only differences.
- Run available syntax or package tests only when they are discoverable and non-destructive.

## Finding Contract

Each reported finding must include severity, `file:line`, concrete impact, supporting execution path or evidence, and a focused remediation. Findings are ordered by severity. Pure preferences and speculative concerns are omitted.

## Safety And Limitations

- Do not invoke commands that install packages, alter services, reset Kubernetes, or modify host configuration.
- Do not edit reviewed code.
- Unit tests and static checks cannot replace Debian/Ubuntu VM coverage; remaining end-to-end risk is stated explicitly.
