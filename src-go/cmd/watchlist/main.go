// Command watchlist keeps the repository watch list: it mirrors the watched repositories,
// reports what changed since each was last reviewed, and records a review.
package main

import (
	"context"
	"io"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/watchlist"
)

func main() {
	os.Exit(int(run(os.Args[1:], execx.OS{}, os.Stdout, os.Stderr)))
}

// run roots the command at the working directory; --root overrides it.
func run(args []string, r execx.Runner, stdout, stderr io.Writer) term.ExitCode {
	return watchlist.NewApp(".", r, stdout, stderr).Run(context.Background(), args)
}
