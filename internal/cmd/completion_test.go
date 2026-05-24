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

func TestBashCompletion_HasYMLPath(t *testing.T) {
	s := readCompletionScript(t, "completions/kdiag.bash")
	if !strings.Contains(s, "--yml-path") {
		t.Error("bash script missing --yml-path")
	}
	if strings.Contains(s, "--find-path") {
		t.Error("bash script still references --find-path")
	}
}

func TestBashCompletion_ViewFlagFiltering(t *testing.T) {
	s := readCompletionScript(t, "completions/kdiag.bash")
	for _, want := range []string{
		"view_seen=ymlpath",
		"view_seen=resources",
		"view_seen=spec",
		"view_seen=az",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("bash script missing %q", want)
		}
	}
}

func TestZshCompletion_HasYMLPath(t *testing.T) {
	s := readCompletionScript(t, "completions/kdiag.zsh")
	if !strings.Contains(s, "--yml-path") {
		t.Error("zsh script missing --yml-path")
	}
	if strings.Contains(s, "--find-path") {
		t.Error("zsh script still references --find-path")
	}
}

func TestZshCompletion_ViewFlagFiltering(t *testing.T) {
	s := readCompletionScript(t, "completions/kdiag.zsh")
	for _, want := range []string{
		"view_seen=ymlpath",
		"view_seen=resources",
		"view_seen=spec",
		"view_seen=az",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("zsh script missing %q", want)
		}
	}
}
