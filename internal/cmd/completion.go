package cmd

import (
	"embed"
	"fmt"
	"os"

	"example.com/kdiag/internal/cli"
)

//go:embed completions/kdiag.bash completions/kdiag.zsh
var completionScripts embed.FS

// RunCompletion writes the embedded completion script for the requested shell
// to stdout. Output is suitable for `eval "$(kdiag completion bash)"` or
// redirecting into the shell's completion directory.
func RunCompletion(args []string) {
	if cli.WantsHelp(args) {
		cli.PrintCompletionUsage(os.Stdout)
		return
	}
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "Error: completion requires exactly one shell argument")
		fmt.Fprintln(os.Stderr)
		cli.PrintCompletionUsage(os.Stderr)
		os.Exit(1)
	}
	var path string
	switch args[0] {
	case "bash":
		path = "completions/kdiag.bash"
	case "zsh":
		path = "completions/kdiag.zsh"
	default:
		fmt.Fprintf(os.Stderr, "Error: unsupported shell: %s\n\n", args[0])
		cli.PrintCompletionUsage(os.Stderr)
		os.Exit(1)
	}
	data, err := completionScripts.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: read embedded completion: %v\n", err)
		os.Exit(1)
	}
	os.Stdout.Write(data)
}
