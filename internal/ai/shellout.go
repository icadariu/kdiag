package ai

import (
	"fmt"
	"io"
)

// trackingIssue is where the shell-out backend (launching a provider CLI in
// read-only mode) is tracked. Until it lands, the provider names below resolve
// to a stub that explains how to get a prompt today.
const trackingIssue = "#36"

func init() {
	// The seam for option 1: replace notImplemented with a real os/exec backend
	// per provider when #36 is implemented.
	for _, name := range []string{"claude", "gemini", "chatgpt"} {
		Register(name, notImplemented{name})
	}
}

type notImplemented struct{ name string }

func (n notImplemented) Run(_ io.Writer, _ Request) error {
	return fmt.Errorf(
		"launching the %s CLI is not implemented yet (tracked in %s) — "+
			"run with bare --ai to get a paste-ready prompt instead", n.name, trackingIssue)
}
