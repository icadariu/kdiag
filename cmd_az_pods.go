// cmd_az_pods.go
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
)

func runAZ(args []string) {
	if len(args) < 1 || args[0] != "pods" {
		fmt.Fprintln(os.Stderr, "Error: az requires subcommand: pods")
		printUsage(os.Stderr)
		os.Exit(1)
	}

	fs := flag.NewFlagSet("az pods", flag.ExitOnError)
	var k kubeFlags
	fs.StringVar(&k.Kubeconfig, "kubeconfig", "", "path to kubeconfig")
	fs.StringVar(&k.Context, "context", "", "kube context")
	fs.StringVar(&k.Namespace, "namespace", "", "namespace")
	fs.StringVar(&k.Namespace, "n", "", "namespace (shorthand)")
	var selector string
	fs.StringVar(&selector, "selector", "", "label selector (required)")
	fs.StringVar(&selector, "l", "", "label selector (required, shorthand)")

	_ = fs.Parse(args[1:])
	selector = strings.TrimSpace(selector)
	if selector == "" {
		fmt.Fprintln(os.Stderr, "Error: az pods requires --selector/-l")
		os.Exit(1)
	}

	env, err := newKubeEnv(k)
	if err != nil {
		fatal(err)
	}

	ctx := context.Background()
	pods, err := env.Clientset.CoreV1().Pods(env.Namespace).List(ctx, listOptions(selector))
	if err != nil {
		fatal(fmt.Errorf("list pods: %w", err))
	}
	if len(pods.Items) == 0 {
		fmt.Println("No pods found.")
		return
	}

	nodes, err := env.Clientset.CoreV1().Nodes().List(ctx, listOptions(""))
	if err != nil {
		fatal(fmt.Errorf("list nodes: %w", err))
	}

	nodeToZone := map[string]string{}
	for _, n := range nodes.Items {
		nodeToZone[n.Name] = zoneForNodeLabels(n.Labels)
	}

	fmt.Printf("Namespace: %s\n", env.Namespace)
	fmt.Printf("Pods placement (selector: %s)\n", selector)
	fmt.Println("------------------------------------------")

	tw := newTabWriter(os.Stdout)
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
