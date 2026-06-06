# Quality Guidelines

> Code quality standards for backend development.

---

## Overview

<!--
Document your project's quality standards here.

Questions to answer:
- What patterns are forbidden?
- What linting rules do you enforce?
- What are your testing requirements?
- What code review standards apply?
-->

(To be filled by the team)

---

## Forbidden Patterns

<!-- Patterns that should never be used and why -->

(To be filled by the team)

---

## Required Patterns

### Kubernetes CRI Socket Contract

#### 1. Scope / Trigger

This contract applies to Kubernetes installer code that runs `kubeadm` with a
container runtime choice (`containerd` or `docker + cri-dockerd`). It is required
for image pulls, init, join, reset, and generated worker join commands.

#### 2. Signatures

The kubeadm command signatures must include the selected socket:

```text
kubeadm config images pull --kubernetes-version=<version> --cri-socket=<socket>
kubeadm init ... --cri-socket=<socket>
kubeadm join ... --cri-socket=<socket>
kubeadm reset -f --cri-socket=<socket>
```

#### 3. Contracts

Runtime-to-socket selection is centralized through `internal/cri.Socket(runtime)`:

```text
runtime="docker"     -> unix:///var/run/cri-dockerd.sock
runtime="containerd" -> unix:///var/run/containerd/containerd.sock
runtime=""/unknown   -> unix:///var/run/containerd/containerd.sock
```

Do not duplicate these string constants in new kubeadm command builders. Reuse
`internal/cri` directly, or reuse a package-local wrapper that delegates to it,
so image pull, init, join, reset, cleanup, uninstall, and generated join command
behavior cannot drift.

#### 4. Validation & Error Matrix

| Condition | Required behavior |
|---|---|
| Docker runtime selected and containerd also exists | kubeadm commands use the cri-dockerd socket |
| Containerd runtime selected | kubeadm commands use the containerd socket |
| Runtime is empty or unknown | kubeadm commands fall back to the containerd socket |
| China mirror selected | image repository flag is added without dropping `--cri-socket` |

#### 5. Good / Base / Bad Cases

Good: `--runtime=docker` produces `--cri-socket=unix:///var/run/cri-dockerd.sock`
for `kubeadm config images pull` and `kubeadm init`.

Base: default runtime produces
`--cri-socket=unix:///var/run/containerd/containerd.sock`.

Bad: any kubeadm command depends on kubeadm CRI auto-detection. Hosts with both
Docker/cri-dockerd and containerd can fail with "found multiple CRI endpoints".

#### 6. Tests Required

Bug fixes or new kubeadm command builders must assert:

* Docker runtime includes the cri-dockerd socket.
* Default/containerd runtime includes the containerd socket.
* Mirror-specific flags remain present when socket flags are added.
* Full command argument vectors include every user-facing option relevant to
  that kubeadm subcommand.

#### 7. Wrong vs Correct

Wrong:

```go
args := []string{"config", "images", "pull", "--kubernetes-version=" + opts.Version}
```

Correct:

```go
args := []string{
	"config",
	"images",
	"pull",
	"--kubernetes-version=" + opts.Version,
	"--cri-socket=" + cri.Socket(opts.Runtime),
}
```


---

## Testing Requirements

<!-- What level of testing is expected -->

(To be filled by the team)

---

## Code Review Checklist

<!-- What reviewers should check -->

(To be filled by the team)
