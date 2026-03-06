# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`kdiag` is a Kubernetes diagnostic CLI tool (Go). It inspects pod state and availability-zone distribution.

## Commands

```sh
# Build
make build               # go build -o kdiag .

# Unit tests (no cluster)
make test-unit           # go test ./internal/...
go test ./internal/kube/... # single package

# Integration tests (requires kind + kubectl)
make cluster-up          # creates kind cluster, applies fixtures
make test-integration    # KUBECONFIG=/tmp/kdiag-test.kubeconfig go test -v -tags integration ./test/
make cluster-down        # tears down the cluster

# Run all tests
make cluster-up && make test && make cluster-down
```

Integration tests use build tag `integration` and the kubeconfig at `/tmp/kdiag-test.kubeconfig`.

## Architecture

**Entry point:** `main.go` — top-level `switch` dispatches to `cmd.Run*` functions.

**Package layout:**

- `internal/kube/client.go` — `KubeFlags` (user-facing flags), `KubeEnv` (resolved clientset + namespace), `NewKubeEnv()`. Follows standard kubeconfig precedence: `--kubeconfig` flag → `$KUBECONFIG` → `~/.kube/config`.
- `internal/kube/helpers.go` — Pure utility functions: zone label lookup, container state decoding (`ContainerStateKey`, `ContainerStateReason`), resource extraction (`ResourcesForContainer`), and shared `GetOptions`/`ListOptions`.
- `internal/cmd/inspect_pod.go` — `RunInspect(args)` implementing `inspect pod`.
- `internal/cmd/az_pods.go` — `RunAZ(args)` implementing `az pods`.
- `internal/cli/format.go` — `NewTabWriter`, `PrintKVBlock`.
- `internal/cli/usage.go` — `PrintUsage`.
- `internal/cli/errors.go` — `Fatal`.

**Adding a command:**

1. Create `internal/cmd/<name>.go` with `Run<Name>(args []string)`.
   - Parse flags with `pflag.NewFlagSet`, build `kube.KubeFlags`, call `kube.NewKubeEnv(k)`.
2. Add `case "<name>": cmd.Run<Name>(args[1:])` in `main.go`.
3. Add usage text in `internal/cli/usage.go`.
4. Update README.md, if needed.

Kubernetes utility helpers belong in `internal/kube/helpers.go` for reuse across commands.

## Test fixtures

`test/fixtures/kdiag-test.yaml` creates namespace `kdiag-test` with:

- A deployment (`app=test-app`) for label-selector tests.
- A static pod `kdiag-static` for single-pod tests.
- A crashing pod to exercise terminated/waiting container states.
