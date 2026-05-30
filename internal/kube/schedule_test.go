package kube

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func taint(key, value string, effect corev1.TaintEffect) corev1.Taint {
	return corev1.Taint{Key: key, Value: value, Effect: effect}
}

func TestUntoleratedTaints(t *testing.T) {
	tests := []struct {
		name        string
		tolerations []corev1.Toleration
		taints      []corev1.Taint
		wantKeys    []string
	}{
		{
			name:     "no taints",
			taints:   nil,
			wantKeys: nil,
		},
		{
			name:     "NoSchedule taint, no tolerations",
			taints:   []corev1.Taint{taint("dedicated", "gpu", corev1.TaintEffectNoSchedule)},
			wantKeys: []string{"dedicated"},
		},
		{
			name:   "Equal toleration matches key+value+effect",
			taints: []corev1.Taint{taint("dedicated", "gpu", corev1.TaintEffectNoSchedule)},
			tolerations: []corev1.Toleration{{
				Key: "dedicated", Operator: corev1.TolerationOpEqual, Value: "gpu",
				Effect: corev1.TaintEffectNoSchedule,
			}},
			wantKeys: nil,
		},
		{
			name:   "Equal toleration wrong value does not tolerate",
			taints: []corev1.Taint{taint("dedicated", "gpu", corev1.TaintEffectNoSchedule)},
			tolerations: []corev1.Toleration{{
				Key: "dedicated", Operator: corev1.TolerationOpEqual, Value: "cpu",
				Effect: corev1.TaintEffectNoSchedule,
			}},
			wantKeys: []string{"dedicated"},
		},
		{
			name:   "Exists toleration on key ignores value",
			taints: []corev1.Taint{taint("dedicated", "gpu", corev1.TaintEffectNoSchedule)},
			tolerations: []corev1.Toleration{{
				Key: "dedicated", Operator: corev1.TolerationOpExists,
				Effect: corev1.TaintEffectNoSchedule,
			}},
			wantKeys: nil,
		},
		{
			name:   "Exists with empty key and empty effect tolerates everything",
			taints: []corev1.Taint{taint("dedicated", "gpu", corev1.TaintEffectNoSchedule), taint("foo", "", corev1.TaintEffectNoExecute)},
			tolerations: []corev1.Toleration{{
				Operator: corev1.TolerationOpExists,
			}},
			wantKeys: nil,
		},
		{
			name:   "empty effect toleration tolerates all effects for that key",
			taints: []corev1.Taint{taint("dedicated", "gpu", corev1.TaintEffectNoSchedule)},
			tolerations: []corev1.Toleration{{
				Key: "dedicated", Operator: corev1.TolerationOpExists,
			}},
			wantKeys: nil,
		},
		{
			name:   "effect-specific toleration does not cover other effect",
			taints: []corev1.Taint{taint("dedicated", "gpu", corev1.TaintEffectNoExecute)},
			tolerations: []corev1.Toleration{{
				Key: "dedicated", Operator: corev1.TolerationOpExists,
				Effect: corev1.TaintEffectNoSchedule,
			}},
			wantKeys: []string{"dedicated"},
		},
		{
			name:     "PreferNoSchedule taint is never a hard blocker",
			taints:   []corev1.Taint{taint("soft", "x", corev1.TaintEffectPreferNoSchedule)},
			wantKeys: nil,
		},
		{
			name:     "NoExecute untolerated",
			taints:   []corev1.Taint{taint("evict", "y", corev1.TaintEffectNoExecute)},
			wantKeys: []string{"evict"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UntoleratedTaints(tt.tolerations, tt.taints)
			gotKeys := make([]string, 0, len(got))
			for _, tn := range got {
				gotKeys = append(gotKeys, tn.Key)
			}
			if !equalStrings(gotKeys, tt.wantKeys) {
				t.Errorf("UntoleratedTaints() keys = %v, want %v", gotKeys, tt.wantKeys)
			}
		})
	}
}

func TestNodeSelectorMismatch(t *testing.T) {
	tests := []struct {
		name   string
		sel    map[string]string
		labels map[string]string
		want   []string
	}{
		{name: "empty selector", sel: nil, labels: map[string]string{"a": "b"}, want: nil},
		{name: "all match", sel: map[string]string{"disktype": "ssd"}, labels: map[string]string{"disktype": "ssd"}, want: nil},
		{name: "missing label", sel: map[string]string{"disktype": "ssd"}, labels: map[string]string{"other": "x"}, want: []string{"disktype=ssd"}},
		{name: "wrong value", sel: map[string]string{"disktype": "ssd"}, labels: map[string]string{"disktype": "hdd"}, want: []string{"disktype=ssd"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NodeSelectorMismatch(tt.sel, tt.labels)
			if !equalStrings(got, tt.want) {
				t.Errorf("NodeSelectorMismatch() = %v, want %v", got, tt.want)
			}
		})
	}
}

func affinityWithTerms(terms ...corev1.NodeSelectorTerm) *corev1.Affinity {
	return &corev1.Affinity{
		NodeAffinity: &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
				NodeSelectorTerms: terms,
			},
		},
	}
}

func term(exprs ...corev1.NodeSelectorRequirement) corev1.NodeSelectorTerm {
	return corev1.NodeSelectorTerm{MatchExpressions: exprs}
}

func TestRequiredNodeAffinityMatches(t *testing.T) {
	tests := []struct {
		name   string
		aff    *corev1.Affinity
		labels map[string]string
		want   bool
	}{
		{name: "nil affinity passes", aff: nil, labels: nil, want: true},
		{name: "nil node affinity passes", aff: &corev1.Affinity{}, labels: nil, want: true},
		{
			name:   "In matches",
			aff:    affinityWithTerms(term(corev1.NodeSelectorRequirement{Key: "disktype", Operator: corev1.NodeSelectorOpIn, Values: []string{"ssd", "nvme"}})),
			labels: map[string]string{"disktype": "ssd"},
			want:   true,
		},
		{
			name:   "In no match",
			aff:    affinityWithTerms(term(corev1.NodeSelectorRequirement{Key: "disktype", Operator: corev1.NodeSelectorOpIn, Values: []string{"ssd"}})),
			labels: map[string]string{"disktype": "hdd"},
			want:   false,
		},
		{
			name:   "NotIn matches when value absent from set",
			aff:    affinityWithTerms(term(corev1.NodeSelectorRequirement{Key: "disktype", Operator: corev1.NodeSelectorOpNotIn, Values: []string{"hdd"}})),
			labels: map[string]string{"disktype": "ssd"},
			want:   true,
		},
		{
			name:   "Exists matches when key present",
			aff:    affinityWithTerms(term(corev1.NodeSelectorRequirement{Key: "gpu", Operator: corev1.NodeSelectorOpExists})),
			labels: map[string]string{"gpu": "true"},
			want:   true,
		},
		{
			name:   "DoesNotExist matches when key absent",
			aff:    affinityWithTerms(term(corev1.NodeSelectorRequirement{Key: "gpu", Operator: corev1.NodeSelectorOpDoesNotExist})),
			labels: map[string]string{"disktype": "ssd"},
			want:   true,
		},
		{
			name:   "Gt matches",
			aff:    affinityWithTerms(term(corev1.NodeSelectorRequirement{Key: "cores", Operator: corev1.NodeSelectorOpGt, Values: []string{"4"}})),
			labels: map[string]string{"cores": "8"},
			want:   true,
		},
		{
			name:   "Gt no match",
			aff:    affinityWithTerms(term(corev1.NodeSelectorRequirement{Key: "cores", Operator: corev1.NodeSelectorOpGt, Values: []string{"16"}})),
			labels: map[string]string{"cores": "8"},
			want:   false,
		},
		{
			name: "multi-term OR: second term matches",
			aff: affinityWithTerms(
				term(corev1.NodeSelectorRequirement{Key: "disktype", Operator: corev1.NodeSelectorOpIn, Values: []string{"hdd"}}),
				term(corev1.NodeSelectorRequirement{Key: "disktype", Operator: corev1.NodeSelectorOpIn, Values: []string{"ssd"}}),
			),
			labels: map[string]string{"disktype": "ssd"},
			want:   true,
		},
		{
			name: "multi-expr AND: one expr fails fails the term",
			aff: affinityWithTerms(term(
				corev1.NodeSelectorRequirement{Key: "disktype", Operator: corev1.NodeSelectorOpIn, Values: []string{"ssd"}},
				corev1.NodeSelectorRequirement{Key: "zone", Operator: corev1.NodeSelectorOpIn, Values: []string{"a"}},
			)),
			labels: map[string]string{"disktype": "ssd", "zone": "b"},
			want:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RequiredNodeAffinityMatches(tt.aff, tt.labels); got != tt.want {
				t.Errorf("RequiredNodeAffinityMatches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDeferredConstraintDetectors(t *testing.T) {
	plain := &corev1.Pod{}
	if HasTopologySpread(plain) || HasInterPodAffinity(plain) || HasPersistentVolumeClaims(plain) {
		t.Fatalf("plain pod should report no deferred constraints")
	}

	spread := &corev1.Pod{Spec: corev1.PodSpec{
		TopologySpreadConstraints: []corev1.TopologySpreadConstraint{{TopologyKey: "zone"}},
	}}
	if !HasTopologySpread(spread) {
		t.Errorf("HasTopologySpread() = false, want true")
	}

	antiAff := &corev1.Pod{Spec: corev1.PodSpec{Affinity: &corev1.Affinity{
		PodAntiAffinity: &corev1.PodAntiAffinity{},
	}}}
	if !HasInterPodAffinity(antiAff) {
		t.Errorf("HasInterPodAffinity() = false, want true")
	}

	withPVC := &corev1.Pod{Spec: corev1.PodSpec{Volumes: []corev1.Volume{{
		Name:         "data",
		VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "c"}},
	}}}}
	if !HasPersistentVolumeClaims(withPVC) {
		t.Errorf("HasPersistentVolumeClaims() = false, want true")
	}
}

// equalStrings compares two string slices, treating nil and empty as equal.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
