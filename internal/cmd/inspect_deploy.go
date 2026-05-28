package cmd

import (
	"context"
	"fmt"
	"io"
	"os"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/spf13/pflag"

	"example.com/kdiag/internal/cli"
	"example.com/kdiag/internal/kube"
)

// runInspectDeploy implements `inspect deploy`. Unlike ds/sts/rs (which share
// runWorkload), deploy supports YAML-mode flags that emit subtrees of the
// deployment's pod template — designed for piping into yq.
func runInspectDeploy(args []string) {
	fs := pflag.NewFlagSet("inspect deploy", pflag.ExitOnError)
	k, showResources := commonFlags(fs)
	var (
		selector string
		showAZ   bool
		showSpec bool
		output   string
	)
	fs.StringVarP(&selector, "label", "l", "", "label selector to identify the deployment")
	if cli.ViewFlagSeen(args) != "path" {
		fs.BoolVar(&showAZ, "az", false, "show availability-zone placement")
		fs.BoolVar(&showSpec, "deployment-spec", false, "print pod template spec (text or structured)")
		fs.StringVarP(&output, "output", "o", "", "output format: json|yaml (default: text)")
	} else {
		// --resources was registered in commonFlags(); the dispatcher's
		// extractPathArgs rejects --resources with --path explicitly,
		// but hide it from -h so users aren't tempted.
		_ = fs.MarkHidden("resources")
	}
	fs.Usage = func() { printInspectDeployHelp(os.Stderr, fs, args) }

	// Check for help in args (it may not be the first element if other flags
	// like --path appear before -h).
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			printInspectDeployHelp(os.Stdout, fs, args)
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
	switch output {
	case "", "json", "yaml":
	default:
		cli.Fatal(fmt.Errorf("-o/--output must be 'json' or 'yaml', got %q", output))
	}
	structured := output != ""

	// Resolve the deployment: either a positional <name> or --label <selector>
	// (mirrors `diff rs`). Selector must resolve to exactly one deployment.
	switch {
	case len(rest) == 1 && selector == "":
	case len(rest) == 0 && selector != "":
	case len(rest) > 0 && selector != "":
		fmt.Fprintln(os.Stderr, "Error: provide either <name> OR --label (not both)")
		fs.Usage()
		os.Exit(1)
	default:
		fmt.Fprintln(os.Stderr, "Error: inspect deploy requires exactly one <name> or --label")
		fs.Usage()
		os.Exit(1)
	}

	if boolsSet(*showResources, showSpec) > 1 {
		fmt.Fprintln(os.Stderr, "Error: --resources and --deployment-spec are mutually exclusive")
		os.Exit(1)
	}

	env, err := kube.NewKubeEnv(*k)
	if err != nil {
		cli.Fatal(err)
	}
	ctx := context.Background()

	var d *appsv1.Deployment
	var name string
	if selector != "" {
		list, err := env.Clientset.AppsV1().Deployments(env.Namespace).List(ctx, kube.ListOptions(selector))
		if err != nil {
			cli.Fatal(fmt.Errorf("list deployments: %w", err))
		}
		switch len(list.Items) {
		case 0:
			fmt.Fprintln(os.Stderr, "Error: no deployments matched --label")
			os.Exit(1)
		case 1:
			d = &list.Items[0]
		default:
			fmt.Fprintf(os.Stderr, "Error: --label matched %d deployments — be more specific:\n", len(list.Items))
			for _, item := range list.Items {
				fmt.Fprintf(os.Stderr, "  %s\n", item.Name)
			}
			os.Exit(1)
		}
		name = d.Name
	} else {
		name = rest[0]
		d, err = env.Clientset.AppsV1().Deployments(env.Namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			cli.Fatal(fmt.Errorf("get deployment: %w", err))
		}
	}

	// --resources/--deployment-spec/--az are view selectors (mutex); -o/--output
	// composes with any view.
	if showAZ && (*showResources || showSpec) {
		fmt.Fprintln(os.Stderr, "Error: --az is mutually exclusive with --resources/--deployment-spec (all select a view)")
		os.Exit(1)
	}

	if structured {
		switch {
		case showAZ:
			labelSel := metav1.FormatLabelSelector(d.Spec.Selector)
			pods, err := env.Clientset.CoreV1().Pods(env.Namespace).List(ctx, kube.ListOptions(labelSel))
			if err != nil {
				cli.Fatal(fmt.Errorf("list pods: %w", err))
			}
			emit(output, collectAZ(env, ctx, pods.Items))
		case showSpec:
			emit(output, d.Spec.Template.Spec)
		case *showResources:
			pods := listDeployPods(env, ctx, d)
			all := make([]containerResourceSlice, 0)
			for _, p := range pods {
				all = append(all, resourceSliceFor(p)...)
			}
			emit(output, all)
		default:
			pods := listDeployPods(env, ctx, d)
			emit(output, deployWorkloadInfo(env, d, pods))
		}
		return
	}

	if showSpec {
		// Wrap the deployment's template spec in a Pod so we can reuse the
		// per-container view collector. No pod-level header is emitted since
		// Node/Pod IP/QoS are meaningless for an unscheduled template.
		tmplPod := corev1.Pod{Spec: d.Spec.Template.Spec}
		printContainerBlocks(tmplPod, *showResources)
		return
	}

	summary := deploySummary(d)
	if showAZ {
		labelSel := metav1.FormatLabelSelector(d.Spec.Selector)
		pods, err := env.Clientset.CoreV1().Pods(env.Namespace).List(ctx, kube.ListOptions(labelSel))
		if err != nil {
			cli.Fatal(fmt.Errorf("list pods: %w", err))
		}
		fmt.Printf("Deployment: %s\n", name)
		if len(pods.Items) == 0 {
			fmt.Println("No pods found.")
			return
		}
		printAZTable(env, ctx, pods.Items)
		return
	}

	inspectWorkloadPods(env, "Deployment", name, summary, d.Spec.Selector, *showResources)
}

func boolsSet(bs ...bool) int {
	n := 0
	for _, b := range bs {
		if b {
			n++
		}
	}
	return n
}

// workloadInfo is the kdiag-shaped YAML payload for `inspect deploy --yaml`
// (and for the other workload kinds in Task 10). It deliberately mirrors
// what the text mode shows so the YAML is a structured version of the same
// diagnostic — NOT the raw apps/v1.Deployment object (use kubectl for that).
type workloadInfo struct {
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	Namespace string    `json:"namespace"`
	Replicas  string    `json:"replicas,omitempty"`
	Strategy  string    `json:"strategy,omitempty"`
	Selector  string    `json:"selector,omitempty"`
	Pods      []podInfo `json:"pods"`
}

func deployWorkloadInfo(env *kube.KubeEnv, d *appsv1.Deployment, pods []corev1.Pod) workloadInfo {
	sum := deploySummary(d)
	out := workloadInfo{
		Name:      d.Name,
		Kind:      "Deployment",
		Namespace: env.Namespace,
		Replicas:  sum["Replicas"],
		Strategy:  sum["Strategy"],
		Selector:  sum["Selector"],
		Pods:      make([]podInfo, 0, len(pods)),
	}
	for _, p := range pods {
		out.Pods = append(out.Pods, podInfoFrom(p))
	}
	return out
}

func listDeployPods(env *kube.KubeEnv, ctx context.Context, d *appsv1.Deployment) []corev1.Pod {
	labelSel := metav1.FormatLabelSelector(d.Spec.Selector)
	pods, err := env.Clientset.CoreV1().Pods(env.Namespace).List(ctx, kube.ListOptions(labelSel))
	if err != nil {
		cli.Fatal(fmt.Errorf("list pods: %w", err))
	}
	return pods.Items
}

func printInspectDeployHelp(w io.Writer, fs *pflag.FlagSet, args []string) {
	seen := cli.ViewFlagSeen(args)
	fmt.Fprintln(w, "Usage: kdiag inspect deploy [flags] [<deployment-name> | --label <selector>]")
	fmt.Fprintln(w, "\nShow deployment summary and per-pod container state.")
	fmt.Fprintln(w, "\nFormat: default is text; -o/--output json|yaml emits a structured document.")
	switch seen {
	case "path":
		fmt.Fprintln(w, "\nView: --path is set. Pass --path <needle> with --namespace/--label only. See `kdiag help yml-path`.")
	case "":
		fmt.Fprintln(w, "\nViews: --resources, --deployment-spec, --az, --path are mutually exclusive.")
		fmt.Fprintln(w, "  -o/--output composes with --resources/--deployment-spec/--az; --path takes only --namespace/--label.")
	}
	fmt.Fprintln(w, "\nFlags:")
	fmt.Fprint(w, cli.FormatFlagsLongOnly(fs))
	fmt.Fprintln(w, "\nExamples:")
	for _, ex := range deployExamples(seen) {
		fmt.Fprintln(w, ex)
	}
}

func deployExamples(seen string) []string {
	switch seen {
	case "path":
		return []string{
			"  kdiag inspect deploy my-deploy --path memory",
			"  kdiag inspect deploy --label app=my-app --path '*image*'",
		}
	case "resources":
		return []string{
			"  kdiag inspect deploy my-deploy --resources",
			"  kdiag inspect deploy my-deploy --resources -o yaml",
		}
	case "deployment-spec":
		return []string{
			"  kdiag inspect deploy my-deploy --deployment-spec",
			"  kdiag inspect deploy my-deploy --deployment-spec -o yaml",
		}
	case "az":
		return []string{
			"  kdiag inspect deploy my-deploy --az",
			"  kdiag inspect deploy my-deploy --az -o yaml",
		}
	default:
		return []string{
			"  kdiag inspect deploy my-deploy",
			"  kdiag inspect deploy my-deploy --resources -o yaml",
			"  kdiag inspect deploy my-deploy --path memory",
		}
	}
}
