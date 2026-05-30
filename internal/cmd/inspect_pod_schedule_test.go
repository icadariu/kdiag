package cmd

import (
	"bytes"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func node(name string, cpu, mem string, opts ...func(*corev1.Node)) corev1.Node {
	n := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.NodeStatus{
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(cpu),
				corev1.ResourceMemory: resource.MustParse(mem),
			},
			Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
		},
	}
	for _, o := range opts {
		o(&n)
	}
	return n
}

func withLabels(l map[string]string) func(*corev1.Node) {
	return func(n *corev1.Node) { n.Labels = l }
}

func withTaint(key, value string, effect corev1.TaintEffect) func(*corev1.Node) {
	return func(n *corev1.Node) {
		n.Spec.Taints = append(n.Spec.Taints, corev1.Taint{Key: key, Value: value, Effect: effect})
	}
}

func pendingPod(name string, cpuReq string, opts ...func(*corev1.Pod)) corev1.Pod {
	p := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "demo"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "app",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse(cpuReq)},
				},
			}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodPending},
	}
	for _, o := range opts {
		o(&p)
	}
	return p
}

func TestBuildScheduleReport_AlreadyScheduled(t *testing.T) {
	p := pendingPod("running-pod", "100m", func(p *corev1.Pod) {
		p.Spec.NodeName = "node-a"
		p.Status.Phase = corev1.PodRunning
	})
	rep := buildScheduleReport(p, []corev1.Node{node("node-a", "4", "8Gi")}, nil, nil, time.Now())

	if rep.ScheduledNode != "node-a" {
		t.Errorf("ScheduledNode = %q, want node-a", rep.ScheduledNode)
	}
	if len(rep.Nodes) != 0 {
		t.Errorf("scheduled pod should have no per-node verdicts, got %d", len(rep.Nodes))
	}
}

func TestBuildScheduleReport_NodeVerdicts(t *testing.T) {
	p := pendingPod("pending", "2", func(p *corev1.Pod) {
		p.Spec.NodeSelector = map[string]string{"disktype": "ssd"}
	})
	nodes := []corev1.Node{
		// fails nodeSelector AND insufficient cpu (only 1 allocatable, needs 2)
		node("node-small", "1", "8Gi", withLabels(map[string]string{"disktype": "hdd"})),
		// satisfies everything
		node("node-ok", "4", "8Gi", withLabels(map[string]string{"disktype": "ssd"})),
	}
	rep := buildScheduleReport(p, nodes, nil, nil, time.Now())

	byName := map[string]nodeVerdict{}
	for _, v := range rep.Nodes {
		byName[v.Name] = v
	}

	small := byName["node-small"]
	if small.Fits {
		t.Errorf("node-small should not fit")
	}
	joined := strings.Join(small.Blockers, " | ")
	if !strings.Contains(joined, "disktype=ssd") {
		t.Errorf("node-small blockers missing nodeSelector: %v", small.Blockers)
	}
	if !strings.Contains(strings.ToLower(joined), "cpu") {
		t.Errorf("node-small blockers missing cpu shortfall: %v", small.Blockers)
	}

	ok := byName["node-ok"]
	if !ok.Fits {
		t.Errorf("node-ok should fit, blockers: %v", ok.Blockers)
	}
}

func TestBuildScheduleReport_TaintBlocks(t *testing.T) {
	p := pendingPod("pending", "100m")
	nodes := []corev1.Node{node("tainted", "4", "8Gi", withTaint("dedicated", "gpu", corev1.TaintEffectNoSchedule))}
	rep := buildScheduleReport(p, nodes, nil, nil, time.Now())

	if rep.Nodes[0].Fits {
		t.Fatalf("tainted node should not fit")
	}
	if !strings.Contains(strings.Join(rep.Nodes[0].Blockers, " "), "dedicated") {
		t.Errorf("expected taint blocker, got %v", rep.Nodes[0].Blockers)
	}
}

func TestBuildScheduleReport_DeferredPredicates(t *testing.T) {
	p := pendingPod("pending", "100m", func(p *corev1.Pod) {
		p.Spec.TopologySpreadConstraints = []corev1.TopologySpreadConstraint{{TopologyKey: "zone"}}
		p.Spec.Volumes = []corev1.Volume{{
			Name:         "data",
			VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "c"}},
		}}
	})
	rep := buildScheduleReport(p, []corev1.Node{node("n", "4", "8Gi")}, nil, nil, time.Now())

	got := strings.Join(rep.Deferred, " ")
	if !strings.Contains(got, "topology") {
		t.Errorf("deferred should mention topology spread: %v", rep.Deferred)
	}
	if !strings.Contains(strings.ToLower(got), "volume") {
		t.Errorf("deferred should mention volume binding: %v", rep.Deferred)
	}
}

func TestPrintScheduleBlock_DiscrepancyNote(t *testing.T) {
	// A node fits per kdiag's predicates, but the pod is still unscheduled and a
	// scheduler message exists → the printer must surface the discrepancy note.
	p := pendingPod("pending", "100m")
	ev := &corev1.Event{
		Reason:        "FailedScheduling",
		Message:       "0/1 nodes are available: 1 node(s) didn't match pod topology spread constraints.",
		LastTimestamp: metav1.NewTime(time.Now()),
	}
	rep := buildScheduleReport(p, []corev1.Node{node("n", "4", "8Gi")}, nil, ev, time.Now())

	var buf bytes.Buffer
	printScheduleBlock(&buf, rep)
	out := buf.String()

	if !strings.Contains(out, "FailedScheduling") {
		t.Errorf("output missing scheduler message:\n%s", out)
	}
	if !strings.Contains(strings.ToLower(out), "deferred predicate") {
		t.Errorf("output missing discrepancy note:\n%s", out)
	}
}
