# Code Review Findings

## Findings

### Critical

1. `internal/cli/k8s.go:149`, `internal/cli/k8s_bundle.go:50`, `internal/kube/kube.go:174`, `internal/kube/kube.go:204`, `internal/aptrepo/aptrepo.go:211`, `internal/aptrepo/aptrepo.go:216`: unvalidated `--version` input reaches a concatenated `bash -c` command. A value such as `v1.35;id>/tmp/pwn;#` survives `minorVersion` and executes shell syntax as root when the Kubernetes keyring is absent. Validate version syntax at the CLI boundary and replace the shell pipeline with argument-safe process execution.

### High

2. `internal/exec/exec.go:17`, `internal/exec/exec.go:68`, `internal/kube/join.go:113`, `internal/kube/init.go:547`, `internal/kube/init.go:551`, `internal/kube/init.go:555`: kubeadm bootstrap tokens are logged verbatim and the generated join script is created with mode `0755` under a `0755` directory. Other local users can read a still-valid cluster join credential. Redact sensitive arguments, use directory mode `0700` and file mode `0600`, and do not print the complete join command by default.

3. `internal/detect/detect.go:98`, `internal/detect/detect.go:113`, `internal/detect/detect.go:120`, `internal/detect/detect.go:158`: the overwrite prompt treats Enter as approval, then cleanup removes Kubernetes, Docker, containerd, etcd, and container data. An accidental Enter can destroy an existing host. Default to cancellation and require explicit confirmation for runtime data removal.

4. `internal/offlinebundle/offlinebundle.go:317`, `internal/offlinebundle/offlinebundle.go:323`, `internal/kube/kube.go:152`, `internal/kube/kube.go:157`: bundle creation downloads explicit packages with the preparation host's live APT/dpkg state. Already-installed transitive dependencies may not be copied, so a bundle can pass creation and fail on a clean disconnected host. Resolve dependencies against isolated empty state or build a local APT repository, then test in clean offline VMs.

### Medium

5. `.opencode/lib/session-utils.js:407`, `.opencode/lib/session-utils.js:412`, `.claude/hooks/session-start.py:691`, `.claude/hooks/session-start.py:702`, `.trellis/workflow.md:275`, `.trellis/workflow.md:283`: the new compact SessionStart removes platform block markers but retains both mutually exclusive bodies. Agent-dispatch and inline-routing instructions are therefore injected together. Filter platform blocks before stripping markers or preserve markers for a later platform-aware consumer.

6. `internal/assets/assets.go:34`, `internal/assets/assets.go:54`, `internal/assets/assets.go:55`, `internal/assets/assets.go:82`: offline validation only checks that any glob match exists. It does not require each Kubernetes package or verify file type, version, architecture, or checksum, so incomplete/corrupt bundles fail after system changes begin. Generate and verify a manifest with required package identities and SHA-256 values.

7. `cmd/xsh/main.go:33`, `cmd/xsh/main.go:41`, `cmd/xsh/main.go:83`, `internal/detect/detect.go:172`: non-root execution only warns. The flow can delete the invoking user's `~/.kube` before privileged commands fail. Fail preflight on Linux for all mutating commands while retaining exemptions for help, completion, and version.

### Low

8. `.trellis/.template-hashes.json:45`, `.opencode/package.json:3`: the stored template hash is `4b155e...`, while the file is `ea056e...`. Future updates can misclassify a managed dependency update as a local customization. Regenerate the hash from the authoritative template or document the file as a deliberate local override.

9. `.opencode/commands/trellis/start.md:3`, `.opencode/plugins/session-start.js:40`, `.opencode/plugins/session-start.js:69`: the new start command says OpenCode has no SessionStart hook, but the plugin already injects equivalent context. Running it duplicates work and communicates the wrong runtime contract. Define it as an explicit refresh/fallback or remove it.

## Verification

- Configuration review: Python syntax, JavaScript syntax, JSON/TOML parsing, task manifest validation, template-hash comparison, and platform phase-context checks passed except for the reported hash mismatch.
- `git diff --check` fails on widespread existing CRLF/trailing-whitespace churn, especially archived task metadata, workspace journals, and `LICENSE`; this was not treated as a behavioral defect.
- `go test ./...`, `go vet ./...`, and `go build ./cmd/xsh` could not run because the environment has no `go` executable.
- No Debian/Ubuntu VM or destructive Kubernetes/Docker end-to-end command was run.
