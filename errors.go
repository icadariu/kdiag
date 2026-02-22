// errors.go
package main

import (
	"fmt"
	"os"
)

func fatal(err error) {
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, err.Error())
	os.Exit(1)
}
