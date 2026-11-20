// Command watchlist keeps the watch list: it syncs the owner's stars into full clones sorted by
// licence, and runs studies of them that record the commit each study read.
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
