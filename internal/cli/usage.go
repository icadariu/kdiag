package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/pflag"
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

// rootCommand is the canonical (name, description) pair for one top-level
// command. rootCommands is the single source of truth used by every root
// help/usage renderer below — kept alphabetically sorted so output is
// deterministic everywhere.
type rootCommand struct {
	Name string
	Desc string
	// Meta is true for housekeeping commands (completion, help) that are
	// hidden from the bare `kdiag` banner but still listed under `--help`.
	Meta bool
}

var rootCommands = []rootCommand{
	{Name: "completion", Desc: "Generate shell completion (bash|zsh)", Meta: true},
	{Name: "diff", Desc: "Diff Kubernetes resources (rs, pod, node, …)"},
	{Name: "events", Desc: "Show events in the current namespace"},
	{Name: "help", Desc: "Show help for a command or topic (e.g. kdiag help inspect)", Meta: true},
	{Name: "inspect", Desc: "Inspect resources (pod, deploy, ds, sts, rs, node); --az for zone placement"},
	{Name: "sort", Desc: "Sort resources by creation date (newest last)"},
}

// printCommandList renders the alphabetically-sorted Available Commands
// block. When includeMeta is false, housekeeping commands (completion, help)
// are dropped — that's the bare-banner shape. Column width is fixed wide
// enough for the longest current name ("completion", 10 chars) plus a
// 3-space gap.
func printCommandList(w io.Writer, includeMeta bool) {
	fmt.Fprintln(w, "Available Commands:")
	for _, c := range rootCommands {
		if !includeMeta && c.Meta {
			continue
		}
		fmt.Fprintf(w, "  %-13s%s\n", c.Name, c.Desc)
	}
}

// PrintRootBanner is the bare `kdiag` (no-args) screen — a terse pointer:
// branded title, usage line, and a hint to run `kdiag -h` for the command
// list. Intentionally does NOT enumerate commands so the bare invocation
// stays compact; the full list lives behind `kdiag -h` / `kdiag help`.
func PrintRootBanner(w io.Writer) {
	fmt.Fprint(w, `kdiag — Kubernetes diagnostic CLI

Usage:
  kdiag <command> [flags] [args]

Run "kdiag -h" for the list of available commands.
`)
}

// PrintRootUsage is the `kdiag --help` / `kdiag -h` screen. Branded title,
// command list, usage line, and a one-line pointer explaining that flags
// vary per command.
func PrintRootUsage(w io.Writer) {
	fmt.Fprint(w, "kdiag — Kubernetes diagnostic CLI\n\n")
	printRootUsageBody(w)
}

// PrintRootError is the unknown-command fallback. Same body as PrintRootUsage
// but without the branded title — the title belongs only to the explicit
// help screen, not to error output.
func PrintRootError(w io.Writer) {
	printRootUsageBody(w)
}

func printRootUsageBody(w io.Writer) {
	printCommandList(w, true)
	fmt.Fprint(w, `
Usage:
  kdiag <command> [flags] [args]

Flags vary by command. Run "kdiag help <command>" or "kdiag <command> --help" to see them.
`)
}

// PrintRootHelp is the `kdiag help` (no topic) screen: just the sorted
// command list. Matches §3 of the user spec.
func PrintRootHelp(w io.Writer) {
	printCommandList(w, true)
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

Flags:
  --namespace <ns>       Namespace (defaults to current context)
  --label <selector>     Label selector (pod, deploy, node)
  <partial-name>         Partial pod name match (pod only)
`)
	if showFormat(seen) {
		fmt.Fprintln(w, "  --yaml                 Emit YAML instead of text (default: text)")
	}
	if showPath(seen) {
		fmt.Fprintln(w, "  --path <needle>        Only print yq paths matching <needle> (kdiag help yml-path for details)")
	}
	if showResources(seen) {
		fmt.Fprintln(w, "  --resources            Narrow output to container resources (text or structured)")
	}
	if showSpec(seen) {
		fmt.Fprintln(w, "  --deployment-spec      Deploy only: emit .spec.template.spec (text or structured)")
	}
	if showAZ(seen) {
		fmt.Fprint(w, `  --az                   Availability-zone placement (pod, deploy, ds, sts).
                         Composes with --yaml; mutually exclusive with
                         --resources / --deployment-spec / --path (each selects a view).
`)
	}
	if showTroubleshoot(seen) {
		fmt.Fprintln(w, "  --troubleshoot         Diagnose problems (any kind): pod scheduling/runtime, workload")
		fmt.Fprintln(w, "                         replica health, or node health (text or structured)")
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Examples:")
	for _, ex := range inspectExamples(seen) {
		fmt.Fprintln(w, ex)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, `Use "kdiag inspect <subcommand> --help" for details.`)
}

// Composition rules for view selectors:
//
//	none seen        → show everything
//	path             → show only --namespace, --label, --path
//	resources        → show --resources, --yaml, --az; hide --path, --deployment-spec
//	deployment-spec  → show --deployment-spec, --yaml; hide --path, --resources, --az
//	az               → show --az, --yaml; hide --path, --resources, --deployment-spec
func showPath(seen string) bool         { return seen == "" || seen == "path" }
func showFormat(seen string) bool       { return seen != "path" }
func showResources(seen string) bool    { return seen == "" || seen == "resources" }
func showSpec(seen string) bool         { return seen == "" || seen == "deployment-spec" }
func showAZ(seen string) bool           { return seen == "" || seen == "az" || seen == "resources" }
func showTroubleshoot(seen string) bool { return seen == "" || seen == "troubleshoot" }

func inspectExamples(seen string) []string {
	switch seen {
	case "path":
		return []string{
			"  kdiag inspect pod my-pod --path qosClass",
			"  kdiag inspect deploy --label app=my-app --path '*image*'",
		}
	case "resources":
		return []string{
			"  kdiag inspect pod my-pod --resources",
			"  kdiag inspect deploy my-deploy --resources --yaml",
		}
	case "deployment-spec":
		return []string{
			"  kdiag inspect deploy my-deploy --deployment-spec",
			"  kdiag inspect deploy my-deploy --deployment-spec --yaml",
		}
	case "az":
		return []string{
			"  kdiag inspect pod --az --namespace my-ns --label app=my-app",
			"  kdiag inspect deploy my-deploy --az --yaml",
		}
	default:
		return []string{
			"  kdiag inspect pod my-pod",
			"  kdiag inspect deploy my-deploy --resources --yaml",
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

Sources:
  Two documents are searched per resource, each under its own header:
    # kubectl get <kind> <name> -o yaml   the raw API object
    # kdiag inspect <kind> <name> --yaml  kdiag's curated view
  The curated view synthesizes fields the raw object lacks (e.g. tag/digest
  split from an image), so a needle like '*tag*' resolves there. A header prints
  only when that document matched; its paths are valid yq targets against the
  command named in the header. CRDs and kinds without a curated view show the
  raw section only.

Matching:
  Default match is exact (full key or full value).
  Use '*' as a glob: 'name*' (prefix), '*name' (suffix), '*name*' (substring).
  Smart-case: lowercase needle is case-insensitive; uppercase makes it case-sensitive.

Compatibility:
  --path composes with --namespace and --label.
  --path is mutually exclusive with --yaml, --resources, --deployment-spec,
  --az (each selects a different view).

Examples:
  kdiag inspect pod my-pod --path qosClass
  kdiag inspect deploy --label app=my-app --path memory
  kdiag inspect node --label kubernetes.io/hostname --path '*hostname*'
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
  kdiag diff <kind> [--namespace <ns>] [--full] <name-a> <name-b>

  # Replicaset has an additional revision-diff shape:
  kdiag diff rs [--namespace <ns>] [--full] <deployment-name> [<rev-from> <rev-to>]
  kdiag diff rs [--namespace <ns>] [--full] --label <selector>  [<rev-from> <rev-to>]

Flags:
  --namespace <ns>   namespace (defaults to current context)
  --full             show the raw API server response (no per-kind noise stripping;
                     for rs, dump the full RS objects instead of just .spec.template)

Examples:
  kdiag diff pod --namespace my-ns pod-abc pod-def
  kdiag diff rs  --namespace my-ns my-deployment           # last two revisions
  kdiag diff rs  --namespace my-ns my-deployment 2 5 --full

Use "kdiag diff rs --help" for the full revision-diff help.
`)
}

// PrintDiffKindUsage prints help for `kdiag diff <kind>` for a generic (non-rs) kind.
// Shows usage specific to that kind with one relevant example.
func PrintDiffKindUsage(w io.Writer, kind string) {
	fmt.Fprintf(w, `Diff two %s resources.

By default, noise is stripped per-kind. Pass --full to show the raw API server
response with no filtering.

Usage:
  kdiag diff %s [--namespace <ns>] [--full] <name-a> <name-b>

Flags:
  --namespace <ns>   namespace (defaults to current context)
  --full             show the raw API server response (no per-kind noise stripping)

Examples:
  kdiag diff %s --namespace my-ns resource-a resource-b

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
  kdiag sort <kind> [--namespace <ns> | --all-namespaces]

Kinds:
  Any resource the API server exposes — built-ins (pod, deployment, daemonset,
  statefulset, replicaset, node, namespace, configmap, secret, service,
  ingress, persistentvolumeclaim, serviceaccount, role, rolebinding,
  horizontalpodautoscaler, …) and CRDs (e.g. certificate.cert-manager.io).
  Canonical names, plurals, and shortnames (po, deploy, cm, svc, ing, pvc, sa)
  are all accepted.

Flags:
  --namespace <ns>     Namespace (defaults to current context)
  --all-namespaces     List resources across all namespaces (overrides --namespace)

Notes:
  Cluster-scoped kinds (node, namespace, pv, …) ignore --namespace and --all-namespaces.

Examples:
  kdiag sort pod
  kdiag sort deploy --all-namespaces
  kdiag sort certificates.cert-manager.io --all-namespaces
`)
}

// PrintEventsUsage prints help for `kdiag events`.
func PrintEventsUsage(w io.Writer) {
	fmt.Fprint(w, `Show events (Normal and Warning) in the current namespace.

Sorted by their effective timestamp ascending (newest entry last, like `+"`kubectl logs`"+`).

Usage:
  kdiag events [--namespace <ns> | --all-namespaces] [--since <duration>]

Flags:
  --namespace <ns>       Namespace (defaults to current context)
  --all-namespaces       List events across all namespaces (overrides --namespace)
  --since <duration>     Only show events newer than this duration, e.g. 30s, 5m, 2h (default: 1h)

Examples:
  kdiag events
  kdiag events --all-namespaces --since 30m
  kdiag events --namespace my-ns --since 24h
`)
}

// ViewFlagSeen scans args for the first view-selector flag and returns its
// short name ("path", "resources", "deployment-spec", "az", "troubleshoot") or
// "" if none is present. Accepts both the space-separated form (--path x) and
// the inline form (--path=x). Used by help printers and the completion scripts
// (transitively) to filter what gets suggested once a view is set.
func ViewFlagSeen(args []string) string {
	for _, a := range args {
		switch {
		case a == "--path" || strings.HasPrefix(a, "--path="):
			return "path"
		case a == "--resources":
			return "resources"
		case a == "--deployment-spec":
			return "deployment-spec"
		case a == "--az":
			return "az"
		case a == "--troubleshoot":
			return "troubleshoot"
		}
	}
	return ""
}

// FormatFlagsLongOnly renders pflag flag usages in pflag's standard
// two-column shape, but omits the short-form alias from the head column
// (so `-n, --namespace string` becomes `--namespace string`).
//
// Used by per-command help printers that want a visible Flags: block
// without advertising the single-dash aliases (which remain functional
// at parse time — they're just hidden from documentation).
func FormatFlagsLongOnly(fs *pflag.FlagSet) string {
	type row struct {
		head string
		desc string
	}
	var rows []row
	fs.VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		head := "--" + f.Name
		if t := f.Value.Type(); t != "bool" {
			head += " " + t
		}
		desc := f.Usage
		if showDefault(f) {
			desc += fmt.Sprintf(" (default %s)", f.DefValue)
		}
		rows = append(rows, row{head, desc})
	})
	width := 0
	for _, r := range rows {
		if len(r.head) > width {
			width = len(r.head)
		}
	}
	var b strings.Builder
	for _, r := range rows {
		b.WriteString("  ")
		b.WriteString(r.head)
		b.WriteString(strings.Repeat(" ", width-len(r.head)+3))
		b.WriteString(r.desc)
		b.WriteString("\n")
	}
	return b.String()
}

// showDefault decides whether a flag's default is worth printing — true
// for non-zero values of any type. Mirrors pflag's own behaviour closely
// enough that the resulting block reads the same minus the short alias.
func showDefault(f *pflag.Flag) bool {
	switch f.Value.Type() {
	case "bool":
		return f.DefValue != "false"
	case "string":
		return f.DefValue != ""
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"float32", "float64":
		return f.DefValue != "0"
	case "duration":
		return f.DefValue != "0s"
	default:
		return f.DefValue != ""
	}
}
