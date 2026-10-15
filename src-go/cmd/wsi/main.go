// Command wsi is the batch speech-to-text recorder the panel extension starts.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/wsi"
)

func main() {
	// Subscribed first, so a stop that arrives during the preflight is held, not fatal (W18).
	signals := make(chan os.Signal, 8)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	os.Exit(int(run(os.Args[1:], signals)))
}

func run(args []string, signals <-chan os.Signal) term.ExitCode {
	return wsi.App{Env: wsi.NewEnv(signals)}.Run(context.Background(), args)
}
