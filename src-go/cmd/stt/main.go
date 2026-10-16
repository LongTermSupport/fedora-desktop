// Command stt transcribes audio recordings into .stt.md sidecars with the warm
// speech-to-text server, the model dictation uses.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/stt"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func main() {
	os.Exit(int(run(os.Args[1:])))
}

func run(args []string) term.ExitCode {
	env, err := stt.NewEnv()
	if err != nil {
		if _, werr := fmt.Fprintf(os.Stderr, "stt: %v\n", err); werr != nil {
			return term.ExitFailure
		}
		return term.ExitFailure
	}
	// Ctrl-C ends the wait for the server; the recording in progress gets no sidecar
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return stt.App{Env: env}.Run(ctx, args)
}
