package cli

import (
	"fmt"
	"io"
	"strings"
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

// PrintRootBanner prints the terse banner shown when the user runs `kdiag`
// with no arguments. Deliberately minimal — no command list — so the bare
// invocation doesn't impersonate the help screen. `kdiag -h` remains the
// place to discover commands.
func PrintRootBanner(w io.Writer) {
	fmt.Fprint(w, `kdiag — Kubernetes diagnostic CLI

Usage:
  kdiag <command> [flags] [args]

Use "kdiag -h" for the command list.
`)
}

// PrintRootUsage prints the top-level help for kdiag. Single-subcommand groups
// (az, rs) are collapsed onto one line so users see the actual entry point
// rather than a two-step tree. Kinds for `inspect` are summarized in the
// description; full list is one level down via `kdiag inspect -h`.
//
// `full` selects the help screen (true) vs. the error-fallback screen
// (false). The branded title line and the auxiliary `completion` command
// appear only on the help screen; the error paths (unknown command) stay
// terse and just show the command list + usage hint. `--version` is a
// flag, not a subcommand, so it does not appear in either mode.
func PrintRootUsage(w io.Writer, full bool) {
	if full {
		fmt.Fprint(w, "kdiag — Kubernetes diagnostic CLI\n\n")
	}
	fmt.Fprint(w, `Available Commands:
  inspect      Inspect resources (pod, deploy, ds, sts, rs, node); --az for zone placement
  diff         Diff Kubernetes resources (rs, pod, node)
  events       Show events in the current namespace
  sort         Sort resources by creation date (newest last)
`)
	if full {
		fmt.Fprint(w, `  completion   Generate shell completion (bash|zsh)
  help         Show help for a command or topic (e.g. kdiag help inspect)
`)
	}
	fmt.Fprint(w, `
Usage:
  kdiag <command> [flags] [args]

Use "kdiag <command> -h" (or "kdiag help <command>") for more information about a command.
`)
}

// PrintInspectUsage prints help for `kdiag inspect`. args is the raw argv
// (after the "inspect" token) so the help filters out flags that don't
// compose with whatever view the user has already selected.
func PrintInspectUsage(w io.Writer, args []string) {
	seen := ViewFlagSeen(args)

	fmt.Fprint(w, `Inspect Kubernetes resources.

Available Subcommands:
  pod    deploy    ds    sts    rs    node

Usage:
  kdiag inspect <subcommand> [flags] [args]

Options:
  -n, --namespace        Namespace (defaults to current context)
  -l, --label            Label selector (pod, deploy, node)
  <partial-name>         Partial pod name match (pod only)
`)
	if showFormat(seen) {
		fmt.Fprintln(w, "  --format <text|yaml>   Output format (default: text)")
	}
	if showPath(seen) {
		fmt.Fprintln(w, "  --path <needle>        Only print yq paths matching <needle> (kdiag help yml-path for details)")
	}
	if showResources(seen) {
		fmt.Fprintln(w, "  --resources            Narrow output to container resources (text or YAML)")
	}
	if showSpec(seen) {
		fmt.Fprintln(w, "  --spec                 Deploy only: emit .spec.template.spec (text or YAML)")
	}
	if showAZ(seen) {
		fmt.Fprint(w, `  --az                   Availability-zone placement (pod, deploy, ds, sts).
                         Composes with --format yaml; mutually exclusive with
                         --resources / --spec / --path (each selects a view).
`)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Examples:")
	for _, ex := range inspectExamples(seen) {
		fmt.Fprintln(w, ex)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, `Use "kdiag inspect <subcommand> -h" for details.`)
}

// Composition rules for view selectors:
//   none seen   → show everything
//   path        → show only -n, -l, --path
//   resources   → show --resources, --format, --az; hide --path, --spec
//   spec        → show --spec, --format; hide --path, --resources, --az
//   az          → show --az, --format; hide --path, --resources, --spec
func showPath(seen string) bool      { return seen == "" || seen == "path" }
func showFormat(seen string) bool    { return seen != "path" }
func showResources(seen string) bool { return seen == "" || seen == "resources" }
func showSpec(seen string) bool      { return seen == "" || seen == "spec" }
func showAZ(seen string) bool        { return seen == "" || seen == "az" || seen == "resources" }

func inspectExamples(seen string) []string {
	switch seen {
	case "path":
		return []string{
			"  kdiag inspect pod my-pod --path qosClass",
			"  kdiag inspect deploy my-deploy --path '*image*'",
			"  kdiag inspect deploy -l app=my-app --path memory",
		}
	case "resources":
		return []string{
			"  kdiag inspect pod my-pod --resources",
			"  kdiag inspect pod my-pod --resources --format yaml",
			"  kdiag inspect deploy my-deploy --resources",
		}
	case "spec":
		return []string{
			"  kdiag inspect deploy my-deploy --spec",
			"  kdiag inspect deploy my-deploy --spec --format yaml",
		}
	case "az":
		return []string{
			"  kdiag inspect pod --az -n my-ns -l app=my-app",
			"  kdiag inspect deploy my-deploy --az --format yaml",
		}
	default:
		return []string{
			"  kdiag inspect pod my-pod",
			"  kdiag inspect pod my-pod --format yaml | yq '.containers[].name'",
			"  kdiag inspect deploy my-deploy",
			"  kdiag inspect deploy my-deploy --resources --format yaml",
			"  kdiag inspect deploy my-deploy --spec",
			"  kdiag inspect deploy my-deploy --path memory",
		}
	}
}

// PrintYMLPathTopic prints the long-form explanation of `--path`. Reachable
// via `kdiag help yml-path`. The topic keeps the legacy name "yml-path"
// even though the flag itself is now `--path` — both are accepted as topic
// aliases by the help dispatcher.
func PrintYMLPathTopic(w io.Writer) {
	fmt.Fprint(w, `kdiag inspect --path <needle>

Walk the resource YAML and print every yq path whose key or value matches
<needle>. Works for any kind including CRDs.

Matching:
  Default match is exact (full key or full value).
  Use '*' as a glob: 'name*' (prefix), '*name' (suffix), '*name*' (substring).
  Smart-case: lowercase needle is case-insensitive; uppercase makes it case-sensitive.

Compatibility:
  --path composes with -n/--namespace and -l/--label.
  --path is mutually exclusive with --format, --resources, --spec, --az
  (each selects a different view).

Examples:
  kdiag inspect pod my-pod --path qosClass
  kdiag inspect deploy my-deploy --path '*image*'
  kdiag inspect deploy -l app=my-app --path memory
  kdiag inspect node -l kubernetes.io/hostname --path '*hostname*'
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

// ViewFlagSeen scans args for the first view-selector flag and returns its
// short name ("path", "resources", "spec", "az") or "" if none is present.
// Accepts both the space-separated form (--path x) and the inline form
// (--path=x). Used by help printers and the completion scripts (transitively)
// to filter what gets suggested once a view is set.
func ViewFlagSeen(args []string) string {
	for _, a := range args {
		switch {
		case a == "--path" || strings.HasPrefix(a, "--path="):
			return "path"
		case a == "--resources":
			return "resources"
		case a == "--spec":
			return "spec"
		case a == "--az":
			return "az"
		}
	}
	return ""
}
