// Command vmtest is the host CLI of the VM acceptance-test lab: it fetches and builds bases,
// runs scenarios on throwaway overlays, judges them, and keeps lab members.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/vmtest"
)

func main() {
	os.Exit(int(run(os.Args[1:])))
}

func run(args []string) term.ExitCode {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigs)
	ctx, release := vmtest.WithSignals(context.Background(), sigs)
	defer release()
	app := vmtest.App{Env: vmtest.NewEnv()}
	return app.Run(ctx, args)
}
