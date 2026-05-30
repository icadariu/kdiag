package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/spf13/pflag"

	"example.com/kdiag/internal/cli"
	"example.com/kdiag/internal/kube"
)

func runInspectPod(args []string) {
	fs := pflag.NewFlagSet("inspect pod", pflag.ExitOnError)
	k, showResources := commonFlags(fs)
	var (
		selector string
		showAZ   bool
	)
	wantYAML := func() bool { return false }
	fs.StringVarP(&selector, "label", "l", "", "label selector")
	if cli.ViewFlagSeen(args) != "path" {
		fs.BoolVar(&showAZ, "az", false, "show availability-zone placement")
		wantYAML = registerYAMLFlag(fs)
	} else {
		// --resources was registered in commonFlags(); the dispatcher's
		// extractPathArgs rejects --resources with --path explicitly,
		// but hide it from -h so users aren't tempted.
		_ = fs.MarkHidden("resources")
	}
	fs.Usage = func() { printInspectPodHelp(os.Stderr, fs, args) }

	// Check for help in args (it may not be the first element if other flags
	// like --path appear before -h).
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			printInspectPodHelp(os.Stdout, fs, args)
			return
		}
	}

	// Only parse args that don't contain unregistered flags (like --path).
	// The dispatcher handles --path before calling the handler, but we
	// make an exception for help (above) to allow filtering help when --path
	// is present. Any remaining --path is an error.
	if cli.ViewFlagSeen(args) == "path" {
		fmt.Fprintln(os.Stderr, "Error: --path is handled by the dispatcher and should not reach the handler")
		os.Exit(1)
	}

	_ = fs.Parse(args)
	rest := fs.Args()
	structured := wantYAML()
	selector = strings.TrimSpace(selector)

	if len(rest) > 0 && selector != "" {
		fmt.Fprintln(os.Stderr, "Error: provide either <pod-name> OR --label (not both)")
		fs.Usage()
		os.Exit(1)
	}
	if len(rest) > 1 {
		fmt.Fprintln(os.Stderr, "Error: inspect pod accepts only one <partial-pod-name>")
		os.Exit(1)
	}

	// --resources, --az (and --spec on deploy) are view selectors — at most
	// one may be chosen. --format is orthogonal and composes with any view.
	if *showResources && showAZ {
		fmt.Fprintln(os.Stderr, "Error: --resources and --az are mutually exclusive (both select a view)")
		os.Exit(1)
	}

	env, err := kube.NewKubeEnv(*k)
	if err != nil {
		cli.Fatal(err)
	}
	ctx := context.Background()

	// Partial name match: list all pods and filter by substring.
	if len(rest) == 1 {
		partial := rest[0]
		all, err := env.Clientset.CoreV1().Pods(env.Namespace).List(ctx, kube.ListOptions(""))
		if err != nil {
			cli.Fatal(fmt.Errorf("list pods: %w", err))
		}
		var matches []corev1.Pod
		for _, p := range all.Items {
			if strings.Contains(p.Name, partial) {
				matches = append(matches, p)
			}
		}
		switch len(matches) {
		case 0:
			fmt.Fprintf(os.Stderr, "Error: no pod found matching %q\n", partial)
			os.Exit(1)
		case 1:
			switch {
			case showAZ && structured:
				emit(collectAZ(env, ctx, matches))
			case showAZ:
				printAZTable(env, ctx, matches)
			case structured && *showResources:
				emit(resourceSliceFor(matches[0]))
			case structured:
				emit(podInfoFrom(matches[0]))
			default:
				inspectPodObject(matches[0], *showResources)
			}
		default:
			fmt.Fprintf(os.Stderr, "Error: %d pods match %q — be more specific:\n", len(matches), partial)
			for _, p := range matches {
				fmt.Fprintf(os.Stderr, "  %s\n", p.Name)
			}
			os.Exit(1)
		}
		return
	}

	// List by selector, or all pods when selector is empty.
	pods, err := env.Clientset.CoreV1().Pods(env.Namespace).List(ctx, kube.ListOptions(selector))
	if err != nil {
		cli.Fatal(fmt.Errorf("list pods: %w", err))
	}
	if len(pods.Items) == 0 {
		if structured {
			fmt.Fprintln(os.Stderr, "No pods found.")
			os.Exit(1)
		}
		fmt.Println("No pods found.")
		return
	}
	switch {
	case showAZ && structured:
		emit(collectAZ(env, ctx, pods.Items))
	case showAZ:
		printAZTable(env, ctx, pods.Items)
	case structured && *showResources:
		all := make([]containerResourceSlice, 0)
		for _, p := range pods.Items {
			all = append(all, resourceSliceFor(p)...)
		}
		emit(all)
	case structured:
		out := make([]podInfo, 0, len(pods.Items))
		for _, p := range pods.Items {
			out = append(out, podInfoFrom(p))
		}
		emit(out)
	default:
		for i := range pods.Items {
			p := pods.Items[i]
			fmt.Println("==========================================")
			inspectPodObject(p, *showResources)
		}
	}
}

// containerInfo is the per-container shape inside podInfo.
// `kind` is "Init", "Sidecar", or "Regular" so YAML consumers can
// filter without re-deriving sidecar semantics.
type containerInfo struct {
	Name         string                      `json:"name"`
	Kind         string                      `json:"kind"`
	Image        string                      `json:"image"`
	Tag          string                      `json:"tag,omitempty"`
	Digest       string                      `json:"digest,omitempty"`
	Ports        []string                    `json:"ports,omitempty"`
	State        string                      `json:"state"`
	StateReason  string                      `json:"stateReason,omitempty"`
	LastState    string                      `json:"lastState,omitempty"`
	LastReason   string                      `json:"lastStateReason,omitempty"`
	Ready        bool                        `json:"ready"`
	RestartCount int32                       `json:"restartCount"`
	Resources    corev1.ResourceRequirements `json:"resources"`
}

// podInfo is the per-pod YAML shape.
type podInfo struct {
	Name       string          `json:"name"`
	Namespace  string          `json:"namespace"`
	Node       string          `json:"node,omitempty"`
	PodIP      string          `json:"podIP,omitempty"`
	QoS        string          `json:"qosClass,omitempty"`
	Phase      string          `json:"phase,omitempty"`
	Containers []containerInfo `json:"containers"`
}

func podInfoFrom(p corev1.Pod) podInfo {
	views := kube.CollectContainerViews(&p)
	containers := make([]containerInfo, 0, len(views))
	for _, v := range views {
		repo, ref, isDigest := kube.ParseImage(v.Spec.Image)
		ci := containerInfo{
			Name:      v.Spec.Name,
			Kind:      kindString(v.Kind),
			Image:     repo,
			Resources: v.Spec.Resources,
		}
		if isDigest {
			ci.Digest = ref
		} else {
			ci.Tag = ref
		}
		for _, port := range v.Spec.Ports {
			ci.Ports = append(ci.Ports, fmt.Sprintf("%d/%s", port.ContainerPort, port.Protocol))
		}
		if v.Status != nil {
			ci.State = kube.ContainerStateKey(v.Status.State)
			ci.StateReason = kube.ContainerStateReason(v.Status.State)
			ci.LastState = kube.ContainerStateKey(v.Status.LastTerminationState)
			ci.LastReason = kube.ContainerStateReason(v.Status.LastTerminationState)
			ci.Ready = v.Status.Ready
			ci.RestartCount = v.Status.RestartCount
		}
		containers = append(containers, ci)
	}
	return podInfo{
		Name:       p.Name,
		Namespace:  p.Namespace,
		Node:       p.Spec.NodeName,
		PodIP:      p.Status.PodIP,
		QoS:        string(p.Status.QOSClass),
		Phase:      string(p.Status.Phase),
		Containers: containers,
	}
}

func kindString(k kube.ContainerKind) string {
	switch k {
	case kube.ContainerKindInit:
		return "Init"
	case kube.ContainerKindSidecar:
		return "Sidecar"
	default:
		return "Regular"
	}
}

// containerResourceSlice is the per-container shape for --resources --yaml.
type containerResourceSlice struct {
	Name      string                      `json:"name"`
	Kind      string                      `json:"kind"`
	Resources corev1.ResourceRequirements `json:"resources"`
}

func resourceSliceFor(p corev1.Pod) []containerResourceSlice {
	views := kube.CollectContainerViews(&p)
	out := make([]containerResourceSlice, 0, len(views))
	for _, v := range views {
		out = append(out, containerResourceSlice{
			Name:      v.Spec.Name,
			Kind:      kindString(v.Kind),
			Resources: v.Spec.Resources,
		})
	}
	return out
}

func printInspectPodHelp(w io.Writer, fs *pflag.FlagSet, args []string) {
	seen := cli.ViewFlagSeen(args)
	fmt.Fprintln(w, "Usage: kdiag inspect pod [flags] [<partial-pod-name> | --label <selector>]")
	fmt.Fprintln(w, "\nShow container state for one pod or a set of pods.")
	if seen != "path" {
		fmt.Fprintln(w, "\nFormat: default is text; --yaml/--yml emits a structured YAML doc (map for one pod, list for many).")
	}
	switch seen {
	case "path":
		fmt.Fprintln(w, "\nView: --path is set. Pass --path <needle> with --namespace/--label only. See `kdiag help yml-path`.")
	case "":
		fmt.Fprintln(w, "\nViews: --resources, --az, --path are mutually exclusive.")
		fmt.Fprintln(w, "  --yaml/--yml composes with --resources and --az; --path takes only --namespace/--label.")
	}
	fmt.Fprintln(w, "\nFlags:")
	fmt.Fprint(w, cli.FormatFlagsLongOnly(fs))
	fmt.Fprintln(w, "\nExamples:")
	for _, ex := range podExamples(seen) {
		fmt.Fprintln(w, ex)
	}
}

func podExamples(seen string) []string {
	switch seen {
	case "path":
		return []string{
			"  kdiag inspect pod my-pod --path qosClass",
			"  kdiag inspect pod --label app=my-app --path '*image*'",
		}
	case "resources":
		return []string{
			"  kdiag inspect pod my-pod --resources",
			"  kdiag inspect pod my-pod --resources --yaml",
		}
	case "az":
		return []string{
			"  kdiag inspect pod --az --namespace my-ns --label app=my-app",
			"  kdiag inspect pod --az --yaml --label app=my-app",
		}
	default:
		return []string{
			"  kdiag inspect pod my-pod",
			"  kdiag inspect pod --label app=my-app --yaml",
			"  kdiag inspect pod --az --namespace my-ns --label app=my-app",
		}
	}
}
