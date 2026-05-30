package ai

import (
	"strings"
	"testing"
)

func TestPromptBackendRun(t *testing.T) {
	b, ok := Get(DefaultProvider)
	if !ok {
		t.Fatal("default backend not registered")
	}
	var sb strings.Builder
	req := Request{
		Kind:       "pod",
		Target:     "web-7f9",
		Namespace:  "shop",
		Verdict:    "Unhealthy",
		ReportYAML: "pod: web-7f9\nverdict: Unhealthy\n",
	}
	if err := b.Run(&sb, req); err != nil {
		t.Fatalf("Run: %v", err)
	}
	out := sb.String()
	for _, want := range []string{
		"sre-debug-v2",       // points at the skill (source of truth)
		"read-only",          // safety posture
		"kdiag troubleshoot", // advertises kdiag tooling
		"web-7f9",            // target
		"shop",               // namespace
		"verdict: Unhealthy", // embedded report
	} {
		if !strings.Contains(out, want) {
			t.Errorf("prompt missing %q\n---\n%s", want, out)
		}
	}
}
