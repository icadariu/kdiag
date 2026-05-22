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

// runInspectDeploy implements `inspect deploy`. Unlike ds/sts/rs (which share
// runWorkload), deploy supports YAML-mode flags that emit subtrees of the
// deployment's pod template — designed for piping into yq.
func runInspectDeploy(args []string) {
	fs := pflag.NewFlagSet("inspect deploy", pflag.ExitOnError)
	k, showResources := commonFlags(fs)
	var (
		selector          string
		showAZ            bool
		showSpec          bool
		showContainerSpec bool
	)
	fs.StringVarP(&selector, "label", "l", "", "label selector to identify the deployment")
	fs.BoolVar(&showAZ, "az", false, "show availability-zone placement")
	fs.BoolVar(&showSpec, "spec", false, "print .spec.template.spec as YAML")
	fs.BoolVar(&showContainerSpec, "container-spec", false, "print .spec.template.spec.containers[] as YAML")
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

	yamlFlags := boolsSet(*showResources, showSpec, showContainerSpec)
	if yamlFlags > 1 {
		fmt.Fprintln(os.Stderr, "Error: --resources, --spec, --container-spec are mutually exclusive")
		os.Exit(1)
	}
	if yamlFlags == 1 && showAZ {
		fmt.Fprintln(os.Stderr, "Error: --az cannot be combined with --resources, --spec, or --container-spec")
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

	if yamlFlags == 1 {
		emitDeployTemplateYAML(d, *showResources, showSpec, showContainerSpec)
		return
	}

	summary := deploySummary(d)
	if showAZ {
		labelSel := metav1.FormatLabelSelector(d.Spec.Selector)
		pods, err := env.Clientset.CoreV1().Pods(env.Namespace).List(ctx, kube.ListOptions(labelSel))
		if err != nil {
			cli.Fatal(fmt.Errorf("list pods: %w", err))
		}
		fmt.Printf("Namespace: %s\n", env.Namespace)
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

// emitDeployTemplateYAML marshals the requested subtree of the deployment's
// pod template to stdout as YAML. Exactly one of the three flags is set when
// this is called; the caller enforces the mutual-exclusion invariant.
func emitDeployTemplateYAML(d *appsv1.Deployment, res, spec, contSpec bool) {
	switch {
	case spec:
		emitYAML(d.Spec.Template.Spec)
	case contSpec:
		emitYAML(d.Spec.Template.Spec.Containers)
	case res:
		emitYAML(containerResourceList(d.Spec.Template.Spec.Containers))
	}
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

func printInspectDeployHelp(w io.Writer, fs *pflag.FlagSet) {
	fmt.Fprintln(w, "Usage: kdiag inspect deploy [flags] [<name> | -l <label>]")
	fmt.Fprintln(w, "\nShow summary and container state for all pods belonging to a Deployment.")
	fmt.Fprintln(w, "Pass <name> or --label/-l (the selector must match exactly one Deployment).")
	fmt.Fprintln(w, "\nYAML-mode flags emit subtrees of the deployment's pod template — useful for piping to yq.")
	fmt.Fprintln(w, "They are mutually exclusive and incompatible with --az.")
	fmt.Fprintln(w, "\nFlags:")
	fmt.Fprint(w, fs.FlagUsages())
	fmt.Fprintln(w, "\nExamples:")
	fmt.Fprintln(w, "  kdiag inspect deploy -n my-ns my-deploy")
	fmt.Fprintln(w, "  kdiag inspect deploy -n my-ns -l app=my-app")
	fmt.Fprintln(w, "  kdiag inspect deploy --az -n my-ns my-deploy")
	fmt.Fprintln(w, "  kdiag inspect deploy --resources -n my-ns my-deploy        # YAML: [{name, resources}, ...]")
	fmt.Fprintln(w, "  kdiag inspect deploy --spec -n my-ns my-deploy             # YAML: .spec.template.spec")
	fmt.Fprintln(w, "  kdiag inspect deploy --container-spec -n my-ns my-deploy   # YAML: containers[]")
	fmt.Fprintln(w, "  kdiag inspect deploy --container-spec my-deploy | yq '.[].name'")
}
