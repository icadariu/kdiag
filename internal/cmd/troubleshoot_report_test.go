package cmd

import (
	"bytes"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func scheduledPod(name, node string, statuses ...corev1.ContainerStatus) corev1.Pod {
	specs := make([]corev1.Container, 0, len(statuses))
	for _, s := range statuses {
		specs = append(specs, corev1.Container{Name: s.Name})
	}
	phase := corev1.PodRunning
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "demo"},
		Spec:       corev1.PodSpec{NodeName: node, Containers: specs},
		Status:     corev1.PodStatus{Phase: phase, ContainerStatuses: statuses},
	}
}

func runningReady(name string) corev1.ContainerStatus {
	return corev1.ContainerStatus{Name: name, Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}
}

func waitingCrashLoop(name string) corev1.ContainerStatus {
	return corev1.ContainerStatus{Name: name, State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff", Message: "back-off restarting"}}}
}

func TestBuildPodTroubleshoot_Unscheduled(t *testing.T) {
	p := pendingPod("pending", "100m")
	sched := scheduleReport{Pod: "pending", ScheduledNode: ""}
	rep := buildPodTroubleshoot(p, &sched, nil)
	if rep.Verdict != "Unschedulable" {
		t.Errorf("verdict = %q, want Unschedulable", rep.Verdict)
	}
	if rep.Scheduling == nil {
		t.Errorf("unscheduled pod should carry a scheduling report")
	}
	if len(rep.Issues) != 0 {
		t.Errorf("unscheduled pod should not have runtime issues, got %v", rep.Issues)
	}
}

func TestBuildPodTroubleshoot_HealthyScheduled(t *testing.T) {
	p := scheduledPod("ok", "node-a", runningReady("app"))
	rep := buildPodTroubleshoot(p, nil, nil)
	if rep.Verdict != "Healthy" {
		t.Errorf("verdict = %q, want Healthy", rep.Verdict)
	}
	if rep.Node != "node-a" {
		t.Errorf("node = %q, want node-a", rep.Node)
	}
	if len(rep.Issues) != 0 {
		t.Errorf("healthy pod should have no issues, got %v", rep.Issues)
	}
}

func TestBuildPodTroubleshoot_UnhealthyScheduled(t *testing.T) {
	p := scheduledPod("bad", "node-a", waitingCrashLoop("app"))
	rep := buildPodTroubleshoot(p, nil, []string{"BackOff: restarting (2m)"})
	if rep.Verdict != "Unhealthy" {
		t.Errorf("verdict = %q, want Unhealthy", rep.Verdict)
	}
	if len(rep.Issues) == 0 {
		t.Errorf("crashlooping pod should report issues")
	}
	if len(rep.Warnings) == 0 {
		t.Errorf("warnings should be carried through")
	}
}

func TestClassifyWorkloadPods(t *testing.T) {
	pods := []corev1.Pod{
		scheduledPod("good-1", "node-a", runningReady("app")),
		scheduledPod("bad-1", "node-a", waitingCrashLoop("app")),
		pendingPod("pending-1", "100m"), // unscheduled → unhealthy
	}
	healthy, unhealthy := classifyWorkloadPods(pods)
	if healthy != 1 {
		t.Errorf("healthy = %d, want 1", healthy)
	}
	if len(unhealthy) != 2 {
		t.Errorf("unhealthy = %d pods, want 2", len(unhealthy))
	}
}

func TestBuildNodeTroubleshoot(t *testing.T) {
	healthy := node("node-a", "4", "8Gi")
	if got := buildNodeTroubleshoot(healthy); got.Verdict != "Healthy" || len(got.Issues) != 0 {
		t.Errorf("healthy node: verdict=%q issues=%v", got.Verdict, got.Issues)
	}

	notReady := node("node-b", "4", "8Gi")
	notReady.Status.Conditions = []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionFalse, Reason: "KubeletDown"}}
	got := buildNodeTroubleshoot(notReady)
	if got.Verdict == "Healthy" {
		t.Errorf("not-ready node should not be Healthy")
	}
	if len(got.Issues) == 0 {
		t.Errorf("not-ready node should list issues")
	}
}

func TestPrintPodTroubleshoot_Runtime(t *testing.T) {
	p := scheduledPod("bad", "node-a", waitingCrashLoop("app"))
	rep := buildPodTroubleshoot(p, nil, nil)
	var buf bytes.Buffer
	printPodTroubleshoot(&buf, rep)
	out := buf.String()
	if !strings.Contains(out, "CrashLoopBackOff") {
		t.Errorf("output missing the issue:\n%s", out)
	}
	if !strings.Contains(out, "Unhealthy") {
		t.Errorf("output missing verdict:\n%s", out)
	}
}

func TestPrintPodTroubleshoot_Healthy(t *testing.T) {
	p := scheduledPod("ok", "node-a", runningReady("app"))
	rep := buildPodTroubleshoot(p, nil, nil)
	var buf bytes.Buffer
	printPodTroubleshoot(&buf, rep)
	if !strings.Contains(strings.ToLower(buf.String()), "no problems") {
		t.Errorf("healthy pod should say no problems:\n%s", buf.String())
	}
}
