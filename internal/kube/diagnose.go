package kube

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

// diagnose.go holds the pure runtime/node diagnostics used by
// `inspect <kind> --troubleshoot`. They decode container and node status into a
// flat list of human-readable problems. Pure (no client calls), unit-tested.

// PodIssue is a single runtime problem found on a pod, attributed to a
// container when applicable.
type PodIssue struct {
	Container string `json:"container,omitempty"`
	Symptom   string `json:"symptom"`
	Detail    string `json:"detail,omitempty"`
}

// problemWaitingReasons are container "waiting" reasons that indicate a real
// problem rather than a transient startup state (ContainerCreating,
// PodInitializing).
var problemWaitingReasons = map[string]bool{
	"ImagePullBackOff":           true,
	"ErrImagePull":               true,
	"ErrImageNeverPull":          true,
	"InvalidImageName":           true,
	"CrashLoopBackOff":           true,
	"CreateContainerConfigError": true,
	"CreateContainerError":       true,
	"RunContainerError":          true,
}

// PodRuntimeIssues inspects a (scheduled) pod's container statuses and returns
// every runtime problem: image-pull failures, crash loops, OOM kills, non-zero
// exits, not-ready running containers, and prior crashes on a now-running
// container. Returns nil for a fully healthy pod.
func PodRuntimeIssues(pod *corev1.Pod) []PodIssue {
	if pod == nil {
		return nil
	}
	var issues []PodIssue
	for _, v := range CollectContainerViews(pod) {
		st := v.Status
		if st == nil {
			continue
		}
		name := v.Spec.Name
		switch {
		case st.State.Waiting != nil:
			r := st.State.Waiting.Reason
			if problemWaitingReasons[r] {
				issues = append(issues, PodIssue{Container: name, Symptom: r, Detail: st.State.Waiting.Message})
			}
		case st.State.Terminated != nil:
			t := st.State.Terminated
			switch {
			case t.Reason == "OOMKilled":
				issues = append(issues, PodIssue{Container: name, Symptom: "OOMKilled", Detail: fmt.Sprintf("exit code %d", t.ExitCode)})
			case t.ExitCode != 0:
				issues = append(issues, PodIssue{Container: name, Symptom: "Terminated", Detail: fmt.Sprintf("exit code %d (%s)", t.ExitCode, t.Reason)})
			}
		case st.State.Running != nil:
			if !st.Ready {
				issues = append(issues, PodIssue{Container: name, Symptom: "NotReady", Detail: "running but readiness probe not passing"})
			} else if lt := st.LastTerminationState.Terminated; lt != nil && st.RestartCount > 0 {
				// Healthy now, but it has flapped — surface the prior crash.
				issues = append(issues, PodIssue{
					Container: name,
					Symptom:   "PreviousRestart",
					Detail:    fmt.Sprintf("restartCount=%d, last exit %d (%s)", st.RestartCount, lt.ExitCode, lt.Reason),
				})
			}
		}
	}
	return issues
}

// NodeIssues returns the node-level problems that make a node unhealthy or
// unable to accept work: NotReady, cordoned (unschedulable), and resource
// pressure conditions. Returns nil for a healthy node. Taints are reported
// separately by the caller (they restrict, but do not break, the node).
func NodeIssues(n *corev1.Node) []string {
	if n == nil {
		return nil
	}
	var issues []string
	for _, c := range n.Status.Conditions {
		switch c.Type {
		case corev1.NodeReady:
			if c.Status != corev1.ConditionTrue {
				detail := c.Reason
				if detail == "" {
					detail = string(c.Status)
				}
				issues = append(issues, "NotReady ("+detail+")")
			}
		case corev1.NodeMemoryPressure, corev1.NodeDiskPressure, corev1.NodePIDPressure:
			if c.Status == corev1.ConditionTrue {
				issues = append(issues, string(c.Type)+"=True")
			}
		}
	}
	if n.Spec.Unschedulable {
		issues = append(issues, "cordoned (scheduling disabled)")
	}
	return issues
}
