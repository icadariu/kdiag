package kube

import (
	"fmt"
	"slices"
	"strconv"

	corev1 "k8s.io/api/core/v1"
)

// schedule.go holds the pure scheduling-predicate evaluators used by
// `inspect pod --schedule`. They re-derive only the cheap, high-confidence
// predicates (taints, nodeSelector, required nodeAffinity); the authoritative
// verdict always remains the kube-scheduler's FailedScheduling event. The
// harder predicates (inter-pod affinity, topology spread, volume binding) are
// only *detected* here (Has* helpers) and surfaced as deferred, never
// re-evaluated — re-implementing them risks diverging from the real scheduler.

// UntoleratedTaints returns the node taints with a hard scheduling effect
// (NoSchedule / NoExecute) that the given tolerations do NOT tolerate.
// PreferNoSchedule taints are soft and never block scheduling, so they are
// skipped. An empty result means the pod tolerates every hard taint.
func UntoleratedTaints(tolerations []corev1.Toleration, taints []corev1.Taint) []corev1.Taint {
	var out []corev1.Taint
	for _, t := range taints {
		if t.Effect == corev1.TaintEffectPreferNoSchedule {
			continue
		}
		if !taintTolerated(tolerations, t) {
			out = append(out, t)
		}
	}
	return out
}

// taintTolerated reports whether any toleration tolerates the taint, following
// the standard match: an empty toleration effect matches every effect; the
// Exists operator (with an empty key) tolerates all taints; Equal requires the
// value to match.
func taintTolerated(tolerations []corev1.Toleration, t corev1.Taint) bool {
	for _, tol := range tolerations {
		if tol.Effect != "" && tol.Effect != t.Effect {
			continue
		}
		// Empty key with Exists tolerates every taint key.
		if tol.Key == "" {
			if tol.Operator == corev1.TolerationOpExists {
				return true
			}
			continue
		}
		if tol.Key != t.Key {
			continue
		}
		switch tol.Operator {
		case corev1.TolerationOpExists:
			return true
		case corev1.TolerationOpEqual, "":
			if tol.Value == t.Value {
				return true
			}
		}
	}
	return false
}

// NodeSelectorMismatch returns the `key=value` entries of a pod's
// .spec.nodeSelector that the node's labels fail to satisfy. Empty result
// means the node matches every entry.
func NodeSelectorMismatch(sel map[string]string, nodeLabels map[string]string) []string {
	var out []string
	for k, v := range sel {
		if nodeLabels[k] != v {
			out = append(out, fmt.Sprintf("%s=%s", k, v))
		}
	}
	return out
}

// RequiredNodeAffinityMatches reports whether the node's labels satisfy the
// pod's requiredDuringSchedulingIgnoredDuringExecution node affinity. A nil
// affinity (or no required term) trivially matches. The node matches when ANY
// nodeSelectorTerm matches (OR across terms); a term matches when ALL of its
// matchExpressions match (AND within a term) — the scheduler's semantics.
func RequiredNodeAffinityMatches(aff *corev1.Affinity, nodeLabels map[string]string) bool {
	if aff == nil || aff.NodeAffinity == nil ||
		aff.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		return true
	}
	terms := aff.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if len(terms) == 0 {
		return true
	}
	for _, term := range terms {
		if termMatches(term, nodeLabels) {
			return true
		}
	}
	return false
}

func termMatches(term corev1.NodeSelectorTerm, labels map[string]string) bool {
	for _, expr := range term.MatchExpressions {
		if !exprMatches(expr, labels) {
			return false
		}
	}
	return true
}

func exprMatches(expr corev1.NodeSelectorRequirement, labels map[string]string) bool {
	val, present := labels[expr.Key]
	switch expr.Operator {
	case corev1.NodeSelectorOpIn:
		return present && slices.Contains(expr.Values, val)
	case corev1.NodeSelectorOpNotIn:
		return !present || !slices.Contains(expr.Values, val)
	case corev1.NodeSelectorOpExists:
		return present
	case corev1.NodeSelectorOpDoesNotExist:
		return !present
	case corev1.NodeSelectorOpGt, corev1.NodeSelectorOpLt:
		if !present || len(expr.Values) == 0 {
			return false
		}
		nodeN, err1 := strconv.ParseInt(val, 10, 64)
		wantN, err2 := strconv.ParseInt(expr.Values[0], 10, 64)
		if err1 != nil || err2 != nil {
			return false
		}
		if expr.Operator == corev1.NodeSelectorOpGt {
			return nodeN > wantN
		}
		return nodeN < wantN
	default:
		return false
	}
}

// HasTopologySpread reports whether the pod declares topology spread
// constraints. These are detected (and surfaced as deferred) but not evaluated.
func HasTopologySpread(pod *corev1.Pod) bool {
	return pod != nil && len(pod.Spec.TopologySpreadConstraints) > 0
}

// HasInterPodAffinity reports whether the pod declares pod (anti-)affinity.
// Detected and surfaced as deferred, not evaluated.
func HasInterPodAffinity(pod *corev1.Pod) bool {
	return pod != nil && pod.Spec.Affinity != nil &&
		(pod.Spec.Affinity.PodAffinity != nil || pod.Spec.Affinity.PodAntiAffinity != nil)
}

// HasPersistentVolumeClaims reports whether the pod mounts any PVC. PVCs are
// the volumes that *may* bind a pod to a zone; kdiag does not resolve the PVC's
// actual zone (that needs the PVC/PV objects), so this only flags the
// constraint as deferred.
func HasPersistentVolumeClaims(pod *corev1.Pod) bool {
	if pod == nil {
		return false
	}
	for _, v := range pod.Spec.Volumes {
		if v.PersistentVolumeClaim != nil {
			return true
		}
	}
	return false
}
