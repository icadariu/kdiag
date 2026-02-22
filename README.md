# kdiag

A Kubernetes diagnostic CLI tool for inspecting pod state
and availability-zone distribution.

## Commands

### `inspect pod`

Show the state of one pod or a group of pods matched by a label selector.

```text
kdiag inspect pod [flags] <pod_name>
kdiag inspect pod [flags] -l <label_selector>
```

Output per container:

- State (`running` / `waiting` / `terminated`) with reason
- Last termination state
- Ready status
- Restart count
- Optionally: CPU/memory requests and limits (`--resources`)

### Examples

```sh
# Single pod
kdiag inspect pod gateway-proxy-abc123

# Single pod in a specific namespace with resource usage
kdiag inspect pod -n gloo-system --resources gateway-proxy-abc123

# All pods matching a selector
kdiag inspect pod -n gloo-system -l 'app=gateway-proxy'
```

---

### `az pods`

List pods with their node assignment and availability zone, then print
a per-zone count summary. Useful for verifying that a workload is spread
evenly across failure domains.

Zone is derived from node labels in this order:

1. `topology.kubernetes.io/zone`
2. `failure-domain.beta.kubernetes.io/zone` (legacy fallback)

```text
kdiag az pods [flags] -l <label_selector>
```

### Example

```sh
kdiag az pods -n gloo-system -l 'app=gateway-proxy'
```

Sample output:

```text
Namespace: gloo-system
Pods placement (selector: app=gateway-proxy)
------------------------------------------
POD                        NODE             ZONE
gateway-proxy-abc123       node-1           eu-west-1a
gateway-proxy-def456       node-2           eu-west-1b
gateway-proxy-ghi789       node-3           eu-west-1c

Summary (pods per ZONE):
  1  eu-west-1a
  1  eu-west-1b
  1  eu-west-1c
```

---

## Common flags

| Flag | Short | Description |
| ------ | ------- | ------------- |
| `--kubeconfig <path>` | | Path to kubeconfig file |
| `--context <name>` | | Kubernetes context to use |
| `--namespace <ns>` | `-n` | Namespace (defaults to current context) |

All flags follow the same kubeconfig precedence as `kubectl`:
`--kubeconfig` flag → `$KUBECONFIG` env var → `~/.kube/config`.

---

## Installation

**Build from source** (requires Go 1.22+):

```sh
git clone <repo>
cd kdiag-go
go build -o kdiag .
```

Move the binary somewhere on your `$PATH`:

```sh
mv kdiag /usr/local/bin/
```

---

## Project layout

```text
main.go                    # Entry point — routes top-level commands
internal/
  kube/
    client.go              # KubeFlags, KubeEnv, NewKubeEnv
    helpers.go             # Zone lookup, container state, resource extraction
  cli/
    usage.go               # PrintUsage
    format.go              # NewTabWriter, PrintKVBlock
    errors.go              # Fatal
  cmd/
    inspect_pod.go         # inspect pod command
    az_pods.go             # az pods command
```

### Adding a new command

1. Create `internal/cmd/<name>.go` and implement `Run<Name>(args []string)`
   using the same pattern as the existing commands:
   - Parse flags with a `FlagSet` that supports interspersed arguments,
     so the pod name can appear before or after flags (like `kubectl`)
   - Build a `*kube.KubeEnv` via `kube.NewKubeEnv(k)`
   - Use helpers from `internal/kube` and `internal/cli` as needed
2. Add a `case "<name>": cmd.Run<Name>(args[1:])` branch in `main.go`
3. Add usage text to `internal/cli/usage.go`

Kubernetes-specific utility functions (zone lookup, container state decoding,
resource extraction) belong in `internal/kube/helpers.go` so they can be
reused across commands.

---

## Testing

### Unit tests

No cluster required. Covers pure logic: zone label lookup, container state
decoding, resource extraction, and output formatting.

```sh
go test ./internal/...
```

The `...` wildcard is required — `./internal` alone has no Go files and will
fail. It must recurse into the sub-packages (`cli`, `kube`).

### Integration tests

Requires [kind](https://kind.sigs.k8s.io) and `kubectl`. The Makefile manages
the cluster lifecycle.

```sh
# 1. Create a kind cluster and apply test fixtures
make cluster-up

# 2. Run integration tests against it
make test-integration

# 3. Tear down when done
make cluster-down
```

Run everything in one shot:

```sh
make cluster-up && make test && make cluster-down
```

Test fixtures live in `test/fixtures/kdiag-test.yaml` and create a dedicated
`kdiag-test` namespace with a deployment, a static pod with known name, and a
crashing pod to exercise different container states.

---

## Requirements

- Go 1.22+
- A reachable Kubernetes cluster and a valid kubeconfig
