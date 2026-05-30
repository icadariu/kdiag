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

// alphabetical sort is asserted by every root-screen test below — but the
// canonical list itself is also checked here so a future re-ordering of
// rootCommands is caught even if the renderers happen to read them in
// insertion order.
func TestRootCommands_AlphabeticalAndComplete(t *testing.T) {
	want := []string{"completion", "diff", "events", "help", "inspect", "sort", "troubleshoot"}
	if len(rootCommands) != len(want) {
		t.Fatalf("rootCommands length = %d, want %d", len(rootCommands), len(want))
	}
	for i, c := range rootCommands {
		if c.Name != want[i] {
			t.Errorf("rootCommands[%d].Name = %q, want %q", i, c.Name, want[i])
		}
	}
}

// commandsInOrder reports whether s mentions every command in rootCommands
// strictly in the canonical (alphabetical) sequence. includeMeta mirrors the
// flag passed to printCommandList — when false, completion/help are skipped
// because they're hidden from the bare-banner screen.
func commandsInOrder(t *testing.T, s string, includeMeta bool) {
	t.Helper()
	idx := 0
	for _, c := range rootCommands {
		if !includeMeta && c.Meta {
			continue
		}
		off := strings.Index(s[idx:], c.Name)
		if off < 0 {
			t.Errorf("output missing command %q (or out of order) — saw:\n%s", c.Name, s)
			return
		}
		idx += off + len(c.Name)
	}
}

// PrintRootBanner is the bare `kdiag` (no args) screen — terse: branded
// title, usage line, and a hint to run `kdiag -h`. The command list is
// reserved for `kdiag -h` and `kdiag help`.
func TestPrintRootBanner_TerseHint(t *testing.T) {
	var b bytes.Buffer
	PrintRootBanner(&b)
	out := b.String()

	for _, want := range []string{
		"kdiag — Kubernetes diagnostic CLI",
		"Usage:",
		"kdiag <command> [flags] [args]",
		`kdiag -h`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("PrintRootBanner missing %q:\n%s", want, out)
		}
	}
	// Bare banner must NOT enumerate commands; that lives behind --help.
	for _, banned := range []string{
		"Available Commands:",
		"inspect ",
		"diff ",
		"events ",
		"sort ",
	} {
		if strings.Contains(out, banned) {
			t.Errorf("PrintRootBanner should not contain %q:\n%s", banned, out)
		}
	}
}

// PrintRootError is the unknown-command fallback: same body as PrintRootUsage
// but without the branded title (which belongs only to the explicit help
// screen).
func TestPrintRootError_NoBrandedTitle(t *testing.T) {
	var b bytes.Buffer
	PrintRootError(&b)
	out := b.String()

	for _, want := range []string{
		"Available Commands:",
		"Usage:",
		"kdiag <command> [flags] [args]",
		"Flags vary by command",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("PrintRootError missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "kdiag — Kubernetes diagnostic CLI") {
		t.Errorf("PrintRootError should not contain the branded title:\n%s", out)
	}
}

// PrintRootUsage is the `kdiag --help` / `kdiag -h` screen — branded
// title, sorted command list, Usage line, and the flags-vary pointer.
// Matches §2 of the spec.
func TestPrintRootUsage_FullHelp(t *testing.T) {
	var b bytes.Buffer
	PrintRootUsage(&b)
	out := b.String()

	for _, want := range []string{
		"kdiag — Kubernetes diagnostic CLI",
		"Available Commands:",
		"Usage:",
		"kdiag <command> [flags] [args]",
		`Flags vary by command. Run "kdiag help <command>" or "kdiag <command> --help" to see them.`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("PrintRootUsage missing %q:\n%s", want, out)
		}
	}
	commandsInOrder(t, out, true)
	// `version` is no longer a subcommand — must not appear in --help.
	if strings.Contains(out, "version") {
		t.Errorf("PrintRootUsage should not contain 'version' (flag, not subcommand):\n%s", out)
	}
}

// PrintRootHelp is the `kdiag help` (no topic) screen — JUST the
// Available Commands block. Matches §3 of the spec.
func TestPrintRootHelp_OnlyCommandList(t *testing.T) {
	var b bytes.Buffer
	PrintRootHelp(&b)
	out := b.String()

	if !strings.Contains(out, "Available Commands:") {
		t.Errorf("PrintRootHelp missing 'Available Commands:':\n%s", out)
	}
	commandsInOrder(t, out, true)
	for _, banned := range []string{
		"kdiag — Kubernetes diagnostic CLI",
		"Usage:",
		"Flags vary by command",
	} {
		if strings.Contains(out, banned) {
			t.Errorf("PrintRootHelp should not contain %q (commands-only):\n%s", banned, out)
		}
	}
}

func TestPrintInspectUsage(t *testing.T) {
	var buf bytes.Buffer
	PrintInspectUsage(&buf, nil)
	out := buf.String()

	for _, want := range []string{
		"pod", "deploy", "ds", "sts", "rs", "node",
		"Usage:", "Examples:", "kdiag inspect <subcommand> --help",
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

	for _, want := range []string{"rs", "Examples:", "--full", "kdiag diff rs --help"} {
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

	for _, want := range []string{"pod", "Usage:", "--full", "--namespace", "resource-a resource-b"} {
		if !strings.Contains(out, want) {
			t.Errorf("PrintDiffKindUsage(pod) missing %q\n%s", want, out)
		}
	}
	// Should not contain old flag name
	if strings.Contains(out, "--full-diff") {
		t.Errorf("PrintDiffKindUsage(pod) should not contain --full-diff\n%s", out)
	}

	// Pod help should not mention rs revision-diff
	for _, banned := range []string{"revision", "replicaset", "deployment-name"} {
		if strings.Contains(out, banned) {
			t.Errorf("PrintDiffKindUsage(pod) should not contain %q\n%s", banned, out)
		}
	}
}

func TestPrintDiffKindUsage_DifferentKind(t *testing.T) {
	var buf bytes.Buffer
	PrintDiffKindUsage(&buf, "configmap")
	out := buf.String()

	for _, want := range []string{"configmap", "configmap --namespace my-ns resource-a resource-b"} {
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

	for _, want := range []string{"events", "since", "all-namespaces", "Examples:"} {
		if !strings.Contains(out, want) {
			t.Errorf("PrintEventsUsage missing %q\n%s", want, out)
		}
	}
}

func TestPrintTroubleshootUsage(t *testing.T) {
	var buf bytes.Buffer
	PrintTroubleshootUsage(&buf)
	out := buf.String()

	for _, want := range []string{
		"troubleshoot", "--ai", "sre-debug-v2", "--namespace", "--label", "--yaml",
		"Examples:", "kdiag troubleshoot pod my-pod",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("PrintTroubleshootUsage missing %q\n%s", want, out)
		}
	}
}

// Every per-command help text must drop the single-dash short aliases from
// the documented body. The shorts continue to work at parse time, they
// just aren't advertised — guard against accidentally reintroducing them.
func TestUsageText_NoDocumentedShortFlags(t *testing.T) {
	type tc struct {
		name string
		fn   func(*bytes.Buffer)
	}
	cases := []tc{
		{"PrintRootBanner", func(b *bytes.Buffer) { PrintRootBanner(b) }},
		{"PrintRootUsage", func(b *bytes.Buffer) { PrintRootUsage(b) }},
		{"PrintRootHelp", func(b *bytes.Buffer) { PrintRootHelp(b) }},
		{"PrintInspectUsage", func(b *bytes.Buffer) { PrintInspectUsage(b, nil) }},
		{"PrintDiffUsage", func(b *bytes.Buffer) { PrintDiffUsage(b) }},
		{"PrintDiffKindUsage", func(b *bytes.Buffer) { PrintDiffKindUsage(b, "pod") }},
		{"PrintCompletionUsage", func(b *bytes.Buffer) { PrintCompletionUsage(b) }},
		{"PrintSortUsage", func(b *bytes.Buffer) { PrintSortUsage(b) }},
		{"PrintEventsUsage", func(b *bytes.Buffer) { PrintEventsUsage(b) }},
		{"PrintTroubleshootUsage", func(b *bytes.Buffer) { PrintTroubleshootUsage(b) }},
		{"PrintYMLPathTopic", func(b *bytes.Buffer) { PrintYMLPathTopic(b) }},
	}
	// Tokens that would appear as documented short-flag mentions: space- or
	// comma-prefixed `-x` where x is one of our short aliases. Bare `-` in
	// text (e.g. "newest-last") is fine; the guard only catches obvious
	// flag mentions like ` -n `, ` -l `, ` -A `, ` -h `.
	bad := []string{" -n ", " -l ", " -A ", " -h ", " -n,", " -l,", " -A,", " -h,"}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var b bytes.Buffer
			c.fn(&b)
			out := b.String()
			for _, tok := range bad {
				if strings.Contains(out, tok) {
					t.Errorf("%s documents short flag %q in body:\n%s", c.name, tok, out)
				}
			}
		})
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
		{"path space form", []string{"--path", "x"}, "path"},
		{"path equals form", []string{"--path=x"}, "path"},
		{"resources", []string{"--resources"}, "resources"},
		{"deployment-spec", []string{"--deployment-spec"}, "deployment-spec"},
		{"az", []string{"--az"}, "az"},
		{"first wins when multiple present", []string{"--resources", "--az"}, "resources"},
		{"path wins when first", []string{"--path", "x", "--resources"}, "path"},
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
	for _, flag := range []string{"--path", "--yaml", "--resources", "--deployment-spec", "--az"} {
		if !strings.Contains(out, "  "+flag) {
			t.Errorf("output missing flag line for %q:\n%s", flag, out)
		}
	}
}

func TestPrintInspectUsage_FilteredByPath(t *testing.T) {
	var b bytes.Buffer
	PrintInspectUsage(&b, []string{"--path", "memory"})
	out := b.String()
	if !strings.Contains(out, "  --path") {
		t.Errorf("output missing flag line for --path:\n%s", out)
	}
	for _, flag := range []string{"--yaml", "--resources", "--deployment-spec", "--az"} {
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
	for _, flag := range []string{"--path", "--deployment-spec"} {
		if strings.Contains(out, "  "+flag) {
			t.Errorf("output unexpectedly contains flag line for %q:\n%s", flag, out)
		}
	}
}

func TestPrintYMLPathTopic(t *testing.T) {
	var b bytes.Buffer
	PrintYMLPathTopic(&b)
	out := b.String()
	for _, want := range []string{
		"--path <needle>",
		"Walk the resource YAML",
		"Smart-case",
		"glob",
		"Examples:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("PrintYMLPathTopic missing %q\n%s", want, out)
		}
	}
}
