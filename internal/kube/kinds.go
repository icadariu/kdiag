package kube

import "slices"

// Kind describes a Kubernetes resource kind that `kdiag inspect` can target.
type Kind struct {
	Canonical    string
	Aliases      []string
	ClusterScope bool
}

// Kinds is the registry of resource kinds supported by `kdiag inspect`.
// Order is preserved for help/usage rendering.
var Kinds = []Kind{
	{Canonical: "pod", Aliases: []string{"po"}},
	{Canonical: "deployment", Aliases: []string{"deploy"}},
	{Canonical: "daemonset", Aliases: []string{"ds"}},
	{Canonical: "statefulset", Aliases: []string{"sts"}},
	{Canonical: "replicaset", Aliases: []string{"rs"}},
	{Canonical: "node", Aliases: []string{"no"}, ClusterScope: true},
}

// CanonicalKind resolves a user-typed kind (canonical or alias) to its
// canonical name. Returns "" when the input matches no known kind.
func CanonicalKind(s string) string {
	for _, k := range Kinds {
		if s == k.Canonical || slices.Contains(k.Aliases, s) {
			return k.Canonical
		}
	}
	return ""
}

// IsClusterScoped reports whether a canonical kind is cluster-scoped.
// Unknown kinds report false.
func IsClusterScoped(canonical string) bool {
	for _, k := range Kinds {
		if k.Canonical == canonical {
			return k.ClusterScope
		}
	}
	return false
}
