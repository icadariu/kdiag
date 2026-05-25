package cmd

import (
	"strings"
	"testing"
)

func readCompletionScript(t *testing.T, path string) string {
	t.Helper()
	data, err := completionScripts.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestBashCompletion_HasPathAndFormat(t *testing.T) {
	s := readCompletionScript(t, "completions/kdiag.bash")
	for _, want := range []string{"--path", "--format"} {
		if !strings.Contains(s, want) {
			t.Errorf("bash script missing %q", want)
		}
	}
	for _, banned := range []string{"--yml-path", "--find-path", "--yaml"} {
		if strings.Contains(s, banned) {
			t.Errorf("bash script still references %q", banned)
		}
	}
}

func TestBashCompletion_ViewFlagFiltering(t *testing.T) {
	s := readCompletionScript(t, "completions/kdiag.bash")
	for _, want := range []string{
		"view_seen=path",
		"view_seen=resources",
		"view_seen=spec",
		"view_seen=az",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("bash script missing %q", want)
		}
	}
}

func TestZshCompletion_HasPathAndFormat(t *testing.T) {
	s := readCompletionScript(t, "completions/kdiag.zsh")
	for _, want := range []string{"--path", "--format"} {
		if !strings.Contains(s, want) {
			t.Errorf("zsh script missing %q", want)
		}
	}
	for _, banned := range []string{"--yml-path", "--find-path", "--yaml"} {
		if strings.Contains(s, banned) {
			t.Errorf("zsh script still references %q", banned)
		}
	}
}

func TestZshCompletion_ViewFlagFiltering(t *testing.T) {
	s := readCompletionScript(t, "completions/kdiag.zsh")
	for _, want := range []string{
		"view_seen=path",
		"view_seen=resources",
		"view_seen=spec",
		"view_seen=az",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("zsh script missing %q", want)
		}
	}
}
