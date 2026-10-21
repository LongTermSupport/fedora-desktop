// Command wsi-pause-media pauses the media players that are playing while a speech-to-text
// recording runs, and plays them again when the panel says it is over.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/wsimedia"
)

func main() {
	// Subscribed first, so a release that arrives while the players are being paused is held,
	// not fatal (M4).
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	os.Exit(int(run(os.Args[1:], signals)))
}

func run(args []string, signals <-chan os.Signal) term.ExitCode {
	return wsimedia.App{Env: wsimedia.NewEnv(signals)}.Run(context.Background(), args)
}
