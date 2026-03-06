package cli

import (
	"fmt"
	"os"
)

func Fatal(err error) {
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, err.Error())
	os.Exit(1)
}
