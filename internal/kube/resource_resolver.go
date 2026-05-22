package kube

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ResolvedResource carries everything `kdiag sort` needs to issue a List
// against the dynamic client and to render the kind in the output banner.
type ResolvedResource struct {
	GVR        schema.GroupVersionResource
	GVK        schema.GroupVersionKind
	Namespaced bool
}

// ResolveResource maps a user-typed resource token to a concrete
// GroupVersionResource using the cluster's discovery information.
//
// The token may be a canonical singular ("pod", "configmap"), a plural
// ("pods", "configmaps"), a shortname ("po", "cm", "svc"), or a fully
// qualified form ("pods.v1.", "certificates.cert-manager.io"). RESTMapper
// handles all of these via the live discovery doc.
func ResolveResource(mapper meta.RESTMapper, input string) (*ResolvedResource, error) {
	if input == "" {
		return nil, fmt.Errorf("empty resource")
	}

	// Parse the input into a partial GVR. Two forms are accepted:
	//   * "resource"        → bare resource ("pod", "cm", "widgets")
	//   * "resource.group"  → kubectl-style with the API group as the rest
	//                          of the string after the first dot
	//                          ("deployments.apps",
	//                           "certificates.cert-manager.io",
	//                           "widgets.demo.example.com")
	// ParseGroupResource splits at the first dot, so any dots inside the
	// group name are preserved correctly.
	gr := schema.ParseGroupResource(input)
	gvr := &schema.GroupVersionResource{Group: gr.Group, Resource: gr.Resource}

	gvk, err := mapper.KindFor(*gvr)
	if err != nil {
		return nil, err
	}

	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, err
	}

	return &ResolvedResource{
		GVR:        mapping.Resource,
		GVK:        mapping.GroupVersionKind,
		Namespaced: mapping.Scope.Name() == meta.RESTScopeNameNamespace,
	}, nil
}
