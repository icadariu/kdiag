package main

import (
	"fmt"
	"os"
)

var (
	version   = "dev"
	commit    = "none"
	buildTime = "unknown"
)

// HandleVersionFlag prints the version line and returns true if --version
// (or -version) was passed. Call at the top of main():
//
//	if HandleVersionFlag() { return }
//
// Composes cleanly with cobra, urfave/cli, or plain flag — call it before
// any framework setup so it short-circuits before flag parsing.
func HandleVersionFlag() bool {
	for _, a := range os.Args[1:] {
		if a == "--version" || a == "-version" {
			fmt.Printf("%s (built %s, commit %s)\n", version, buildTime, commit)
			return true
		}
	}
	return false
}
