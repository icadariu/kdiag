// k8s_helpers.go
package main

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
)

func listOptions(selector string) v1.ListOptions {
	return v1.ListOptions{
		LabelSelector: selector,
	}
}

func getOptions() v1.GetOptions {
	return v1.GetOptions{}
}

func zoneForNodeLabels(labels map[string]string) string {
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

func containerStateKey(st corev1.ContainerState) string {
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

func containerStateReason(st corev1.ContainerState) string {
	if st.Waiting != nil {
		return st.Waiting.Reason
	}
	if st.Terminated != nil {
		return st.Terminated.Reason
	}
	return ""
}

func resourcesForContainer(containers []corev1.Container, name string) (map[string]string, map[string]string) {
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
