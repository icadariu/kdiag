package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

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
	var showPods bool
	wantYAML := func() bool { return false }
	if cli.ViewFlagSeen(args) != "path" {
		wantYAML = registerYAMLFlag(fs)
		fs.BoolVar(&showPods, "pods", false, "list non-terminated pods scheduled on the node (text or structured)")
	}
	fs.Usage = func() { printInspectNodeHelp(os.Stderr, fs, args) }

	// Check for help in args (it may not be the first element if other flags
	// like --path appear before -h).
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			printInspectNodeHelp(os.Stdout, fs, args)
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
	structured := wantYAML()
	selector = strings.TrimSpace(selector)

	if len(rest) > 0 && selector != "" {
		fmt.Fprintln(os.Stderr, "Error: provide either <node-name> OR --label (not both)")
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
		if structured {
			fmt.Fprintln(os.Stderr, "No nodes found.")
			os.Exit(1)
		}
		fmt.Println("No nodes found.")
		return
	}
	if showPods {
		podList, err := env.Clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
		if err != nil {
			cli.Fatal(fmt.Errorf("list pods: %w", err))
		}
		pods := podList.Items
		if structured {
			if len(nodes) == 1 && len(rest) == 1 {
				emit(nodePodsViewFrom(nodes[0], pods))
				return
			}
			views := make([]nodePodsView, 0, len(nodes))
			for _, n := range nodes {
				views = append(views, nodePodsViewFrom(n, pods))
			}
			emit(views)
			return
		}
		for i := range nodes {
			fmt.Println("==========================================")
			printNodePodsBlock(nodes[i], pods)
		}
		return
	}
	if structured {
		if len(nodes) == 1 && len(rest) == 1 {
			emit(nodeInfoFrom(nodes[0]))
			return
		}
		infos := make([]nodeInfo, 0, len(nodes))
		for _, n := range nodes {
			infos = append(infos, nodeInfoFrom(n))
		}
		emit(infos)
		return
	}
	for i := range nodes {
		n := nodes[i]
		fmt.Println("==========================================")
		printNodeBlock(n)
	}
}

// nodeInfo is the kdiag-shaped structured payload for `inspect node --yaml`.
// Mirrors what printNodeBlock prints in text mode — NOT the raw corev1.Node
// (use kubectl for that).
type nodeInfo struct {
	Name           string            `json:"name"`
	Zone           string            `json:"zone,omitempty"`
	InstanceType   string            `json:"instanceType,omitempty"`
	KubeletVersion string            `json:"kubeletVersion,omitempty"`
	Age            string            `json:"age,omitempty"`
	PodCIDR        string            `json:"podCIDR,omitempty"`
	Unschedulable  bool              `json:"unschedulable"`
	Taints         []string          `json:"taints,omitempty"`
	Conditions     map[string]string `json:"conditions,omitempty"`
	Allocatable    map[string]string `json:"allocatable,omitempty"`
	Capacity       map[string]string `json:"capacity,omitempty"`
}

func nodeInfoFrom(n corev1.Node) nodeInfo {
	taints := make([]string, 0, len(n.Spec.Taints))
	for _, t := range n.Spec.Taints {
		taints = append(taints, fmt.Sprintf("%s=%s:%s", t.Key, t.Value, t.Effect))
	}
	return nodeInfo{
		Name:           n.Name,
		Zone:           kube.ZoneForNodeLabels(n.Labels),
		InstanceType:   kube.InstanceTypeForNodeLabels(n.Labels),
		KubeletVersion: n.Status.NodeInfo.KubeletVersion,
		Age:            kube.FormatAge(n.CreationTimestamp.Time, time.Now()),
		PodCIDR:        dashIfEmpty(n.Spec.PodCIDR),
		Unschedulable:  n.Spec.Unschedulable,
		Taints:         taints,
		Conditions:     kube.NodeConditionsSummary(n.Status.Conditions),
		Allocatable:    kube.AllocatableMap(n.Status.Allocatable),
		Capacity:       kube.AllocatableMap(n.Status.Capacity),
	}
}

// printNodeBlock renders the per-node summary: zone, instance type, kubelet,
// age, pod CIDR, schedulability, taints, conditions, allocatable, and
// capacity.
func printNodeBlock(n corev1.Node) {
	fmt.Printf("Node: %s\n", n.Name)
	fmt.Printf("  Zone:            %s\n", kube.ZoneForNodeLabels(n.Labels))
	fmt.Printf("  Instance Type:   %s\n", kube.InstanceTypeForNodeLabels(n.Labels))
	fmt.Printf("  Kubelet Version: %s\n", n.Status.NodeInfo.KubeletVersion)
	fmt.Printf("  Age:             %s\n", kube.FormatAge(n.CreationTimestamp.Time, time.Now()))
	fmt.Printf("  Pod CIDR:        %s\n", dashIfEmpty(n.Spec.PodCIDR))
	fmt.Printf("  Unschedulable:   %t\n", n.Spec.Unschedulable)
	fmt.Printf("  Taints:          %s\n", kube.FormatTaints(n.Spec.Taints))
	fmt.Println("  Conditions:")
	cli.PrintKVBlock(os.Stdout, "    ", kube.NodeConditionsSummary(n.Status.Conditions))
	fmt.Println("  Allocatable:")
	cli.PrintKVBlock(os.Stdout, "    ", kube.AllocatableMap(n.Status.Allocatable))
	fmt.Println("  Capacity:")
	cli.PrintKVBlock(os.Stdout, "    ", kube.AllocatableMap(n.Status.Capacity))
}

// nodePodsView is the structured payload for `inspect node --pods --yaml`.
// Per-pod rows carry plain quantity strings (e.g. "100m"); percentages are not
// duplicated per row — the node's allocatable is included so consumers can
// recompute, and the headline percentages live in Allocated. The text view
// renders per-pod percentages for human readability.
type nodePodsView struct {
	Node        string             `json:"node"`
	Allocatable map[string]string  `json:"allocatable,omitempty"`
	Total       int                `json:"total"`
	Pods        []nodePodRow       `json:"pods"`
	Allocated   allocatedResources `json:"allocated"`
}

type nodePodRow struct {
	Namespace      string `json:"namespace"`
	Name           string `json:"name"`
	CPURequests    string `json:"cpuRequests"`
	CPULimits      string `json:"cpuLimits"`
	MemoryRequests string `json:"memoryRequests"`
	MemoryLimits   string `json:"memoryLimits"`
	Age            string `json:"age,omitempty"`
}

type resourceUsage struct {
	Requests        string `json:"requests"`
	RequestsPercent int    `json:"requestsPercent"`
	Limits          string `json:"limits"`
	LimitsPercent   int    `json:"limitsPercent"`
}

type allocatedResources struct {
	CPU    resourceUsage `json:"cpu"`
	Memory resourceUsage `json:"memory"`
}

// podsOnNode returns the non-terminated pods scheduled on node, sorted by
// namespace then name for deterministic output.
func podsOnNode(all []corev1.Pod, node string) []corev1.Pod {
	var out []corev1.Pod
	for _, p := range all {
		if p.Spec.NodeName == node && !kube.IsPodTerminated(p) {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// printNodePodsBlock renders the kubectl-style "Non-terminated Pods" table for
// a node plus an "Allocated resources" totals summary. Percentages are computed
// against the node's allocatable CPU/memory.
func printNodePodsBlock(n corev1.Node, all []corev1.Pod) {
	pods := podsOnNode(all, n.Name)
	cpuAlloc := n.Status.Allocatable[corev1.ResourceCPU]
	memAlloc := n.Status.Allocatable[corev1.ResourceMemory]

	fmt.Printf("Node: %s\n", n.Name)
	fmt.Printf("Non-terminated Pods: (%d in total)\n", len(pods))

	tw := cli.NewTabWriter(os.Stdout)
	fmt.Fprintln(tw, "  NAMESPACE\tNAME\tCPU REQUESTS\tCPU LIMITS\tMEMORY REQUESTS\tMEMORY LIMITS\tAGE")
	var tot kube.PodResources
	for _, p := range pods {
		r := kube.SumPodResources(p)
		tot.CPUReq.Add(r.CPUReq)
		tot.CPULim.Add(r.CPULim)
		tot.MemReq.Add(r.MemReq)
		tot.MemLim.Add(r.MemLim)
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			p.Namespace, p.Name,
			kube.FormatQtyPct(r.CPUReq, cpuAlloc),
			kube.FormatQtyPct(r.CPULim, cpuAlloc),
			kube.FormatQtyPct(r.MemReq, memAlloc),
			kube.FormatQtyPct(r.MemLim, memAlloc),
			kube.FormatAge(p.CreationTimestamp.Time, time.Now()))
	}
	_ = tw.Flush()

	fmt.Println("  Allocated resources:")
	fmt.Printf("    CPU Requests:    %s\n", kube.FormatQtyPct(tot.CPUReq, cpuAlloc))
	fmt.Printf("    CPU Limits:      %s\n", kube.FormatQtyPct(tot.CPULim, cpuAlloc))
	fmt.Printf("    Memory Requests: %s\n", kube.FormatQtyPct(tot.MemReq, memAlloc))
	fmt.Printf("    Memory Limits:   %s\n", kube.FormatQtyPct(tot.MemLim, memAlloc))
}

func nodePodsViewFrom(n corev1.Node, all []corev1.Pod) nodePodsView {
	pods := podsOnNode(all, n.Name)
	cpuAlloc := n.Status.Allocatable[corev1.ResourceCPU]
	memAlloc := n.Status.Allocatable[corev1.ResourceMemory]

	rows := make([]nodePodRow, 0, len(pods))
	var tot kube.PodResources
	for _, p := range pods {
		r := kube.SumPodResources(p)
		tot.CPUReq.Add(r.CPUReq)
		tot.CPULim.Add(r.CPULim)
		tot.MemReq.Add(r.MemReq)
		tot.MemLim.Add(r.MemLim)
		rows = append(rows, nodePodRow{
			Namespace:      p.Namespace,
			Name:           p.Name,
			CPURequests:    r.CPUReq.String(),
			CPULimits:      r.CPULim.String(),
			MemoryRequests: r.MemReq.String(),
			MemoryLimits:   r.MemLim.String(),
			Age:            kube.FormatAge(p.CreationTimestamp.Time, time.Now()),
		})
	}
	return nodePodsView{
		Node:        n.Name,
		Allocatable: kube.AllocatableMap(n.Status.Allocatable),
		Total:       len(pods),
		Pods:        rows,
		Allocated: allocatedResources{
			CPU: resourceUsage{
				Requests:        tot.CPUReq.String(),
				RequestsPercent: kube.PercentOf(tot.CPUReq, cpuAlloc),
				Limits:          tot.CPULim.String(),
				LimitsPercent:   kube.PercentOf(tot.CPULim, cpuAlloc),
			},
			Memory: resourceUsage{
				Requests:        tot.MemReq.String(),
				RequestsPercent: kube.PercentOf(tot.MemReq, memAlloc),
				Limits:          tot.MemLim.String(),
				LimitsPercent:   kube.PercentOf(tot.MemLim, memAlloc),
			},
		},
	}
}

func printInspectNodeHelp(w io.Writer, fs *pflag.FlagSet, args []string) {
	seen := cli.ViewFlagSeen(args)
	fmt.Fprintln(w, "Usage: kdiag inspect node [<node-name> | --label <selector>]")
	fmt.Fprintln(w, "\nShow zone for one node or a set of nodes.")
	if seen == "path" {
		fmt.Fprintln(w, "\nView: --path is set. Pass --path <needle> with --namespace/--label only. See `kdiag help yml-path`.")
	} else {
		fmt.Fprintln(w, "\nFormat: default is text; --yaml/--yml emits a structured YAML document.")
		fmt.Fprintln(w, "View: --pods lists the non-terminated pods scheduled on the node (all namespaces),")
		fmt.Fprintln(w, "      with CPU/memory requests & limits as a % of node allocatable, plus a totals summary.")
	}
	fmt.Fprintln(w, "\nFlags:")
	fmt.Fprint(w, cli.FormatFlagsLongOnly(fs))
	fmt.Fprintln(w, "\nExamples:")
	switch seen {
	case "path":
		fmt.Fprintln(w, "  kdiag inspect node my-node --path zone")
		fmt.Fprintln(w, "  kdiag inspect node --label topology.kubernetes.io/zone=eu-west-1a --path taints")
	default:
		fmt.Fprintln(w, "  kdiag inspect node my-node")
		fmt.Fprintln(w, "  kdiag inspect node my-node --yaml")
		fmt.Fprintln(w, "  kdiag inspect node my-node --pods")
		fmt.Fprintln(w, "  kdiag inspect node")
	}
}
