# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`kdiag` is a Kubernetes diagnostic CLI tool (Go). It inspects pod state and availability-zone distribution.

## Commands

```sh
# Build
make build               # go build -o kdiag .

# Unit tests (no cluster)
make unit-tests          # go test ./internal/...
go test ./internal/kube/... # single package

# Integration tests (requires kind + kubectl)
make cluster-up          # creates kind cluster, applies fixtures
make integration-tests   # KUBECONFIG=/tmp/kdiag-test.kubeconfig go test -v -tags integration ./test/
make cluster-down        # tears down the cluster

# Run all tests
make cluster-up && make test && make cluster-down
```

Integration tests use build tag `integration` and the kubeconfig at `/tmp/kdiag-test.kubeconfig`.

## Architecture

**Entry point:** `main.go` — top-level `switch` dispatches to `cmd.Run*` functions.

**Package layout:**

- `internal/kube/client.go` — `KubeFlags` (just `Namespace`), `KubeEnv` (resolved clientset + namespace), `NewKubeEnv()`. Follows standard kubeconfig precedence: `$KUBECONFIG` → `~/.kube/config`. There is no `--kubeconfig`/`--context` flag — set the env var or switch context with `kubectl` first.
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

## General rules

After any code change:
- Always check if `README.md` needs an update and apply it without being asked.
- Always check if existing tests need updating and add new tests to cover the change. For `internal/kube` and `internal/cli` use unit tests. For `internal/cmd` use integration tests in `test/integration_test.go`. Run `make unit-tests` to verify nothing is broken.

## Test fixtures

`make cluster-up` creates a 4-node kind cluster (`test/kind-config.yaml`: 1
control-plane + 3 workers). worker/worker2 are labelled with zones + instance
types and stay schedulable; worker3 is cordoned + tainted as a broken-node demo.

`test/fixtures/kdiag-test.yaml` creates namespace `kdiag-test` (used by the
integration tests) with:

- A deployment (`app=test-app`) for label-selector tests.
- A static pod `kdiag-static` for single-pod tests.
- A crashing pod to exercise terminated/waiting container states.
- `kdiag-unschedulable` for `inspect pod --troubleshoot` scheduling tests.

`test/fixtures/scenarios.yaml` is a separate manual-testing playground (NOT used
by integration tests) with `kdiag-scheduling` / `kdiag-runtime` /
`kdiag-workloads` namespaces of healthy + deliberately-broken resources for
exercising `--troubleshoot` by hand.

`--troubleshoot` is a universal `inspect` view handled centrally in the
`RunInspect` dispatcher (like `--path`): `internal/cmd/inspect_troubleshoot.go`
orchestrates and renders; pure diagnostics live in `internal/kube/diagnose.go`
(runtime/node) and `internal/kube/schedule.go` (scheduling predicates), both
unit-tested.
