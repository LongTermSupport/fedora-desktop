// Command checkout-gate refuses a run of plays from a checkout that is dirty, or not level with
// its upstream after a fetch; --force runs anyway. run.bash runs it before --changed and
// --rerun. The judgement is internal/checkoutgate's.
package main

import (
	"context"
	"io"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/checkoutgate"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/gitx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func main() {
	os.Exit(int(run(os.Args[1:], os.Stderr)))
}

func run(args []string, stderr io.Writer) term.ExitCode {
	return checkoutgate.App{Git: gitx.New(execx.OS{}), Stderr: stderr}.Run(context.Background(), args)
}
