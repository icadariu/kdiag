package kube

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func ready(name string) corev1.ContainerStatus {
	return corev1.ContainerStatus{
		Name:  name,
		Ready: true,
		State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
	}
}

func podWith(statuses ...corev1.ContainerStatus) *corev1.Pod {
	specs := make([]corev1.Container, 0, len(statuses))
	for _, s := range statuses {
		specs = append(specs, corev1.Container{Name: s.Name})
	}
	return &corev1.Pod{
		Spec:   corev1.PodSpec{Containers: specs},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: statuses},
	}
}

func TestPodRuntimeIssues_Healthy(t *testing.T) {
	if got := PodRuntimeIssues(podWith(ready("app"))); len(got) != 0 {
		t.Errorf("healthy pod should have no issues, got %v", got)
	}
}

func TestPodRuntimeIssues_Waiting(t *testing.T) {
	cases := []struct {
		name    string
		reason  string
		problem bool
	}{
		{"image pull backoff", "ImagePullBackOff", true},
		{"err image pull", "ErrImagePull", true},
		{"crash loop", "CrashLoopBackOff", true},
		{"config error", "CreateContainerConfigError", true},
		{"invalid image", "InvalidImageName", true},
		{"container creating is transient", "ContainerCreating", false},
		{"pod initializing is transient", "PodInitializing", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := corev1.ContainerStatus{
				Name: "app",
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
					Reason: c.reason, Message: "boom",
				}},
			}
			got := PodRuntimeIssues(podWith(st))
			if c.problem {
				if len(got) != 1 || got[0].Symptom != c.reason || got[0].Container != "app" {
					t.Errorf("expected one %q issue for app, got %v", c.reason, got)
				}
			} else if len(got) != 0 {
				t.Errorf("reason %q should be transient (no issue), got %v", c.reason, got)
			}
		})
	}
}

func TestPodRuntimeIssues_OOMKilled(t *testing.T) {
	st := corev1.ContainerStatus{
		Name: "app",
		State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
			Reason: "OOMKilled", ExitCode: 137,
		}},
	}
	got := PodRuntimeIssues(podWith(st))
	if len(got) != 1 || got[0].Symptom != "OOMKilled" {
		t.Fatalf("expected OOMKilled issue, got %v", got)
	}
	if !strings.Contains(got[0].Detail, "137") {
		t.Errorf("expected exit code in detail, got %q", got[0].Detail)
	}
}

func TestPodRuntimeIssues_NonZeroExit(t *testing.T) {
	st := corev1.ContainerStatus{
		Name: "app",
		State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
			Reason: "Error", ExitCode: 1,
		}},
	}
	got := PodRuntimeIssues(podWith(st))
	if len(got) != 1 {
		t.Fatalf("expected one issue, got %v", got)
	}
	if !strings.Contains(got[0].Detail, "1") {
		t.Errorf("expected exit code 1 in detail, got %q", got[0].Detail)
	}
}

func TestPodRuntimeIssues_RunningNotReady(t *testing.T) {
	st := corev1.ContainerStatus{
		Name:  "app",
		Ready: false,
		State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
	}
	got := PodRuntimeIssues(podWith(st))
	if len(got) != 1 || got[0].Container != "app" {
		t.Fatalf("expected a not-ready issue for app, got %v", got)
	}
	if !strings.Contains(strings.ToLower(got[0].Symptom+got[0].Detail), "ready") {
		t.Errorf("expected readiness wording, got %+v", got[0])
	}
}

func TestPodRuntimeIssues_PriorCrash(t *testing.T) {
	// Currently running & ready, but restarted after an OOM kill — surface the
	// prior termination so flapping pods aren't reported as fully healthy.
	st := corev1.ContainerStatus{
		Name:         "app",
		Ready:        true,
		RestartCount: 5,
		State:        corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
		LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
			Reason: "OOMKilled", ExitCode: 137,
		}},
	}
	got := PodRuntimeIssues(podWith(st))
	if len(got) != 1 {
		t.Fatalf("expected a prior-restart issue, got %v", got)
	}
	if !strings.Contains(got[0].Detail, "5") {
		t.Errorf("expected restart count in detail, got %q", got[0].Detail)
	}
}

func TestNodeIssues(t *testing.T) {
	readyNode := corev1.Node{Status: corev1.NodeStatus{
		Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
	}}
	if got := NodeIssues(&readyNode); len(got) != 0 {
		t.Errorf("ready node should have no issues, got %v", got)
	}

	notReady := corev1.Node{Status: corev1.NodeStatus{
		Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionFalse, Reason: "KubeletDown"}},
	}}
	if got := NodeIssues(&notReady); len(got) == 0 || !strings.Contains(strings.Join(got, " "), "NotReady") {
		t.Errorf("expected NotReady issue, got %v", got)
	}

	cordoned := corev1.Node{
		Spec:   corev1.NodeSpec{Unschedulable: true},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}},
	}
	if got := NodeIssues(&cordoned); len(got) == 0 || !strings.Contains(strings.ToLower(strings.Join(got, " ")), "cordon") {
		t.Errorf("expected cordoned issue, got %v", got)
	}

	pressure := corev1.Node{Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
		{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
		{Type: corev1.NodeMemoryPressure, Status: corev1.ConditionTrue},
	}}}
	if got := NodeIssues(&pressure); len(got) == 0 || !strings.Contains(strings.Join(got, " "), "MemoryPressure") {
		t.Errorf("expected MemoryPressure issue, got %v", got)
	}
}
