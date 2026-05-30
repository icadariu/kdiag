package ai

import (
	_ "embed"
	"io"
	"text/template"
)

//go:embed prompt_template.md
var promptTemplate string

func init() { Register(DefaultProvider, promptBackend{}) }

// promptBackend is the default `--ai` backend: it renders a paste-ready,
// read-only SRE troubleshooting prompt (kdiag's report + a preamble pointing at
// the sre-debug-v2 skill) to w. No external calls — the user pastes it into the
// assistant of their choice.
type promptBackend struct{}

func (promptBackend) Run(w io.Writer, req Request) error {
	t, err := template.New("prompt").Parse(promptTemplate)
	if err != nil {
		return err
	}
	return t.Execute(w, req)
}
