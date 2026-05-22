package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/spf13/pflag"

	"example.com/kdiag/internal/cli"
	"example.com/kdiag/internal/kube"
)

// runInspectNode handles `kdiag inspect node`. Nodes are cluster-scoped, so
// `-n/--namespace` is accepted (uniform CLI shape) but ignored — matches
// `kubectl describe`.
func runInspectNode(args []string) {
	fs := pflag.NewFlagSet("inspect node", pflag.ExitOnError)
	var k kube.KubeFlags
	fs.StringVarP(&k.Namespace, "namespace", "n", "", "namespace (ignored — node is cluster-scoped)")
	var selector string
	fs.StringVarP(&selector, "label", "l", "", "label selector")
	fs.Usage = func() { printInspectNodeHelp(os.Stderr, fs) }

	if cli.WantsHelp(args) {
		printInspectNodeHelp(os.Stdout, fs)
		return
	}
	_ = fs.Parse(args)
	rest := fs.Args()
	selector = strings.TrimSpace(selector)

	if len(rest) > 0 && selector != "" {
		fmt.Fprintln(os.Stderr, "Error: provide either <node-name> OR --label/-l (not both)")
		fs.Usage()
		os.Exit(1)
	}
	if len(rest) > 1 {
		fmt.Fprintln(os.Stderr, "Error: inspect node accepts only one <name>")
		os.Exit(1)
	}

	env, err := kube.NewKubeEnv(k)
	if err != nil {
		cli.Fatal(err)
	}
	ctx := context.Background()

	var nodes []corev1.Node
	if len(rest) == 1 {
		n, err := env.Clientset.CoreV1().Nodes().Get(ctx, rest[0], metav1.GetOptions{})
		if err != nil {
			cli.Fatal(fmt.Errorf("get node: %w", err))
		}
		nodes = []corev1.Node{*n}
	} else {
		list, err := env.Clientset.CoreV1().Nodes().List(ctx, kube.ListOptions(selector))
		if err != nil {
			cli.Fatal(fmt.Errorf("list nodes: %w", err))
		}
		nodes = list.Items
	}

	if len(nodes) == 0 {
		fmt.Println("No nodes found.")
		return
	}
	for i := range nodes {
		n := nodes[i]
		fmt.Println("==========================================")
		printNodeBlock(n)
	}
}

// printNodeBlock renders the per-node summary: zone, instance type, kubelet,
// taints, conditions, and allocatable.
func printNodeBlock(n corev1.Node) {
	fmt.Printf("Node: %s\n", n.Name)
	fmt.Printf("  Zone:            %s\n", kube.ZoneForNodeLabels(n.Labels))
	fmt.Printf("  Instance Type:   %s\n", kube.InstanceTypeForNodeLabels(n.Labels))
	fmt.Printf("  Kubelet Version: %s\n", n.Status.NodeInfo.KubeletVersion)
	fmt.Printf("  Taints:          %s\n", kube.FormatTaints(n.Spec.Taints))
	fmt.Println("  Conditions:")
	cli.PrintKVBlock(os.Stdout, "    ", kube.NodeConditionsSummary(n.Status.Conditions))
	fmt.Println("  Allocatable:")
	cli.PrintKVBlock(os.Stdout, "    ", kube.AllocatableMap(n.Status.Allocatable))
}

func printInspectNodeHelp(w io.Writer, fs *pflag.FlagSet) {
	fmt.Fprintln(w, "Usage: kdiag inspect node [<node-name> | -l <label>]")
	fmt.Fprintln(w, "\nShow zone for one node or a set of nodes.")
	fmt.Fprintln(w, "\nFlags:")
	fmt.Fprint(w, fs.FlagUsages())
	fmt.Fprintln(w, "\nExamples:")
	fmt.Fprintln(w, "  kdiag inspect node my-node")
	fmt.Fprintln(w, "  kdiag inspect node -l topology.kubernetes.io/zone=eu-west-1a")
	fmt.Fprintln(w, "  kdiag inspect node")
}
