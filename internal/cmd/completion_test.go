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

func TestBashCompletion_HasPathAndOutput(t *testing.T) {
	s := readCompletionScript(t, "completions/kdiag.bash")
	for _, want := range []string{"--path", "--output"} {
		if !strings.Contains(s, want) {
			t.Errorf("bash script missing %q", want)
		}
	}
	for _, banned := range []string{"--yml-path", "--find-path", "--yaml", "--format"} {
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

func TestZshCompletion_HasPathAndOutput(t *testing.T) {
	s := readCompletionScript(t, "completions/kdiag.zsh")
	for _, want := range []string{"--path", "--output"} {
		if !strings.Contains(s, want) {
			t.Errorf("zsh script missing %q", want)
		}
	}
	for _, banned := range []string{"--yml-path", "--find-path", "--yaml", "--format"} {
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

// Top-level completion must suggest only the primary commands. `completion`
// and `help` remain valid invocations but are hidden from `kdiag <TAB>`,
// matching the bare-banner split (`kdiag -h` shows the full list).
func TestBashCompletion_HidesMetaTopCommands(t *testing.T) {
	s := readCompletionScript(t, "completions/kdiag.bash")
	if !strings.Contains(s, `top_cmds="diff events inspect sort"`) {
		t.Errorf("bash top_cmds should list only primary commands, got:\n%s",
			grepLine(s, "top_cmds="))
	}
}

func TestZshCompletion_HidesMetaTopCommands(t *testing.T) {
	s := readCompletionScript(t, "completions/kdiag.zsh")
	// Each meta command appears in script text (top_cmds definition, help_topics,
	// dispatch case branches). The guard here is that the `top_cmds=` array
	// itself does not enumerate them.
	for _, banned := range []string{
		"'completion:Generate shell completion",
		"'help:Show help for a command",
	} {
		if strings.Contains(s, banned) {
			t.Errorf("zsh top_cmds still advertises meta command: %q", banned)
		}
	}
}

func grepLine(s, needle string) string {
	for ln := range strings.SplitSeq(s, "\n") {
		if strings.Contains(ln, needle) {
			return ln
		}
	}
	return ""
}
