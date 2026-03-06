package main

import (
	"fmt"
	"os"

	"example.com/kdiag/internal/cli"
	"example.com/kdiag/internal/cmd"
)

func main() {
	if len(os.Args) < 2 {
		cli.PrintUsage(os.Stderr)
		os.Exit(1)
	}

	args := os.Args[1:]
	switch args[0] {
	case "inspect":
		cmd.RunInspect(args[1:])
	case "az":
		cmd.RunAZ(args[1:])
	case "rs":
		cmd.RunRS(args[1:])
	case "-h", "--help", "help":
		cli.PrintUsage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "Error: unknown command: %s\n\n", args[0])
		cli.PrintUsage(os.Stderr)
		os.Exit(1)
	}
}
