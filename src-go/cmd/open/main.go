// Command open opens a file, a directory or a URL with the right application, on a desktop or
// over SSH.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/open"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func main() {
	// Caught before anything runs, so the terminal's Ctrl-C and Ctrl-\ belong to the handler
	// that has the terminal (a caught signal is reset to its default in each child) and only
	// cancel a prompt of open's own (SPEC-open E19, E26).
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGQUIT)
	os.Exit(int(run(os.Args[1:], sig)))
}

func run(args []string, sig <-chan os.Signal) term.ExitCode {
	return open.App{Env: open.NewEnv(sig)}.Run(context.Background(), args)
}
