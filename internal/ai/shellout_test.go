package ai

import (
	"io"
	"strings"
	"testing"
)

func TestShellOutBackendsNotImplemented(t *testing.T) {
	for _, name := range []string{"claude", "gemini", "chatgpt"} {
		b, ok := Get(name)
		if !ok {
			t.Fatalf("provider %q not registered", name)
		}
		err := b.Run(io.Discard, Request{Kind: "pod"})
		if err == nil {
			t.Fatalf("provider %q: want not-implemented error, got nil", name)
		}
		msg := err.Error()
		for _, want := range []string{name, "not implemented", "#36", "bare --ai"} {
			if !strings.Contains(msg, want) {
				t.Errorf("provider %q error %q missing %q", name, msg, want)
			}
		}
	}
}
