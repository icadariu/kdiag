package cmd

import (
	"reflect"
	"testing"
)

func TestMakeMatcher_Semantics(t *testing.T) {
	cases := []struct {
		name     string
		hay      string
		needle   string
		smart    bool
		expected bool
	}{
		// Exact (no *): full-string match, smart-case.
		{"exact smart match", "Burstable", "burstable", true, true},
		{"exact no-substring", "namespace", "name", true, false},
		{"exact no-camel-substring", "imagePullPolicy", "imagepull", true, false},
		{"exact uppercase miss", "burstable", "Burstable", false, false},
		{"exact uppercase hit", "Burstable", "Burstable", false, true},
		// Glob (*): expands to .*, full-string match.
		{"glob suffix", "imagePullPolicy", "*imagepull*", true, true},
		{"glob prefix matches", "namespace", "name*", true, true},
		{"glob prefix anchored", "podname", "name*", true, false},
		{"glob substr both sides", "container-1-tiny", "*tiny*", true, true},
		// Empty needle.
		{"empty needle never matches", "anything", "", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			match := makeMatcher(tc.needle, tc.smart)
			if got := match(tc.hay); got != tc.expected {
				t.Fatalf("matcher(%q) for needle %q smart=%v = %v, want %v",
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

func TestWalkYMLPath_KeyMatch(t *testing.T) {
	// Mimic the shape of a pod's .status.qosClass: search by value.
	obj := map[string]any{
		"status": map[string]any{
			"qosClass": "Burstable",
			"phase":    "Running",
		},
	}
	got := walkYMLPath(obj, "", "", "Burstable", false)
	want := []string{".status.qosClass"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("walk by value: got %v, want %v", got, want)
	}
}

func TestWalkYMLPath_SmartCaseValue(t *testing.T) {
	obj := map[string]any{
		"status": map[string]any{
			"qosClass": "Burstable",
		},
	}
	// Lowercase needle → smart-case ON.
	got := walkYMLPath(obj, "", "", "burstable", true)
	want := []string{".status.qosClass"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("smart-case value match: got %v, want %v", got, want)
	}
}

func TestWalkYMLPath_KeyAndValueBoth(t *testing.T) {
	// The needle `qosClass` matches the key. `Burstable` matches the value.
	obj := map[string]any{
		"status": map[string]any{
			"qosClass": "Burstable",
		},
	}
	got := walkYMLPath(obj, "", "", "qosClass", false)
	want := []string{".status.qosClass"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("key match: got %v, want %v", got, want)
	}
}

func TestWalkYMLPath_ArrayWithNameAnnotation(t *testing.T) {
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
	got := walkYMLPath(obj, "", "", "*imagepull*", true)
	want := []string{
		"# name=app\n.spec.template.spec.containers[].imagePullPolicy",
		"# name=sidecar\n.spec.template.spec.containers[].imagePullPolicy",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("array w/ name annotation: got %v, want %v", got, want)
	}
}

func TestWalkYMLPath_ArrayWithoutNameField(t *testing.T) {
	// finalizers is []string with no name field → no annotation.
	obj := map[string]any{
		"metadata": map[string]any{
			"finalizers": []any{"foregroundDeletion"},
		},
	}
	got := walkYMLPath(obj, "", "", "finalizers", false)
	// Key match prints the array path (no value).
	want := []string{".metadata.finalizers"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("array w/o name: got %v, want %v", got, want)
	}
	// And a value-mode search inside the array should not get an annotation.
	gotVal := walkYMLPath(obj, "", "", "foregroundDeletion", false)
	wantVal := []string{".metadata.finalizers[]"}
	if !reflect.DeepEqual(gotVal, wantVal) {
		t.Fatalf("array w/o name (value match): got %v, want %v", gotVal, wantVal)
	}
}

func TestWalkYMLPath_BoolAndIntValues(t *testing.T) {
	obj := map[string]any{
		"spec": map[string]any{
			"hostNetwork":    true,
			"terminationGPS": int64(30),
		},
	}
	// Bool value match: needle "true" stringifies to match.
	gotBool := walkYMLPath(obj, "", "", "true", false)
	wantBool := []string{".spec.hostNetwork"}
	if !reflect.DeepEqual(gotBool, wantBool) {
		t.Fatalf("bool match: got %v, want %v", gotBool, wantBool)
	}
	// Int value match.
	gotInt := walkYMLPath(obj, "", "", "30", false)
	wantInt := []string{".spec.terminationGPS"}
	if !reflect.DeepEqual(gotInt, wantInt) {
		t.Fatalf("int match: got %v, want %v", gotInt, wantInt)
	}
}

func TestWalkYMLPath_NoMatch(t *testing.T) {
	obj := map[string]any{
		"status": map[string]any{
			"qosClass": "Burstable",
		},
	}
	got := walkYMLPath(obj, "", "", "Guaranteed", false)
	if len(got) != 0 {
		t.Fatalf("expected no matches, got %v", got)
	}
}

func TestWalkYMLPath_SortedMapKeys(t *testing.T) {
	// Two sibling keys both matching — order must be deterministic.
	obj := map[string]any{
		"zeta":  "match-me",
		"alpha": "match-me",
		"mid":   "match-me",
	}
	got := walkYMLPath(obj, "", "", "match-me", false)
	want := []string{
		".alpha",
		".mid",
		".zeta",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sorted keys: got %v, want %v", got, want)
	}
}

func TestWalkYMLPath_SingleNamedElementOmitsAnnotation(t *testing.T) {
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
	got := walkYMLPath(obj, "", "", "*imagepull*", true)
	want := []string{".spec.template.spec.containers[].imagePullPolicy"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("single named element: got %v, want %v", got, want)
	}
}

func TestWalkYMLPath_DedupIdenticalLines(t *testing.T) {
	// Unnamed array siblings that yield the same path collapse to one line.
	obj := map[string]any{
		"spec": map[string]any{
			"tolerations": []any{
				map[string]any{"operator": "Exists"},
				map[string]any{"operator": "Exists"},
				map[string]any{"operator": "Exists"},
			},
		},
	}
	got := walkYMLPath(obj, "", "", "Exists", false)
	want := []string{".spec.tolerations[].operator"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dedup identical lines: got %v, want %v", got, want)
	}
}

func TestWalkYMLPath_MultilineValueMatch(t *testing.T) {
	// ConfigMap-style multi-line value. The emitted output is the path only.
	obj := map[string]any{
		"data": map[string]any{
			"config": "line1\nline2\nline3",
		},
	}
	got := walkYMLPath(obj, "", "", "*line2*", false)
	want := []string{`.data.config`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("multiline value: got %v, want %v", got, want)
	}
}

func TestWalkYMLPath_DuplicateContainerNameDedups(t *testing.T) {
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
	got := walkYMLPath(obj, "", "", "*imagepull*", true)
	want := []string{"# name=app\n.spec.containers[].imagePullPolicy"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("duplicate container name: got %v, want %v", got, want)
	}
}

func TestWalkYMLPath_SkipsManagedFields(t *testing.T) {
	// .metadata.managedFields is SSA bookkeeping. Its synthetic `f:`/`k:`
	// keys mirror real field names ("image", "spec", "containers", ...) and
	// would otherwise dominate every --yml-path result.
	obj := map[string]any{
		"metadata": map[string]any{
			"managedFields": []any{
				map[string]any{
					"fieldsV1": map[string]any{
						"f:spec": map[string]any{
							"f:containers": map[string]any{
								`k:{"name":"c1"}`: map[string]any{
									"f:image": map[string]any{},
								},
							},
						},
					},
				},
			},
		},
		"spec": map[string]any{
			"containers": []any{
				map[string]any{"name": "c1", "image": "nginx:1.27"},
			},
		},
	}
	got := walkYMLPath(obj, "", "", "image", true)
	want := []string{".spec.containers[].image"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("managedFields not skipped: got %v, want %v", got, want)
	}
}

func TestWalkYMLPath_BracketKeyForSpecialChars(t *testing.T) {
	// An annotation key with slash/dot must be rendered with bracket syntax.
	obj := map[string]any{
		"metadata": map[string]any{
			"annotations": map[string]any{
				"deployment.kubernetes.io/revision": "3",
			},
		},
	}
	got := walkYMLPath(obj, "", "", "*revision*", false)
	want := `.metadata.annotations["deployment.kubernetes.io/revision"]`
	if len(got) != 1 || got[0] != want {
		t.Fatalf("special-char key: got %v, want %q", got, want)
	}
}
