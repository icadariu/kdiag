package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"

	corev1 "k8s.io/api/core/v1"

	"example.com/kdiag/internal/cli"
	"example.com/kdiag/internal/kube"
)

// printAZTable writes the POD/NODE/ZONE table and per-zone summary for
// a pre-fetched slice of pods. Used by the --az flag on inspect subcommands.
func printAZTable(env *kube.KubeEnv, ctx context.Context, pods []corev1.Pod) {
	nodes, err := env.Clientset.CoreV1().Nodes().List(ctx, kube.ListOptions(""))
	if err != nil {
		cli.Fatal(fmt.Errorf("list nodes: %w", err))
	}
	nodeToZone := map[string]string{}
	for _, n := range nodes.Items {
		nodeToZone[n.Name] = kube.ZoneForNodeLabels(n.Labels)
	}

	tw := cli.NewTabWriter(os.Stdout)
	fmt.Fprintln(tw, "POD\tNODE\tZONE")
	counts := map[string]int{}
	for _, p := range pods {
		node := p.Spec.NodeName
		zone := "-"
		if node == "" {
			node = "<unscheduled>"
		} else if z, ok := nodeToZone[node]; ok && z != "" {
			zone = z
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
