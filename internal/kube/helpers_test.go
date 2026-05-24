package kube

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func TestParseImage(t *testing.T) {
	tests := []struct {
		in       string
		repo     string
		ref      string
		isDigest bool
	}{
		{in: "nginx", repo: "nginx", ref: "latest", isDigest: false},
		{in: "nginx:alpine", repo: "nginx", ref: "alpine", isDigest: false},
		{in: "nginx:1.25.3", repo: "nginx", ref: "1.25.3", isDigest: false},
		{
			in:       "nginx@sha256:abcdef0123456789",
			repo:     "nginx",
			ref:      "sha256:abcdef0123456789",
			isDigest: true,
		},
		{
			in:   "registry.example.com:5000/library/nginx:1.0",
			repo: "registry.example.com:5000/library/nginx",
			ref:  "1.0",
		},
		{
			in:   "registry.example.com:5000/library/nginx",
			repo: "registry.example.com:5000/library/nginx",
			ref:  "latest",
		},
		{
			in:       "ghcr.io/owner/img@sha256:deadbeef",
			repo:     "ghcr.io/owner/img",
			ref:      "sha256:deadbeef",
			isDigest: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			repo, ref, digest := ParseImage(tt.in)
			if repo != tt.repo || ref != tt.ref || digest != tt.isDigest {
				t.Errorf("ParseImage(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tt.in, repo, ref, digest, tt.repo, tt.ref, tt.isDigest)
			}
		})
	}
}

func TestFormatPorts(t *testing.T) {
	tests := []struct {
		name  string
		ports []corev1.ContainerPort
		want  string
	}{
		{name: "empty", ports: nil, want: "<none>"},
		{name: "single tcp", ports: []corev1.ContainerPort{
			{ContainerPort: 80, Protocol: corev1.ProtocolTCP},
		}, want: "80/TCP"},
		{name: "default protocol", ports: []corev1.ContainerPort{
			{ContainerPort: 8080},
		}, want: "8080/TCP"},
		{name: "named", ports: []corev1.ContainerPort{
			{Name: "http", ContainerPort: 80, Protocol: corev1.ProtocolTCP},
		}, want: "http:80/TCP"},
		{name: "multiple", ports: []corev1.ContainerPort{
			{ContainerPort: 80, Protocol: corev1.ProtocolTCP},
			{ContainerPort: 443, Protocol: corev1.ProtocolTCP},
			{ContainerPort: 53, Protocol: corev1.ProtocolUDP},
		}, want: "80/TCP, 443/TCP, 53/UDP"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatPorts(tt.ports); got != tt.want {
				t.Errorf("FormatPorts() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInstanceTypeForNodeLabels(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{name: "nil", labels: nil, want: "-"},
		{name: "empty", labels: map[string]string{}, want: "-"},
		{name: "primary", labels: map[string]string{
			"node.kubernetes.io/instance-type": "m5.xlarge",
		}, want: "m5.xlarge"},
		{name: "legacy", labels: map[string]string{
			"beta.kubernetes.io/instance-type": "n1-standard-4",
		}, want: "n1-standard-4"},
		{name: "primary takes precedence", labels: map[string]string{
			"node.kubernetes.io/instance-type": "m5.xlarge",
			"beta.kubernetes.io/instance-type": "legacy",
		}, want: "m5.xlarge"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := InstanceTypeForNodeLabels(tt.labels); got != tt.want {
				t.Errorf("InstanceTypeForNodeLabels() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatTaints(t *testing.T) {
	tests := []struct {
		name   string
		taints []corev1.Taint
		want   string
	}{
		{name: "empty", taints: nil, want: "<none>"},
		{name: "key only", taints: []corev1.Taint{
			{Key: "node-role.kubernetes.io/control-plane", Effect: corev1.TaintEffectNoSchedule},
		}, want: "node-role.kubernetes.io/control-plane:NoSchedule"},
		{name: "key=value", taints: []corev1.Taint{
			{Key: "dedicated", Value: "gpu", Effect: corev1.TaintEffectNoSchedule},
		}, want: "dedicated=gpu:NoSchedule"},
		{name: "multiple", taints: []corev1.Taint{
			{Key: "a", Effect: corev1.TaintEffectNoSchedule},
			{Key: "b", Value: "v", Effect: corev1.TaintEffectPreferNoSchedule},
		}, want: "a:NoSchedule, b=v:PreferNoSchedule"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatTaints(tt.taints); got != tt.want {
				t.Errorf("FormatTaints() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNodeConditionsSummary(t *testing.T) {
	conds := []corev1.NodeCondition{
		{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
		{Type: corev1.NodeMemoryPressure, Status: corev1.ConditionFalse},
		{Type: corev1.NodeDiskPressure, Status: corev1.ConditionFalse},
		{Type: corev1.NodePIDPressure, Status: corev1.ConditionFalse},
		{Type: corev1.NodeNetworkUnavailable, Status: corev1.ConditionFalse}, // dropped
	}
	got := NodeConditionsSummary(conds)
	want := map[string]string{
		"Ready":           "True",
		"MemoryPressure":  "False",
		"DiskPressure":    "False",
		"PIDPressure":     "False",
	}
	if len(got) != len(want) {
		t.Errorf("expected %d conditions, got %d: %v", len(want), len(got), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("conditions[%q] = %q, want %q", k, got[k], v)
		}
	}
	if _, ok := got["NetworkUnavailable"]; ok {
		t.Error("NetworkUnavailable should not be in the summary")
	}
}

func TestPortsForContainer(t *testing.T) {
	containers := []corev1.Container{
		{Name: "app", Ports: []corev1.ContainerPort{
			{ContainerPort: 8080, Protocol: corev1.ProtocolTCP},
		}},
		{Name: "sidecar"},
	}
	if got := PortsForContainer(containers, "app"); got != "8080/TCP" {
		t.Errorf("app ports = %q, want %q", got, "8080/TCP")
	}
	if got := PortsForContainer(containers, "sidecar"); got != "<none>" {
		t.Errorf("sidecar ports = %q, want %q", got, "<none>")
	}
	if got := PortsForContainer(containers, "missing"); got != "<none>" {
		t.Errorf("missing container = %q, want %q", got, "<none>")
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

func TestEffectiveEventTime(t *testing.T) {
	t1 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	t4 := time.Date(2026, 1, 1, 13, 0, 0, 0, time.UTC)
	t5 := time.Date(2026, 1, 1, 14, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		ev   corev1.Event
		want time.Time
	}{
		{
			name: "series wins over everything",
			ev: corev1.Event{
				Series:            &corev1.EventSeries{LastObservedTime: metav1.MicroTime{Time: t5}},
				LastTimestamp:     metav1.Time{Time: t4},
				EventTime:         metav1.MicroTime{Time: t3},
				FirstTimestamp:    metav1.Time{Time: t2},
				ObjectMeta:        metav1.ObjectMeta{CreationTimestamp: metav1.Time{Time: t1}},
			},
			want: t5,
		},
		{
			name: "lastTimestamp used when series absent",
			ev: corev1.Event{
				LastTimestamp:  metav1.Time{Time: t4},
				EventTime:      metav1.MicroTime{Time: t3},
				FirstTimestamp: metav1.Time{Time: t2},
				ObjectMeta:     metav1.ObjectMeta{CreationTimestamp: metav1.Time{Time: t1}},
			},
			want: t4,
		},
		{
			name: "eventTime used when legacy timestamps zero (events.k8s.io/v1 path)",
			ev: corev1.Event{
				EventTime:  metav1.MicroTime{Time: t3},
				ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.Time{Time: t1}},
			},
			want: t3,
		},
		{
			name: "firstTimestamp used as fallback",
			ev: corev1.Event{
				FirstTimestamp: metav1.Time{Time: t2},
				ObjectMeta:     metav1.ObjectMeta{CreationTimestamp: metav1.Time{Time: t1}},
			},
			want: t2,
		},
		{
			name: "creationTimestamp as last resort",
			ev: corev1.Event{
				ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.Time{Time: t1}},
			},
			want: t1,
		},
		{
			name: "all zero returns zero",
			ev:   corev1.Event{},
			want: time.Time{},
		},
		{
			name: "series with zero LastObservedTime falls through",
			ev: corev1.Event{
				Series:    &corev1.EventSeries{},
				EventTime: metav1.MicroTime{Time: t3},
			},
			want: t3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EffectiveEventTime(tt.ev)
			if !got.Equal(tt.want) {
				t.Errorf("EffectiveEventTime() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCollectContainerViews(t *testing.T) {
	always := corev1.ContainerRestartPolicyAlways
	pod := corev1.Pod{
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{
				{Name: "init-perms"},
				{Name: "log-shipper", RestartPolicy: &always},
			},
			Containers: []corev1.Container{
				{Name: "app"},
				{Name: "no-status-yet"},
			},
		},
		Status: corev1.PodStatus{
			InitContainerStatuses: []corev1.ContainerStatus{
				{Name: "init-perms", Ready: false},
				{Name: "log-shipper", Ready: true},
			},
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: "app", Ready: true},
			},
		},
	}

	views := CollectContainerViews(&pod)

	if len(views) != 4 {
		t.Fatalf("want 4 views, got %d", len(views))
	}
	want := []struct {
		name      string
		kind      ContainerKind
		hasStatus bool
	}{
		{"init-perms", ContainerKindInit, true},
		{"log-shipper", ContainerKindSidecar, true},
		{"app", ContainerKindRegular, true},
		{"no-status-yet", ContainerKindRegular, false},
	}
	for i, w := range want {
		if views[i].Spec.Name != w.name {
			t.Errorf("view[%d].Spec.Name = %q, want %q", i, views[i].Spec.Name, w.name)
		}
		if views[i].Kind != w.kind {
			t.Errorf("view[%d].Kind = %v, want %v", i, views[i].Kind, w.kind)
		}
		if (views[i].Status != nil) != w.hasStatus {
			t.Errorf("view[%d] status presence = %v, want %v", i, views[i].Status != nil, w.hasStatus)
		}
	}
}

func TestCollectContainerViews_EmptyAndNoInit(t *testing.T) {
	pod := corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "only"}},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{{Name: "only", Ready: true}},
		},
	}
	views := CollectContainerViews(&pod)
	if len(views) != 1 || views[0].Kind != ContainerKindRegular {
		t.Fatalf("want 1 regular view, got %+v", views)
	}

	views = CollectContainerViews(&corev1.Pod{})
	if len(views) != 0 {
		t.Fatalf("empty pod should yield no views, got %d", len(views))
	}
}
