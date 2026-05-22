package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"
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

func TestFormatAge(t *testing.T) {
	now := time.Now()
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{60 * time.Second, "60s"},
		// boundary: exactly at 90s falls into the minutes branch
		{90 * time.Second, "1m"},
		{5 * time.Minute, "5m"},
		{45 * time.Minute, "45m"},
		// boundary: exactly at 90m falls into the hours branch
		{90 * time.Minute, "1h"},
		{3 * time.Hour, "3h"},
		{24 * time.Hour, "24h"},
		// boundary: exactly at 48h falls into the days branch
		{48 * time.Hour, "2d"},
		{5 * 24 * time.Hour, "5d"},
	}
	for _, tc := range cases {
		got := FormatAge(now.Add(-tc.d))
		if got != tc.want {
			t.Errorf("FormatAge(now-%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}
