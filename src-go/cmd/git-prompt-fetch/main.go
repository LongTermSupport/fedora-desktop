// Command git-prompt-fetch is the shell prompt's background fetch: git fetch in the working
// directory, unable to ask for a passphrase, a password or a host key.
package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/promptfetch"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func main() {
	os.Exit(int(run(os.Args[1:], os.Stderr)))
}

func run(args []string, stderr io.Writer) term.ExitCode {
	dir, err := os.Getwd()
	if err != nil {
		if _, werr := fmt.Fprintf(stderr, "git-prompt-fetch: %v\n", err); werr != nil {
			return term.ExitFailure
		}
		return term.ExitFailure
	}
	app := promptfetch.App{Dir: dir, Getenv: os.Getenv, Runner: execx.OS{}, Stderr: stderr}
	return app.Run(context.Background(), args)
}
