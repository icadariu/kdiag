package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/spf13/pflag"
	"sigs.k8s.io/yaml"

	"example.com/kdiag/internal/cli"
	"example.com/kdiag/internal/kube"
)

// RunInspect dispatches `kdiag inspect <kind> ...` to the per-kind handler.
func RunInspect(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Error: inspect requires a kind: pod, deploy, ds, sts, rs, node")
		fmt.Fprintln(os.Stderr)
		cli.PrintInspectUsage(os.Stderr, args)
		os.Exit(1)
	}
	if cli.WantsHelp(args) {
		cli.PrintInspectUsage(os.Stdout, args)
		return
	}

	// Locate the kind token, skipping flags and their values so that flags
	// may appear before the subcommand (e.g. `inspect --az deploy coredns`).
	kindIdx := kindIndex(args)
	if kindIdx < 0 {
		fmt.Fprintln(os.Stderr, "Error: inspect requires a kind: pod, deploy, ds, sts, rs, node")
		fmt.Fprintln(os.Stderr)
		cli.PrintInspectUsage(os.Stderr, args)
		os.Exit(1)
	}
	kind := args[kindIdx]
	// handlerArgs = all tokens except the kind itself.
	handlerArgs := append(args[:kindIdx:kindIdx], args[kindIdx+1:]...)

	// Check for help on the specific kind, even if --yml-path is present.
	// This allows `inspect pod --yml-path memory -h` to show kind-specific help.
	// We check anywhere in handlerArgs (not just the first element) to support
	// help requests after other flags.
	if cli.WantsHelp(handlerArgs) || hasFlag(handlerArgs, "-h") || hasFlag(handlerArgs, "--help") {
		switch kube.CanonicalKind(kind) {
		case "pod":
			// The handler will receive the full handlerArgs and can filter
			// help based on what view is selected (via cli.ViewFlagSeen).
			runInspectPod(handlerArgs)
		case "deployment":
			runInspectDeploy(handlerArgs)
		case "daemonset":
			runInspectDaemonSet(handlerArgs)
		case "statefulset":
			runInspectStatefulSet(handlerArgs)
		case "replicaset":
			runInspectReplicaSet(handlerArgs)
		case "node":
			runInspectNode(handlerArgs)
		default:
			fmt.Fprintf(os.Stderr, "Error: unknown inspect kind: %s\n\n", kind)
			cli.PrintInspectUsage(os.Stderr, args)
			os.Exit(1)
		}
		return
	}

	// --spec is only valid for the deploy kind. Catch it here so the error
	// path is uniform regardless of where the flag appears.
	if hasFlag(handlerArgs, "--spec") && kube.CanonicalKind(kind) != "deployment" {
		fmt.Fprintln(os.Stderr, "Error: --spec is only valid for `inspect deploy`")
		os.Exit(1)
	}

	// --path short-circuits the per-kind handlers with a generic
	// dynamic-client walker. Parsing happens here (rather than in commonFlags)
	// because the walker is kind-agnostic and we want CRD support.
	if needle, name, selector, ns, ok := extractPathArgs(handlerArgs); ok {
		env, err := kube.NewKubeEnv(kube.KubeFlags{Namespace: ns})
		if err != nil {
			cli.Fatal(err)
		}
		runInspectPath(env, kind, name, selector, needle)
		return
	}

	switch kube.CanonicalKind(kind) {
	case "pod":
		runInspectPod(handlerArgs)
	case "deployment":
		runInspectDeploy(handlerArgs)
	case "daemonset":
		runInspectDaemonSet(handlerArgs)
	case "statefulset":
		runInspectStatefulSet(handlerArgs)
	case "replicaset":
		runInspectReplicaSet(handlerArgs)
	case "node":
		runInspectNode(handlerArgs)
	default:
		fmt.Fprintf(os.Stderr, "Error: unknown inspect kind: %s\n\n", kind)
		cli.PrintInspectUsage(os.Stderr, args)
		os.Exit(1)
	}
}

// kindIndex returns the index of the first non-flag token in args, skipping
// flags and their values. Flags that consume a value token are listed
// explicitly; all other dash-prefixed tokens are treated as boolean flags.
// Returns -1 when no positional token is found.
func kindIndex(args []string) int {
	valueFlags := map[string]bool{
		"--namespace": true, "-n": true,
		"--label":  true, "-l": true,
		"--path":   true,
		"--format": true,
	}
	for i := 0; i < len(args); i++ {
		if valueFlags[args[i]] {
			i++ // skip the flag's value
			continue
		}
		if !strings.HasPrefix(args[i], "-") {
			return i
		}
	}
	return -1
}

// extractPathArgs scans handlerArgs (after the kind has been removed)
// for `--path <v>` / `--path=<v>` together with `-n/--namespace`
// and `-l/--label`. Remaining non-flag tokens become the positional name.
//
// Returns ok=false when --path is absent, so the caller falls through
// to the existing per-kind handlers.
//
// Errors (missing/empty value, unknown flag, both name+selector, multiple
// names) are fatal once --path has been seen — they would also fail
// inside the per-kind handler, but here we fail earlier because the kind
// switch has been bypassed. Whitespace-only needles are rejected too:
// they would otherwise match any whitespace-containing scalar.
func extractPathArgs(handlerArgs []string) (needle, name, selector, ns string, ok bool) {
	var (
		rest    []string
		seen    bool   // --path was present (even with empty value)
		unknown string // first unknown -flag seen; only fatal if --path is set
	)
	for i := 0; i < len(handlerArgs); i++ {
		a := handlerArgs[i]
		switch {
		case a == "--path":
			if i+1 >= len(handlerArgs) {
				cli.Fatal(fmt.Errorf("--path requires a value"))
			}
			needle = handlerArgs[i+1]
			seen = true
			i++
		case strings.HasPrefix(a, "--path="):
			needle = strings.TrimPrefix(a, "--path=")
			seen = true
		case a == "-n" || a == "--namespace":
			if i+1 >= len(handlerArgs) {
				cli.Fatal(fmt.Errorf("%s requires a value", a))
			}
			ns = handlerArgs[i+1]
			i++
		case strings.HasPrefix(a, "--namespace="):
			ns = strings.TrimPrefix(a, "--namespace=")
		case a == "-l" || a == "--label":
			if i+1 >= len(handlerArgs) {
				cli.Fatal(fmt.Errorf("%s requires a value", a))
			}
			selector = handlerArgs[i+1]
			i++
		case strings.HasPrefix(a, "--label="):
			selector = strings.TrimPrefix(a, "--label=")
		case strings.HasPrefix(a, "-"):
			// Stash for later. We only know it's "unknown" relative to the
			// --path handler; if --path is absent we fall through to per-kind
			// handlers, which parse and reject these themselves.
			if unknown == "" {
				unknown = a
			}
		default:
			rest = append(rest, a)
		}
	}
	if !seen {
		return "", "", "", "", false
	}
	if strings.TrimSpace(needle) == "" {
		cli.Fatal(fmt.Errorf("--path requires a non-empty value"))
	}
	switch {
	case unknown == "--format" || strings.HasPrefix(unknown, "--format="),
		unknown == "--resources",
		unknown == "--spec",
		unknown == "--az":
		cli.Fatal(fmt.Errorf(
			"--path is mutually exclusive with %s (each selects a view). "+
				"Drop one of them, or run `kdiag inspect <kind> -h` for usage.", unknown))
	}
	if unknown != "" {
		cli.Fatal(fmt.Errorf(
			"--path: unknown flag %q (only -n/--namespace and -l/--label compose with --path; "+
				"--format, --resources, --spec, --az are mutually exclusive)", unknown))
	}
	if len(rest) > 1 {
		cli.Fatal(fmt.Errorf("inspect accepts only one name argument, got %d", len(rest)))
	}
	if len(rest) == 1 {
		name = rest[0]
	}
	if name != "" && selector != "" {
		cli.Fatal(fmt.Errorf("--path: provide either <name> or --label/-l (not both)"))
	}
	if name == "" && selector == "" {
		cli.Fatal(fmt.Errorf("--path: provide either <name> or --label/-l"))
	}
	return needle, name, selector, ns, true
}

// commonFlags wires -n/--resources onto fs and returns pointers the caller
// reads after Parse.
func commonFlags(fs *pflag.FlagSet) (*kube.KubeFlags, *bool) {
	var k kube.KubeFlags
	var showResources bool
	fs.StringVarP(&k.Namespace, "namespace", "n", "", "namespace (defaults to current context)")
	fs.BoolVar(&showResources, "resources", false, "show resource requests/limits")
	return &k, &showResources
}

// inspectPodObject renders the pod-level summary plus the per-container blocks
// for a single pod. Header (Pod: <name>, Node, Pod IP, QoS) is always printed —
// callers should not also print "Pod: <name>".
func inspectPodObject(podObj corev1.Pod, showResources bool) {
	if !showResources {
		fmt.Printf("Pod: %s\n", podObj.Name)
		fmt.Printf("  Node:          %s\n", dashIfEmpty(podObj.Spec.NodeName))
		fmt.Printf("  Pod IP:        %s\n", dashIfEmpty(podObj.Status.PodIP))
		fmt.Printf("  QoS:           %s\n", dashIfEmpty(string(podObj.Status.QOSClass)))
		fmt.Println()
	}

	views := kube.CollectContainerViews(&podObj)
	if len(views) == 0 {
		fmt.Println("No containers found.")
		return
	}

	for _, v := range views {
		if showResources {
			fmt.Printf("  %-19s%s\n", v.Kind.String()+":", v.Spec.Name)
			req, lim := kube.ResourcesFromSpec(v.Spec)
			fmt.Println("    Resources:")
			fmt.Println("      Requests:")
			cli.PrintKVBlock(os.Stdout, "        ", req)
			fmt.Println("      Limits:")
			cli.PrintKVBlock(os.Stdout, "        ", lim)
		} else {
			// Header uses the kind label (Init Container / Sidecar Container / Container).
			// 19-char width gives every label at least one space before the container name.
			fmt.Printf("%-19s%s\n", v.Kind.String()+":", v.Spec.Name)

			repo, ref, isDigest := kube.ParseImage(v.Spec.Image)
			fmt.Printf("  Image:         %s\n", repo)
			if isDigest {
				fmt.Printf("  Digest:        %s\n", ref)
			} else {
				fmt.Printf("  Tag:           %s\n", ref)
			}
			fmt.Printf("  Ports:         %s\n", kube.FormatPorts(v.Spec.Ports))

			if v.Status != nil {
				fmt.Printf("  State:         %s\n", kube.ContainerStateKey(v.Status.State))
				if r := kube.ContainerStateReason(v.Status.State); r != "" {
					fmt.Printf("    Reason:      %s\n", r)
				}
				fmt.Printf("  Last State:    %s\n", kube.ContainerStateKey(v.Status.LastTerminationState))
				if r := kube.ContainerStateReason(v.Status.LastTerminationState); r != "" {
					fmt.Printf("    Reason:      %s\n", r)
				}
				fmt.Printf("  Ready:         %t\n", v.Status.Ready)
				fmt.Printf("  Restart Count: %d\n", v.Status.RestartCount)
			} else {
				fmt.Println("  State:         <not started>")
			}
		}
		fmt.Println()
	}
}

// inspectWorkloadPods prints the workload's summary block and then enumerates
// pods matching its selector, emitting the per-pod container blocks. Used by
// deploy/ds/sts/rs handlers.
func inspectWorkloadPods(env *kube.KubeEnv, kindLabel, name string, summary map[string]string, selector *metav1.LabelSelector, showResources bool) {
	fmt.Printf("%s: %s\n", kindLabel, name)
	cli.PrintKVBlock(os.Stdout, "  ", summary)
	fmt.Println()

	labelSel := metav1.FormatLabelSelector(selector)
	pods, err := env.Clientset.CoreV1().Pods(env.Namespace).List(context.Background(), kube.ListOptions(labelSel))
	if err != nil {
		cli.Fatal(fmt.Errorf("list pods: %w", err))
	}

	if len(pods.Items) == 0 {
		fmt.Println("No pods found.")
		return
	}
	for i := range pods.Items {
		p := pods.Items[i]
		fmt.Println("==========================================")
		inspectPodObject(p, showResources)
	}
}

func dashIfEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// emitYAML marshals v to YAML on stdout. Used wherever --format yaml is set
// (alone or combined with --resources / --spec) to produce stdout that is
// valid YAML (yq-pipeable, no banners).
func emitYAML(v any) {
	y, err := yaml.Marshal(v)
	if err != nil {
		cli.Fatal(fmt.Errorf("marshal yaml: %w", err))
	}
	fmt.Print(string(y))
}

// hasFlag reports whether name appears in args either as a bare token
// (`--spec`) or with an inline value (`--spec=...`).
func hasFlag(args []string, name string) bool {
	prefix := name + "="
	for _, a := range args {
		if a == name || strings.HasPrefix(a, prefix) {
			return true
		}
	}
	return false
}
