package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestWantsHelp(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{nil, false},
		{[]string{}, false},
		{[]string{"foo"}, false},
		{[]string{"-h"}, true},
		{[]string{"--help"}, true},
		{[]string{"help"}, true},
		// Only the first arg counts. A pod named "help" is unlikely but the
		// dispatcher cares about positional intent, so this is intentional.
		{[]string{"foo", "--help"}, false},
	}
	for _, c := range cases {
		if got := WantsHelp(c.args); got != c.want {
			t.Errorf("WantsHelp(%v) = %v, want %v", c.args, got, c.want)
		}
	}
}

// Root help in full mode must NOT enumerate each kind. That bloat is the
// regression we're guarding against. It must list every command — the only
// auxiliary command left is `completion`. `--version` is a flag, not a
// subcommand, so it does not appear in either help mode. The branded
// title line "kdiag — Kubernetes diagnostic CLI" belongs to the help
// screen only (gated on full).
func TestPrintRootUsage_Full(t *testing.T) {
	var buf bytes.Buffer
	PrintRootUsage(&buf, true)
	out := buf.String()

	for _, want := range []string{
		"kdiag — Kubernetes diagnostic CLI",
		"inspect", "diff", "events", "completion",
		"kdiag <command> -h",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("PrintRootUsage(full) missing %q\n%s", want, out)
		}
	}
	// `version` is no longer a subcommand — must not appear in help.
	if strings.Contains(out, "version") {
		t.Errorf("PrintRootUsage(full) should not contain 'version' (flag, not subcommand)\n%s", out)
	}
	// az pods must not appear at root level — functionality is under inspect --az.
	if strings.Contains(out, "az pods") {
		t.Errorf("PrintRootUsage(full) should not contain 'az pods'\n%s", out)
	}
	// Per-kind descriptions must not appear at the root level.
	for _, banned := range []string{
		"Show container state for all pods in a deployment",
		"Show container state for all pods in a daemonset",
		"Show container state for all pods in a statefulset",
		"Show container state for all pods in a replicaset",
		"Show zone for one node",
	} {
		if strings.Contains(out, banned) {
			t.Errorf("PrintRootUsage(full) should not contain per-kind line %q\n%s", banned, out)
		}
	}
}

// Compact mode is the error-fallback (no-args, unknown command). It hides
// the `completion` aux command and — per user feedback — must NOT print
// the branded title line. Errors should stay terse; the title belongs on
// the help screen only.
func TestPrintRootUsage_Compact_HidesAuxCommands(t *testing.T) {
	var buf bytes.Buffer
	PrintRootUsage(&buf, false)
	out := buf.String()

	for _, want := range []string{
		"inspect", "diff", "events", "kdiag <command> -h",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("PrintRootUsage(compact) missing %q\n%s", want, out)
		}
	}
	for _, banned := range []string{
		"completion",
		"kdiag — Kubernetes diagnostic CLI",
	} {
		if strings.Contains(out, banned) {
			t.Errorf("PrintRootUsage(compact) should not contain %q\n%s", banned, out)
		}
	}
}

func TestPrintInspectUsage(t *testing.T) {
	var buf bytes.Buffer
	PrintInspectUsage(&buf, nil)
	out := buf.String()

	for _, want := range []string{
		"pod", "deploy", "ds", "sts", "rs", "node",
		"Usage:", "Examples:", "kdiag inspect <subcommand> -h",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("PrintInspectUsage missing %q\n%s", want, out)
		}
	}
}


func TestPrintDiffUsage(t *testing.T) {
	var buf bytes.Buffer
	PrintDiffUsage(&buf)
	out := buf.String()

	for _, want := range []string{"rs", "Examples:", "--full", "kdiag diff rs -h"} {
		if !strings.Contains(out, want) {
			t.Errorf("PrintDiffUsage missing %q\n%s", want, out)
		}
	}
	// Should not contain old flag name
	if strings.Contains(out, "--full-diff") {
		t.Errorf("PrintDiffUsage should not contain --full-diff\n%s", out)
	}
}

func TestPrintDiffKindUsage(t *testing.T) {
	var buf bytes.Buffer
	PrintDiffKindUsage(&buf, "pod")
	out := buf.String()

	for _, want := range []string{"pod", "Usage:", "--full", "-n", "resource-a resource-b"} {
		if !strings.Contains(out, want) {
			t.Errorf("PrintDiffKindUsage(pod) missing %q\n%s", want, out)
		}
	}
	// Should not contain old flag name
	if strings.Contains(out, "--full-diff") {
		t.Errorf("PrintDiffKindUsage(pod) should not contain --full-diff\n%s", out)
	}

	// Pod help should not mention rs revision-diff
	for _, banned := range []string{"revision", "replicaset", "-l <label>", "deployment-name"} {
		if strings.Contains(out, banned) {
			t.Errorf("PrintDiffKindUsage(pod) should not contain %q\n%s", banned, out)
		}
	}
}

func TestPrintDiffKindUsage_DifferentKind(t *testing.T) {
	var buf bytes.Buffer
	PrintDiffKindUsage(&buf, "configmap")
	out := buf.String()

	for _, want := range []string{"configmap", "configmap -n my-ns resource-a resource-b"} {
		if !strings.Contains(out, want) {
			t.Errorf("PrintDiffKindUsage(configmap) missing %q\n%s", want, out)
		}
	}
}

func TestPrintCompletionUsage(t *testing.T) {
	var buf bytes.Buffer
	PrintCompletionUsage(&buf)
	out := buf.String()

	for _, want := range []string{"bash", "zsh", "Examples:"} {
		if !strings.Contains(out, want) {
			t.Errorf("PrintCompletionUsage missing %q\n%s", want, out)
		}
	}
}

func TestPrintSortUsage(t *testing.T) {
	var buf bytes.Buffer
	PrintSortUsage(&buf)
	out := buf.String()

	for _, want := range []string{
		"sort", "Kinds:", "pod", "deployment", "node",
		"--namespace", "--all-namespaces", "Examples:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("PrintSortUsage missing %q\n%s", want, out)
		}
	}
}

func TestPrintEventsUsage(t *testing.T) {
	var buf bytes.Buffer
	PrintEventsUsage(&buf)
	out := buf.String()

	for _, want := range []string{"events", "since", "all-namespaces", "Examples:", "kdiag events -h"} {
		if !strings.Contains(out, want) {
			t.Errorf("PrintEventsUsage missing %q\n%s", want, out)
		}
	}
}

func TestViewFlagSeen(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"empty", nil, ""},
		{"no view flags", []string{"my-pod", "-n", "ns"}, ""},
		{"yml-path space form", []string{"--yml-path", "x"}, "yml-path"},
		{"yml-path equals form", []string{"--yml-path=x"}, "yml-path"},
		{"resources", []string{"--resources"}, "resources"},
		{"spec", []string{"--spec"}, "spec"},
		{"az", []string{"--az"}, "az"},
		{"first wins when multiple present", []string{"--resources", "--az"}, "resources"},
		{"yml-path wins when first", []string{"--yml-path", "x", "--resources"}, "yml-path"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ViewFlagSeen(c.args); got != c.want {
				t.Errorf("ViewFlagSeen(%v) = %q, want %q", c.args, got, c.want)
			}
		})
	}
}

func TestPrintInspectUsage_NoViewShowsAll(t *testing.T) {
	var b bytes.Buffer
	PrintInspectUsage(&b, nil)
	out := b.String()
	for _, flag := range []string{"--yml-path", "--yaml", "--resources", "--spec", "--az"} {
		if !strings.Contains(out, "  "+flag) {
			t.Errorf("output missing flag line for %q:\n%s", flag, out)
		}
	}
}

func TestPrintInspectUsage_FilteredByYMLPath(t *testing.T) {
	var b bytes.Buffer
	PrintInspectUsage(&b, []string{"--yml-path", "memory"})
	out := b.String()
	if !strings.Contains(out, "  --yml-path") {
		t.Errorf("output missing flag line for --yml-path:\n%s", out)
	}
	for _, flag := range []string{"--yaml", "--resources", "--spec", "--az"} {
		if strings.Contains(out, "  "+flag) {
			t.Errorf("output unexpectedly contains flag line for %q:\n%s", flag, out)
		}
	}
}

func TestPrintInspectUsage_FilteredByResources(t *testing.T) {
	var b bytes.Buffer
	PrintInspectUsage(&b, []string{"--resources"})
	out := b.String()
	// These flags should have their own option lines
	for _, flag := range []string{"--resources", "--yaml", "--az"} {
		if !strings.Contains(out, "  "+flag) {
			t.Errorf("output missing flag line for %q:\n%s", flag, out)
		}
	}
	// These should NOT have their own option lines (but may appear in descriptions)
	for _, flag := range []string{"--yml-path", "--spec"} {
		if strings.Contains(out, "  "+flag) {
			t.Errorf("output unexpectedly contains flag line for %q:\n%s", flag, out)
		}
	}
}
