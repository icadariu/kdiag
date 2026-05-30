// Package ai is a small pluggable seam for routing kdiag's troubleshoot report
// to an AI assistant. Backends register by name; the `--ai` flag's value selects
// one ("" → the default paste-ready prompt). Adding a new backend (e.g. a CLI
// shell-out) is one Register call — no runtime plugin loading.
//
// The sre-debug-v2 skill is the methodology source of truth; backends point the
// AI at it rather than re-deriving the method here.
package ai

import (
	"io"
	"sort"
)

// Request carries everything a backend needs: the diagnostic kdiag already
// collected (ReportYAML) plus identifying metadata for the prompt header.
type Request struct {
	Kind       string // pod, deployment, node, …
	Target     string // resource name, or a description of the label selector
	Namespace  string
	Verdict    string // overall verdict for a single target (optional)
	ReportYAML string // the troubleshoot report marshaled to YAML
}

// Backend turns a Request into output written to w. The default ("prompt")
// backend writes a paste-ready prompt; future backends launch a provider CLI.
type Backend interface {
	Run(w io.Writer, req Request) error
}

// DefaultProvider is the backend selected by a bare `--ai` (no value).
const DefaultProvider = "prompt"

var registry = map[string]Backend{}

// Register adds a backend under name. Called from package init functions.
func Register(name string, b Backend) { registry[name] = b }

// Get resolves a provider name to its backend. An empty name maps to
// DefaultProvider. ok is false for an unknown provider.
func Get(name string) (Backend, bool) {
	if name == "" {
		name = DefaultProvider
	}
	b, ok := registry[name]
	return b, ok
}

// Providers returns the registered provider names, sorted, for help and errors.
func Providers() []string {
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
