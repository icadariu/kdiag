package main

import (
	"fmt"
	"os"

	"example.com/kdiag/internal/cli"
	"example.com/kdiag/internal/cmd"
)

// Build-time metadata, overridden via -ldflags '-X main.Version=...' etc.
var (
	Version   = "dev"
	BuildDate = "unknown"
	Commit    = "none"
)

func main() {
	if len(os.Args) < 2 {
		cli.PrintRootUsage(os.Stderr, false)
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
	case "version", "--version":
		fmt.Printf("kdiag %s (built %s, commit %s)\n", Version, BuildDate, Commit)
	case "-h", "--help", "help":
		cli.PrintRootUsage(os.Stdout, true)
	default:
		fmt.Fprintf(os.Stderr, "Error: unknown command: %s\n\n", args[0])
		cli.PrintRootUsage(os.Stderr, false)
		os.Exit(1)
	}
}
