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
		showYAML bool
	)
	fs.StringVarP(&selector, "label", "l", "", "label selector")
	fs.BoolVar(&showAZ, "az", false, "show availability-zone placement")
	fs.BoolVar(&showYAML, "yaml", false, "emit yq-safe YAML instead of text")
	// `--yml` is an accepted alias for `--yaml`.
	fs.SetNormalizeFunc(func(f *pflag.FlagSet, name string) pflag.NormalizedName {
		if name == "yml" {
			return "yaml"
		}
		return pflag.NormalizedName(name)
	})
	fs.Usage = func() { printInspectPodHelp(os.Stderr, fs) }

	if cli.WantsHelp(args) {
		printInspectPodHelp(os.Stdout, fs)
		return
	}
	_ = fs.Parse(args)
	rest := fs.Args()
	selector = strings.TrimSpace(selector)

	if len(rest) > 0 && selector != "" {
		fmt.Fprintln(os.Stderr, "Error: provide either <pod-name> OR --label/-l (not both)")
		fs.Usage()
		os.Exit(1)
	}
	if len(rest) > 1 {
		fmt.Fprintln(os.Stderr, "Error: inspect pod accepts only one <partial-pod-name>")
		os.Exit(1)
	}

	if *showResources && showAZ {
		fmt.Fprintln(os.Stderr, "Error: --resources cannot be combined with --az")
		os.Exit(1)
	}

	// Validation: --yaml is incompatible with --az.
	if showYAML && showAZ {
		fmt.Fprintln(os.Stderr, "Error: --yaml cannot be combined with --az")
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
			case showAZ:
				printAZTable(env, ctx, matches)
			case showYAML && *showResources:
				emitYAML(resourceSliceFor(matches[0]))
			case showYAML:
				emitYAML(podInfoFrom(matches[0]))
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
		if showYAML {
			fmt.Fprintln(os.Stderr, "No pods found.")
			os.Exit(1)
		}
		fmt.Println("No pods found.")
		return
	}
	switch {
	case showAZ:
		printAZTable(env, ctx, pods.Items)
	case showYAML && *showResources:
		all := make([]containerResourceSlice, 0)
		for _, p := range pods.Items {
			all = append(all, resourceSliceFor(p)...)
		}
		emitYAML(all)
	case showYAML:
		out := make([]podInfo, 0, len(pods.Items))
		for _, p := range pods.Items {
			out = append(out, podInfoFrom(p))
		}
		emitYAML(out)
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

func printInspectPodHelp(w io.Writer, fs *pflag.FlagSet) {
	fmt.Fprintln(w, "Usage: kdiag inspect pod [flags] [<partial-pod-name> | -l <label>]")
	fmt.Fprintln(w, "\nShow container state for one pod or a set of pods.")
	fmt.Fprintln(w, "\nFormat:")
	fmt.Fprintln(w, "  Default output is human-readable text. Pass --yaml (or --yml) for a single")
	fmt.Fprintln(w, "  YAML document on stdout, safe to pipe through yq.")
	fmt.Fprintln(w, "    single pod:    map { name, namespace, containers, ... }")
	fmt.Fprintln(w, "    multiple pods: flat list of pod-info maps")
	fmt.Fprintln(w, "\n--resources narrows the content (text or YAML). --az is mutually exclusive with --yaml.")
	fmt.Fprintln(w, "\nFlags:")
	fmt.Fprint(w, fs.FlagUsages())
	fmt.Fprintln(w, "\nExamples:")
	fmt.Fprintln(w, "  kdiag inspect pod my-pod")
	fmt.Fprintln(w, "  kdiag inspect pod my-pod --yaml | yq '.containers[].name'")
	fmt.Fprintln(w, "  kdiag inspect pod -l app=my-app --yaml | yq '.[0].name'")
	fmt.Fprintln(w, "  kdiag inspect pod my-pod --resources --yaml")
	fmt.Fprintln(w, "  kdiag inspect pod --az -n my-ns -l app=my-app")
}
