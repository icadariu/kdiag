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

// azPlacement is one row of the AZ view: which node a pod landed on
// and which availability zone that node belongs to.
type azPlacement struct {
	Pod  string `json:"pod"`
	Node string `json:"node"`
	Zone string `json:"zone"`
}

// azData is the structured AZ view, ready for either text rendering or
// YAML emission. ZoneSummary's keys are zone names; values are pod counts.
type azData struct {
	Placements  []azPlacement  `json:"placements"`
	ZoneSummary map[string]int `json:"zoneSummary"`
}

// collectAZ builds an azData from a pre-fetched pod slice. Pulled out so
// both the text table and the YAML emitter share the same node-zone lookup.
func collectAZ(env *kube.KubeEnv, ctx context.Context, pods []corev1.Pod) azData {
	nodes, err := env.Clientset.CoreV1().Nodes().List(ctx, kube.ListOptions(""))
	if err != nil {
		cli.Fatal(fmt.Errorf("list nodes: %w", err))
	}
	nodeToZone := map[string]string{}
	for _, n := range nodes.Items {
		nodeToZone[n.Name] = kube.ZoneForNodeLabels(n.Labels)
	}

	out := azData{ZoneSummary: map[string]int{}}
	for _, p := range pods {
		node := p.Spec.NodeName
		zone := "-"
		if node == "" {
			node = "<unscheduled>"
		} else if z, ok := nodeToZone[node]; ok && z != "" {
			zone = z
		}
		out.Placements = append(out.Placements, azPlacement{Pod: p.Name, Node: node, Zone: zone})
		out.ZoneSummary[zone]++
	}
	return out
}

// printAZTable writes the POD/NODE/ZONE table and per-zone summary for
// a pre-fetched slice of pods. Used by the --az flag on inspect subcommands.
func printAZTable(env *kube.KubeEnv, ctx context.Context, pods []corev1.Pod) {
	data := collectAZ(env, ctx, pods)

	tw := cli.NewTabWriter(os.Stdout)
	fmt.Fprintln(tw, "POD\tNODE\tZONE")
	for _, r := range data.Placements {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", r.Pod, r.Node, r.Zone)
	}
	_ = tw.Flush()

	fmt.Println()
	fmt.Println("Summary (pods per ZONE):")
	zones := make([]string, 0, len(data.ZoneSummary))
	for z := range data.ZoneSummary {
		zones = append(zones, z)
	}
	sort.Strings(zones)
	for _, z := range zones {
		fmt.Printf("  %d\t%s\n", data.ZoneSummary[z], z)
	}
}

