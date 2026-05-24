package cmd

import "testing"

func TestExtractYMLPathArgs_HappyPath(t *testing.T) {
	needle, name, selector, ns, ok := extractYMLPathArgs([]string{"--yml-path", "memory", "my-deploy"})
	if !ok {
		t.Fatal("ok=false, want true")
	}
	if needle != "memory" || name != "my-deploy" || selector != "" || ns != "" {
		t.Errorf("got needle=%q name=%q sel=%q ns=%q", needle, name, selector, ns)
	}
}

func TestExtractYMLPathArgs_EqualsForm(t *testing.T) {
	needle, _, _, _, ok := extractYMLPathArgs([]string{"--yml-path=memory", "my-deploy"})
	if !ok || needle != "memory" {
		t.Errorf("got needle=%q ok=%v", needle, ok)
	}
}

func TestExtractYMLPathArgs_Absent(t *testing.T) {
	_, _, _, _, ok := extractYMLPathArgs([]string{"my-deploy"})
	if ok {
		t.Error("ok=true, want false")
	}
}

func TestExtractYMLPathArgs_WithLabel(t *testing.T) {
	_, _, selector, _, ok := extractYMLPathArgs([]string{"--yml-path", "memory", "-l", "app=foo"})
	if !ok || selector != "app=foo" {
		t.Errorf("got selector=%q ok=%v", selector, ok)
	}
}
