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

func TestBashCompletion_HasPathAndYAML(t *testing.T) {
	s := readCompletionScript(t, "completions/kdiag.bash")
	for _, want := range []string{"--path", "--yaml"} {
		if !strings.Contains(s, want) {
			t.Errorf("bash script missing %q", want)
		}
	}
	for _, banned := range []string{"--yml", "--find-path", "--output", "--format"} {
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
		"view_seen=pods",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("bash script missing %q", want)
		}
	}
}

func TestZshCompletion_HasPathAndYAML(t *testing.T) {
	s := readCompletionScript(t, "completions/kdiag.zsh")
	for _, want := range []string{"--path", "--yaml"} {
		if !strings.Contains(s, want) {
			t.Errorf("zsh script missing %q", want)
		}
	}
	for _, banned := range []string{"--yml", "--find-path", "--output", "--format"} {
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
		"view_seen=pods",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("zsh script missing %q", want)
		}
	}
}

// `--pods` must reach the node completion path in both shells. bash is not
// kind-aware (one union for every kind), so a single occurrence suffices. zsh
// IS kind-aware: it needs `--pods` both in the pre-kind union AND in the
// per-kind `node)` kflags block — hence at least two occurrences. A drop to one
// means the node-block entry was removed and `inspect node -<TAB>` would stop
// offering `--pods`.
func TestCompletion_NodePodsFlag(t *testing.T) {
	bash := readCompletionScript(t, "completions/kdiag.bash")
	if !strings.Contains(bash, "--pods") {
		t.Errorf("bash script missing --pods")
	}
	zsh := readCompletionScript(t, "completions/kdiag.zsh")
	if n := strings.Count(zsh, "--pods["); n < 2 {
		t.Errorf("zsh script has %d --pods entries, want >=2 (union + node kflags block)", n)
	}
}

// `troubleshoot` is now its own top-level command (no longer an inspect view).
// Both shells must complete it as a command with its --ai flag, and must NOT
// advertise the removed `--troubleshoot` inspect flag anywhere.
func TestCompletion_TroubleshootCommand(t *testing.T) {
	for _, shell := range []string{"bash", "zsh"} {
		s := readCompletionScript(t, "completions/kdiag."+shell)
		if strings.Contains(s, "--troubleshoot") {
			t.Errorf("%s script still references the removed --troubleshoot inspect flag", shell)
		}
		if !strings.Contains(s, "troubleshoot)") {
			t.Errorf("%s script missing a troubleshoot command branch", shell)
		}
		if !strings.Contains(s, "--ai") {
			t.Errorf("%s script missing troubleshoot --ai flag", shell)
		}
	}
}

// Top-level completion must suggest only the primary commands. `completion`
// and `help` remain valid invocations but are hidden from `kdiag <TAB>`,
// matching the bare-banner split (`kdiag -h` shows the full list).
func TestBashCompletion_HidesMetaTopCommands(t *testing.T) {
	s := readCompletionScript(t, "completions/kdiag.bash")
	if !strings.Contains(s, `top_cmds="diff events inspect sort troubleshoot"`) {
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
