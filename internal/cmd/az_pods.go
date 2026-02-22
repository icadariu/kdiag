package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/pflag"

	"example.com/kdiag/internal/cli"
	"example.com/kdiag/internal/kube"
)

func RunAZ(args []string) {
	if len(args) < 1 || args[0] != "pods" {
		fmt.Fprintln(os.Stderr, "Error: az requires subcommand: pods")
		cli.PrintUsage(os.Stderr)
		os.Exit(1)
	}

	fs := pflag.NewFlagSet("az pods", pflag.ExitOnError)
	var k kube.KubeFlags
	fs.StringVar(&k.Kubeconfig, "kubeconfig", "", "path to kubeconfig")
	fs.StringVar(&k.Context, "context", "", "kube context")
	fs.StringVarP(&k.Namespace, "namespace", "n", "", "namespace")
	var selector string
	fs.StringVarP(&selector, "selector", "l", "", "label selector (required)")

	_ = fs.Parse(args[1:])
	selector = strings.TrimSpace(selector)
	if selector == "" {
		fmt.Fprintln(os.Stderr, "Error: az pods requires --selector/-l")
		os.Exit(1)
	}

	env, err := kube.NewKubeEnv(k)
	if err != nil {
		cli.Fatal(err)
	}

	ctx := context.Background()
	pods, err := env.Clientset.CoreV1().Pods(env.Namespace).List(ctx, kube.ListOptions(selector))
	if err != nil {
		cli.Fatal(fmt.Errorf("list pods: %w", err))
	}
	if len(pods.Items) == 0 {
		fmt.Println("No pods found.")
		return
	}

	// Nodes are cluster-scoped (not namespaced), so we list all of them.
	nodes, err := env.Clientset.CoreV1().Nodes().List(ctx, kube.ListOptions(""))
	if err != nil {
		cli.Fatal(fmt.Errorf("list nodes: %w", err))
	}

	nodeToZone := map[string]string{}
	for _, n := range nodes.Items {
		nodeToZone[n.Name] = kube.ZoneForNodeLabels(n.Labels)
	}

	fmt.Printf("Namespace: %s\n", env.Namespace)
	fmt.Printf("Pods placement (selector: %s)\n", selector)
	fmt.Println("------------------------------------------")

	tw := cli.NewTabWriter(os.Stdout)
	fmt.Fprintln(tw, "POD\tNODE\tZONE")
	counts := map[string]int{}

	for _, p := range pods.Items {
		node := p.Spec.NodeName
		zone := "-"
		if node == "" {
			node = "<unscheduled>"
		} else {
			if z, ok := nodeToZone[node]; ok && z != "" {
				zone = z
			}
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", p.Name, node, zone)
		counts[zone]++
	}
	_ = tw.Flush()

	fmt.Println()
	fmt.Println("Summary (pods per ZONE):")
	zones := make([]string, 0, len(counts))
	for z := range counts {
		zones = append(zones, z)
	}
	sort.Strings(zones)
	for _, z := range zones {
		fmt.Printf("  %d\t%s\n", counts[z], z)
	}
}
