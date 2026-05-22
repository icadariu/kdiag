package kube

import "testing"

func TestCanonicalKind(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "pod", want: "pod"},
		{in: "po", want: "pod"},
		{in: "deployment", want: "deployment"},
		{in: "deploy", want: "deployment"},
		{in: "daemonset", want: "daemonset"},
		{in: "ds", want: "daemonset"},
		{in: "statefulset", want: "statefulset"},
		{in: "sts", want: "statefulset"},
		{in: "replicaset", want: "replicaset"},
		{in: "rs", want: "replicaset"},
		{in: "node", want: "node"},
		{in: "no", want: "node"},
		{in: "", want: ""},
		{in: "service", want: ""},
		{in: "POD", want: ""}, // case-sensitive
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := CanonicalKind(tt.in); got != tt.want {
				t.Errorf("CanonicalKind(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestIsClusterScoped(t *testing.T) {
	tests := []struct {
		canonical string
		want      bool
	}{
		{canonical: "node", want: true},
		{canonical: "pod", want: false},
		{canonical: "deployment", want: false},
		{canonical: "daemonset", want: false},
		{canonical: "statefulset", want: false},
		{canonical: "replicaset", want: false},
		{canonical: "unknown", want: false},
		{canonical: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.canonical, func(t *testing.T) {
			if got := IsClusterScoped(tt.canonical); got != tt.want {
				t.Errorf("IsClusterScoped(%q) = %v, want %v", tt.canonical, got, tt.want)
			}
		})
	}
}
