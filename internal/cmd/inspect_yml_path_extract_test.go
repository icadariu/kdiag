package cmd

import "testing"

func TestExtractPathArgs_HappyPath(t *testing.T) {
	needle, name, selector, ns, ok := extractPathArgs([]string{"--path", "memory", "my-deploy"})
	if !ok {
		t.Fatal("ok=false, want true")
	}
	if needle != "memory" || name != "my-deploy" || selector != "" || ns != "" {
		t.Errorf("got needle=%q name=%q sel=%q ns=%q", needle, name, selector, ns)
	}
}

func TestExtractPathArgs_EqualsForm(t *testing.T) {
	needle, _, _, _, ok := extractPathArgs([]string{"--path=memory", "my-deploy"})
	if !ok || needle != "memory" {
		t.Errorf("got needle=%q ok=%v", needle, ok)
	}
}

func TestExtractPathArgs_Absent(t *testing.T) {
	_, _, _, _, ok := extractPathArgs([]string{"my-deploy"})
	if ok {
		t.Error("ok=true, want false")
	}
}

func TestExtractPathArgs_WithLabel(t *testing.T) {
	_, _, selector, _, ok := extractPathArgs([]string{"--path", "memory", "-l", "app=foo"})
	if !ok || selector != "app=foo" {
		t.Errorf("got selector=%q ok=%v", selector, ok)
	}
}
