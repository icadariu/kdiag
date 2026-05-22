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
