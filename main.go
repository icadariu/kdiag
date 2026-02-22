// main.go
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		printUsage(os.Stderr)
		os.Exit(1)
	}

	args := os.Args[1:]
	switch args[0] {
	case "inspect":
		runInspect(args[1:])
	case "az":
		runAZ(args[1:])
	case "-h", "--help", "help":
		printUsage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "Error: unknown command: %s\n\n", args[0])
		printUsage(os.Stderr)
		os.Exit(1)
	}
}
