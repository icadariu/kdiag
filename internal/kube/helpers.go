package kube

import (
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func ListOptions(selector string) v1.ListOptions {
	return v1.ListOptions{
		LabelSelector: selector,
	}
}

// EffectiveEventTime returns the best available timestamp for an Event,
// falling back across the fields that may be populated depending on which
// API the event was created through (events.k8s.io/v1 vs core/v1) and
// whether it is part of a Series. Returns the zero time only when no
// field is set.
func EffectiveEventTime(ev corev1.Event) time.Time {
	if ev.Series != nil && !ev.Series.LastObservedTime.IsZero() {
		return ev.Series.LastObservedTime.Time
	}
	if !ev.LastTimestamp.IsZero() {
		return ev.LastTimestamp.Time
	}
	if !ev.EventTime.IsZero() {
		return ev.EventTime.Time
	}
	if !ev.FirstTimestamp.IsZero() {
		return ev.FirstTimestamp.Time
	}
	return ev.CreationTimestamp.Time
}

func GetOptions() v1.GetOptions {
	return v1.GetOptions{}
}

func ZoneForNodeLabels(labels map[string]string) string {
	if labels == nil {
		return "-"
	}
	if z := labels["topology.kubernetes.io/zone"]; z != "" {
		return z
	}
	if z := labels["failure-domain.beta.kubernetes.io/zone"]; z != "" {
		return z
	}
	return "-"
}

func ContainerStateKey(st corev1.ContainerState) string {
	switch {
	case st.Running != nil:
		return "running"
	case st.Waiting != nil:
		return "waiting"
	case st.Terminated != nil:
		return "terminated"
	default:
		return "<none>"
	}
}

func ContainerStateReason(st corev1.ContainerState) string {
	if st.Waiting != nil {
		return st.Waiting.Reason
	}
	if st.Terminated != nil {
		return st.Terminated.Reason
	}
	return ""
}

// ParseImage splits a container image reference into (repo, ref, isDigest).
//
//	"nginx"               → ("nginx", "latest", false)
//	"nginx:alpine"        → ("nginx", "alpine", false)
//	"nginx@sha256:abc..." → ("nginx", "sha256:abc...", true)
//	"reg:5000/nginx:1.0"  → ("reg:5000/nginx", "1.0", false)
func ParseImage(image string) (repo, ref string, isDigest bool) {
	if at := strings.LastIndex(image, "@"); at != -1 {
		return image[:at], image[at+1:], true
	}
	// A trailing colon is a tag separator only when it appears after the
	// last slash — otherwise it's a registry port.
	if colon := strings.LastIndex(image, ":"); colon > strings.LastIndex(image, "/") {
		return image[:colon], image[colon+1:], false
	}
	return image, "latest", false
}

// FormatPorts renders a container port list as "name:80/TCP, 443/TCP".
// Empty list returns "<none>".
func FormatPorts(ports []corev1.ContainerPort) string {
	if len(ports) == 0 {
		return "<none>"
	}
	parts := make([]string, len(ports))
	for i, p := range ports {
		proto := string(p.Protocol)
		if proto == "" {
			proto = "TCP"
		}
		if p.Name != "" {
			parts[i] = fmt.Sprintf("%s:%d/%s", p.Name, p.ContainerPort, proto)
		} else {
			parts[i] = fmt.Sprintf("%d/%s", p.ContainerPort, proto)
		}
	}
	return strings.Join(parts, ", ")
}

// PortsForContainer returns the formatted ports for a named container,
// or "<none>" if the container is missing or has no ports.
func PortsForContainer(containers []corev1.Container, name string) string {
	for _, c := range containers {
		if c.Name == name {
			return FormatPorts(c.Ports)
		}
	}
	return "<none>"
}

// InstanceTypeForNodeLabels returns the cloud instance type from node labels,
// preferring the canonical label and falling back to the legacy beta label.
// Returns "-" if neither is set.
func InstanceTypeForNodeLabels(labels map[string]string) string {
	if labels == nil {
		return "-"
	}
	if v := labels["node.kubernetes.io/instance-type"]; v != "" {
		return v
	}
	if v := labels["beta.kubernetes.io/instance-type"]; v != "" {
		return v
	}
	return "-"
}

// FormatTaints renders node taints as "key=value:effect, …" (or "key:effect"
// when value is empty). Returns "<none>" for an empty list.
func FormatTaints(taints []corev1.Taint) string {
	if len(taints) == 0 {
		return "<none>"
	}
	parts := make([]string, len(taints))
	for i, t := range taints {
		if t.Value == "" {
			parts[i] = fmt.Sprintf("%s:%s", t.Key, t.Effect)
		} else {
			parts[i] = fmt.Sprintf("%s=%s:%s", t.Key, t.Value, t.Effect)
		}
	}
	return strings.Join(parts, ", ")
}

// NodeConditionsSummary returns the four conditions operators usually want
// at a glance (Ready, MemoryPressure, DiskPressure, PIDPressure) keyed by
// type with their status as the value.
func NodeConditionsSummary(conditions []corev1.NodeCondition) map[string]string {
	interesting := map[corev1.NodeConditionType]bool{
		corev1.NodeReady:          true,
		corev1.NodeMemoryPressure: true,
		corev1.NodeDiskPressure:   true,
		corev1.NodePIDPressure:    true,
	}
	out := map[string]string{}
	for _, c := range conditions {
		if interesting[c.Type] {
			out[string(c.Type)] = string(c.Status)
		}
	}
	return out
}

// AllocatableMap converts a node's Allocatable ResourceList into a flat
// string map for printing.
func AllocatableMap(rl corev1.ResourceList) map[string]string {
	out := make(map[string]string, len(rl))
	for k, v := range rl {
		out[string(k)] = v.String()
	}
	return out
}

func ResourcesForContainer(containers []corev1.Container, name string) (map[string]string, map[string]string) {
	for _, c := range containers {
		if c.Name != name {
			continue
		}
		req := map[string]string{}
		lim := map[string]string{}
		for k, v := range c.Resources.Requests {
			req[string(k)] = v.String()
		}
		for k, v := range c.Resources.Limits {
			lim[string(k)] = v.String()
		}
		return req, lim
	}
	return map[string]string{}, map[string]string{}
}

// ResourcesFromSpec returns (requests, limits) as map[string]string for a
// single container spec. Use when you already have the container value (e.g.
// from CollectContainerViews); ResourcesForContainer is the by-name variant.
func ResourcesFromSpec(c corev1.Container) (map[string]string, map[string]string) {
	return AllocatableMap(c.Resources.Requests), AllocatableMap(c.Resources.Limits)
}

// ContainerKind identifies whether a containerView came from initContainers
// (Init), an initContainer with restartPolicy=Always (Sidecar, k8s 1.28+),
// or .spec.containers (Regular).
type ContainerKind int

const (
	ContainerKindInit ContainerKind = iota
	ContainerKindSidecar
	ContainerKindRegular
)

func (k ContainerKind) String() string {
	switch k {
	case ContainerKindInit:
		return "Init Container"
	case ContainerKindSidecar:
		return "Sidecar Container"
	default:
		return "Container"
	}
}

// ContainerView pairs a container spec with its runtime status (if any) and
// tags whether it is an init, sidecar, or regular container.
// Status is nil when the container has not started yet.
type ContainerView struct {
	Kind   ContainerKind
	Spec   corev1.Container
	Status *corev1.ContainerStatus
}

// CollectContainerViews returns one ContainerView per container in the pod,
// ordered init → sidecar → regular. Init/sidecar specs come from
// Spec.InitContainers (a sidecar is an initContainer with
// RestartPolicy=Always, k8s 1.28+). Status is paired by name; a missing
// status leaves Status nil.
func CollectContainerViews(pod *corev1.Pod) []ContainerView {
	if pod == nil {
		return nil
	}
	initStatus := map[string]*corev1.ContainerStatus{}
	for i := range pod.Status.InitContainerStatuses {
		s := &pod.Status.InitContainerStatuses[i]
		initStatus[s.Name] = s
	}
	mainStatus := map[string]*corev1.ContainerStatus{}
	for i := range pod.Status.ContainerStatuses {
		s := &pod.Status.ContainerStatuses[i]
		mainStatus[s.Name] = s
	}

	out := make([]ContainerView, 0, len(pod.Spec.InitContainers)+len(pod.Spec.Containers))
	// First pass: init (non-sidecar) entries, preserving spec order.
	for i := range pod.Spec.InitContainers {
		c := pod.Spec.InitContainers[i]
		if isSidecar(c) {
			continue
		}
		out = append(out, ContainerView{Kind: ContainerKindInit, Spec: c, Status: initStatus[c.Name]})
	}
	// Second pass: sidecars.
	for i := range pod.Spec.InitContainers {
		c := pod.Spec.InitContainers[i]
		if !isSidecar(c) {
			continue
		}
		out = append(out, ContainerView{Kind: ContainerKindSidecar, Spec: c, Status: initStatus[c.Name]})
	}
	// Third pass: regular containers.
	for i := range pod.Spec.Containers {
		c := pod.Spec.Containers[i]
		out = append(out, ContainerView{Kind: ContainerKindRegular, Spec: c, Status: mainStatus[c.Name]})
	}
	return out
}

func isSidecar(c corev1.Container) bool {
	return c.RestartPolicy != nil && *c.RestartPolicy == corev1.ContainerRestartPolicyAlways
}
