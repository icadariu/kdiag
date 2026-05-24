package cli

import (
	"fmt"
	"io"
)

// WantsHelp reports whether the first arg requests help (-h, --help, or "help").
// Used by every dispatcher level so the rule lives in one place.
func WantsHelp(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "-h", "--help", "help":
		return true
	}
	return false
}

// PrintRootUsage prints the top-level help for kdiag. Single-subcommand groups
// (az, rs) are collapsed onto one line so users see the actual entry point
// rather than a two-step tree. Kinds for `inspect` are summarized in the
// description; full list is one level down via `kdiag inspect -h`.
//
// When full is false, the auxiliary `completion` command is hidden to keep
// the no-arg landing screen focused on the diagnostic verbs. It remains
// listed under `kdiag --help`. `--version` is a flag, not a subcommand, so
// it does not appear in either mode.
func PrintRootUsage(w io.Writer, full bool) {
	fmt.Fprint(w, `kdiag — Kubernetes diagnostic CLI

Available Commands:
  inspect      Inspect resources (pod, deploy, ds, sts, rs, node); --az for zone placement
  diff         Diff Kubernetes resources (rs, pod, node)
  events       Show events in the current namespace
  sort         Sort resources by creation date (newest last)
`)
	if full {
		fmt.Fprint(w, `  completion   Generate shell completion (bash|zsh)
`)
	}
	fmt.Fprint(w, `
Usage:
  kdiag <command> [flags] [args]

Use "kdiag <command> -h" for more information about a command.
`)
}

// PrintInspectUsage prints help for `kdiag inspect`, listing the kinds it
// dispatches to.
func PrintInspectUsage(w io.Writer) {
	fmt.Fprint(w, `Inspect Kubernetes resources.

Available Subcommands:
  pod    deploy    ds    sts    rs    node

Usage:
  kdiag inspect <subcommand> [flags] [args]

Options:
  -n, --namespace     Namespace (defaults to current context)
  -l, --label         Label selector (pod, deploy, node)
  <partial-name>      Partial pod name match (pod only)
  --az                Show availability-zone placement (pod, deploy, ds, sts)
  --spec              YAML: deployment .spec.template.spec (deploy only)
  --container-spec    YAML: containers[] of the pod (pod) or pod template (deploy)
  --resources         pod/deploy: YAML list of [{name, resources}, ...];
                      ds/sts/rs: per-pod requests/limits text block
  --yaml-field <s>    Search the resource's YAML (keys and values) for <s> and
                      print the yq path to each hit. Works for any kind. Smart-case:
                      lowercase needle is case-insensitive; uppercase makes it
                      case-sensitive. Compatible with --label / -l.

YAML-mode flags (--spec, --container-spec, --resources for pod/deploy) emit pure
YAML on stdout (no banners) — pipeable to yq. They are mutually exclusive and
incompatible with --az. With pod + --label matching multiple pods, output is
a YAML map keyed by pod name.

Examples:
  kdiag inspect pod -l app=my-app
  kdiag inspect pod --az -n my-ns
  kdiag inspect pod --resources -n my-ns my-pod
  kdiag inspect pod --container-spec my-pod | yq '.[].name'
  kdiag inspect deploy -n my-ns my-deployment
  kdiag inspect deploy --az -n my-ns my-deployment
  kdiag inspect deploy --container-spec -n my-ns my-deployment | yq '.[].name'
  kdiag inspect pod my-pod --yaml-field Burstable           # find yq path of a value
  kdiag inspect deploy my-deploy --yaml-field imagePull     # find yq path of a key

Use "kdiag inspect <subcommand> -h" for details.
`)
}

// PrintDiffUsage prints the full generic help for `kdiag diff` (with no kind specified).
// Two shapes: revision-diff (rs only) and generic two-name diff (any kind).
func PrintDiffUsage(w io.Writer) {
	fmt.Fprint(w, `Diff two Kubernetes resources of the same kind.

By default, the diff is opinionated for investigation — per-kind noise is
stripped to surface what matters. For Pods, status and node-assigned fields are
hidden. For Services, cluster-assigned IPs are hidden. For workloads (Deployment,
StatefulSet, DaemonSet, ReplicaSet), status is hidden. Pass --full to show the
raw API server response with no filtering.

The generic form accepts any kind the cluster exposes (built-in or CRD):

Usage:
  kdiag diff <kind> [-n <ns>] [--full] <name-a> <name-b>

  # Replicaset has an additional revision-diff shape:
  kdiag diff rs [-n <ns>] [--full] <deployment-name> [<rev-from> <rev-to>]
  kdiag diff rs [-n <ns>] [--full] -l <label>          [<rev-from> <rev-to>]

Flags:
  -n, --namespace string   namespace (defaults to current context)
      --full               show the raw API server response (no per-kind noise stripping;
                           for rs, dump the full RS objects instead of just .spec.template)

Examples:
  kdiag diff pod   -n my-ns pod-abc pod-def
  kdiag diff cm    -n my-ns config-a config-b
  kdiag diff svc   -n my-ns api-v1 api-v2
  kdiag diff deploy -n my-ns app-blue app-green
  kdiag diff node  node-1 node-2
  kdiag diff rs -n my-ns my-deployment           # last two revisions
  kdiag diff rs -n my-ns my-deployment 2 5       # specific revisions
  kdiag diff rs -n my-ns -l app=my-app 1 3       # via selector
  kdiag diff pod   -n my-ns a b --full           # include managedFields and status

Use "kdiag diff rs -h" for the full revision-diff help.
`)
}

// PrintDiffKindUsage prints help for `kdiag diff <kind>` for a generic (non-rs) kind.
// Shows usage specific to that kind with one relevant example.
func PrintDiffKindUsage(w io.Writer, kind string) {
	fmt.Fprintf(w, `Diff two %s resources.

By default, noise is stripped per-kind. Pass --full to show the raw API server
response with no filtering.

Usage:
  kdiag diff %s [-n <ns>] [--full] <name-a> <name-b>

Flags:
  -n, --namespace string   namespace (defaults to current context)
      --full               show the raw API server response (no per-kind noise stripping)

Examples:
  kdiag diff %s -n my-ns resource-a resource-b

Use "kdiag diff --help" for a complete list of supported kinds and examples.
`, kind, kind, kind)
}

// PrintCompletionUsage prints help for `kdiag completion`.
func PrintCompletionUsage(w io.Writer) {
	fmt.Fprint(w, `Generate a shell completion script.

Usage:
  kdiag completion <bash|zsh>

Examples:
  # Load in current bash shell
  source <(kdiag completion bash)

  # Persist (zsh)
  kdiag completion zsh > "${fpath[1]}/_kdiag"
`)
}

// PrintSortUsage prints help for `kdiag sort`.
func PrintSortUsage(w io.Writer) {
	fmt.Fprint(w, `Sort resources by creation date (ascending — newest entry last, like `+"`kubectl logs`"+`).

Usage:
  kdiag sort <kind> [-n <ns> | -A]

Kinds:
  Any resource the API server exposes — built-ins (pod, deployment, daemonset,
  statefulset, replicaset, node, namespace, configmap, secret, service,
  ingress, persistentvolumeclaim, serviceaccount, role, rolebinding,
  horizontalpodautoscaler, …) and CRDs (e.g. certificate.cert-manager.io).
  Canonical names, plurals, and shortnames (po, deploy, cm, svc, ing, pvc, sa)
  are all accepted.

Flags:
  -n, --namespace        Namespace (defaults to current context)
  -A, --all-namespaces   List resources across all namespaces (overrides -n)

Notes:
  Cluster-scoped kinds (node, namespace, pv, …) ignore -n and -A.

Examples:
  kdiag sort pod
  kdiag sort deploy -n my-ns
  kdiag sort cm -A
  kdiag sort node
  kdiag sort certificates.cert-manager.io -A
`)
}

// PrintEventsUsage prints help for `kdiag events`.
func PrintEventsUsage(w io.Writer) {
	fmt.Fprint(w, `Show events (Normal and Warning) in the current namespace.

Sorted by their effective timestamp ascending (newest entry last, like ` + "`kubectl logs`" + `).

Usage:
  kdiag events [-n <ns> | -A] [--since <duration>]

Flags:
  --since <duration>     Only show events newer than this duration, e.g. 30s, 5m, 2h (default: 1h)
  -n, --namespace        Namespace (defaults to current context)
  -A, --all-namespaces   List events across all namespaces (overrides -n)

Examples:
  kdiag events
  kdiag events -n my-ns
  kdiag events -A --since 30m
  kdiag events -n my-ns --since 24h

Use "kdiag events -h" for flags.
`)
}
