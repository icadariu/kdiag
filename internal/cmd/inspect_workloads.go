package cmd

import (
	"context"
	"fmt"
	"io"
	"os"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/spf13/pflag"

	"example.com/kdiag/internal/cli"
	"example.com/kdiag/internal/kube"
)

func runInspectDaemonSet(args []string)   { runWorkload("ds", "DaemonSet", args) }
func runInspectStatefulSet(args []string) { runWorkload("sts", "StatefulSet", args) }
func runInspectReplicaSet(args []string)  { runWorkload("rs", "ReplicaSet", args) }

// runWorkload handles `inspect <short>` for any pod-bearing workload kind.
// All four (deploy/ds/sts/rs) share the same shape: get the workload, build
// a kind-specific summary map, then list pods via Spec.Selector and print
// container blocks per pod.
//
// short is the user-facing CLI verb ("deploy", "ds", "sts", "rs").
// label is the human-readable kind name printed in output ("Deployment", …).
func runWorkload(short, label string, args []string) {
	fs := pflag.NewFlagSet("inspect "+short, pflag.ExitOnError)
	k, showResources := commonFlags(fs)
	var (
		showAZ bool
		format string
	)
	if cli.ViewFlagSeen(args) != "path" {
		fs.BoolVar(&showAZ, "az", false, "show availability-zone placement")
		fs.StringVar(&format, "format", "text", "output format: text|yaml")
	} else {
		// --resources was registered in commonFlags(); the dispatcher's
		// extractPathArgs rejects --resources with --path explicitly,
		// but hide it from -h so users aren't tempted.
		_ = fs.MarkHidden("resources")
	}
	fs.Usage = func() { printWorkloadHelp(os.Stderr, fs, short, label, args) }

	// Check for help in args (it may not be the first element if other flags
	// like --path appear before -h).
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			printWorkloadHelp(os.Stdout, fs, short, label, args)
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
	switch format {
	case "", "text", "yaml":
	default:
		cli.Fatal(fmt.Errorf("--format must be 'text' or 'yaml', got %q", format))
	}
	showYAML := format == "yaml"
	if len(rest) != 1 {
		fmt.Fprintf(os.Stderr, "Error: inspect %s requires exactly one <name>\n", short)
		fs.Usage()
		os.Exit(1)
	}
	name := rest[0]

	env, err := kube.NewKubeEnv(*k)
	if err != nil {
		cli.Fatal(err)
	}
	ctx := context.Background()

	summary, selector, err := workloadSummary(ctx, env, short, name)
	if err != nil {
		cli.Fatal(err)
	}

	// --resources/--az are view selectors (mutex); --format composes with any view.
	if *showResources && showAZ {
		fmt.Fprintln(os.Stderr, "Error: --resources and --az are mutually exclusive (both select a view)")
		os.Exit(1)
	}
	if showYAML {
		labelSel := metav1.FormatLabelSelector(selector)
		pods, err := env.Clientset.CoreV1().Pods(env.Namespace).List(ctx, kube.ListOptions(labelSel))
		if err != nil {
			cli.Fatal(fmt.Errorf("list pods: %w", err))
		}
		if showAZ {
			emitAZYAML(env, ctx, pods.Items)
			return
		}
		out := workloadInfo{
			Name:      name,
			Kind:      label,
			Namespace: env.Namespace,
			Replicas:  summary["Replicas"],
			Selector:  summary["Selector"],
			Pods:      make([]podInfo, 0, len(pods.Items)),
		}
		if s, ok := summary["Strategy"]; ok {
			out.Strategy = s
		} else if s, ok := summary["Update Strategy"]; ok {
			out.Strategy = s
		}
		if *showResources {
			all := make([]containerResourceSlice, 0)
			for _, p := range pods.Items {
				all = append(all, resourceSliceFor(p)...)
			}
			emitYAML(all)
			return
		}
		for _, p := range pods.Items {
			out.Pods = append(out.Pods, podInfoFrom(p))
		}
		emitYAML(out)
		return
	}

	if showAZ {
		labelSel := metav1.FormatLabelSelector(selector)
		pods, err := env.Clientset.CoreV1().Pods(env.Namespace).List(ctx, kube.ListOptions(labelSel))
		if err != nil {
			cli.Fatal(fmt.Errorf("list pods: %w", err))
		}
		fmt.Printf("%s: %s\n", label, name)
		if len(pods.Items) == 0 {
			fmt.Println("No pods found.")
			return
		}
		printAZTable(env, ctx, pods.Items)
		return
	}

	inspectWorkloadPods(env, label, name, summary, selector, *showResources)
}

// workloadSummary fetches the named workload and returns a kind-specific
// summary map (Replicas, Selector, Strategy, …) along with its Spec.Selector.
func workloadSummary(ctx context.Context, env *kube.KubeEnv, short, name string) (map[string]string, *metav1.LabelSelector, error) {
	switch short {
	case "ds":
		d, err := env.Clientset.AppsV1().DaemonSets(env.Namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, nil, fmt.Errorf("get daemonset: %w", err)
		}
		return daemonsetSummary(d), d.Spec.Selector, nil
	case "sts":
		s, err := env.Clientset.AppsV1().StatefulSets(env.Namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, nil, fmt.Errorf("get statefulset: %w", err)
		}
		return statefulsetSummary(s), s.Spec.Selector, nil
	case "rs":
		r, err := env.Clientset.AppsV1().ReplicaSets(env.Namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, nil, fmt.Errorf("get replicaset: %w", err)
		}
		return replicasetSummary(r), r.Spec.Selector, nil
	default:
		return nil, nil, fmt.Errorf("internal: unknown workload short %q", short)
	}
}

func deploySummary(d *appsv1.Deployment) map[string]string {
	desired := int32(0)
	if d.Spec.Replicas != nil {
		desired = *d.Spec.Replicas
	}
	return map[string]string{
		"Replicas": fmt.Sprintf("%d desired / %d ready / %d available / %d updated",
			desired, d.Status.ReadyReplicas, d.Status.AvailableReplicas, d.Status.UpdatedReplicas),
		"Strategy": string(d.Spec.Strategy.Type),
		"Selector": metav1.FormatLabelSelector(d.Spec.Selector),
	}
}

func daemonsetSummary(d *appsv1.DaemonSet) map[string]string {
	return map[string]string{
		"Replicas": fmt.Sprintf("%d desired / %d current / %d ready / %d updated / %d available",
			d.Status.DesiredNumberScheduled, d.Status.CurrentNumberScheduled,
			d.Status.NumberReady, d.Status.UpdatedNumberScheduled, d.Status.NumberAvailable),
		"Update Strategy": string(d.Spec.UpdateStrategy.Type),
		"Selector":        metav1.FormatLabelSelector(d.Spec.Selector),
	}
}

func statefulsetSummary(s *appsv1.StatefulSet) map[string]string {
	desired := int32(0)
	if s.Spec.Replicas != nil {
		desired = *s.Spec.Replicas
	}
	return map[string]string{
		"Replicas": fmt.Sprintf("%d desired / %d ready / %d available / %d current",
			desired, s.Status.ReadyReplicas, s.Status.AvailableReplicas, s.Status.CurrentReplicas),
		"Service Name":    s.Spec.ServiceName,
		"Update Strategy": string(s.Spec.UpdateStrategy.Type),
		"Selector":        metav1.FormatLabelSelector(s.Spec.Selector),
	}
}

func replicasetSummary(r *appsv1.ReplicaSet) map[string]string {
	desired := int32(0)
	if r.Spec.Replicas != nil {
		desired = *r.Spec.Replicas
	}
	out := map[string]string{
		"Replicas": fmt.Sprintf("%d desired / %d current / %d ready",
			desired, r.Status.Replicas, r.Status.ReadyReplicas),
		"Selector": metav1.FormatLabelSelector(r.Spec.Selector),
	}
	if owner := metav1.GetControllerOf(r); owner != nil {
		out["Owner"] = fmt.Sprintf("%s/%s", owner.Kind, owner.Name)
	}
	return out
}

func printWorkloadHelp(w io.Writer, fs *pflag.FlagSet, short, label string, args []string) {
	seen := cli.ViewFlagSeen(args)
	fmt.Fprintf(w, "Usage: kdiag inspect %s [flags] <name>\n", short)
	fmt.Fprintf(w, "\nShow summary and container state for all pods belonging to a %s.\n", label)
	fmt.Fprintln(w, "\nFormat: default is text; --format yaml emits a yq-safe YAML document.")
	switch seen {
	case "path":
		fmt.Fprintln(w, "\nView: --path is set. Pass --path <needle> with --namespace/--label only. See `kdiag help yml-path`.")
	case "":
		fmt.Fprintln(w, "\nViews: --resources, --az, --path are mutually exclusive; --format composes with --resources/--az.")
	}
	fmt.Fprintln(w, "\nFlags:")
	fmt.Fprint(w, cli.FormatFlagsLongOnly(fs))
	fmt.Fprintln(w, "\nExamples:")
	switch seen {
	case "path":
		fmt.Fprintf(w, "  kdiag inspect %s my-%s --path memory\n", short, short)
		fmt.Fprintf(w, "  kdiag inspect %s --label app=my-app --path '*image*'\n", short)
	case "resources":
		fmt.Fprintf(w, "  kdiag inspect %s --resources --namespace my-ns my-%s\n", short, short)
		fmt.Fprintf(w, "  kdiag inspect %s --resources --format yaml --namespace my-ns my-%s\n", short, short)
	case "az":
		fmt.Fprintf(w, "  kdiag inspect %s --az --namespace my-ns my-%s\n", short, short)
		fmt.Fprintf(w, "  kdiag inspect %s --az --format yaml --namespace my-ns my-%s\n", short, short)
	default:
		fmt.Fprintf(w, "  kdiag inspect %s --namespace my-ns my-%s\n", short, short)
		fmt.Fprintf(w, "  kdiag inspect %s --resources --format yaml --namespace my-ns my-%s\n", short, short)
		fmt.Fprintf(w, "  kdiag inspect %s my-%s --path memory\n", short, short)
	}
}
