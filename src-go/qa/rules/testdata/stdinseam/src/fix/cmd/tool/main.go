// Command tool's main reads its stdin itself: a command is judged as a package is.
package main

import (
	"io"
	"os"
)

func main() {
	data, err := io.ReadAll(os.Stdin) // want `io.ReadAll reads stdin outside internal/stdinread`
	if err != nil || len(data) == 0 {
		os.Exit(1)
	}
}
