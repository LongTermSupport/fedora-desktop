// Command git-fetch-age prints the shell prompt's mark for a checkout whose last fetch is old,
// so the git status beside it is not read as current. It never fetches.
package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fetchage"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func main() {
	os.Exit(int(run(os.Args[1:], os.Stdout, os.Stderr)))
}

func run(args []string, stdout, stderr io.Writer) term.ExitCode {
	dir, err := os.Getwd()
	if err != nil {
		if _, werr := fmt.Fprintf(stderr, "git-fetch-age: %v\n", err); werr != nil {
			return term.ExitFailure
		}
		return term.ExitFailure
	}
	return fetchage.App{Dir: dir, Now: time.Now(), Stdout: stdout, Stderr: stderr}.Run(args)
}
