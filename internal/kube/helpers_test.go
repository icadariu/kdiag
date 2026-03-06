package kube

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestZoneForNodeLabels(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{name: "nil labels", labels: nil, want: "-"},
		{name: "empty labels", labels: map[string]string{}, want: "-"},
		{name: "unrelated labels", labels: map[string]string{"foo": "bar"}, want: "-"},
		{name: "primary label", labels: map[string]string{
			"topology.kubernetes.io/zone": "us-east-1a",
		}, want: "us-east-1a"},
		{name: "legacy label", labels: map[string]string{
			"failure-domain.beta.kubernetes.io/zone": "eu-west-1b",
		}, want: "eu-west-1b"},
		{name: "primary takes precedence over legacy", labels: map[string]string{
			"topology.kubernetes.io/zone":             "primary",
			"failure-domain.beta.kubernetes.io/zone": "legacy",
		}, want: "primary"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ZoneForNodeLabels(tt.labels)
			if got != tt.want {
				t.Errorf("ZoneForNodeLabels() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestContainerStateKey(t *testing.T) {
	tests := []struct {
		name  string
		state corev1.ContainerState
		want  string
	}{
		{name: "no state set", state: corev1.ContainerState{}, want: "<none>"},
		{name: "running", state: corev1.ContainerState{
			Running: &corev1.ContainerStateRunning{},
		}, want: "running"},
		{name: "waiting", state: corev1.ContainerState{
			Waiting: &corev1.ContainerStateWaiting{},
		}, want: "waiting"},
		{name: "terminated", state: corev1.ContainerState{
			Terminated: &corev1.ContainerStateTerminated{},
		}, want: "terminated"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ContainerStateKey(tt.state)
			if got != tt.want {
				t.Errorf("ContainerStateKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestContainerStateReason(t *testing.T) {
	tests := []struct {
		name  string
		state corev1.ContainerState
		want  string
	}{
		{name: "no state", state: corev1.ContainerState{}, want: ""},
		{name: "running has no reason", state: corev1.ContainerState{
			Running: &corev1.ContainerStateRunning{},
		}, want: ""},
		{name: "waiting without reason", state: corev1.ContainerState{
			Waiting: &corev1.ContainerStateWaiting{},
		}, want: ""},
		{name: "waiting with reason", state: corev1.ContainerState{
			Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
		}, want: "CrashLoopBackOff"},
		{name: "terminated without reason", state: corev1.ContainerState{
			Terminated: &corev1.ContainerStateTerminated{},
		}, want: ""},
		{name: "terminated with reason", state: corev1.ContainerState{
			Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled"},
		}, want: "OOMKilled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ContainerStateReason(tt.state)
			if got != tt.want {
				t.Errorf("ContainerStateReason() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResourcesForContainer(t *testing.T) {
	containers := []corev1.Container{
		{
			Name: "app",
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("100m"),
					corev1.ResourceMemory: resource.MustParse("128Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("256Mi"),
				},
			},
		},
		{Name: "sidecar"}, // no resources defined
	}

	t.Run("container with resources", func(t *testing.T) {
		req, lim := ResourcesForContainer(containers, "app")
		if req["cpu"] != "100m" {
			t.Errorf("cpu request: got %q, want %q", req["cpu"], "100m")
		}
		if req["memory"] != "128Mi" {
			t.Errorf("memory request: got %q, want %q", req["memory"], "128Mi")
		}
		if lim["memory"] != "256Mi" {
			t.Errorf("memory limit: got %q, want %q", lim["memory"], "256Mi")
		}
		if _, ok := lim["cpu"]; ok {
			t.Error("expected no cpu limit, but found one")
		}
	})

	t.Run("container without resources", func(t *testing.T) {
		req, lim := ResourcesForContainer(containers, "sidecar")
		if len(req) != 0 {
			t.Errorf("expected empty requests, got %v", req)
		}
		if len(lim) != 0 {
			t.Errorf("expected empty limits, got %v", lim)
		}
	})

	t.Run("nonexistent container", func(t *testing.T) {
		req, lim := ResourcesForContainer(containers, "missing")
		if len(req) != 0 || len(lim) != 0 {
			t.Error("expected empty maps for missing container")
		}
	})
}
