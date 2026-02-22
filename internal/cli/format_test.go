package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrintKVBlock_Empty(t *testing.T) {
	var buf bytes.Buffer
	PrintKVBlock(&buf, "  ", map[string]string{})
	if !strings.Contains(buf.String(), "<none>") {
		t.Errorf("expected <none> for empty map, got %q", buf.String())
	}
}

func TestPrintKVBlock_Indent(t *testing.T) {
	var buf bytes.Buffer
	PrintKVBlock(&buf, ">>", map[string]string{"key": "val"})
	if !strings.HasPrefix(buf.String(), ">>") {
		t.Errorf("expected output to start with indent, got %q", buf.String())
	}
}

func TestPrintKVBlock_Sorted(t *testing.T) {
	var buf bytes.Buffer
	PrintKVBlock(&buf, "", map[string]string{
		"z": "last",
		"a": "first",
		"m": "middle",
	})
	out := buf.String()
	aIdx := strings.Index(out, "a: first")
	mIdx := strings.Index(out, "m: middle")
	zIdx := strings.Index(out, "z: last")
	if aIdx < 0 || mIdx < 0 || zIdx < 0 {
		t.Fatalf("one or more keys missing in output: %q", out)
	}
	if !(aIdx < mIdx && mIdx < zIdx) {
		t.Errorf("keys not in alphabetical order in output: %q", out)
	}
}
