# kdiag

A Kubernetes diagnostic CLI tool for inspecting pod state
and availability-zone distribution.

Help is nested kubectl-style: `kdiag -h` lists top-level commands,
`kdiag inspect -h` lists kinds, and `kdiag inspect pod -h` shows that
leaf's flags and examples.

## Commands

### `inspect pod`

Show the state of one pod or a group of pods matched by a label selector.

```text
kdiag inspect pod [flags]
kdiag inspect pod [flags] <partial-pod-name>
kdiag inspect pod [flags] -l <label>
kdiag inspect po  [flags] <partial-pod-name>
kdiag inspect po  [flags] -l <label>
```

With no name and no selector, lists every pod in the namespace.
A partial name is matched as a substring against all pods in the namespace —
errors if zero or more than one pod matches.

Pod-level summary:

- Node, Pod IP, QoS class

Per-container output:

- Image, Tag (or Digest for sha-pinned refs)
- Ports
- State (`running` / `waiting` / `terminated`) with reason
- Last termination state
- Ready status
- Restart count

Pass `--az` to show a POD/NODE/ZONE table and per-zone count summary
instead of container state.

#### Pod YAML flags

`--container-spec` and `--resources` emit pure YAML on stdout — pipeable to
`yq`. They are mutually exclusive and incompatible with `--az`.

| Flag | Emits |
| ---- | ----- |
| `--container-spec` | `.spec.containers[]` of the pod |
| `--resources` | `[{ name, resources }]` for each container |

With a positional partial name, output is flat YAML. With `--label`, output
is a YAML map keyed by pod name (also when only one pod matches), so
downstream pipelines stay predictable.

### Examples

```sh
# Single pod by partial name
kdiag inspect pod gateway-proxy

# All pods matching a selector
kdiag inspect pod -n example-system -l 'app=gateway-proxy'

# Availability-zone placement for all pods in namespace
kdiag inspect pod --az -n example-system

# AZ placement filtered by selector
kdiag inspect pod --az -n example-system -l 'app=gateway-proxy'

# Single pod, resources as YAML (yq-pipeable)
kdiag inspect pod --resources -n example-system gateway-proxy

# Container spec, list container names with yq
kdiag inspect pod --container-spec gateway-proxy | yq '.[].name'

# Resources for every matching pod (YAML map keyed by pod name)
kdiag inspect pod --resources -l 'app=gateway-proxy' | yq 'keys'
```

---

### `inspect deploy` / `ds` / `sts` / `rs`

Show a kind-specific workload summary on top of the per-pod container
state. Pod selection follows each workload's own `Spec.Selector`.

```text
kdiag inspect deploy [flags] [<deployment-name> | -l <label>]
kdiag inspect ds     [flags] <daemonset-name>
kdiag inspect sts    [flags] <statefulset-name>
kdiag inspect rs     [flags] <replicaset-name>
```

For `inspect deploy`, the deployment can be identified either by positional
name or by `--label`/`-l` (which must match exactly one Deployment in the
namespace — mirrors `diff rs`).

Aliases match `kubectl`: `deploy` ↔ `deployment`, `ds` ↔ `daemonset`,
`sts` ↔ `statefulset`, `rs` ↔ `replicaset`.

For `ds`/`sts`/`rs`, `--resources` shows per-container CPU/memory requests
and limits as a text block alongside the container state.
`--az` shows a POD/NODE/ZONE placement table and per-zone summary instead
of the container state blocks.

Workload summary fields:

| Kind          | Summary                                          |
| ------------- | ------------------------------------------------ |
| `deploy`      | Replicas, Strategy, Selector                     |
| `ds`          | Replicas, Update Strategy, Selector              |
| `sts`         | Replicas, Service Name, Update Strategy, Selector |
| `rs`          | Replicas, Selector, Owner (controlling deploy)   |

#### Deployment template (YAML) flags

`inspect deploy` additionally accepts YAML-mode flags that emit subtrees of
the deployment's pod template (`.spec.template`), designed for piping into
`yq`. They are mutually exclusive and incompatible with `--az`.

| Flag | Emits |
| ---- | ----- |
| `--resources` | `[{ name, resources }]` for each container in `.spec.template.spec.containers` |
| `--spec` | `.spec.template.spec` (the full PodSpec) |
| `--container-spec` | `.spec.template.spec.containers[]` |

For `inspect deploy`, `--resources` operates on the deployment template (no
pod lookup). For `ds`/`sts`/`rs`, `--resources` keeps the per-pod text-block
meaning.

### Examples

```sh
# Inspect all pods in a deployment
kdiag inspect deploy my-deployment

# Identify the deployment via label instead of name
kdiag inspect deploy -n example-system -l 'app=my-app'

# In a specific namespace
kdiag inspect deploy -n example-system my-deployment

# AZ placement for a deployment's pods
kdiag inspect deploy --az -n example-system my-deployment

# Daemonset, statefulset, replicaset
kdiag inspect ds  -n kube-system kube-proxy
kdiag inspect sts -n my-ns my-statefulset
kdiag inspect rs  -n my-ns my-replicaset-abc123

# Deployment template as YAML (pipe into yq)
kdiag inspect deploy --resources      -n my-ns my-deployment
kdiag inspect deploy --spec           -n my-ns my-deployment
kdiag inspect deploy --container-spec -n my-ns my-deployment | yq '.[].name'
```

---

### `inspect node`

Show a per-node summary for one node or a set of nodes. Nodes are
cluster-scoped, so `-n/--namespace` is accepted (for uniform CLI shape) but
silently ignored.

Node summary fields:

- Zone, Instance Type, Kubelet Version
- Taints
- Conditions: Ready, MemoryPressure, DiskPressure, PIDPressure
- Allocatable: cpu, memory, pods (and any other resources the node exposes)

```text
kdiag inspect node [<node-name> | -l <label>]
kdiag inspect no   [<node-name> | -l <label>]
```

### `inspect node` examples

```sh
# Single node by name
kdiag inspect node my-node

# All nodes in a zone
kdiag inspect node -l topology.kubernetes.io/zone=eu-west-1a

# All nodes in the cluster
kdiag inspect node
```

---

### `inspect --yaml-field`

Find the `yq` path of any key or value inside a resource's YAML. Useful when
you know the keyword (`Burstable`, `imagePullPolicy`, …) but not where it
sits in the object. Works for **every** kind the cluster exposes, including
CRDs.

Output is `<yq-path>: <value>` per line. Array elements render the path
with a generalized `[]` (yq-pipeable, iterates all elements). When the
array has more than one element **and** each element carries a `name`
field (containers, ports, volumes, …), each match is preceded by a
`# name=<n>` header line so siblings stay distinguishable. Single-element
*named* arrays suppress the annotation (nothing to disambiguate);
unnamed arrays never had one. Identical blocks are deduplicated.

Multi-line string values (e.g. ConfigMap `data` keys with embedded
newlines) render Go-quoted (`"line1\nline2"`) so each match stays on one
physical line — re-run `yq <path>` on the raw resource to read the value
unescaped.

Key-match recursion: when a needle matches a *key* the walker emits the
match and continues descending into the value, so common needles like
`name` or `spec` will surface every nested occurrence. This is
intentional — `--yaml-field` is grep-like, not deepest-match-only.

Smart-case matching, like ripgrep: an **all-lowercase** needle is
case-insensitive; any uppercase character makes the match case-sensitive.

```text
kdiag inspect <kind> [<name> | -l <label>] --yaml-field <keyword>
```

```sh
# Find the yq path of a value (case-sensitive — `Burstable` has uppercase)
kdiag inspect pod my-pod --yaml-field Burstable
# .status.qosClass: Burstable

# Find the yq path of a key, case-insensitive (multi-container deployment)
kdiag inspect deploy my-deploy --yaml-field imagePull
# # name=app
# .spec.template.spec.containers[].imagePullPolicy: IfNotPresent
# # name=sidecar
# .spec.template.spec.containers[].imagePullPolicy: Always

# Search across all pods matched by a selector
kdiag inspect pod -l app=my-app --yaml-field qosClass
# pod/my-app-7d4...-abcd:
#   .status.qosClass: Burstable

# Works for CRDs too
kdiag inspect certificates.cert-manager.io my-cert --yaml-field renewBefore
```

---

### `diff rs`

Diff the pod template between two ReplicaSet revisions of a deployment.
Covers both `spec.template.metadata` (labels, annotations) and
`spec.template.spec` (containers, probes, resources, etc.).

Without an explicit revision pair, diffs the previous and current
revision (last two). Pass `<rev-from> <rev-to>` (the revision numbers
shown by `kubectl rollout history deployment/<name>`) to compare any
two revisions; order is preserved in the output.

Output uses coloured unified diff (`diff --color=always -u`).

```text
kdiag diff rs [flags] <deployment-name> [<rev-from> <rev-to>]
kdiag diff rs [flags] -l <label> [<rev-from> <rev-to>]
```

`diff rs` also accepts the generic two-name form
(`kdiag diff rs <rs-a> <rs-b>`); passing `--full` to either form
dumps the full RS objects (managedFields preserved) instead of just
`.spec.template`.

### `diff rs` examples

```sh
# By deployment name (default: last two revisions)
kdiag diff rs -n my-ns my-deployment

# Specific revisions — compare current with three behind
kdiag diff rs -n my-ns my-deployment 2 5

# By label selector (errors if more than one deployment matches)
kdiag diff rs -n my-ns -l 'app=my-app'

# Selector + specific revisions
kdiag diff rs -n my-ns -l 'app=my-app' 1 3

# Two RS by name (generic shape, full objects)
kdiag diff rs -n my-ns my-rs-abc my-rs-def --full
```

If a requested revision isn't in the deployment's history, the error
lists the available revisions.

Sample output:

```text
Deployment: my-ns/my-deployment
Diff: revision 1 → 2

--- revision/1 (my-deployment-74494695b8)
+++ revision/2 (my-deployment-769554785d)
@@ -3,7 +3,7 @@
     labels:
       app: my-deployment
-      stage: v1
+      stage: v2
 spec:
   containers:
   - livenessProbe:
```

---

### `diff <any-kind>` (generic two-name)

Diff two resources of the same kind as coloured unified YAML. Works for
any kind the cluster exposes — built-in (pod, node, configmap, secret,
service, deployment, …) or CRD. Resolution is done via the cluster's
discovery doc, so shortnames (`cm`, `svc`, `ing`, `pvc`) and
group-qualified forms (`certificates.cert-manager.io`) work the same
way they do in `kubectl`.

By default the diff is **opinionated for investigation**: per-kind noise is
stripped to surface what matters. All kinds first strip etcd bookkeeping
(`resourceVersion`, `uid`, `generation`, `creationTimestamp`), `managedFields`,
the `kubectl.kubernetes.io/last-applied-configuration` annotation,
`status.observedGeneration`, and per-container runtime IDs.

Then, kind-specific cleanup is applied:

- **Pod**: entire `status` hidden, `spec.nodeName` hidden (pods land on different
  nodes), pod-template-hash and controller labels hidden, auto-injected tolerations
  hidden, metadata annotations hidden.
- **Deployment/StatefulSet/DaemonSet/ReplicaSet**: entire `status` hidden,
  `deployment.kubernetes.io/revision` annotation hidden, pod-template-hash from
  selectors and labels hidden.
- **Service**: entire `status` hidden, cluster-assigned IPs and port assignments
  (`clusterIP`, `clusterIPs`, `ipFamilies`, `ipFamilyPolicy`, `internalTrafficPolicy`,
  `ports[].nodePort`) hidden.
- **Node**: entire `status` hidden (capacity, conditions, addresses, etc.).
- **Ingress**: entire `status` hidden (loadBalancer assignments).
- **PersistentVolumeClaim**: entire `status` hidden, provisioner annotations and
  `spec.volumeName` hidden.
- **PersistentVolume**: entire `status` hidden, claimRef metadata hidden.
- **ConfigMap/Secret**: baseline stripping only (no kind-specific changes).
- **Other kinds (CRDs, etc.)**: baseline stripping only.

Pass `--full` to emit the API server response verbatim with no
stripping at all. Use this when you specifically want the raw "compare
two files in Linux" view.

```text
kdiag diff <kind> [-n <ns>] [--full] <name-a> <name-b>
```

### `diff <any-kind>` examples

```sh
kdiag diff pod    -n my-ns pod-abc123 pod-def456
kdiag diff cm     -n my-ns config-a config-b
kdiag diff svc    -n my-ns api-v1 api-v2
kdiag diff deploy -n my-ns app-blue app-green
kdiag diff node   node-1 node-2
kdiag diff pod    -n my-ns a b --full      # include status, managedFields, annotations
```

---

### `events`

Show recent events (Normal and Warning) in the current namespace.

```text
kdiag events [-n <ns> | -A] [--since <duration>]
```

| Flag | Default | Meaning |
|---|---|---|
| `-n, --namespace` | current context namespace | Namespace to inspect |
| `-A, --all-namespaces` | false | List events across all namespaces (overrides `-n`) |
| `--since` | `1h` | Only show events newer than this duration. Accepts Go duration syntax (e.g. `30s`, `5m`, `2h`) |

Output columns: `AGE`, `TYPE`, `REASON`, `OBJECT` (`Kind/name`), `MESSAGE`.
With `-A`, a `NAMESPACE` column is inserted after `AGE`.
Sorted by effective event timestamp ascending — newest entry is last, the same orientation as `kubectl logs`.
The effective timestamp falls back across `Series.LastObservedTime` → `LastTimestamp` → `EventTime` → `FirstTimestamp` → `CreationTimestamp`, so events emitted via `events.k8s.io/v1` (e.g. `FailedScheduling` from the scheduler) are included.

### Examples

```sh
# All events in the current-context namespace (last 1h)
kdiag events

# All events across all namespaces (last 30 minutes)
kdiag events -A --since 30m

# Look back 24h
kdiag events -n my-ns --since 24h
```

---

### `sort`

List resources of a given kind sorted by creation date (ascending — newest entry last,
the same orientation as `kubectl logs`).

```text
kdiag sort <kind> [-n <ns> | -A]
```

Supported kinds: **any resource the API server exposes** — built-ins (pod, deployment,
daemonset, statefulset, replicaset, node, namespace, configmap, secret, service,
ingress, persistentvolumeclaim, persistentvolume, serviceaccount, role, rolebinding,
clusterrole, clusterrolebinding, horizontalpodautoscaler, poddisruptionbudget,
job, cronjob, …) and CRDs (e.g. `certificates.cert-manager.io`,
`widgets.demo.example.com`). The kind is resolved against the cluster's live
discovery information, so:

- Canonical names, plurals, and `kubectl` shortnames all work (`pod` / `pods` / `po`,
  `configmap` / `cm`, `service` / `svc`, `ingress` / `ing`).
- Fully qualified `resource.group` forms work for disambiguating CRDs
  (`widgets.demo.example.com`).
- Cluster-scoped kinds (`node`, `namespace`, `persistentvolume`, `customresourcedefinition`,
  …) are detected automatically; `-n` and `-A` are ignored for them.

| Flag | Default | Meaning |
|---|---|---|
| `-n, --namespace` | current context namespace | Namespace to query |
| `-A, --all-namespaces` | false | List across all namespaces (overrides `-n`) |

Output columns: `AGE`, `CREATED` (RFC3339, UTC), `NAME`. With `-A` a `NAMESPACE`
column is inserted after `CREATED`.

### `sort` examples

```sh
# Pods in current namespace, oldest first / newest last
kdiag sort pod

# Deployments across all namespaces
kdiag sort deploy -A

# ConfigMaps in a specific namespace (shortname)
kdiag sort cm -n kube-system

# All ingresses cluster-wide
kdiag sort ing -A

# Nodes by creation date
kdiag sort node

# A CRD, group-qualified
kdiag sort certificates.cert-manager.io -A
```

---

### `version`

Print the binary's version, build date, and short commit. Both forms are
equivalent.

```text
kdiag version
kdiag --version
```

Sample output:

```text
kdiag v0.1.0 (built 09-05-26_10:30, commit abc1234)
```

The values are stamped at build time via `-ldflags` on `Version`,
`BuildDate`, and `Commit` (see Installation). Unstamped builds report
`dev` / `unknown` / `none`.

---

## Common flags

| Flag | Short | Description |
| ------ | ------- | ------------- |
| `--namespace <ns>` | `-n` | Namespace (defaults to current context) |
| `--label <selector>` | `-l` | Label selector (where applicable) |

kdiag uses the standard kubeconfig precedence — `$KUBECONFIG` env var →
`~/.kube/config`. There is no `--kubeconfig`/`--context` flag; set `KUBECONFIG`
or switch context with `kubectl config use-context` before invoking kdiag.

---

## Installation

Requires Go 1.23+.

**Recommended — `make install`** drops a version-stamped binary into your
Go bin directory (`$(go env GOBIN)`, falling back to `$(go env GOPATH)/bin`,
which is typically `~/go/bin` and on most Go developers' `$PATH`):

```sh
git clone <repo>
cd kdiag
make install
```

Verify the install:

```sh
kdiag version
# kdiag v0.1.0 (built 09-05-26_10:30, commit abc1234)
```

`make build` produces `./kdiag` in the working directory with the same
version stamping — useful for local dev without touching `$GOPATH/bin`.

The binary embeds `Version` (from `git describe --tags --always --dirty`),
`BuildDate` (UTC ISO-8601), and `Commit` (short SHA) via `-ldflags`.

---

## Shell completion

`kdiag completion <shell>` prints a completion script to stdout for
`bash` or `zsh`. Scripts cover top-level subcommands, `inspect`
kinds, `diff` and `sort` kinds (any kind the cluster exposes — built-in
or CRD), and per-command flags (`-n/--namespace`, `-l/--label`,
`--full`, `--resources`, `--spec`, `--container-spec`, `--az`).

Namespace and resource names are completed dynamically by querying the
cluster — for example:

```sh
kdiag inspect deploy -n <TAB>          # → list of namespaces
kdiag inspect deploy -n my-ns my-<TAB> # → deployments named my-* in my-ns
kdiag diff cm -n my-ns <TAB>           # → configmap names in my-ns
kdiag diff rs -n my-ns <TAB>           # → deployment names (diff target)
kdiag inspect node <TAB>               # → cluster nodes
```

Dynamic lookups happen via a hidden `kdiag __complete` helper invoked by
the completion script. If the cluster is unreachable, completion silently
falls back to flag-only suggestions (no shell errors).

### bash

```sh
# one-off (current shell)
source <(kdiag completion bash)

# persistent
kdiag completion bash | sudo tee /etc/bash_completion.d/kdiag >/dev/null
```

### zsh

```sh
# install into a directory on $fpath
kdiag completion zsh > "${fpath[1]}/_kdiag"
# then start a fresh shell, or run: autoload -Uz compinit && compinit
```

After changing flag definitions, regenerate persisted completion files and
bust the zsh cache in one shot:

```sh
make autocompletion
exec zsh
```

---

## Project layout

```text
main.go                    # Entry point — routes top-level commands
internal/
  kube/
    client.go              # KubeFlags, KubeEnv, NewKubeEnv
    helpers.go             # Zone lookup, container state, resource extraction
    kinds.go               # Inspect kind registry (canonical/alias/cluster-scope)
  cli/
    usage.go               # PrintRootUsage / Print*Usage / WantsHelp
    format.go              # NewTabWriter, PrintKVBlock
    errors.go              # Fatal
  cmd/
    inspect.go             # inspect dispatcher + shared helpers
    inspect_pod.go         # inspect pod
    inspect_workloads.go   # inspect deploy / ds / sts / rs
    inspect_node.go        # inspect node
    az_pods.go             # az pods command
    events.go              # events command
    diff.go                # diff rs command
    completion.go          # completion command (embeds scripts below)
    complete.go            # __complete hidden helper for shell completion
    completions/           # bash/zsh scripts (//go:embed)
```

### Adding a new command

1. Create `internal/cmd/<name>.go` and implement `Run<Name>(args []string)`
   using the same pattern as the existing commands:
   - Parse flags with a `FlagSet` that supports interspersed arguments,
     so the pod name can appear before or after flags (like `kubectl`)
   - Build a `*kube.KubeEnv` via `kube.NewKubeEnv(k)`
   - Use helpers from `internal/kube` and `internal/cli` as needed
2. Add a `case "<name>": cmd.Run<Name>(args[1:])` branch in `main.go`
3. Add a one-line entry to `PrintRootUsage` in `internal/cli/usage.go`
   and a `PrintXyzUsage` printer if the command has its own subcommands
4. Handle `cli.WantsHelp(args)` in the new dispatcher (route to the right
   printer and return) so `kdiag <name> -h` works at every level
5. Update the static completion scripts in `internal/cmd/completions/`
   (`kdiag.bash`, `kdiag.zsh`) to surface the new command, any subcommands,
   and any new flags. Run `make autocompletion` to refresh persisted
   files and bust the zsh cache.

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
make integration-tests

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

- Go 1.23+
- A reachable Kubernetes cluster and a valid kubeconfig
