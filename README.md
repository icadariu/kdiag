# kdiag

A Kubernetes diagnostic CLI tool for inspecting pod state
and availability-zone distribution.

Help is nested kubectl-style:

- `kdiag` (no args) prints a Usage line plus the sorted Available Commands
  list, scoped to the primary commands (`completion` and `help` are hidden
  to keep the bare banner focused).
- `kdiag --help` (or `-h`) adds the branded title, a pointer explaining
  that flags vary per command, and includes every command (`completion`
  and `help` reappear here).
- `kdiag help` (no topic) prints just the Available Commands list — terse,
  scriptable; same full set as `--help`.
- `kdiag help <command>` is equivalent to `kdiag <command> --help` (byte-for-byte
  identical output); long-form topics live under `kdiag help <topic>`
  (e.g. `kdiag help yml-path` for the `--path` flag).

All flags use the `--long` form in the documentation below.

## Commands

### `inspect pod`

Show the state of one pod or a group of pods matched by a label selector.

```text
kdiag inspect pod [flags]
kdiag inspect pod [flags] <partial-pod-name>
kdiag inspect pod [flags] --label <selector>
kdiag inspect po  [flags] <partial-pod-name>
kdiag inspect po  [flags] --label <selector>
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

#### Pod output flags

`-o`/`--output` (kubectl-style) selects a structured format: `json` or `yaml`.
Default output is text; passing `-o json` or `-o yaml` emits a single
yq/jq-pipeable document instead.
`--resources` narrows the output to per-container resource info (text or structured).
`--path <keyword>` finds all paths matching the keyword.
`--az` composes with `-o`/`--output` (emits `{placements, zoneSummary}`); it is
mutually exclusive with `--resources` / `--path` since each
of those selects a different view.

| Flag | Description |
| ---- | ----------- |
| `-o`, `--output <json\|yaml>` | Structured output (default: text) |
| `--resources` | Narrow output to per-container resource info (text or structured) |
| `--path <keyword>` | Find all paths matching the keyword in the YAML |

Output includes init containers and sidecar containers (initContainer with
`restartPolicy: Always`, k8s 1.28+), labeled `Init Container:` / `Sidecar Container:`
/ `Container:` respectively.

With a positional partial name, output is a flat YAML object. With `--label`,
output is a flat YAML list, so downstream pipelines stay predictable.

### Examples

```sh
# Single pod by partial name
kdiag inspect pod gateway-proxy

# All pods matching a selector
kdiag inspect pod --namespace example-system --label 'app=gateway-proxy'

# Availability-zone placement for all pods in namespace
kdiag inspect pod --az --namespace example-system

# AZ placement filtered by selector
kdiag inspect pod --az --namespace example-system --label 'app=gateway-proxy'

# Single pod, full output as YAML (yq-pipeable)
kdiag inspect pod my-pod -o yaml | yq '.containers[].name'

# Resources for every matching pod as JSON (flat list)
kdiag inspect pod --label 'app=gateway-proxy' -o json | jq '.[0].name'
```

---

### `inspect deploy` / `ds` / `sts` / `rs`

Show a kind-specific workload summary on top of the per-pod container
state. Pod selection follows each workload's own `Spec.Selector`.

```text
kdiag inspect deploy [flags] [<deployment-name> | --label <selector>]
kdiag inspect ds     [flags] <daemonset-name>
kdiag inspect sts    [flags] <statefulset-name>
kdiag inspect rs     [flags] <replicaset-name>
```

For `inspect deploy`, the deployment can be identified either by positional
name or by `--label` (which must match exactly one Deployment in the
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

#### Workload output flags

`-o`/`--output` (kubectl-style) emits a kdiag-shaped document in `json` or
`yaml`: `{ name, kind, namespace, replicas, strategy, selector, pods: [...] }`.
`--deployment-spec` (deploy only) emits the pod template spec (text or structured).
`--resources` narrows output to per-container resource info (text or structured).
`--path <keyword>` finds all paths matching the keyword.
`--az` composes with `-o`/`--output` (emits `{placements, zoneSummary}`); it is
mutually exclusive with `--resources` / `--deployment-spec` / `--path` since each
of those selects a different view.

| Flag | Description |
| ---- | ----------- |
| `-o`, `--output <json\|yaml>` | Structured output (kdiag-shaped) |
| `--deployment-spec` | Emit the pod template spec (deploy only; errors on other kinds) |
| `--resources` | Narrow output to per-container resource info (text or structured) |
| `--path <keyword>` | Find all paths matching the keyword in the YAML |

For `inspect deploy`, `--resources` operates on the deployment template (no
pod lookup). For `ds`/`sts`/`rs`, `--resources` keeps the per-pod text-block
meaning.

### Examples

```sh
# Inspect all pods in a deployment
kdiag inspect deploy my-deployment

# Identify the deployment via label instead of name
kdiag inspect deploy --namespace example-system --label 'app=my-app'

# In a specific namespace
kdiag inspect deploy --namespace example-system my-deployment

# AZ placement for a deployment's pods
kdiag inspect deploy --az --namespace example-system my-deployment

# Daemonset, statefulset, replicaset
kdiag inspect ds  --namespace kube-system kube-proxy
kdiag inspect sts --namespace my-ns my-statefulset
kdiag inspect rs  --namespace my-ns my-replicaset-abc123

# Deployment summary as YAML (yq-pipeable)
kdiag inspect deploy my-deployment -o yaml | yq '.pods | length'

# Pod template spec as text
kdiag inspect deploy my-deployment --deployment-spec

# Deployment template resources as YAML
kdiag inspect deploy --resources --namespace my-ns my-deployment

# Deployment template resources as JSON
kdiag inspect deploy --resources -o json --namespace my-ns my-deployment
```

---

### `inspect node`

Show a per-node summary for one node or a set of nodes. Nodes are
cluster-scoped, so `--namespace` is accepted (for uniform CLI shape) but
silently ignored.

Node summary fields:

- Zone, Instance Type, Kubelet Version
- Age (e.g. `3d2h`, `5h31m`), Pod CIDR, Unschedulable (cordoned)
- Taints
- Conditions: Ready, MemoryPressure, DiskPressure, PIDPressure
- Allocatable and Capacity: cpu, memory, pods (and any other resources the node exposes)

```text
kdiag inspect node [<node-name> | --label <selector>]
kdiag inspect no   [<node-name> | --label <selector>]
```

### `inspect node` examples

```sh
# Single node by name
kdiag inspect node my-node

# All nodes in a zone
kdiag inspect node --label topology.kubernetes.io/zone=eu-west-1a

# All nodes in the cluster
kdiag inspect node

# Single node as YAML
kdiag inspect node my-node -o yaml
```

---

### `inspect --path`

See also `kdiag help yml-path` for a concise topic page covering the same
material.

Find the `yq` path of any key inside a resource's YAML. Useful when
you know the keyword (`Burstable`, `memory`, `imagePullPolicy`, …) but not where it
sits in the object. Works for **every** kind the cluster exposes, including
CRDs.

Output is paths only (one per line). Array elements render with concrete
indices (e.g., `[0]`, `[1]`) rather than generalized `[]`. When a resource has
named arrays (containers, ports, volumes, …) with multiple elements, matches
are grouped under a `<name>:` header for clarity. Single-element or unnamed
arrays have no header.

Match semantics: by default the needle must equal the **full** key —
so `--path name` matches the key `name` but NOT `namespace`, `generateName`, or
`container-1-tiny`. Use `*` as a glob for fuzzier matches: `name*` (prefix),
`*name` (suffix), `*name*` (substring). The whole string still has to match
end-to-end.

Key-match recursion: when a needle matches a *key* the walker emits the
match and continues descending into the value, so common needles like
`*name*` or `*spec*` will surface every nested occurrence. This is
intentional — `--path` is grep-like, not deepest-match-only.

Smart-case matching, like ripgrep: an **all-lowercase** needle is
case-insensitive; any uppercase character makes the match case-sensitive.

`.metadata.managedFields` is skipped — its server-side-apply bookkeeping
keys (`f:image`, `k:{"name":"..."}`, …) shadow real field names and would
otherwise dominate every result.

```text
kdiag inspect <kind> [<name> | -l <label>] --path <keyword>
```

```sh
# Find the yq path of a key
kdiag inspect pod my-pod --path qosClass
# .status.qosClass

# Exact key match (no substring) — `name` does NOT match `namespace`
kdiag inspect pod my-pod --path name
# .metadata.name
# .spec.containers[0].name

# Glob match for partial keys (multi-container deployment)
kdiag inspect deploy kdiag-multicont --namespace kdiag-test --path memory
# api:
#   .spec.template.spec.containers[0].resources.limits.memory
#   .spec.template.spec.containers[0].resources.requests.memory
# sidecar:
#   .spec.template.spec.containers[1].resources.limits.memory
#   .spec.template.spec.containers[1].resources.requests.memory

# Search across all pods matched by a selector
kdiag inspect pod --label app=test-app --namespace kdiag-test --path image
# Pod/test-app-6dd566fbff-jd2vw:
#   .spec.containers[0].image
#   .status.containerStatuses[0].image
# Pod/test-app-6dd566fbff-xrg84:
#   .spec.containers[0].image
#   .status.containerStatuses[0].image

# Works for CRDs too
kdiag inspect certificates.cert-manager.io my-cert --path renewBefore
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
kdiag diff rs [flags] --label <selector> [<rev-from> <rev-to>]
```

`diff rs` also accepts the generic two-name form
(`kdiag diff rs <rs-a> <rs-b>`); passing `--full` to either form
dumps the full RS objects (managedFields preserved) instead of just
`.spec.template`.

### `diff rs` examples

```sh
# By deployment name (default: last two revisions)
kdiag diff rs --namespace my-ns my-deployment

# Specific revisions — compare current with three behind
kdiag diff rs --namespace my-ns my-deployment 2 5

# By label selector (errors if more than one deployment matches)
kdiag diff rs --namespace my-ns --label 'app=my-app'

# Selector + specific revisions
kdiag diff rs --namespace my-ns --label 'app=my-app' 1 3

# Two RS by name (generic shape, full objects)
kdiag diff rs --namespace my-ns my-rs-abc my-rs-def --full
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
kdiag diff <kind> [--namespace <ns>] [--full] <name-a> <name-b>
```

### `diff <any-kind>` examples

```sh
kdiag diff pod    --namespace my-ns pod-abc123 pod-def456
kdiag diff cm     --namespace my-ns config-a config-b
kdiag diff svc    --namespace my-ns api-v1 api-v2
kdiag diff deploy --namespace my-ns app-blue app-green
kdiag diff node   node-1 node-2
kdiag diff pod    --namespace my-ns a b --full      # include status, managedFields, annotations
```

---

### `events`

Show recent events (Normal and Warning) in the current namespace.

```text
kdiag events [--namespace <ns> | --all-namespaces] [--since <duration>]
```

| Flag | Default | Meaning |
|---|---|---|
| `--namespace` | current context namespace | Namespace to inspect |
| `--all-namespaces` | false | List events across all namespaces (overrides `--namespace`) |
| `--since` | `1h` | Only show events newer than this duration. Accepts Go duration syntax (e.g. `30s`, `5m`, `2h`) |

Output columns: `AGE`, `TYPE`, `REASON`, `OBJECT` (`Kind/name`), `MESSAGE`.
With `--all-namespaces`, a `NAMESPACE` column is inserted after `AGE`.
Sorted by effective event timestamp ascending — newest entry is last, the same orientation as `kubectl logs`.
The effective timestamp falls back across `Series.LastObservedTime` → `LastTimestamp` → `EventTime` → `FirstTimestamp` → `CreationTimestamp`, so events emitted via `events.k8s.io/v1` (e.g. `FailedScheduling` from the scheduler) are included.

### Examples

```sh
# All events in the current-context namespace (last 1h)
kdiag events

# All events across all namespaces (last 30 minutes)
kdiag events --all-namespaces --since 30m

# Look back 24h
kdiag events --namespace my-ns --since 24h
```

---

### `sort`

List resources of a given kind sorted by creation date (ascending — newest entry last,
the same orientation as `kubectl logs`).

```text
kdiag sort <kind> [--namespace <ns> | --all-namespaces]
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
  …) are detected automatically; `--namespace` and `--all-namespaces` are ignored for them.

| Flag | Default | Meaning |
|---|---|---|
| `--namespace` | current context namespace | Namespace to query |
| `--all-namespaces` | false | List across all namespaces (overrides `--namespace`) |

Output columns: `AGE`, `CREATED` (RFC3339, UTC), `NAME`. With `--all-namespaces` a `NAMESPACE`
column is inserted after `CREATED`.

### `sort` examples

```sh
# Pods in current namespace, oldest first / newest last
kdiag sort pod

# Deployments across all namespaces
kdiag sort deploy --all-namespaces

# ConfigMaps in a specific namespace (shortname)
kdiag sort cm --namespace kube-system

# All ingresses cluster-wide
kdiag sort ing --all-namespaces

# Nodes by creation date
kdiag sort node

# A CRD, group-qualified
kdiag sort certificates.cert-manager.io --all-namespaces
```

---

### `--version`

Print the binary's version, build time, and short commit.

```text
kdiag --version
```

Sample output:

```text
v0.1.0 (built 09-05-26_10:30, commit abc1234)
```

The values are stamped at build time via `-ldflags` on `version`,
`buildTime`, and `commit` (see Installation). Unstamped builds report
`dev` / `unknown` / `none`.

---

## Common flags

| Flag | Description |
| ---- | ----------- |
| `--namespace <ns>` | Namespace (defaults to current context) |
| `--label <selector>` | Label selector (where applicable) |
| `--all-namespaces` | List across all namespaces (`events`, `sort`) |

kdiag uses the standard kubeconfig precedence — `$KUBECONFIG` env var →
`~/.kube/config`. There is no `--kubeconfig`/`--context` flag; set `KUBECONFIG`
or switch context with `kubectl config use-context` before invoking kdiag.

---

## Installation

Requires Go 1.26.3+.

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
kdiag --version
# v0.1.0 (built 09-05-26_10:30, commit abc1234)
```

`make build` produces `./kdiag` in the working directory with the same
version stamping — useful for local dev without touching `$GOPATH/bin`.

The binary embeds `version` (from `git describe --tags --always --dirty`),
`buildTime` (UTC, `dd-mm-yy_HH:MM`), and `commit` (short SHA) via `-ldflags`.

---

## Shell completion

`kdiag completion <shell>` prints a completion script to stdout for
`bash` or `zsh`. Scripts cover top-level subcommands (including `help` and
`completion`), `inspect` kinds, `diff` and `sort` kinds (any kind the
cluster exposes — built-in or CRD), and per-command flags (`--namespace`,
`--label`, `--all-namespaces`, `--full`, `-o`/`--output`, `--resources`,
`--deployment-spec`, `--az`, `--path`).

Namespace and resource names are completed dynamically by querying the
cluster — for example:

```sh
kdiag inspect deploy --namespace <TAB>          # → list of namespaces
kdiag inspect deploy --namespace my-ns my-<TAB> # → deployments named my-* in my-ns
kdiag diff cm --namespace my-ns <TAB>           # → configmap names in my-ns
kdiag diff rs --namespace my-ns <TAB>           # → deployment names (diff target)
kdiag inspect node <TAB>                        # → cluster nodes
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
    resource_resolver.go   # Generic kind→GVR/GVK resolution via discovery (sort, diff)
  cli/
    usage.go               # PrintRootUsage / Print*Usage / WantsHelp
    format.go              # NewTabWriter, PrintKVBlock
    errors.go              # Fatal
  cmd/
    inspect.go             # inspect dispatcher + shared helpers
    inspect_pod.go         # inspect pod
    inspect_deploy.go      # inspect deploy (YAML-mode flags on the pod template)
    inspect_workloads.go   # inspect ds / sts / rs
    inspect_node.go        # inspect node
    inspect_yml_path.go    # --path walker (shared across inspect kinds)
    az_pods.go             # printAZTable helper for --az on inspect subcommands
    events.go              # events command
    diff.go                # diff command — diff rs + generic two-name diff
    sort.go                # sort command (kind list by creation date)
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
2. Add a `case "<name>": cmd.Run<Name>(args[1:])` branch in `main.go` plus a
   matching branch in `handleHelp` so `kdiag help <name>` re-dispatches as
   `kdiag <name> --help`
3. Add the new command to `rootCommands` in `internal/cli/usage.go` (kept
   alphabetically sorted — the three root screens read from this single
   source of truth) and add a `PrintXyzUsage` printer if the command has
   its own subcommands
4. Handle `cli.WantsHelp(args)` in the new dispatcher (route to the right
   printer and return) so `kdiag <name> --help` produces the same output
   as `kdiag help <name>`. Per-command help printers should render their
   `Flags:` block via `cli.FormatFlagsLongOnly(fs)` — that hides the
   single-dash aliases from the documentation while leaving them
   functional at parse time
5. Update the static completion scripts in `internal/cmd/completions/`
   (`kdiag.bash`, `kdiag.zsh`) to surface the new command in `top_cmds`
   (alphabetical) and the `help`-arg handler, any subcommands, and any new
   flags. Run `make autocompletion` to refresh persisted files and bust
   the zsh cache.

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
`kdiag-test` namespace populated with the resources every command exercises:

- Deployment `test-app` (rolled to revisions 1/2/3 by `make cluster-up` so
  `diff rs` has history to compare)
- Static Pod `kdiag-static` (single-pod tests) and `kdiag-crasher`
  (terminated/waiting container states)
- DaemonSet `kdiag-ds`, StatefulSet `kdiag-sts`
- Zero-replica Deployments `kdiag-multi-a` / `kdiag-multi-b` sharing label
  `kdiag-multi=yes` (exercises the `inspect deploy -l` multi-match error)
- ConfigMaps `kdiag-cm-a`, `kdiag-cm-b`, `kdiag-cm-multiline`
- CRD `widgets.kdiag.test` + Widget CR `kdiag-widget` (CRD-path coverage for
  `inspect`, `diff`, `sort`)
- Event `kdiag-multiline-test` (multiline-message sanitisation in `events`)


---

## Requirements

- Go 1.26.3+
- A reachable Kubernetes cluster and a valid kubeconfig
