package main

import (
	"fmt"
	"os"

	"example.com/kdiag/internal/cli"
	"example.com/kdiag/internal/cmd"
)

func main() {
	if HandleVersionFlag() {
		return
	}

	if len(os.Args) < 2 {
		cli.PrintRootBanner(os.Stderr)
		os.Exit(1)
	}

	args := os.Args[1:]
	switch args[0] {
	case "inspect":
		cmd.RunInspect(args[1:])
	case "diff":
		cmd.RunDiff(args[1:])
	case "events":
		cmd.RunEvents(args[1:])
	case "sort":
		cmd.RunSort(args[1:])
	case "completion":
		cmd.RunCompletion(args[1:])
	case "__complete":
		// Hidden helper invoked by shell completion scripts. Not advertised
		// in PrintRootUsage. See internal/cmd/complete.go.
		cmd.RunComplete(args[1:])
	case "-h", "--help", "help":
		handleHelp(args)
	default:
		fmt.Fprintf(os.Stderr, "Error: unknown command: %s\n\n", args[0])
		cli.PrintRootUsage(os.Stderr, false)
		os.Exit(1)
	}
}

// handleHelp implements `kdiag help [topic]` plus the top-level `-h`/`--help`
// shortcuts. Forms supported:
//
//	kdiag help                  → root usage
//	kdiag help yml-path | path  → topic page for --path
//	kdiag help <command> [...]  → equivalent to `kdiag <command> ... -h`
//
// args[0] is always one of "-h", "--help", "help". Anything after it is the
// topic/command path. For `-h`/`--help` we only honor the bare form.
func handleHelp(args []string) {
	// `-h` / `--help` as the top-level token: only the bare form prints root
	// usage. We don't treat them as a generic dispatcher to avoid surprising
	// users who type `kdiag --help inspect`.
	if args[0] != "help" || len(args) == 1 {
		cli.PrintRootUsage(os.Stdout, true)
		return
	}

	topic := args[1]
	switch topic {
	case "yml-path", "path":
		cli.PrintYMLPathTopic(os.Stdout)
		return
	}

	// Re-enter the dispatch by appending `-h` and routing to the matching
	// subcommand. `kdiag help inspect pod` → runInspect with ["pod", "-h"].
	sub := append([]string{}, args[2:]...)
	sub = append(sub, "-h")
	switch topic {
	case "inspect":
		cmd.RunInspect(sub)
	case "diff":
		cmd.RunDiff(sub)
	case "events":
		cmd.RunEvents(sub)
	case "sort":
		cmd.RunSort(sub)
	case "completion":
		cmd.RunCompletion(sub)
	default:
		fmt.Fprintf(os.Stderr, "Error: unknown help topic: %s\n\n", topic)
		cli.PrintRootUsage(os.Stderr, false)
		os.Exit(1)
	}
}
