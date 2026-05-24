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
		showYAML bool
	)
	fs.StringVarP(&selector, "label", "l", "", "label selector to identify the deployment")
	fs.BoolVar(&showAZ, "az", false, "show availability-zone placement")
	fs.BoolVar(&showSpec, "spec", false, "print pod template spec (text or YAML)")
	fs.BoolVar(&showYAML, "yaml", false, "emit yq-safe YAML instead of text")
	// `--yml` is an accepted alias for `--yaml`.
	fs.SetNormalizeFunc(func(f *pflag.FlagSet, name string) pflag.NormalizedName {
		if name == "yml" {
			return "yaml"
		}
		return pflag.NormalizedName(name)
	})
	fs.Usage = func() { printInspectDeployHelp(os.Stderr, fs) }

	if cli.WantsHelp(args) {
		printInspectDeployHelp(os.Stdout, fs)
		return
	}
	_ = fs.Parse(args)
	rest := fs.Args()

	// Resolve the deployment: either a positional <name> or --label <selector>
	// (mirrors `diff rs`). Selector must resolve to exactly one deployment.
	switch {
	case len(rest) == 1 && selector == "":
	case len(rest) == 0 && selector != "":
	case len(rest) > 0 && selector != "":
		fmt.Fprintln(os.Stderr, "Error: provide either <name> OR --label/-l (not both)")
		fs.Usage()
		os.Exit(1)
	default:
		fmt.Fprintln(os.Stderr, "Error: inspect deploy requires exactly one <name> or --label/-l")
		fs.Usage()
		os.Exit(1)
	}

	if boolsSet(*showResources, showSpec) > 1 {
		fmt.Fprintln(os.Stderr, "Error: --resources and --spec are mutually exclusive")
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

	// --yaml is incompatible with --az.
	if showYAML && showAZ {
		fmt.Fprintln(os.Stderr, "Error: --yaml cannot be combined with --az")
		os.Exit(1)
	}

	if showYAML {
		switch {
		case showSpec:
			emitYAML(d.Spec.Template.Spec)
		case *showResources:
			pods := listDeployPods(env, ctx, d)
			all := make([]containerResourceSlice, 0)
			for _, p := range pods {
				all = append(all, resourceSliceFor(p)...)
			}
			emitYAML(all)
		default:
			pods := listDeployPods(env, ctx, d)
			emitYAML(deployWorkloadInfo(env, d, pods))
		}
		return
	}

	if showSpec {
		// Synthesize a Pod from the deployment's template so we can reuse the
		// per-container view collector and renderer.
		tmplPod := corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: d.Name + " (template)", Namespace: env.Namespace},
			Spec:       d.Spec.Template.Spec,
		}
		fmt.Printf("Deployment: %s (template)\n\n", d.Name)
		inspectPodObject(tmplPod, *showResources)
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

	inspectWorkloadPods(env, "Deployment", name, summary, d.Spec.Selector, false)
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

func printInspectDeployHelp(w io.Writer, fs *pflag.FlagSet) {
	fmt.Fprintln(w, "Usage: kdiag inspect deploy [flags] [<name> | -l <label>]")
	fmt.Fprintln(w, "\nShow summary and container state for all pods belonging to a Deployment.")
	fmt.Fprintln(w, "\nFormat:")
	fmt.Fprintln(w, "  Default output is text. With --yaml (or --yml) a single YAML document is emitted")
	fmt.Fprintln(w, "  on stdout (kdiag-shaped: { name, kind, replicas, selector, pods: [...] }).")
	fmt.Fprintln(w, "  --resources narrows the content; --spec (deploy only) emits the pod template spec.")
	fmt.Fprintln(w, "\nFlags:")
	fmt.Fprint(w, fs.FlagUsages())
	fmt.Fprintln(w, "\nExamples:")
	fmt.Fprintln(w, "  kdiag inspect deploy -n my-ns my-deploy")
	fmt.Fprintln(w, "  kdiag inspect deploy my-deploy --yaml | yq '.pods | length'")
	fmt.Fprintln(w, "  kdiag inspect deploy my-deploy --resources --yaml")
	fmt.Fprintln(w, "  kdiag inspect deploy my-deploy --spec")
	fmt.Fprintln(w, "  kdiag inspect deploy my-deploy --spec --yaml | yq '.containers[].name'")
}
