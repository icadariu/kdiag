package cmd

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"example.com/kdiag/internal/cli"
	"example.com/kdiag/internal/kube"
)

// inspect_pod_schedule.go implements `inspect pod --schedule`: a hybrid
// scheduling explainer. It leads with the kube-scheduler's authoritative
// FailedScheduling event, then enriches with kdiag-computed per-node verdicts
// for the cheap predicates (resource fit, taints, nodeSelector, required
// nodeAffinity, cordoned, NotReady). Harder predicates (topology spread,
// inter-pod affinity, PV zone binding) are surfaced as deferred, not evaluated.

// scheduleReport is the YAML/structured shape for `--schedule --yaml` and the
// input to the text renderer.
type scheduleReport struct {
	Pod           string              `json:"pod"`
	Namespace     string              `json:"namespace"`
	Phase         string              `json:"phase"`
	ScheduledNode string              `json:"scheduledNode,omitempty"`
	Scheduler     *schedulerNote      `json:"scheduler,omitempty"`
	Requests      scheduleRequests    `json:"requests"`
	Constraints   scheduleConstraints `json:"constraints"`
	Nodes         []nodeVerdict       `json:"nodes,omitempty"`
	Deferred      []string            `json:"deferredPredicates,omitempty"`
}

type schedulerNote struct {
	Reason  string `json:"reason"`
	Age     string `json:"age"`
	Message string `json:"message"`
}

type scheduleRequests struct {
	CPU    string `json:"cpu"`
	Memory string `json:"memory"`
}

type scheduleConstraints struct {
	NodeSelector         map[string]string `json:"nodeSelector,omitempty"`
	RequiredNodeAffinity bool              `json:"requiredNodeAffinity"`
	Tolerations          []string          `json:"tolerations,omitempty"`
	TopologySpread       bool              `json:"topologySpread"`
	InterPodAffinity     bool              `json:"interPodAffinity"`
	PVCs                 bool              `json:"persistentVolumeClaims"`
}

type nodeVerdict struct {
	Name     string   `json:"name"`
	Fits     bool     `json:"fits"`
	CPUFree  string   `json:"cpuFree"`
	MemFree  string   `json:"memFree"`
	Blockers []string `json:"blockers,omitempty"`
}

// scheduleReportFor fetches the cluster state needed and builds the report.
// Thin I/O wrapper around the pure buildScheduleReport (which is unit-tested).
func scheduleReportFor(env *kube.KubeEnv, ctx context.Context, pod corev1.Pod) scheduleReport {
	nodeList, err := env.Clientset.CoreV1().Nodes().List(ctx, kube.ListOptions(""))
	if err != nil {
		cli.Fatal(fmt.Errorf("list nodes: %w", err))
	}
	podList, err := env.Clientset.CoreV1().Pods("").List(ctx, kube.ListOptions(""))
	if err != nil {
		cli.Fatal(fmt.Errorf("list pods: %w", err))
	}
	ev := latestFailedScheduling(env, ctx, pod)
	return buildScheduleReport(pod, nodeList.Items, podList.Items, ev, time.Now())
}

// latestFailedScheduling returns the most recent FailedScheduling event for the
// pod, or nil if none exists.
func latestFailedScheduling(env *kube.KubeEnv, ctx context.Context, pod corev1.Pod) *corev1.Event {
	evList, err := env.Clientset.CoreV1().Events(pod.Namespace).List(ctx, kube.ListOptions(""))
	if err != nil {
		// Events are enrichment, not essential — degrade gracefully.
		return nil
	}
	var best *corev1.Event
	var bestTS time.Time
	for i := range evList.Items {
		ev := evList.Items[i]
		if ev.Reason != "FailedScheduling" ||
			ev.InvolvedObject.Kind != "Pod" || ev.InvolvedObject.Name != pod.Name {
			continue
		}
		ts := kube.EffectiveEventTime(ev)
		if best == nil || ts.After(bestTS) {
			best = &evList.Items[i]
			bestTS = ts
		}
	}
	return best
}

// buildScheduleReport assembles the report from already-fetched state. Pure:
// no client calls, so it is unit-tested without a cluster.
func buildScheduleReport(pod corev1.Pod, nodes []corev1.Node, allPods []corev1.Pod, schedEvent *corev1.Event, now time.Time) scheduleReport {
	req := kube.SumPodResources(pod)
	rep := scheduleReport{
		Pod:           pod.Name,
		Namespace:     pod.Namespace,
		Phase:         string(pod.Status.Phase),
		ScheduledNode: pod.Spec.NodeName,
		Requests:      scheduleRequests{CPU: req.CPUReq.String(), Memory: req.MemReq.String()},
		Constraints:   constraintsFor(&pod),
		Deferred:      deferredFor(&pod),
	}
	if schedEvent != nil {
		rep.Scheduler = &schedulerNote{
			Reason:  schedEvent.Reason,
			Age:     kube.FormatAge(kube.EffectiveEventTime(*schedEvent), now),
			Message: schedEvent.Message,
		}
	}

	// Already scheduled → no per-node verdicts (the question is moot).
	if pod.Spec.NodeName != "" {
		return rep
	}

	sorted := append([]corev1.Node(nil), nodes...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	for i := range sorted {
		rep.Nodes = append(rep.Nodes, verdictForNode(sorted[i], pod, req, allPods))
	}
	return rep
}

func constraintsFor(pod *corev1.Pod) scheduleConstraints {
	tolerations := make([]string, 0, len(pod.Spec.Tolerations))
	for _, t := range pod.Spec.Tolerations {
		tolerations = append(tolerations, formatToleration(t))
	}
	return scheduleConstraints{
		NodeSelector:         pod.Spec.NodeSelector,
		RequiredNodeAffinity: hasRequiredNodeAffinity(pod),
		Tolerations:          tolerations,
		TopologySpread:       kube.HasTopologySpread(pod),
		InterPodAffinity:     kube.HasInterPodAffinity(pod),
		PVCs:                 kube.HasPersistentVolumeClaims(pod),
	}
}

func deferredFor(pod *corev1.Pod) []string {
	var out []string
	if kube.HasTopologySpread(pod) {
		out = append(out, "topology spread")
	}
	if kube.HasInterPodAffinity(pod) {
		out = append(out, "inter-pod affinity")
	}
	if kube.HasPersistentVolumeClaims(pod) {
		out = append(out, "volume binding (PVC)")
	}
	return out
}

// verdictForNode runs every kdiag-checked predicate against one node and
// records each blocker. A node fits when nothing blocks it.
func verdictForNode(n corev1.Node, pod corev1.Pod, req kube.PodResources, allPods []corev1.Pod) nodeVerdict {
	var blockers []string

	if n.Spec.Unschedulable {
		blockers = append(blockers, "node is cordoned (unschedulable)")
	}
	if !nodeReady(n) {
		blockers = append(blockers, "node is NotReady")
	}
	for _, t := range kube.UntoleratedTaints(pod.Spec.Tolerations, n.Spec.Taints) {
		blockers = append(blockers, "untolerated taint "+kube.FormatTaints([]corev1.Taint{t}))
	}
	for _, m := range kube.NodeSelectorMismatch(pod.Spec.NodeSelector, n.Labels) {
		blockers = append(blockers, fmt.Sprintf("nodeSelector %s not satisfied", m))
	}
	if !kube.RequiredNodeAffinityMatches(pod.Spec.Affinity, n.Labels) {
		blockers = append(blockers, "required nodeAffinity not satisfied")
	}

	cpuFree, memFree := nodeFree(n, allPods)
	if req.CPUReq.Cmp(cpuFree) > 0 {
		blockers = append(blockers, fmt.Sprintf("Insufficient cpu (needs %s, %s free)", req.CPUReq.String(), cpuFree.String()))
	}
	if req.MemReq.Cmp(memFree) > 0 {
		blockers = append(blockers, fmt.Sprintf("Insufficient memory (needs %s, %s free)", req.MemReq.String(), memFree.String()))
	}

	return nodeVerdict{
		Name:     n.Name,
		Fits:     len(blockers) == 0,
		CPUFree:  cpuFree.String(),
		MemFree:  memFree.String(),
		Blockers: blockers,
	}
}

// nodeFree returns the node's allocatable CPU/memory minus the summed requests
// of the non-terminated pods already on it. Mirrors `inspect node --pods`
// accounting (regular containers only — an approximation, see SumPodResources).
func nodeFree(n corev1.Node, allPods []corev1.Pod) (cpuFree, memFree resource.Quantity) {
	cpuFree = n.Status.Allocatable[corev1.ResourceCPU].DeepCopy()
	memFree = n.Status.Allocatable[corev1.ResourceMemory].DeepCopy()
	for _, p := range podsOnNode(allPods, n.Name) {
		r := kube.SumPodResources(p)
		cpuFree.Sub(r.CPUReq)
		memFree.Sub(r.MemReq)
	}
	return cpuFree, memFree
}

func nodeReady(n corev1.Node) bool {
	for _, c := range n.Status.Conditions {
		if c.Type == corev1.NodeReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

func hasRequiredNodeAffinity(pod *corev1.Pod) bool {
	aff := pod.Spec.Affinity
	if aff == nil || aff.NodeAffinity == nil ||
		aff.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		return false
	}
	return len(aff.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms) > 0
}

func formatToleration(t corev1.Toleration) string {
	switch t.Operator {
	case corev1.TolerationOpExists:
		if t.Key == "" {
			return fmt.Sprintf("Exists (all) :%s", t.Effect)
		}
		return fmt.Sprintf("%s Exists :%s", t.Key, t.Effect)
	default:
		return fmt.Sprintf("%s=%s :%s", t.Key, t.Value, t.Effect)
	}
}

// printScheduleBlock renders the text report.
func printScheduleBlock(w io.Writer, rep scheduleReport) {
	header := fmt.Sprintf("Pod: %s  (%s", rep.Pod, rep.Phase)
	if rep.ScheduledNode != "" {
		fmt.Fprintf(w, "%s, scheduled on %s)\n", header, rep.ScheduledNode)
		return
	}
	fmt.Fprintf(w, "%s, unscheduled)\n", header)

	fmt.Fprintln(w)
	if rep.Scheduler != nil {
		fmt.Fprintf(w, "Scheduler (%s, %s ago):\n", rep.Scheduler.Reason, rep.Scheduler.Age)
		fmt.Fprintf(w, "  %s\n", rep.Scheduler.Message)
	} else {
		fmt.Fprintln(w, "Scheduler: no FailedScheduling event found (pod may be newly created).")
	}

	fmt.Fprintf(w, "\nPod requests:  cpu=%s  memory=%s\n", rep.Requests.CPU, rep.Requests.Memory)
	fmt.Fprintln(w, "Constraints:")
	fmt.Fprintf(w, "  nodeSelector:           %s\n", kvOrNone(rep.Constraints.NodeSelector))
	fmt.Fprintf(w, "  required nodeAffinity:  %s\n", yesNo(rep.Constraints.RequiredNodeAffinity))
	fmt.Fprintf(w, "  tolerations:            %s\n", listOrNone(rep.Constraints.Tolerations))
	fmt.Fprintf(w, "  topology spread:        %s\n", deferredMark(rep.Constraints.TopologySpread))
	fmt.Fprintf(w, "  inter-pod affinity:     %s\n", deferredMark(rep.Constraints.InterPodAffinity))
	fmt.Fprintf(w, "  volumes (PVC):          %s\n", deferredMark(rep.Constraints.PVCs))

	fmt.Fprintln(w, "\nPer-node fit (kdiag-computed predicates only):")
	tw := cli.NewTabWriter(w)
	fmt.Fprintln(tw, "  NODE\tFITS\tCPU FREE\tMEM FREE\tBLOCKERS")
	anyFits := false
	for _, v := range rep.Nodes {
		if v.Fits {
			anyFits = true
		}
		blockers := "—"
		if len(v.Blockers) > 0 {
			blockers = joinBlockers(v.Blockers)
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n", v.Name, yesNoShort(v.Fits), v.CPUFree, v.MemFree, blockers)
	}
	_ = tw.Flush()

	// The hybrid payoff: a node passing every kdiag predicate while the pod is
	// still unscheduled means the real blocker is a predicate kdiag defers.
	if anyFits {
		fmt.Fprintln(w, "\nNote: at least one node passes all kdiag-checked predicates but the pod is")
		fmt.Fprintln(w, "still unscheduled. The blocker is a deferred predicate (topology spread /")
		fmt.Fprintln(w, "inter-pod affinity / volume binding) — see the scheduler message above.")
	}
}

func joinBlockers(b []string) string {
	return strings.Join(b, "; ")
}

func kvOrNone(m map[string]string) string {
	if len(m) == 0 {
		return "<none>"
	}
	pairs := make([]string, 0, len(m))
	for k, v := range m {
		pairs = append(pairs, k+"="+v)
	}
	sort.Strings(pairs)
	return strings.Join(pairs, ", ")
}

func listOrNone(l []string) string {
	if len(l) == 0 {
		return "<none>"
	}
	return strings.Join(l, ", ")
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "<none>"
}

func yesNoShort(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func deferredMark(present bool) string {
	if present {
		return "present  (deferred — see scheduler message)"
	}
	return "<none>"
}
