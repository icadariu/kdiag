package cmd

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// TestStripPodNoise verifies that Pod status and node-specific fields are stripped.
func TestStripPodNoise(t *testing.T) {
	obj := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]any{
				"name":      "test-pod",
				"namespace": "default",
				"labels": map[string]any{
					"app":              "myapp",
					"pod-template-hash": "abc123",
				},
				"annotations": map[string]any{
					"some-key": "some-value",
				},
				"ownerReferences": []any{
					map[string]any{"kind": "ReplicaSet", "name": "my-rs"},
				},
				"generateName": "test-pod-",
			},
			"spec": map[string]any{
				"nodeName": "node-1",
				"containers": []any{
					map[string]any{"name": "app", "image": "app:v1"},
				},
				"tolerations": []any{
					map[string]any{
						"key":      "node.kubernetes.io/not-ready",
						"operator": "Exists",
					},
					map[string]any{
						"key":      "custom-key",
						"operator": "Equal",
						"value":    "custom-value",
					},
				},
			},
			"status": map[string]any{
				"podIP": "10.0.0.1",
				"phase": "Running",
			},
		},
	}

	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}
	stripKindNoise(&obj, gvk)

	// Pod status should be stripped.
	if _, found, _ := unstructured.NestedMap(obj.Object, "status"); found {
		t.Errorf("expected status to be stripped, but found it")
	}

	// spec.nodeName should be stripped.
	if _, found, _ := unstructured.NestedString(obj.Object, "spec", "nodeName"); found {
		t.Errorf("expected spec.nodeName to be stripped, but found it")
	}

	// pod-template-hash label should be stripped.
	labels, _, _ := unstructured.NestedStringMap(obj.Object, "metadata", "labels")
	if _, exists := labels["pod-template-hash"]; exists {
		t.Errorf("expected pod-template-hash label to be stripped")
	}

	// Regular app label should be retained.
	if _, exists := labels["app"]; !exists {
		t.Errorf("expected app label to be retained")
	}

	// Annotations should be stripped entirely.
	if _, found, _ := unstructured.NestedMap(obj.Object, "metadata", "annotations"); found {
		t.Errorf("expected metadata.annotations to be stripped")
	}

	// ownerReferences should be stripped.
	if _, found, _ := unstructured.NestedSlice(obj.Object, "metadata", "ownerReferences"); found {
		t.Errorf("expected metadata.ownerReferences to be stripped")
	}

	// generateName should be stripped.
	if _, found, _ := unstructured.NestedString(obj.Object, "metadata", "generateName"); found {
		t.Errorf("expected metadata.generateName to be stripped")
	}

	// name should be stripped.
	if _, found, _ := unstructured.NestedString(obj.Object, "metadata", "name"); found {
		t.Errorf("expected metadata.name to be stripped")
	}

	// Custom toleration should be retained.
	if spec, ok := obj.Object["spec"].(map[string]any); ok {
		if tolerations, ok := spec["tolerations"].([]any); ok {
			if len(tolerations) != 1 {
				t.Errorf("expected 1 toleration (custom), got %d", len(tolerations))
			} else {
				tol := tolerations[0].(map[string]any)
				if tol["key"] != "custom-key" {
					t.Errorf("expected custom toleration to be retained")
				}
			}
		} else {
			t.Errorf("expected tolerations to be a slice")
		}
	}
}

// TestStripPodNoise_SATokenVolume verifies that the auto-injected
// projected service-account token volume (kube-api-access-<random>) and
// its matching volumeMounts on every container are stripped, while user
// volumes/mounts are retained.
func TestStripPodNoise_SATokenVolume(t *testing.T) {
	obj := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"spec": map[string]any{
				"containers": []any{
					map[string]any{
						"name":  "app",
						"image": "app:v1",
						"volumeMounts": []any{
							map[string]any{"name": "kube-api-access-hsjxd", "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount"},
							map[string]any{"name": "user-config", "mountPath": "/etc/config"},
						},
					},
				},
				"initContainers": []any{
					map[string]any{
						"name":  "init",
						"image": "init:v1",
						"volumeMounts": []any{
							map[string]any{"name": "kube-api-access-hsjxd", "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount"},
						},
					},
				},
				"volumes": []any{
					map[string]any{"name": "kube-api-access-hsjxd", "projected": map[string]any{}},
					map[string]any{"name": "user-config", "configMap": map[string]any{"name": "cm"}},
				},
			},
		},
	}

	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}
	stripKindNoise(&obj, gvk)

	volumes, _, _ := unstructured.NestedSlice(obj.Object, "spec", "volumes")
	if len(volumes) != 1 {
		t.Fatalf("expected 1 volume after strip (user-config), got %d", len(volumes))
	}
	if name, _ := volumes[0].(map[string]any)["name"].(string); name != "user-config" {
		t.Errorf("expected user-config to remain, got %q", name)
	}

	containers, _, _ := unstructured.NestedSlice(obj.Object, "spec", "containers")
	mounts, _ := containers[0].(map[string]any)["volumeMounts"].([]any)
	if len(mounts) != 1 {
		t.Fatalf("expected 1 mount on app container, got %d", len(mounts))
	}
	if name, _ := mounts[0].(map[string]any)["name"].(string); name != "user-config" {
		t.Errorf("expected user-config mount to remain, got %q", name)
	}

	// initContainer had only the auto-injected mount — volumeMounts key
	// should be removed entirely on that container.
	initContainers, _, _ := unstructured.NestedSlice(obj.Object, "spec", "initContainers")
	if _, ok := initContainers[0].(map[string]any)["volumeMounts"]; ok {
		t.Errorf("expected initContainers[0].volumeMounts to be removed (all entries auto-injected)")
	}
}

// TestStripWorkloadNoise verifies that workload status and revision annotations are stripped.
func TestStripWorkloadNoise(t *testing.T) {
	obj := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]any{
				"name":      "my-deploy",
				"namespace": "default",
			},
			"spec": map[string]any{
				"replicas": 2,
				"selector": map[string]any{
					"matchLabels": map[string]any{
						"app":              "myapp",
						"pod-template-hash": "xyz789",
					},
				},
				"template": map[string]any{
					"metadata": map[string]any{
						"labels": map[string]any{
							"app":              "myapp",
							"pod-template-hash": "xyz789",
						},
						"creationTimestamp": nil,
					},
					"spec": map[string]any{
						"containers": []any{
							map[string]any{"name": "app", "image": "app:v2"},
						},
					},
				},
			},
			"status": map[string]any{
				"replicas":      2,
				"readyReplicas": 2,
			},
		},
	}

	gvk := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	stripKindNoise(&obj, gvk)

	// Status should be stripped.
	if _, found, _ := unstructured.NestedMap(obj.Object, "status"); found {
		t.Errorf("expected status to be stripped")
	}

	// spec.replicas should be retained.
	if spec, ok := obj.Object["spec"].(map[string]any); ok {
		if replicas, exists := spec["replicas"]; !exists || replicas != 2 {
			t.Errorf("expected spec.replicas to be retained, got %v", replicas)
		}
	} else {
		t.Errorf("expected spec to be a map")
	}

	// pod-template-hash should be stripped from selector.
	matchLabels, _, _ := unstructured.NestedStringMap(obj.Object, "spec", "selector", "matchLabels")
	if _, exists := matchLabels["pod-template-hash"]; exists {
		t.Errorf("expected pod-template-hash to be stripped from matchLabels")
	}
	if _, exists := matchLabels["app"]; !exists {
		t.Errorf("expected app label to be retained in matchLabels")
	}

	// pod-template-hash should be stripped from template labels.
	templateLabels, _, _ := unstructured.NestedStringMap(obj.Object, "spec", "template", "metadata", "labels")
	if _, exists := templateLabels["pod-template-hash"]; exists {
		t.Errorf("expected pod-template-hash to be stripped from template labels")
	}
	if _, exists := templateLabels["app"]; !exists {
		t.Errorf("expected app label to be retained in template labels")
	}

	// spec.template.metadata.creationTimestamp should be stripped.
	if _, found, _ := unstructured.NestedString(obj.Object, "spec", "template", "metadata", "creationTimestamp"); found {
		t.Errorf("expected spec.template.metadata.creationTimestamp to be stripped")
	}
}

// TestStripServiceNoise verifies that Service status and cluster-assigned fields are stripped.
func TestStripServiceNoise(t *testing.T) {
	obj := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Service",
			"metadata": map[string]any{
				"name":      "my-svc",
				"namespace": "default",
			},
			"spec": map[string]any{
				"clusterIP":          "10.0.0.1",
				"clusterIPs":         []any{"10.0.0.1"},
				"ipFamilies":         []any{"IPv4"},
				"ipFamilyPolicy":     "SingleStack",
				"internalTrafficPolicy": "Cluster",
				"type":               "ClusterIP",
				"ports": []any{
					map[string]any{
						"name":       "http",
						"port":       80,
						"targetPort": 8080,
						"nodePort":   30000,
					},
				},
				"selector": map[string]any{
					"app": "myapp",
				},
			},
			"status": map[string]any{
				"loadBalancer": map[string]any{},
			},
		},
	}

	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Service"}
	stripKindNoise(&obj, gvk)

	// Status should be stripped.
	if _, found, _ := unstructured.NestedMap(obj.Object, "status"); found {
		t.Errorf("expected status to be stripped")
	}

	// Cluster-assigned fields should be stripped.
	if spec, ok := obj.Object["spec"].(map[string]any); ok {
		for _, field := range []string{"clusterIP", "clusterIPs", "ipFamilies", "ipFamilyPolicy", "internalTrafficPolicy"} {
			if _, exists := spec[field]; exists {
				t.Errorf("expected spec.%s to be stripped", field)
			}
		}
	}

	// Selector should be retained.
	if _, found, _ := unstructured.NestedMap(obj.Object, "spec", "selector"); !found {
		t.Errorf("expected spec.selector to be retained")
	}

	// Port and targetPort should be retained.
	if spec, ok := obj.Object["spec"].(map[string]any); ok {
		if ports, ok := spec["ports"].([]any); ok {
			if len(ports) != 1 {
				t.Errorf("expected 1 port entry, got %d", len(ports))
			} else {
				port := ports[0].(map[string]any)
				if _, exists := port["port"]; !exists {
					t.Errorf("expected port to be retained")
				}
				if _, exists := port["targetPort"]; !exists {
					t.Errorf("expected targetPort to be retained")
				}
				if _, exists := port["nodePort"]; exists {
					t.Errorf("expected nodePort to be stripped")
				}
			}
		}
	}
}

// TestStripConfigMapNoise verifies that only baseline stripping is applied to ConfigMap.
func TestStripConfigMapNoise(t *testing.T) {
	obj := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":              "my-config",
				"namespace":         "default",
				"resourceVersion":   "12345",
				"uid":               "abc-def",
				"generation":        1,
				"creationTimestamp": "2021-01-01T00:00:00Z",
				"managedFields": []any{
					map[string]any{"manager": "kubectl"},
				},
				"annotations": map[string]any{
					"some-key": "some-value",
				},
			},
			"data": map[string]any{
				"foo": "bar",
			},
		},
	}

	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	stripKindNoise(&obj, gvk)

	// Baseline etcd fields should be stripped.
	meta, _, _ := unstructured.NestedMap(obj.Object, "metadata")
	for _, field := range []string{"resourceVersion", "uid", "generation", "creationTimestamp", "managedFields"} {
		if _, exists := meta[field]; exists {
			t.Errorf("expected metadata.%s to be stripped (baseline)", field)
		}
	}

	// data should be retained.
	data, _, _ := unstructured.NestedMap(obj.Object, "data")
	if data["foo"] != "bar" {
		t.Errorf("expected data.foo to be retained")
	}

	// Regular annotations should be retained (no kind-specific stripping for ConfigMap).
	if _, found, _ := unstructured.NestedMap(obj.Object, "metadata", "annotations"); !found {
		t.Errorf("expected metadata.annotations to be retained for ConfigMap")
	}
}

// TestStripNodeNoise verifies that Node status is stripped.
func TestStripNodeNoise(t *testing.T) {
	obj := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Node",
			"metadata": map[string]any{
				"name": "node-1",
				"labels": map[string]any{
					"zone": "us-west-1a",
				},
			},
			"spec": map[string]any{
				"unschedulable": false,
				"taints": []any{
					map[string]any{
						"key":    "gpu",
						"value":  "true",
						"effect": "NoSchedule",
					},
				},
			},
			"status": map[string]any{
				"capacity": map[string]any{
					"cpu":    "4",
					"memory": "8Gi",
				},
				"conditions": []any{
					map[string]any{
						"type":   "Ready",
						"status": "True",
					},
				},
			},
		},
	}

	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Node"}
	stripKindNoise(&obj, gvk)

	// Status should be stripped.
	if _, found, _ := unstructured.NestedMap(obj.Object, "status"); found {
		t.Errorf("expected status to be stripped")
	}

	// spec should be retained.
	if _, found, _ := unstructured.NestedMap(obj.Object, "spec"); !found {
		t.Errorf("expected spec to be retained")
	}

	// Labels should be retained.
	labels, _, _ := unstructured.NestedStringMap(obj.Object, "metadata", "labels")
	if labels["zone"] != "us-west-1a" {
		t.Errorf("expected zone label to be retained")
	}
}

// TestStripPVCNoise verifies that PVC status and provisioner annotations are stripped.
func TestStripPVCNoise(t *testing.T) {
	obj := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "PersistentVolumeClaim",
			"metadata": map[string]any{
				"name":      "my-pvc",
				"namespace": "default",
				"annotations": map[string]any{
					"volume.kubernetes.io/storage-provisioner": "ebs.csi.aws.com",
					"pv.kubernetes.io/bind-completed":          "yes",
					"custom-annotation":                        "custom-value",
				},
			},
			"spec": map[string]any{
				"accessModes": []any{"ReadWriteOnce"},
				"volumeName":  "pv-12345",
				"resources": map[string]any{
					"requests": map[string]any{"storage": "10Gi"},
				},
			},
			"status": map[string]any{
				"phase": "Bound",
			},
		},
	}

	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "PersistentVolumeClaim"}
	stripKindNoise(&obj, gvk)

	// Status should be stripped.
	if _, found, _ := unstructured.NestedMap(obj.Object, "status"); found {
		t.Errorf("expected status to be stripped")
	}

	// spec.volumeName should be stripped.
	if _, found, _ := unstructured.NestedString(obj.Object, "spec", "volumeName"); found {
		t.Errorf("expected spec.volumeName to be stripped")
	}

	// Provisioner annotations should be stripped (if they exist).
	annos, exists, _ := unstructured.NestedStringMap(obj.Object, "metadata", "annotations")
	if exists {
		for _, key := range []string{
			"volume.kubernetes.io/storage-provisioner",
			"pv.kubernetes.io/bind-completed",
		} {
			if _, found := annos[key]; found {
				t.Errorf("expected %s annotation to be stripped", key)
			}
		}
	}

	// Custom annotation should be retained.
	if _, exists := annos["custom-annotation"]; !exists {
		t.Errorf("expected custom-annotation to be retained")
	}

	// accessModes should be retained.
	if _, found, _ := unstructured.NestedSlice(obj.Object, "spec", "accessModes"); !found {
		t.Errorf("expected spec.accessModes to be retained")
	}
}

// TestUnknownKindGetsBaselineOnly verifies that unknown kinds only get baseline stripping.
func TestUnknownKindGetsBaselineOnly(t *testing.T) {
	obj := unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "example.com/v1",
			"kind":       "Widget",
			"metadata": map[string]any{
				"name":            "my-widget",
				"namespace":       "default",
				"resourceVersion": "12345",
				"uid":             "abc-def",
				"managedFields": []any{
					map[string]any{"manager": "kubectl"},
				},
			},
			"spec": map[string]any{
				"foo": "bar",
			},
			"status": map[string]any{
				"phase": "Active",
			},
		},
	}

	gvk := schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "Widget"}
	stripKindNoise(&obj, gvk)

	// Baseline fields should be stripped.
	objMeta, _, _ := unstructured.NestedMap(obj.Object, "metadata")
	for _, field := range []string{"resourceVersion", "uid", "managedFields"} {
		if _, exists := objMeta[field]; exists {
			t.Errorf("expected metadata.%s to be stripped (baseline)", field)
		}
	}

	// spec should be retained.
	spec, _, _ := unstructured.NestedMap(obj.Object, "spec")
	if spec["foo"] != "bar" {
		t.Errorf("expected spec.foo to be retained")
	}

	// status should be retained (no kind-specific stripping for unknown kinds).
	if _, found, _ := unstructured.NestedMap(obj.Object, "status"); !found {
		t.Errorf("expected status to be retained for unknown kind")
	}
}
