// Command wsi-claude-token prints the Claude token file speech-to-text post-processing runs
// as: the token Settings name, or the one with the most allowance left.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/wsitoken"
)

func main() {
	os.Exit(int(run(os.Args[1:])))
}

func run(args []string) term.ExitCode {
	env, err := wsitoken.NewEnv()
	if err != nil {
		if _, werr := fmt.Fprintf(os.Stderr, "wsi-claude-token: %v\n", err); werr != nil {
			return term.ExitFailure
		}
		return term.ExitFailure
	}
	return wsitoken.App{Env: env}.Run(context.Background(), args)
}
