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
	fs.StringVarP(&selector, "label", "l", "", "label selector")
	fs.BoolVar(&showAZ, "az", false, "show availability-zone placement")
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
			if *showResources {
				emitPodYAMLFlat(matches[0], *showResources)
			} else if showAZ {
				printAZTable(env, ctx, matches)
			} else {
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
	if *showResources {
		if len(pods.Items) == 0 {
			fmt.Fprintln(os.Stderr, "No pods found.")
			os.Exit(1)
		}
		emitPodYAMLMap(pods.Items, *showResources)
		return
	}
	if len(pods.Items) == 0 {
		fmt.Println("No pods found.")
		return
	}
	if showAZ {
		printAZTable(env, ctx, pods.Items)
		return
	}
	for i := range pods.Items {
		p := pods.Items[i]
		fmt.Println("==========================================")
		inspectPodObject(p, *showResources)
	}
}

// emitPodYAMLFlat prints the single-pod YAML output: [{name, resources}, ...]
// when --resources is set.
func emitPodYAMLFlat(p corev1.Pod, res bool) {
	if res {
		emitYAML(containerResourceList(p.Spec.Containers))
	}
}

// emitPodYAMLMap prints a YAML map keyed by pod name. Used when --label
// matched a set of pods. The shape is chosen by input (--label vs positional)
// rather than match count, so pipelines stay predictable.
func emitPodYAMLMap(pods []corev1.Pod, res bool) {
	out := make(map[string]any, len(pods))
	for _, p := range pods {
		if res {
			out[p.Name] = containerResourceList(p.Spec.Containers)
		}
	}
	emitYAML(out)
}

func printInspectPodHelp(w io.Writer, fs *pflag.FlagSet) {
	fmt.Fprintln(w, "Usage: kdiag inspect pod [flags] [<partial-pod-name> | -l <label>]")
	fmt.Fprintln(w, "\nShow container state for one pod or a set of pods.")
	fmt.Fprintln(w, "\n--resources emits valid YAML on stdout — pipeable to yq.")
	fmt.Fprintln(w, "With --label, output is a YAML map keyed by pod name; with a positional name, output is flat.")
	fmt.Fprintln(w, "It is incompatible with --az.")
	fmt.Fprintln(w, "\nFlags:")
	fmt.Fprint(w, fs.FlagUsages())
	fmt.Fprintln(w, "\nExamples:")
	fmt.Fprintln(w, "  kdiag inspect pod my-pod")
	fmt.Fprintln(w, "  kdiag inspect pod -n kube-system -l app=my-app")
	fmt.Fprintln(w, "  kdiag inspect pod --resources -n my-ns my-pod              # YAML: [{name, resources}, ...]")
	fmt.Fprintln(w, "  kdiag inspect pod --resources -l app=my-app | yq 'keys'    # pod names")
	fmt.Fprintln(w, "  kdiag inspect pod --az -n my-ns -l app=my-app")
}
