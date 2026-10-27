// Command reclaim reports what is using disk and runs targeted cleanups, each behind a
// confirmation.
package main

import (
	"context"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/reclaim"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func main() {
	os.Exit(int(run(os.Args[1:])))
}

func run(args []string) term.ExitCode {
	return reclaim.App{Env: reclaim.NewEnv()}.Run(context.Background(), args)
}
