package ai

import "testing"

func TestGetDefaultBackend(t *testing.T) {
	// Empty provider resolves to the default paste-ready prompt backend.
	if _, ok := Get(""); !ok {
		t.Fatal("Get(\"\") should resolve to the default backend")
	}
	if _, ok := Get(DefaultProvider); !ok {
		t.Fatalf("Get(%q) should resolve", DefaultProvider)
	}
	if _, ok := Get("nope"); ok {
		t.Fatal("Get(\"nope\") should not resolve")
	}
}

func TestProviders(t *testing.T) {
	got := Providers()
	want := map[string]bool{"prompt": false, "claude": false, "gemini": false, "chatgpt": false}
	for _, p := range got {
		if _, ok := want[p]; ok {
			want[p] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("Providers() missing %q (got %v)", name, got)
		}
	}
	// Sorted for deterministic help output.
	for i := 1; i < len(got); i++ {
		if got[i-1] > got[i] {
			t.Errorf("Providers() not sorted: %v", got)
		}
	}
}
