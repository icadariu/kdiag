package kube

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func newTestMapper() meta.RESTMapper {
	m := meta.NewDefaultRESTMapper([]schema.GroupVersion{
		{Group: "", Version: "v1"},
		{Group: "apps", Version: "v1"},
		{Group: "demo.example.com", Version: "v1"},
	})
	m.Add(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}, meta.RESTScopeNamespace)
	m.Add(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}, meta.RESTScopeNamespace)
	m.Add(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Node"}, meta.RESTScopeRoot)
	m.Add(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"}, meta.RESTScopeRoot)
	m.Add(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}, meta.RESTScopeNamespace)
	m.Add(schema.GroupVersionKind{Group: "demo.example.com", Version: "v1", Kind: "Widget"}, meta.RESTScopeNamespace)
	return m
}

func TestResolveResource(t *testing.T) {
	mapper := newTestMapper()

	tests := []struct {
		name           string
		input          string
		wantGVR        schema.GroupVersionResource
		wantNamespaced bool
		wantErr        bool
	}{
		{
			name:           "core singular",
			input:          "pod",
			wantGVR:        schema.GroupVersionResource{Version: "v1", Resource: "pods"},
			wantNamespaced: true,
		},
		{
			name:           "core plural",
			input:          "pods",
			wantGVR:        schema.GroupVersionResource{Version: "v1", Resource: "pods"},
			wantNamespaced: true,
		},
		{
			name:           "compound singular",
			input:          "configmap",
			wantGVR:        schema.GroupVersionResource{Version: "v1", Resource: "configmaps"},
			wantNamespaced: true,
		},
		{
			name:           "cluster-scoped core",
			input:          "node",
			wantGVR:        schema.GroupVersionResource{Version: "v1", Resource: "nodes"},
			wantNamespaced: false,
		},
		{
			name:           "cluster-scoped namespace",
			input:          "namespace",
			wantGVR:        schema.GroupVersionResource{Version: "v1", Resource: "namespaces"},
			wantNamespaced: false,
		},
		{
			name:           "non-core group",
			input:          "deployment",
			wantGVR:        schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
			wantNamespaced: true,
		},
		{
			name:           "group-qualified",
			input:          "deployments.apps",
			wantGVR:        schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
			wantNamespaced: true,
		},
		{
			name:           "CRD with multi-dot group",
			input:          "widgets.demo.example.com",
			wantGVR:        schema.GroupVersionResource{Group: "demo.example.com", Version: "v1", Resource: "widgets"},
			wantNamespaced: true,
		},
		{
			name:    "unknown kind",
			input:   "bogus",
			wantErr: true,
		},
		{
			name:    "empty input",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveResource(mapper, tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil; got=%+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.GVR != tt.wantGVR {
				t.Errorf("GVR = %v, want %v", got.GVR, tt.wantGVR)
			}
			if got.Namespaced != tt.wantNamespaced {
				t.Errorf("Namespaced = %v, want %v", got.Namespaced, tt.wantNamespaced)
			}
		})
	}
}
