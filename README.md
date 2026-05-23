# software-tools

Single-binary CLI (`xsh`) that installs Kubernetes and Docker on Debian 12/13.

## Status

PR1 (skeleton) — `xsh` is runnable, command tree is wired (`k8s`, `k8s join`, `docker`), but every install step prints "not yet implemented". Real logic lands in later PRs.

## Build

```
make build
./bin/xsh --help
```

Requires Go 1.25+. Target platform is Linux (Debian 12 bookworm / 13 trixie); the binary builds and runs `--help` on Windows for development.
