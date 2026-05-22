package cmd

import (
	"reflect"
	"strings"
	"testing"
)

func TestSubstrMatch_SmartCase(t *testing.T) {
	cases := []struct {
		name     string
		hay      string
		needle   string
		smart    bool
		expected bool
	}{
		{"lowercase needle matches uppercase value", "Burstable", "burstable", true, true},
		{"lowercase needle partial match", "imagePullPolicy", "imagepull", true, true},
		{"uppercase needle case-sensitive miss", "burstable", "Burstable", false, false},
		{"uppercase needle case-sensitive hit", "Burstable", "Burstable", false, true},
		{"empty needle never matches", "anything", "", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := substrMatch(tc.hay, tc.needle, tc.smart); got != tc.expected {
				t.Fatalf("substrMatch(%q, %q, smart=%v) = %v, want %v",
					tc.hay, tc.needle, tc.smart, got, tc.expected)
			}
		})
	}
}

func TestIsAllLower(t *testing.T) {
	cases := map[string]bool{
		"":                true,
		"burstable":       true,
		"image-pull":      true,
		"123":             true,
		"Burstable":       false,
		"imagePullPolicy": false,
	}
	for in, want := range cases {
		if got := isAllLower(in); got != want {
			t.Errorf("isAllLower(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestFormatKeyPath(t *testing.T) {
	cases := map[string]string{
		"spec":                                ".spec",
		"qosClass":                            ".qosClass",
		"image-pull-policy":                   `["image-pull-policy"]`,
		"kubectl.kubernetes.io/last-applied":  `["kubectl.kubernetes.io/last-applied"]`,
		"_underscoreStart":                    "._underscoreStart",
		"0starts-with-digit":                  `["0starts-with-digit"]`,
	}
	for in, want := range cases {
		if got := formatKeyPath(in); got != want {
			t.Errorf("formatKeyPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWalkYAMLField_KeyMatch(t *testing.T) {
	// Mimic the shape of a pod's .status.qosClass: search by value.
	obj := map[string]any{
		"status": map[string]any{
			"qosClass": "Burstable",
			"phase":    "Running",
		},
	}
	got := walkYAMLField(obj, "", "", "Burstable", false)
	want := []string{".status.qosClass: Burstable"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("walk by value: got %v, want %v", got, want)
	}
}

func TestWalkYAMLField_SmartCaseValue(t *testing.T) {
	obj := map[string]any{
		"status": map[string]any{
			"qosClass": "Burstable",
		},
	}
	// Lowercase needle → smart-case ON.
	got := walkYAMLField(obj, "", "", "burstable", true)
	want := []string{".status.qosClass: Burstable"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("smart-case value match: got %v, want %v", got, want)
	}
}

func TestWalkYAMLField_KeyAndValueBoth(t *testing.T) {
	// The needle `qosClass` matches the key. `Burstable` matches the value.
	obj := map[string]any{
		"status": map[string]any{
			"qosClass": "Burstable",
		},
	}
	got := walkYAMLField(obj, "", "", "qosClass", false)
	want := []string{".status.qosClass: Burstable"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("key match: got %v, want %v", got, want)
	}
}

func TestWalkYAMLField_ArrayWithNameAnnotation(t *testing.T) {
	// Mimic .spec.template.spec.containers[].imagePullPolicy.
	obj := map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"containers": []any{
						map[string]any{
							"name":            "app",
							"imagePullPolicy": "IfNotPresent",
						},
						map[string]any{
							"name":            "sidecar",
							"imagePullPolicy": "Always",
						},
					},
				},
			},
		},
	}
	got := walkYAMLField(obj, "", "", "imagepull", true)
	want := []string{
		"# name=app\n.spec.template.spec.containers[].imagePullPolicy: IfNotPresent",
		"# name=sidecar\n.spec.template.spec.containers[].imagePullPolicy: Always",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("array w/ name annotation: got %v, want %v", got, want)
	}
}

func TestWalkYAMLField_ArrayWithoutNameField(t *testing.T) {
	// finalizers is []string with no name field → no annotation.
	obj := map[string]any{
		"metadata": map[string]any{
			"finalizers": []any{"foregroundDeletion"},
		},
	}
	got := walkYAMLField(obj, "", "", "finalizers", false)
	// Key match prints the array value as "<array>".
	want := []string{".metadata.finalizers: <array>"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("array w/o name: got %v, want %v", got, want)
	}
	// And a value-mode search inside the array should not get an annotation.
	gotVal := walkYAMLField(obj, "", "", "foregroundDeletion", false)
	wantVal := []string{".metadata.finalizers[]: foregroundDeletion"}
	if !reflect.DeepEqual(gotVal, wantVal) {
		t.Fatalf("array w/o name (value match): got %v, want %v", gotVal, wantVal)
	}
}

func TestWalkYAMLField_BoolAndIntValues(t *testing.T) {
	obj := map[string]any{
		"spec": map[string]any{
			"hostNetwork":    true,
			"terminationGPS": int64(30),
		},
	}
	// Bool value match: needle "true" stringifies to match.
	gotBool := walkYAMLField(obj, "", "", "true", false)
	wantBool := []string{".spec.hostNetwork: true"}
	if !reflect.DeepEqual(gotBool, wantBool) {
		t.Fatalf("bool match: got %v, want %v", gotBool, wantBool)
	}
	// Int value match.
	gotInt := walkYAMLField(obj, "", "", "30", false)
	wantInt := []string{".spec.terminationGPS: 30"}
	if !reflect.DeepEqual(gotInt, wantInt) {
		t.Fatalf("int match: got %v, want %v", gotInt, wantInt)
	}
}

func TestWalkYAMLField_NoMatch(t *testing.T) {
	obj := map[string]any{
		"status": map[string]any{
			"qosClass": "Burstable",
		},
	}
	got := walkYAMLField(obj, "", "", "Guaranteed", false)
	if len(got) != 0 {
		t.Fatalf("expected no matches, got %v", got)
	}
}

func TestWalkYAMLField_SortedMapKeys(t *testing.T) {
	// Two sibling keys both matching — order must be deterministic.
	obj := map[string]any{
		"zeta":  "match-me",
		"alpha": "match-me",
		"mid":   "match-me",
	}
	got := walkYAMLField(obj, "", "", "match-me", false)
	want := []string{
		".alpha: match-me",
		".mid: match-me",
		".zeta: match-me",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sorted keys: got %v, want %v", got, want)
	}
}

func TestWalkYAMLField_SingleNamedElementOmitsAnnotation(t *testing.T) {
	// Single-container case: no name annotation — nothing to disambiguate.
	obj := map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"containers": []any{
						map[string]any{
							"name":            "app",
							"imagePullPolicy": "IfNotPresent",
						},
					},
				},
			},
		},
	}
	got := walkYAMLField(obj, "", "", "imagepull", true)
	want := []string{".spec.template.spec.containers[].imagePullPolicy: IfNotPresent"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("single named element: got %v, want %v", got, want)
	}
}

func TestWalkYAMLField_DedupIdenticalLines(t *testing.T) {
	// Unnamed array siblings that yield the same path+value collapse to one line.
	obj := map[string]any{
		"spec": map[string]any{
			"tolerations": []any{
				map[string]any{"operator": "Exists"},
				map[string]any{"operator": "Exists"},
				map[string]any{"operator": "Exists"},
			},
		},
	}
	got := walkYAMLField(obj, "", "", "Exists", false)
	want := []string{".spec.tolerations[].operator: Exists"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dedup identical lines: got %v, want %v", got, want)
	}
}

func TestWalkYAMLField_MultilineValueQuoted(t *testing.T) {
	// ConfigMap-style multi-line value. The emitted line must stay on one
	// physical line so it doesn't bleed into the next match — Go-quote it.
	obj := map[string]any{
		"data": map[string]any{
			"config": "line1\nline2\nline3",
		},
	}
	got := walkYAMLField(obj, "", "", "line2", false)
	want := []string{`.data.config: "line1\nline2\nline3"`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("multiline value: got %v, want %v", got, want)
	}
}

func TestWalkYAMLField_DuplicateContainerNameDedups(t *testing.T) {
	// Two containers with the same name (invalid k8s, but the walker should
	// not crash on Unstructured input that happens to carry it). The dedup
	// path collapses identical blocks; this test locks that behavior in so a
	// later refactor doesn't accidentally produce a panic or different output.
	obj := map[string]any{
		"spec": map[string]any{
			"containers": []any{
				map[string]any{"name": "app", "imagePullPolicy": "IfNotPresent"},
				map[string]any{"name": "app", "imagePullPolicy": "IfNotPresent"},
			},
		},
	}
	got := walkYAMLField(obj, "", "", "imagepull", true)
	want := []string{"# name=app\n.spec.containers[].imagePullPolicy: IfNotPresent"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("duplicate container name: got %v, want %v", got, want)
	}
}

func TestWalkYAMLField_BracketKeyForSpecialChars(t *testing.T) {
	// An annotation key with slash/dot must be rendered with bracket syntax.
	obj := map[string]any{
		"metadata": map[string]any{
			"annotations": map[string]any{
				"deployment.kubernetes.io/revision": "3",
			},
		},
	}
	got := walkYAMLField(obj, "", "", "revision", false)
	wantPrefix := `.metadata.annotations["deployment.kubernetes.io/revision"]: 3`
	if len(got) != 1 || !strings.HasPrefix(got[0], wantPrefix) {
		t.Fatalf("special-char key: got %v, want prefix %q", got, wantPrefix)
	}
}
