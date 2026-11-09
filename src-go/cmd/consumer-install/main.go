// Command consumer-install is the install for someone who does not write code: no GitHub, no
// developer plays, the optional apps offered at the end. ./run.bash --consumer starts it.
package main

import (
	"context"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/consumer"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func main() {
	os.Exit(int(run(os.Args[1:])))
}

func run(args []string) term.ExitCode {
	ctx, stop := execx.SignalContext(context.Background())
	defer stop()
	return consumer.App{Env: consumer.NewEnv()}.Run(ctx, args)
}
