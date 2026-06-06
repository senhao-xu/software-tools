# Fix kubeadm CRI Socket for Docker Runtime

## Goal

Ensure `xsh k8s --runtime=docker` can initialize Kubernetes on hosts where both `containerd` and `cri-dockerd` sockets exist by passing the selected CRI socket to kubeadm image pre-pull.

## What I Already Know

* The reported install fails during `kubeadm config images pull --kubernetes-version=v1.35.0`.
* kubeadm reports multiple CRI endpoints: `unix:///var/run/containerd/containerd.sock` and `unix:///var/run/cri-dockerd.sock`.
* `internal/kube/init.go` already has `criSocket(runtime)` and uses it for `kubeadm init`, rollback reset, worker join, and generated join commands.
* The online image pre-pull path currently does not pass a CRI socket.

## Requirements

* Online `kubeadm config images pull` must pass `--cri-socket=<selected socket>`.
* `--runtime=docker` must use `unix:///var/run/cri-dockerd.sock`.
* `containerd` and default runtime paths must keep using `unix:///var/run/containerd/containerd.sock`.
* Existing mirror behavior must be preserved.
* Tests must cover the full kubeadm argument vectors, not only `--cri-socket`.
* CRI socket constants and runtime mapping must be shared across kube init/join/reset, uninstall, and cleanup paths.

## Acceptance Criteria

* [x] A regression test covers Docker image pull args including `--cri-socket=unix:///var/run/cri-dockerd.sock`.
* [x] A regression test covers containerd/default or mirror image pull args so existing behavior stays intact.
* [x] Regression tests cover full `kubeadm init`, `kubeadm join`, and `kubeadm config images list` argument vectors.
* [x] Cleanup reset socket selection covers docker-only, containerd-only, and mixed-runtime hosts.
* [x] `go test ./internal/kube` passes.

## Definition of Done

* Tests added/updated where appropriate.
* Go formatting applied.
* Relevant Trellis specs considered for updates.

## Technical Approach

Use the existing `criSocket(runtime)` helper in the image pre-pull command builder, matching the kubeadm init/join/reset paths. Extract the image pull args into a small helper so command construction can be tested without executing kubeadm.

## Out of Scope

* Changing Docker/containerd installation behavior.
* Removing or disabling containerd when Docker runtime is selected.
* Pinning Kubernetes patch versions.

## Technical Notes

* Relevant implementation: `internal/kube/init.go`.
* Relevant tests: `internal/kube/init_test.go`.
* Relevant spec context: `.trellis/spec/backend/index.md`, `.trellis/spec/backend/quality-guidelines.md`, `.trellis/spec/guides/code-reuse-thinking-guide.md`.
